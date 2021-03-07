package microservice

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/resource"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubectl/pkg/cmd/apply"
	kubectlUtil "k8s.io/kubectl/pkg/util"

	"github.com/kelda-inc/kelda/cmd/util"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	clientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	informerfactory "github.com/kelda-inc/kelda/pkg/crd/client/informers/externalversions"
	listers "github.com/kelda-inc/kelda/pkg/crd/client/listers/kelda/v1alpha1"
	"github.com/kelda-inc/kelda/pkg/crd/controller/microservice/annotations"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
	"github.com/kelda-inc/kelda/pkg/minion/server"
	"github.com/kelda-inc/kelda/pkg/update"
	"github.com/kelda-inc/kelda/pkg/version"
)

// The number of times to retry syncing or applying a Microservice before giving
// up.
const maxRetries = 3

const kindDeployment = "Deployment"
const kindDaemonSet = "DaemonSet"
const kindJob = "Job"
const kindStatefulSet = "StatefulSet"

type controller struct {
	kubeClient          kubernetes.Interface
	restConfig          rest.Config
	restMapper          meta.RESTMapper
	keldaClient         clientset.Interface
	msInformer          cache.SharedIndexInformer
	msLister            listers.MicroserviceLister
	podInformer         cache.SharedIndexInformer
	jobInformer         cache.SharedIndexInformer
	daemonSetInformer   cache.SharedIndexInformer
	deploymentInformer  cache.SharedIndexInformer
	replicaSetInformer  cache.SharedIndexInformer
	statefulSetInformer cache.SharedIndexInformer
	updateWorkqueue     workqueue.RateLimitingInterface
	applyWorkqueue      workqueue.RateLimitingInterface
	treeBuilder         kube.TreeBuilder
}

// Run starts the Microservice controller.
func Run(restConfig rest.Config, kubeClientset kubernetes.Interface,
	restMapper meta.RESTMapper, keldaClientset clientset.Interface) {

	c := newController(restConfig, keldaClientset, restMapper, kubeClientset)

	stopCh := make(chan struct{})
	defer close(stopCh)

	c.run(stopCh)
}

func newController(restConfig rest.Config, keldaClient clientset.Interface,
	restMapper meta.RESTMapper, kubeClient kubernetes.Interface) controller {

	keldaInformerFactory := informerfactory.NewSharedInformerFactory(keldaClient, 30*time.Second)
	msInformer := keldaInformerFactory.Kelda().V1alpha1().Microservices()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 30*time.Second)
	podInformer := kubeInformerFactory.Core().V1().Pods()
	daemonSetInformer := kubeInformerFactory.Apps().V1().DaemonSets()
	deploymentInformer := kubeInformerFactory.Apps().V1().Deployments()
	replicaSetInformer := kubeInformerFactory.Apps().V1().ReplicaSets()
	statefulSetInformer := kubeInformerFactory.Apps().V1().StatefulSets()
	jobInformer := kubeInformerFactory.Batch().V1().Jobs()

	treeBuilder := kube.NewTreeBuilder(podInformer.Lister(), jobInformer.Lister(),
		deploymentInformer.Lister(), replicaSetInformer.Lister(),
		statefulSetInformer.Lister(), daemonSetInformer.Lister())

	c := controller{
		kubeClient:          kubeClient,
		restConfig:          restConfig,
		restMapper:          restMapper,
		keldaClient:         keldaClient,
		msInformer:          msInformer.Informer(),
		msLister:            msInformer.Lister(),
		podInformer:         podInformer.Informer(),
		jobInformer:         jobInformer.Informer(),
		daemonSetInformer:   daemonSetInformer.Informer(),
		deploymentInformer:  deploymentInformer.Informer(),
		replicaSetInformer:  replicaSetInformer.Informer(),
		statefulSetInformer: statefulSetInformer.Informer(),
		updateWorkqueue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		applyWorkqueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		treeBuilder:         treeBuilder,
	}

	msInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.updateWorkqueue.Add(key)
				c.applyWorkqueue.Add(key)
			}
		},
		UpdateFunc: func(oldIntf, newIntf interface{}) {
			oldMs := oldIntf.(*kelda.Microservice)
			newMs := newIntf.(*kelda.Microservice)
			// Ignore updates triggered by the resync.
			if oldMs.ResourceVersion == newMs.ResourceVersion {
				return
			}

			key, err := cache.MetaNamespaceKeyFunc(newMs)
			if err == nil {
				c.updateWorkqueue.Add(key)
				if oldMs.SpecVersion != newMs.SpecVersion {
					c.applyWorkqueue.Add(key)
				}
			}
		},
	})

	c.setupChildInformerHandlers(
		podInformer.Informer(),
		daemonSetInformer.Informer(),
		deploymentInformer.Informer(),
		replicaSetInformer.Informer(),
		statefulSetInformer.Informer(),
		jobInformer.Informer())

	return c
}

