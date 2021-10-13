package kube

import (
	"encoding/json"
	"fmt"
	"strconv"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubeJSON "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/sidkik/kelda-v1/pkg/crd/controller/microservice/annotations"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

// TreeBuilder converts the Kubernetes objects in the namespace into a tree
// structure based on the relationships defined by OwnerReferences.
type TreeBuilder struct {
	listers map[string]objectLister
}

// Object represents a Kubernetes object, and its children. For example, a
// Deployment object has a ReplicaSet as a child, which in turn has a pod as
// its child.
type Object struct {
	Object   runtime.Object
	Children []*Object
}

type microserviceKey struct {
	uid         types.UID
	specVersion int
}

// DeepCopy makes a copy of the Object. This is required to work with the
// Microservice CRD.
func (obj *Object) DeepCopy() *Object {
	out := &Object{}
	out.Object = obj.Object.DeepCopyObject()
	out.Children = make([]*Object, len(obj.Children))
	for i, child := range obj.Children {
		out.Children[i] = child.DeepCopy()
	}
	return out
}

// UnmarshalJSON decodes the given JSON bytes into the object. This is required
// to work with the Microservice CRD.
func (obj *Object) UnmarshalJSON(b []byte) (err error) {
	var fields struct {
		Object   interface{}
		Children []interface{}
	}
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}

	serializer := kubeJSON.NewYAMLSerializer(
		kubeJSON.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
	str, _ := json.Marshal(fields.Object)
	if obj.Object, _, err = serializer.Decode(str, nil, nil); err != nil {
		return err
	}

	for _, c := range fields.Children {
		child := &Object{}
		str, _ := json.Marshal(c)
		if err := child.UnmarshalJSON(str); err != nil {
			return err
		}
		obj.Children = append(obj.Children, child)
	}
	return nil
}

// NewTreeBuilder returns a new TreeBuilder.
func NewTreeBuilder(podLister corelisters.PodLister,
	jobLister batchlisters.JobLister,
	deploymentLister appslisters.DeploymentLister,
	replicaSetLister appslisters.ReplicaSetLister,
	statefulSetLister appslisters.StatefulSetLister,
	daemonSetLister appslisters.DaemonSetLister) TreeBuilder {
	return TreeBuilder{listers: map[string]objectLister{
		"Pod":         newPodLister(podLister),
		"Job":         newJobLister(jobLister),
		"Deployment":  newDeploymentLister(deploymentLister),
		"ReplicaSet":  newReplicaSetLister(replicaSetLister),
		"StatefulSet": newStatefulSetLister(statefulSetLister),
		"DaemonSet":   newDaemonSetLister(daemonSetLister),
	}}
}

// build queries all the Kubernetes objects, and builds the tree.
func (tb TreeBuilder) build(namespace string) (map[microserviceKey][]*Object, error) {
	var allObjects []runtime.Object
	for kind, lister := range tb.listers {
		objects, err := lister(namespace)
		if err != nil {
			return nil, errors.WithContext(err, fmt.Sprintf("list %q", kind))
		}

		allObjects = append(allObjects, objects...)
	}

	return makeTree(allObjects), nil
}

// makeTree converts a list of Kubernetes objects into a tree.
func makeTree(objects []runtime.Object) map[microserviceKey][]*Object {
	roots := map[microserviceKey][]*Object{}
	idToObject := map[types.UID]*Object{}
	nonRoots := []*Object{}
	for _, obj := range objects {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			log.WithError(err).
				WithField("object", obj).
				Warn("Failed to parse Kubernetes tree: " +
					"unexpected non-Kubernetes object. Skipping.")
			continue
		}

		id := accessor.GetUID()
		idToObject[id] = &Object{Object: obj}
		for _, ownerRef := range accessor.GetOwnerReferences() {
			if ownerRef.Kind == "Microservice" {
				annots := accessor.GetAnnotations()
				specVersionStr, ok := annots[annotations.MicroserviceVersion]
				if !ok {
					specVersionStr, ok = annots[annotations.DeprecatedMicroserviceVersion]
				}

				if !ok {
					log.Error("Object missing SpecVersion annotation")
					continue
				}

				specVersion, err := strconv.Atoi(specVersionStr)
				if err != nil {
					log.WithError(err).Error("Failed to parse SpecVersion annotation")
					continue
				}

				key := microserviceKey{ownerRef.UID, specVersion}
				roots[key] = append(roots[key], idToObject[id])
			} else {
				nonRoots = append(nonRoots, idToObject[id])
			}
		}
	}

	// For each object, append itself to its owner's `Children`.
	for _, obj := range nonRoots {
		accessor, _ := meta.Accessor(obj.Object)
		for _, owner := range accessor.GetOwnerReferences() {
			if ownerRef, ok := idToObject[owner.UID]; ok {
				ownerRef.Children = append(ownerRef.Children, obj)
			} else {
				log.WithField("object", accessor.GetUID()).
					WithField("owner", owner.UID).
					Debug("Failed to parse Kubernetes tree: unknown owner. " +
						"Kelda is most likely not listing the owner's Kind.")
			}
		}
	}

	return roots
}