func (c *controller) setupChildInformerHandlers(informers ...cache.SharedIndexInformer) {
	enqueueChild := func(intf interface{}) {
		obj, ok := intf.(runtime.Object)
		if !ok {
			return
		}

		accessor, err := meta.Accessor(obj)
		if err != nil {
			return
		}

		annots := accessor.GetAnnotations()
		msName, ok := annots[annotations.MicroserviceName]
		if !ok {
			msName, ok = annots[annotations.DeprecatedMicroserviceName]
		}

		if !ok {
			return
		}

		c.enqueueMsByName(accessor.GetNamespace(), msName)
	}

	for _, informer := range informers {
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    enqueueChild,
			DeleteFunc: enqueueChild,
			UpdateFunc: func(_, intf interface{}) {
				enqueueChild(intf)
			},
		})
	}
}

func (c *controller) enqueueMsByName(namespace, msName string) {
	ms, err := c.msLister.Microservices(namespace).Get(msName)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			log.WithError(err).
				WithField("name", msName).
				Error("Failed to get microservice for status sync")
		}
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(ms)
	if err == nil {
		c.updateWorkqueue.Add(key)
	}
}

func (c *controller) run(stopCh <-chan struct{}) {
	defer c.updateWorkqueue.ShutDown()
	defer c.applyWorkqueue.ShutDown()
	startInformers(stopCh, c.msInformer, c.podInformer, c.jobInformer,
		c.deploymentInformer, c.statefulSetInformer, c.replicaSetInformer,
		c.daemonSetInformer)

	for i := 0; i < 8; i++ {
		go func() {
			defer util.HandlePanic()
			runWorker(c.syncStatusWorker, stopCh)
		}()
	}
	go func() {
		defer util.HandlePanic()
		runWorker(c.syncManifestsWorker, stopCh)
	}()
	<-stopCh
}

func startInformers(stopCh <-chan struct{}, informers ...cache.SharedIndexInformer) {
	for _, informer := range informers {
		go informer.Run(stopCh)
		cache.WaitForCacheSync(stopCh, informer.HasSynced)
	}
}

func runWorker(worker func() bool, stopCh <-chan struct{}) {
	fn := func() {
		for worker() {
		}
	}
	wait.Until(fn, time.Second, stopCh)
}

func (c *controller) syncStatusWorker() bool {
	key, quit := c.updateWorkqueue.Get()
	if quit {
		return false
	}
	defer c.updateWorkqueue.Done(key)

	obj, found, err := c.msInformer.GetIndexer().GetByKey(key.(string))
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Failed to get microservice")
		c.updateWorkqueue.AddRateLimited(key)
		return true
	}

	// The microservice was deleted, so there's nothing to do.
	if !found {
		c.updateWorkqueue.Forget(key)
		return true
	}

	ms := obj.(*kelda.Microservice)
	err = c.syncStatus(ms, ms.Spec.Manifests)
	if err == nil {
		c.updateWorkqueue.Forget(key)
		return true
	}

	log.WithError(err).Error("Microservice status sync failed")
	status := kelda.MicroserviceStatus{
		MetaStatus: kelda.MetaStatus{
			Phase:   kelda.MetaStatusSyncFailed,
			Message: errors.GetPrintableMessage(err),
		},
	}
	updateStatusErr := c.updateStatus(ms, status)
	if updateStatusErr != nil {
		log.WithError(updateStatusErr).Warn("Failed to update status of microservice")
	}

	if c.updateWorkqueue.NumRequeues(key) < maxRetries {
		c.updateWorkqueue.AddRateLimited(key)
	} else {
		log.WithField("key", key).Warn(
			"Too many microservice sync failures. Not requeueing.")
		c.updateWorkqueue.Forget(key)
	}
	return true
}