// Select returns the Objects (and their children) created by the given
// Microservice.
func (tb TreeBuilder) Select(namespace string, msUID types.UID, specVersion int) ([]*Object, error) {
	roots, err := tb.build(namespace)
	if err != nil {
		return nil, errors.WithContext(err, "query objects")
	}

	return roots[microserviceKey{msUID, specVersion}], nil
}

// SelectPods selects all the Pod objects from the tree.
func SelectPods(objects []*Object, onlyRunning bool) (pods []*corev1.Pod) {
	for _, obj := range objects {
		if pod, ok := obj.Object.(*corev1.Pod); ok {
			if !onlyRunning || pod.Status.Phase == corev1.PodRunning {
				pods = append(pods, pod)
			}
		}
		pods = append(pods, SelectPods(obj.Children, onlyRunning)...)
	}
	return pods
}

// objectLister lists the Kubernetes objects of a given type. It must return
// the objects in a form that can be Marshalled and Unmarshalled to JSON --
// specifically, it must ensure that the API version and Kind are set. This
// information isn't automatically set by the API server:
// https://github.com/kubernetes/kubernetes/issues/3030.
type objectLister func(string) ([]runtime.Object, error)

func newPodLister(podLister corelisters.PodLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		pods, err := podLister.Pods(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, pod := range pods {
			pod.APIVersion = corev1.SchemeGroupVersion.String()
			pod.Kind = "Pod"
			objs = append(objs, pod)
		}
		return
	}
}

func newReplicaSetLister(replicaSetLister appslisters.ReplicaSetLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		replicaSets, err := replicaSetLister.ReplicaSets(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, replicaSet := range replicaSets {
			replicaSet.APIVersion = appsv1.SchemeGroupVersion.String()
			replicaSet.Kind = "ReplicaSet"
			objs = append(objs, replicaSet)
		}
		return
	}
}

func newDeploymentLister(deploymentLister appslisters.DeploymentLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		deployments, err := deploymentLister.Deployments(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, deployment := range deployments {
			deployment.APIVersion = appsv1.SchemeGroupVersion.String()
			deployment.Kind = "Deployment"
			objs = append(objs, deployment)
		}
		return
	}
}

func newDaemonSetLister(daemonSetLister appslisters.DaemonSetLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		daemonSets, err := daemonSetLister.DaemonSets(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, daemonSet := range daemonSets {
			daemonSet.APIVersion = appsv1.SchemeGroupVersion.String()
			daemonSet.Kind = "DaemonSet"
			objs = append(objs, daemonSet)
		}
		return
	}
}

func newStatefulSetLister(statefulSetLister appslisters.StatefulSetLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		statefulSets, err := statefulSetLister.StatefulSets(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, statefulSet := range statefulSets {
			statefulSet.APIVersion = appsv1.SchemeGroupVersion.String()
			statefulSet.Kind = "StatefulSet"
			objs = append(objs, statefulSet)
		}
		return
	}
}

func newJobLister(jobLister batchlisters.JobLister) objectLister {
	return func(ns string) (objs []runtime.Object, err error) {
		jobs, err := jobLister.Jobs(ns).List(labels.Everything())
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			job.APIVersion = batchv1.SchemeGroupVersion.String()
			job.Kind = "Job"
			objs = append(objs, job)
		}
		return
	}
}