func (c *controller) syncManifestsWorker() bool {
	key, quit := c.applyWorkqueue.Get()
	if quit {
		return false
	}

	go func() {
		defer util.HandlePanic()
		defer c.applyWorkqueue.Done(key)

		obj, found, err := c.msInformer.GetIndexer().GetByKey(key.(string))
		if err != nil {
			log.WithError(err).WithField("key", key).Error("Failed to get microservice")
			c.applyWorkqueue.AddRateLimited(key)
			return
		}

		// The microservice was deleted, so there's nothing to do.
		if !found {
			c.applyWorkqueue.Forget(key)
			return
		}

		ms := obj.(*kelda.Microservice)

		err = c.syncManifests(ms)
		if err == nil {
			// If the sync was previously failing, then unset the old error.
			if ms.Status.MetaStatus.Phase == kelda.MetaDeployFailed {
				status := kelda.MicroserviceStatus{}
				if ms.Spec.HasJob {
					status.JobStatus.Phase = kelda.JobStarting
				}
				if ms.Spec.HasService {
					status.ServiceStatus.Phase = kelda.ServiceStarting
				}

				if err := c.updateStatus(ms, status); err != nil {
					log.WithError(err).Warn("Failed to update status of microservice")
				}
			}
			c.updateWorkqueue.Forget(key)
			return
		}

		log.WithError(err).Error("Microservice manifest sync failed")
		status := kelda.MicroserviceStatus{
			MetaStatus: kelda.MetaStatus{
				Phase:   kelda.MetaDeployFailed,
				Message: errors.GetPrintableMessage(err),
			},
		}
		updateStatusErr := c.updateStatus(ms, status)
		if updateStatusErr != nil {
			log.WithError(updateStatusErr).Warn("Failed to update status of microservice")
		}

		if c.applyWorkqueue.NumRequeues(key) < maxRetries {
			c.applyWorkqueue.AddRateLimited(key)
		} else {
			log.WithField("key", key).Warn(
				"Too many microservice sync failures. Not requeueing.")
			c.applyWorkqueue.Forget(key)
		}
	}()

	// Return immediately, and process all applies in parallel.
	return true
}

func (c *controller) syncManifests(ms *kelda.Microservice) error {
	// Manifests need to sorted, so they can be updated properly.
	manifestGroups, err := sortManifests(ms.Spec.Manifests)
	if err != nil {
		return errors.WithContext(err, "sort manifests")
	}

	for _, manifestGroup := range manifestGroups {
		for _, manifest := range manifestGroup {
			obj, err := inject(ms, manifest)
			if err != nil {
				return errors.WithContext(err, "transform manifest")
			}

			if err := c.applyObject(ms, obj); err != nil {
				return errors.WithContext(err, "apply manifest")
			}
		}
	}
	return nil
}

// sortManifests sorts the given manifests into groups specifying the order in which they should be updated.
// Eg: PersistentVolumeClaims must be updated after other objects because Kubernetes doesn't allow
// PersistentVolumeClaims to be deleted until all referencing Pods are deleted.
func sortManifests(manifests []string) ([][]string, error) {
	groupedManifests := [][]string{
		{},
		{},
	}
	for _, m := range manifests {
		obj, err := kube.Parse([]byte(m))
		if err != nil {
			return nil, err
		}

		switch obj.GroupVersionKind().Kind {
		case "PersistentVolumeClaim":
			groupedManifests[1] = append(groupedManifests[1], m)
		default:
			groupedManifests[0] = append(groupedManifests[0], m)
		}
	}

	return groupedManifests, nil
}

func (c *controller) syncStatus(ms *kelda.Microservice, manifests []string) error {
	children, err := c.treeBuilder.Select(ms.Namespace, ms.UID, ms.SpecVersion)
	if err != nil {
		return err
	}

	numNonJobControllers, err := getNumNonJobControllers(manifests)
	if err != nil {
		return errors.WithContext(err, "parse manifests")
	}

	nodeList, err := c.kubeClient.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: labels.Everything().String()})
	if err != nil {
		return errors.WithContext(err, "list nodes")
	}

	status := kelda.MicroserviceStatus{Actual: children}
	if ms.Spec.HasJob {
		status.JobStatus = jobStatusForMicroservice(children, nodeList.Items)
	}
	if ms.Spec.HasService {
		status.ServiceStatus = serviceStatusForMicroservice(numNonJobControllers, children, nodeList.Items)
	}
	if ms.Spec.DevMode {
		switch {
		case ms.DevStatus.RunningVersion != ms.DevStatus.TargetVersion:
			status.MetaStatus = kelda.MetaStatus{Phase: kelda.MetaSyncing}
		case ms.DevStatus.TargetVersion != "" && ms.DevStatus.RunningVersion != "":
			status.MetaStatus = kelda.MetaStatus{Phase: kelda.MetaSynced}
		}
	}

	statusFn := func(curr kelda.MicroserviceStatus) kelda.MicroserviceStatus {
		// Don't bother syncing the status if the Microservice deploy failed. This
		// avoids overwriting the deploy error, and the status update would be
		// irrelevant anyways.
		if curr.MetaStatus.Phase == kelda.MetaDeployFailed {
			return curr
		}
		return status
	}
	return c.updateStatusWithFunction(ms, statusFn)
}

func (c *controller) applyObject(ms *kelda.Microservice, obj runtime.Object) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	gk := schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}
	mapping, err := c.restMapper.RESTMapping(gk, gvk.Version)
	if err != nil {
		return errors.WithContext(err, "get resource mapping")
	}

	name, err := meta.NewAccessor().Name(obj)
	if err != nil {
		return errors.WithContext(err, "get name")
	}

	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return errors.NewFriendlyError("%s object %q cannot be scoped to a "+
			"namespace, which is not supported by Kelda. We recommend updating your "+
			"configuration to not require this resource, or manually creating the "+
			"object with `kubectl` so that it can be shared with all development "+
			"namespaces.", gvk.Kind, name)
	}

	restClient, err := c.newRestClient(mapping.GroupVersionKind.GroupVersion())
	if err != nil {
		return errors.WithContext(err, "get rest client")
	}
	restHelper := resource.NewHelper(restClient, mapping)

	// Create the object if it doesn't exist.
	// Otherwise, update it by either gracefully patching the resource, or
	// deleting and recreating the resource.
	curr, err := restHelper.Get(ms.Namespace, name, false)
	if err != nil {
		if kerrors.IsNotFound(err) {
			if err := createObject(restHelper, ms.Namespace, obj); err != nil {
				return errors.WithContext(err, "create")
			}
			return nil
		}
		return errors.WithContext(err, "get")
	}

	annots, err := meta.NewAccessor().Annotations(curr)
	if err != nil {
		return errors.WithContext(err, "get annotations")
	}

	if !shouldUpdateObject(ms, annots) {
		return nil
	}

	switch gvk.Kind {

	// Don't hard update services since they'll come back with a
	// new IP, which will break any containers that still reference the old
	// IP.
	case "Service", "PersistentVolumeClaim":
		return gracefulUpdateObject(restHelper, curr, obj, ms.Namespace, name, mapping)

	// Intentionally circumvent the Update behavior of the object to make
	// the update as quick as possible.
	// For example, Deployments can be configured to delay the termination of
	// the previous pod until the new version is properly responding to health
	// checks.
	default:
		return hardUpdateObject(restHelper, obj, ms.Namespace, name)
	}
}

func (c *controller) newRestClient(gv schema.GroupVersion) (rest.Interface, error) {
	restConfig := c.restConfig
	restConfig.ContentConfig = resource.UnstructuredPlusDefaultContentConfig()
	restConfig.GroupVersion = &gv
	if len(gv.Group) == 0 {
		restConfig.APIPath = "/api"
	} else {
		restConfig.APIPath = "/apis"
	}

	return rest.RESTClientFor(&restConfig)
}

func gracefulUpdateObject(helper *resource.Helper, curr, desired runtime.Object,
	namespace, name string, mapping *meta.RESTMapping) error {

	patcher := &apply.Patcher{
		Mapping:   mapping,
		Helper:    helper,
		BackOff:   clockwork.NewRealClock(),
		Retries:   5,
		Overwrite: true,
	}

	// Add an annotation to the deployed object so that we can track changes to
	// the object between the next update.
	desiredBytes, err := kubectlUtil.GetModifiedConfiguration(desired, true, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return errors.WithContext(err, "add apply annotation")
	}

	_, _, err = patcher.Patch(curr, desiredBytes, "", namespace, name, ioutil.Discard)
	if err != nil {
		return errors.WithContext(err, "apply patch")
	}
	return nil
}

func createObject(helper *resource.Helper, namespace string, obj runtime.Object) error {
	// Add an annotation to the deployed object so that we can track changes to
	// the object between the next update.
	if err := kubectlUtil.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme); err != nil {
		return errors.WithContext(err, "add apply annotation")
	}

	_, err := helper.Create(namespace, false, obj, &metav1.CreateOptions{})
	return err
}

func hardUpdateObject(helper *resource.Helper, obj runtime.Object, namespace, name string) error {
	if err := deleteObject(helper, namespace, name); err != nil {
		return errors.WithContext(err, "delete")
	}

	if err := createObject(helper, namespace, obj); err != nil {
		return errors.WithContext(err, "create")
	}
	return nil
}

func shouldUpdateObject(ms *kelda.Microservice, annots map[string]string) bool {
	// Update the object if the spec has changed.
	specVer, ok := annots[annotations.MicroserviceVersion]
	if !ok {
		specVer, ok = annots[annotations.DeprecatedMicroserviceVersion]
	}

	if !ok || specVer != strconv.Itoa(ms.SpecVersion) {
		return true
	}

	// Update the object if the minion version has changed, and the service is
	// in dev mode. This way, the updated version of the Kelda dev server will
	// be deployed.
	keldaVersion := annots[annotations.KeldaVersion]
	if ms.Spec.DevMode && keldaVersion != version.Version {
		return true
	}

	return false
}

// deleteObject deletes the given object. It waits until it is removed from the
// Kubernetes API.
func deleteObject(helper *resource.Helper, namespace, name string) error {
	zero := int64(0)
	policy := metav1.DeletePropagationForeground
	_, err := helper.DeleteWithOptions(namespace, name, &metav1.DeleteOptions{
		GracePeriodSeconds: &zero,
		PropagationPolicy:  &policy,
	})
	if err != nil {
		return err
	}

	// Poll until the object is deleted.
	timeout := time.After(3 * time.Minute)
	sleep := 1 * time.Second
	maxSleep := 30 * time.Second
	for {
		select {
		case <-timeout:
			return errors.New("timed out waiting for deletion")
		default:
		}

		if _, err = helper.Get(namespace, name, false); err != nil {
			if kerrors.IsNotFound(err) {
				return nil
			}
			return errors.WithContext(err, "check deletion status")
		}

		time.Sleep(sleep)
		sleep *= 2
		if sleep > maxSleep {
			sleep = maxSleep
		}
	}
}

func (c *controller) updateStatusWithFunction(ms *kelda.Microservice,
	statusFn func(kelda.MicroserviceStatus) kelda.MicroserviceStatus) error {

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		curr, err := c.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace).Get(
			ms.Name, metav1.GetOptions{})
		if err != nil {
			// If the error is because the Microservice no longer exists, then
			// the status update is obsolete, and doesn't matter. This can
			// happen when the status update is happening while the namespace is
			// being deleted.
			if kerrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		// The status update is obsolete since it's for an older version of the
		// Microservice spec. For example, the new spec might have completely
		// different pods.
		if curr.SpecVersion != ms.SpecVersion {
			return nil
		}

		status := statusFn(curr.Status)

		// Don't bother updating the status if the update won't change anything.
		if reflect.DeepEqual(status, curr.Status) {
			return nil
		}

		curr.Status = status
		_, err = c.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace).Update(curr)

		// Ignore errors caused by the Microservice being deleted during the
		// sync. See the comment for Get above for more information.
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

// updateStatus updates the status field of the given Microservice. It retries
// if the update failed because of a concurrent update.
func (c *controller) updateStatus(ms *kelda.Microservice, status kelda.MicroserviceStatus) error {
	fn := func(_ kelda.MicroserviceStatus) kelda.MicroserviceStatus {
		return status
	}
	return c.updateStatusWithFunction(ms, fn)
}

func inject(ms *kelda.Microservice, manifest string) (*unstructured.Unstructured, error) {
	obj, err := kube.Parse([]byte(manifest))
	if err != nil {
		return nil, errors.WithContext(err, "parse object")
	}

	t := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         kelda.SchemeGroupVersion.String(),
		Kind:               "Microservice",
		Name:               ms.Name,
		UID:                ms.UID,
		Controller:         &t,
		BlockOwnerDeletion: &t,
	}
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
	obj.SetNamespace(ms.Namespace)

	annots := obj.GetAnnotations()
	if annots == nil {
		annots = map[string]string{}
	}
	annots[annotations.MicroserviceName] = ms.Name
	annots[annotations.MicroserviceVersion] = strconv.Itoa(ms.SpecVersion)
	annots[annotations.KeldaVersion] = version.Version
	obj.SetAnnotations(annots)

	switch obj.GroupVersionKind().Kind {
	// The supported pod controllers.
	case kindDeployment, kindDaemonSet, kindJob, kindStatefulSet:
	default:
		return obj, nil
	}

	unstructuredPodTemplate, _, err := unstructured.NestedMap(obj.Object, "spec", "template")
	if err != nil {
		return nil, errors.WithContext(err, "get podSpec")
	}

	var podTemplate corev1.PodTemplateSpec
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPodTemplate, &podTemplate); err != nil {
		return nil, errors.WithContext(err, "parse podSpec")
	}

	if podTemplate.Annotations == nil {
		podTemplate.Annotations = map[string]string{}
	}
	podTemplate.Annotations[annotations.MicroserviceName] = ms.Name
	podTemplate.Annotations[annotations.MicroserviceVersion] = strconv.Itoa(ms.SpecVersion)
	podTemplate.Annotations[annotations.KeldaVersion] = version.Version

	podSpec := &podTemplate.Spec
	injectImageDigests(ms, obj.GetName(), podSpec)
	if priorityClassName, ok := ms.Annotations[annotations.PriorityClass]; ok {
		podSpec.PriorityClassName = priorityClassName
	}

	if ms.Spec.DevMode {
		if len(podSpec.Containers) != 1 {
			return nil, errors.New("dev is only supported for pods with a single container")
		}
		injectDevServer(ms, podSpec)
	}

	unstructuredPodTemplate, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&podTemplate)
	if err != nil {
		return nil, errors.WithContext(err, "marshal pod template")
	}

	unstructured.SetNestedMap(obj.Object, unstructuredPodTemplate, "spec", "template")
	return obj, nil
}

func injectDevServer(ms *kelda.Microservice, podSpec *corev1.PodSpec) {
	// Create a shared volume that will contain the Kelda executable.
	binVolumeName := "bin-volume"
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: binVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	volumeMount := corev1.VolumeMount{
		Name:      binVolumeName,
		MountPath: "/bin-volume",
	}
	podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, volumeMount)

	podSpec.ServiceAccountName = server.DevServiceAccountName
	podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
		Name:            "kelda-dev-server",
		Image:           version.KeldaImage,
		ImagePullPolicy: corev1.PullAlways,
		Command:         []string{"cp", "/bin/kelda", "/bin-volume/kelda"},
		VolumeMounts:    []corev1.VolumeMount{volumeMount},
	})

	// The original kelda binary is in /bin-volume/kelda, which might not be
	// executable because the mount flag might include "noexec" depending on
	// how the Kubernetes cluster is provisioned. This injected command
	// copies the kelda binary into the container's file system before it's
	// executed.
	podSpec.Containers[0].Command = []string{"sh", "-c",
		fmt.Sprintf("cp /bin-volume/kelda /tmp/kelda && "+
			"/tmp/kelda dev-server %s %d", ms.Name, ms.SpecVersion)}

	// Use the container's development image override if it's specified.
	if ms.Spec.DevImage != "" {
		podSpec.Containers[0].Image = ms.Spec.DevImage
	}

	podSpec.Containers[0].Env = append(podSpec.Containers[0].Env,
		corev1.EnvVar{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		}, corev1.EnvVar{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	)

	// We don't expect services under development to work, so respecting
	// their liveness check doesn't make sense.
	podSpec.Containers[0].LivenessProbe = nil

	// We will want to access the services when they are under development
	// even if they are failing, possibly halfway.
	// Therefore readiness probes need to be nullified as well.
	podSpec.Containers[0].ReadinessProbe = nil
}

func injectImageDigests(ms *kelda.Microservice, controllerName string,
	podSpec *corev1.PodSpec) {
	for i, container := range podSpec.Containers {
		imageDigest, ok := update.FindDigest(ms.Spec.ImageDigests,
			controllerName, container.Name, container.Image)
		if !ok {
			// If the digest is missing, meaning the minion server fails to
			// retrieve it from the registry, it shouldn't crash.
			log.WithFields(log.Fields{
				"controller": controllerName,
				"container":  container.Name,
				"image":      container.Image,
			}).Warn("Missing digest override. Deploying with tag.")
			continue
		}
		podSpec.Containers[i].Image = update.InjectDigestIntoImageURL(
			container.Image, imageDigest.Digest)
	}
}

// jobStatusForMicroservice aggregates the status of all the objects in a
// microservice that are jobs into a single status. The algorithm returns the
// status of the least healthy job in the service.
func jobStatusForMicroservice(actual []*kube.Object, nodes []corev1.Node) kelda.JobStatus {
	jobStatuses := map[kelda.JobPhase]kelda.JobStatus{}

	for _, obj := range actual {
		if obj.Object.GetObjectKind().GroupVersionKind().Kind != kindJob {
			continue
		}

		job := obj.Object.(*batchv1.Job)
		pods := kube.SelectPods(obj.Children, false)
		status := jobStatus(job, pods, nodes)
		jobStatuses[status.Phase] = status
	}

	if len(jobStatuses) == 0 {
		// We are probably still starting.
		return kelda.JobStatus{Phase: kelda.JobStarting}
	}

	// orderedPhases describes how we handle multiple Jobs in different states:
	// we take the first phase that describes at least one Job.
	orderedPhases := []kelda.JobPhase{
		kelda.JobStarting,
		kelda.JobFailed,
		kelda.JobRunning,
		kelda.JobCompleted,
		kelda.JobUnknown,
	}

	for _, phase := range orderedPhases {
		if status, ok := jobStatuses[phase]; ok {
			return status
		}
	}

	// Note: if there are two Jobs and one has unknown status, that one will be
	// ignored.
	return kelda.JobStatus{Phase: kelda.JobUnknown}
}

func jobStatus(job *batchv1.Job, pods []*corev1.Pod, nodes []corev1.Node) kelda.JobStatus {
	haveCondition := func(conditionType batchv1.JobConditionType) (bool, string) {
		for _, cond := range job.Status.Conditions {
			if cond.Type == conditionType && cond.Status == corev1.ConditionTrue {
				return true, cond.Message
			}
		}
		return false, ""
	}

	if isFailed, msg := haveCondition(batchv1.JobFailed); isFailed {
		return kelda.JobStatus{Phase: kelda.JobFailed, Message: msg}
	}

	if isComplete, _ := haveCondition(batchv1.JobComplete); isComplete {
		return kelda.JobStatus{Phase: kelda.JobCompleted}
	}

	for _, pod := range pods {
		if msg, isFailed := isFatalPodState(pod, nodes); isFailed {
			return kelda.JobStatus{Phase: kelda.JobFailed, Message: msg}
		}
	}

	if job.Status.Failed == 0 && job.Status.Active == 0 && job.Status.Succeeded == 0 {
		return kelda.JobStatus{Phase: kelda.JobStarting}
	}

	return kelda.JobStatus{Phase: kelda.JobRunning}
}

// isFatalPodState returns an error message and true if a container in the pod
// terminated prematurely, or the pod is in a failure state that we don't expect
// will resolve itself. Note that our definition of "Failed" can differ from
// Kubernetes in these scenarios -- Kubernetes may treat pods as "Pending"
// in these scenarios.
func isFatalPodState(pod *corev1.Pod, nodes []corev1.Node) (string, bool) {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled &&
			cond.Status != corev1.ConditionTrue &&
			cond.Reason == corev1.PodReasonUnschedulable {

			// Check if this is due to a taint on every single Node e.g.
			// due to MemoryPressure condition. We find taints that this
			// Pod doesn't match and write it in the failed message.
			if strings.Contains(cond.Message, "node(s) had taints that "+
				"the pod didn't tolerate") {
				allUntoleratedTaints := make(map[corev1.Taint]struct{})
				for _, node := range nodes {
					untoleratedTaints := getUntoleratedTaints(pod, node)
					for taint := range untoleratedTaints {
						allUntoleratedTaints[taint] = struct{}{}
					}
				}

				message := "Cannot schedule Pod due to the following Node taint(s): "
				taintMessages := []string{}
				for taint := range allUntoleratedTaints {
					newTaintMessage := taint.Key
					if taint.Value != "" {
						newTaintMessage = newTaintMessage + "=" + taint.Value
					}
					taintMessages = append(taintMessages, newTaintMessage)
				}
				return message + strings.Join(taintMessages, ", ") + ".", true
			}

			return cond.Message, true
		}
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting == nil {
			continue
		}

		waitInfo := *status.State.Waiting
		switch waitInfo.Reason {
		case "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError":
			return waitInfo.Message, true
		case "CrashLoopBackOff":
			// If container is in CrashLoopBackOff then it should have been
			// terminated with a reason.
			if lastTerminated := status.LastTerminationState.Terminated; lastTerminated != nil {
				return fmt.Sprintf("%s. Termination Reason: %s",
					waitInfo.Reason, lastTerminated.Reason), true
			}
			return waitInfo.Message, true
		}
	}

	return "", false
}

// getUntoleratedTaints returns a list of taints on this node that the pod does
// not tolerate.
func getUntoleratedTaints(pod *corev1.Pod, node corev1.Node) map[corev1.Taint]struct{} {
	untoleratedTaints := make(map[corev1.Taint]struct{})
	for _, taint := range node.Spec.Taints {
		podToleratesTaint := false
		for _, toleration := range pod.Spec.Tolerations {
			if toleration.ToleratesTaint(&taint) { // nolint: scopelint
				podToleratesTaint = true
			}
		}
		if !podToleratesTaint {
			untoleratedTaints[taint] = struct{}{}
		}
	}

	return untoleratedTaints
}

// serviceStatus aggregates the status of all the pods in a service into a
// single status. The algorithm returns the status of the least healthy pod in
// the service. For example, if any of the pod statuses are Unknown, then the
// service's status is Unknown. If any of the pods are Failed, then the
// service's status is Failed.
func serviceStatus(service *kube.Object, nodes []corev1.Node) kelda.ServiceStatus {
	pods := kube.SelectPods([]*kube.Object{service}, false)

	// For the Unknown, Failed, and Starting phases, if any pod in the service
	// is in the phase, then we ignored the status of the controller.
	podStatuses := map[kelda.ServicePhase]kelda.ServiceStatus{}
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodUnknown {
			podStatuses[kelda.ServiceUnknown] = kelda.ServiceStatus{Phase: kelda.ServiceUnknown}
		}

		if pod.Status.Phase == corev1.PodFailed {
			podStatuses[kelda.ServiceFailed] = kelda.ServiceStatus{
				Phase:   kelda.ServiceFailed,
				Message: pod.Status.Message,
			}
		}

		// Because a pod can both be in the Pending phase and a fatal state,
		// this case must come before the Pending phase. The fatal state
		// encompasses both unrecoverable states and terminated states.
		if msg, isFailed := isFatalPodState(pod, nodes); isFailed {
			podStatuses[kelda.ServiceFailed] = kelda.ServiceStatus{Phase: kelda.ServiceFailed, Message: msg}
		}

		if pod.Status.Phase == corev1.PodPending {
			podStatuses[kelda.ServiceStarting] = kelda.ServiceStatus{Phase: kelda.ServiceStarting}
		}
	}

	orderedPhases := []kelda.ServicePhase{
		kelda.ServiceUnknown,
		kelda.ServiceFailed,
		kelda.ServiceStarting,
	}
	for _, phase := range orderedPhases {
		if status, ok := podStatuses[phase]; ok {
			return status
		}
	}

	// Determine if all the necessary pods have been created and are ready.
	var numReady, numCreated, numExpected int32
	// We have to treat Deployments, StatefulSets, and DaemonSets differently
	// here.
	switch service.Object.GetObjectKind().GroupVersionKind().Kind {
	case kindDeployment:
		// A Deployment should have a single child, which is a ReplicaSet.
		children := service.Children
		if len(children) == 0 {
			// The Deployment has not yet created the ReplicaSet
			return kelda.ServiceStatus{Phase: kelda.ServiceStarting}
		}
		replicaSet, ok := children[0].Object.(*appsv1.ReplicaSet)
		if !ok {
			log.WithField("deployment", service.Object.(*appsv1.Deployment).Name).
				Warning("Deployment has a child that is not a ReplicaSet")
			return kelda.ServiceStatus{Phase: kelda.ServiceUnknown}
		}
		numReady = replicaSet.Status.ReadyReplicas
		numCreated = replicaSet.Status.Replicas
		numExpected = *replicaSet.Spec.Replicas

	case kindStatefulSet:
		statefulSet := service.Object.(*appsv1.StatefulSet)
		numReady = statefulSet.Status.ReadyReplicas
		numCreated = statefulSet.Status.CurrentReplicas
		numExpected = *statefulSet.Spec.Replicas

	case kindDaemonSet:
		daemonSet := service.Object.(*appsv1.DaemonSet)
		numReady = daemonSet.Status.NumberReady
		numCreated = daemonSet.Status.CurrentNumberScheduled
		numExpected = daemonSet.Status.DesiredNumberScheduled

	default:
		log.WithField("kind", service.Object.GetObjectKind().GroupVersionKind().Kind).
			Error("Unsupported object kind")
		return kelda.ServiceStatus{Phase: kelda.ServiceUnknown}
	}

	if numCreated < numExpected {
		return kelda.ServiceStatus{Phase: kelda.ServiceStarting}
	}

	if numReady < numCreated {
		return kelda.ServiceStatus{Phase: kelda.ServiceNotReady}
	}

	return kelda.ServiceStatus{Phase: kelda.ServiceReady}
}

// serviceStatusForMicroservice aggregates the status of all the objects in a
// microservice that are services (aka Deployments, DaemonSets, or
// StatefulSets) into a single status. The algorithm returns the status of the
// least healthy service in the service.
func serviceStatusForMicroservice(expNonJobControllers int,
	actual []*kube.Object, nodes []corev1.Node) kelda.ServiceStatus {

	var services []*kube.Object
	for _, obj := range actual {
		kind := obj.Object.GetObjectKind().GroupVersionKind().Kind
		switch kind {
		case kindDeployment, kindStatefulSet, kindDaemonSet:
			services = append(services, obj)
		}
	}

	// If we haven't created all the objects in the spec yet, report
	// ServiceStarting. For now, just compare the number of actual objects and
	// the number of objects in the spec. We only count DaemonSets, Deployments,
	// and StatefulSets.
	if expNonJobControllers > len(services) {
		return kelda.ServiceStatus{Phase: kelda.ServiceStarting}
	}

	// Some things that aren't actually services, and don't have pods, will be
	// counted as services (like secrets). In this case just always say that the
	// service is ready.
	if expNonJobControllers == 0 {
		return kelda.ServiceStatus{Phase: kelda.ServiceReady}
	}

	serviceStatuses := map[kelda.ServicePhase]kelda.ServiceStatus{}
	for _, obj := range services {
		status := serviceStatus(obj, nodes)
		serviceStatuses[status.Phase] = status
	}

	// orderedPhases describes how we handle multiple Services in different
	// states: we take the first phase that describes at least one Service.
	orderedPhases := []kelda.ServicePhase{
		kelda.ServiceStarting,
		kelda.ServiceFailed,
		kelda.ServiceNotReady,
		kelda.ServiceReady,
		kelda.ServiceUnknown,
	}
	for _, phase := range orderedPhases {
		if status, ok := serviceStatuses[phase]; ok {
			return status
		}
	}

	// Note: if there are two Services and one has unknown status, that one will
	// be ignored.
	return kelda.ServiceStatus{Phase: kelda.ServiceUnknown}
}

func getNumNonJobControllers(manifests []string) (n int, err error) {
	for _, manifest := range manifests {
		obj, err := kube.Parse([]byte(manifest))
		if err != nil {
			return -1, errors.WithContext(err, "parse object")
		}

		switch obj.GroupVersionKind().Kind {
		case kindDeployment, kindDaemonSet, kindStatefulSet:
			n++
		}
	}
	return n, nil
}
