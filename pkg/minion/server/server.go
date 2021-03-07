package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	core "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	scheduling "k8s.io/api/scheduling/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/analytics"
	"github.com/kelda-inc/kelda/pkg/config"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	annotationKeys "github.com/kelda-inc/kelda/pkg/crd/controller/microservice/annotations"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
	"github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/proto/messages"
	"github.com/kelda-inc/kelda/pkg/proto/minion"
	"github.com/kelda-inc/kelda/pkg/update"
	"github.com/kelda-inc/kelda/pkg/version"

	_ "google.golang.org/grpc/encoding/gzip" // Install the gzip compressor
)

const (
	// DevServiceAccountName is the name of the service account used for
	// booting pods in development mode. It's needed because the development
	// server interacts with the Kelda Microservice CRD.
	DevServiceAccountName = "kelda-dev"

	// MaxPodPriorityValue is the maximum priority value that Kelda will assign
	// to Pods via a PriorityClass.
	MaxPodPriorityValue = int32(10000000)

	keldaManagedResourceKey = "kelda-managed"

	keldaManagedResourceValue = "true"
)

// KeldaManagedResourceLabel allows us to use a LabelSelector to acquire only
// kelda-managed resources.
var KeldaManagedResourceLabel = map[string]string{keldaManagedResourceKey: keldaManagedResourceValue}

// To be mocked in tests.
var updateGetDigestFromContainerInfo = func(info *update.ContainerInfo,
	kubeClient kubernetes.Interface, namespace string) (string, error) {
	return update.GetDigestFromContainerInfo(
		info,
		kubeClient.CoreV1().Secrets(namespace),
		kubeClient.CoreV1().ServiceAccounts(namespace),
	)
}

type server struct {
	kubeClient  kubernetes.Interface
	keldaClient keldaClientset.Interface

	license config.License
}

// Run runs the main Minion server thread.
func Run(license config.License, kubeClient kubernetes.Interface, keldaClient keldaClientset.Interface) error {
	address := fmt.Sprintf("0.0.0.0:%d", client.DefaultPort)
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	log.WithField("address", address).Info("Listening for connections..")
	s := grpc.NewServer()
	minion.RegisterKeldaServer(s, &server{kubeClient, keldaClient, license})
	if err := s.Serve(lis); err != nil {
		return err
	}
	return nil
}

func (s *server) CreateWorkspace(ctx context.Context, req *minion.CreateWorkspaceRequest) (
	*minion.CreateWorkspaceResponse, error) {
	msgs, err := s.createWorkspace(req)
	return &minion.CreateWorkspaceResponse{
		Error:    errors.Marshal(err),
		Messages: msgs,
	}, nil
}

func (s *server) createWorkspace(req *minion.CreateWorkspaceRequest) ([]*messages.Message, error) {
	analytics.Log.Info("minion server: create workspace")

	// Verify the license is still valid.
	msgs, err := s.license.CheckExpiration()
	if err != nil {
		return msgs, errors.WithContext(err, "failed license verification")
	}

	// Create the namespace to wrap the services in this workspace.
	namespace := req.GetWorkspace().GetNamespace()
	seatsMsgs, err := s.setupNamespace(namespace)
	msgs = append(msgs, seatsMsgs...)
	if err != nil {
		return msgs, errors.WithContext(err, "failed license verification")
	}

	var priorityClassName string
	hasPriorityClass, err := s.hasAPIGroupAndVersion("scheduling.k8s.io", "v1beta1")
	if err != nil {
		return msgs, errors.WithContext(err, "get apigroups")
	}
	if hasPriorityClass {
		if err := s.createPriorityClass(namespace); err != nil {
			return msgs, errors.WithContext(err, "create priority class")
		}
		priorityClassName = namespace
	}

	imagePullSecrets := []core.LocalObjectReference{}
	_, err = s.kubeClient.CoreV1().Secrets(client.KeldaNamespace).Get(
		client.RegistrySecretName, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return msgs, errors.WithContext(err, "get secret")
	}

	if regcredExists := err == nil; regcredExists {
		// Set up the secrets for pulling the application container images.
		if err := s.copySecret(namespace, client.RegistrySecretName); err != nil {
			return msgs, errors.WithContext(err, "setup application secret")
		}

		imagePullSecrets = []core.LocalObjectReference{
			{Name: client.RegistrySecretName},
		}
	}

	// Add regcred to the default ServiceAccount for pulling the images.
	if err := s.addRegcredToDefaultSA(namespace, imagePullSecrets); err != nil {
		return msgs, errors.WithContext(err, "add regcred")
	}

	if err := s.createDevServiceAccount(namespace, imagePullSecrets); err != nil {
		return msgs, errors.WithContext(err, "create service account")
	}

	errorsChan := make(chan error)
	var wg sync.WaitGroup

	// Clean up old microservices.
	svcNameMap := map[string]struct{}{}
	for _, svc := range req.GetWorkspace().GetServices() {
		svcNameMap[svc.GetName()] = struct{}{}
	}
	msClient := s.keldaClient.KeldaV1alpha1().Microservices(namespace)
	msList, err := msClient.List(metav1.ListOptions{})
	if err != nil {
		return msgs, errors.WithContext(err, "list microservices")
	}
	for _, ms := range msList.Items {
		if _, ok := svcNameMap[ms.Name]; !ok {
			// This microservice doesn't match any of the ones defined in
			// the manifests.
			err := msClient.Delete(ms.Name, &metav1.DeleteOptions{})
			if err != nil {
				return msgs, errors.WithContext(err, "delete old microservice")
			}
		}
	}

	// Instantiate new services for this workspace.
	for _, svc := range req.GetWorkspace().GetServices() {
		wg.Add(1)
		go func(svc minion.Service) {
			defer util.HandlePanic()
			defer wg.Done()

			ms, err := s.makeMicroserviceFromProto(namespace, priorityClassName, svc)
			if err != nil {
				errorsChan <- errors.WithContext(err,
					fmt.Sprintf("convert service %q", svc.Name))
				return
			}

			if err := s.createOrUpdateService(&ms); err != nil {
				errorsChan <- errors.WithContext(err,
					fmt.Sprintf("create service %q", ms.Name))
			}
		}(*svc)
	}

	// Clean up old tunnels.
	desiredTunnelMap := map[string]struct{}{}
	for _, tunnel := range req.GetWorkspace().GetTunnels() {
		desiredTunnelMap[tunnelName(tunnel)] = struct{}{}
	}
	tunnelsClient := s.keldaClient.KeldaV1alpha1().Tunnels(namespace)
	tunnelList, err := tunnelsClient.List(metav1.ListOptions{})
	if err != nil {
		return msgs, errors.WithContext(err, "list tunnels")
	}
	for _, tunnel := range tunnelList.Items {
		if _, ok := desiredTunnelMap[tunnel.Name]; !ok {
			// This tunnel doesn't match any of the ones defined in
			// the manifests.
			err := tunnelsClient.Delete(tunnel.Name, &metav1.DeleteOptions{})
			if err != nil {
				return msgs, errors.WithContext(err, "delete old tunnel")
			}
		}
	}

	// Instantiate new tunnels.
	for _, tunnel := range req.GetWorkspace().GetTunnels() {
		name := tunnelName(tunnel)
		tunnelSpec := &kelda.Tunnel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: kelda.TunnelSpec{
				Service:    tunnel.GetServiceName(),
				LocalPort:  tunnel.GetLocalPort(),
				RemotePort: tunnel.GetRemotePort(),
			},
			// Clear the old status information so that it isn't associated
			// with the updated tunnel.
			Status: kelda.TunnelStatus{
				Phase: kelda.TunnelStarting,
			},
		}
		wg.Add(1)
		go func(tunnelSpec *kelda.Tunnel) {
			defer util.HandlePanic()
			if err := s.createOrUpdateTunnel(tunnelSpec); err != nil {
				errorsChan <- errors.WithContext(err,
					fmt.Sprintf("create tunnel %q", tunnelSpec.Name))
			}
			wg.Done()
		}(tunnelSpec)
	}

	go func() {
		wg.Wait()
		close(errorsChan)
	}()

	err = <-errorsChan
	if err != nil {
		return msgs, err
	}

	return msgs, nil
}

var (
	errNamespaceTerminating = errors.NewFriendlyError(
		"Aborting deployment because namespace is terminating.\n" +
			"This is usually a transient error caused by `kelda delete` not completing yet.\n" +
			"If this problem persists, you may want to force delete the " +
			"namespace via kubectl.")

	errNonKeldaManagedNamespace = "Aborting deployment because namespace already exists, " +
		"and wasn't created by Kelda.\n" +
		"Either delete the namespace %q, or change the namespace in ~/.kelda.yaml"
)

func (s *server) setupNamespace(namespace string) ([]*messages.Message, error) {
	existingNamespaces, err := s.kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.WithContext(err, "get managed namespaces")
	}

	var usedSeats int
	for _, existingNs := range existingNamespaces.Items {
		isKeldaManaged := existingNs.Labels[keldaManagedResourceKey] == keldaManagedResourceValue
		if isKeldaManaged {
			usedSeats++
		}

		if existingNs.Name == namespace {
			if !isKeldaManaged {
				return nil, errors.NewFriendlyError(errNonKeldaManagedNamespace, namespace)
			}

			if existingNs.Status.Phase == core.NamespaceTerminating {
				return nil, errNamespaceTerminating
			}

			return nil, nil
		}
	}

	msgs, err := s.license.CheckSeats(usedSeats + 1)
	if err != nil {
		return msgs, errors.WithContext(err, "failed license verification")
	}

	_, err = s.kubeClient.CoreV1().Namespaces().Create(&core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
	})
	return msgs, err
}

func (s *server) hasAPIGroupAndVersion(group string, version string) (bool, error) {
	serverGroups, _, err := s.kubeClient.Discovery().ServerGroupsAndResources()
	if err != nil {
		return false, errors.WithContext(err, "get groups")
	}

	for _, serverGroup := range serverGroups {
		if serverGroup.Name != group {
			continue
		}
		for _, serverVersion := range serverGroup.Versions {
			if serverVersion.Version == version {
				return true, nil
			}
		}
	}

	return false, nil
}

func (s *server) makeMicroserviceFromProto(namespace string, priorityClassName string,
	svc minion.Service) (kelda.Microservice, error) {

	hasService := false
	hasJob := false
	numPodControllersExceptJobs := 0
	for _, manifest := range svc.GetManifests() {
		obj, err := kube.Parse([]byte(manifest))
		if err != nil {
			err = errors.NewFriendlyError(
				"Failed to parse the Kubernetes manifests for service %q.\n"+
					"Please check that the service directory contains valid Kubernetes YAML. "+
					"The YAML should be deployable via `kubectl apply -f`.\n\n"+
					"For debugging, the raw error message is shown below:\n%s", svc.Name, err)
			return kelda.Microservice{}, err
		}
		switch obj.GroupVersionKind().Kind {
		case "Deployment", "DaemonSet", "StatefulSet":
			hasService = true
			numPodControllersExceptJobs++
		case "Job":
			hasJob = true
		}
	}

	if svc.GetDevMode() {
		if !hasService && !hasJob {
			return kelda.Microservice{}, errors.NewFriendlyError(
				"Cannot start development on service %q because "+
					"it does not contain a Kubernetes Deployment, DaemonSet or StatefulSet.", svc.Name)
		}
		if !hasService && hasJob {
			return kelda.Microservice{}, errors.NewFriendlyError(
				"Cannot start development on service %q. "+
					"Development on Kubernetes Jobs is currently not supported.", svc.Name)
		}
		if numPodControllersExceptJobs != 1 {
			return kelda.Microservice{}, errors.NewFriendlyError(
				"Cannot start development on service %q. Kelda only supports development for "+
					"services that have exactly one Deployment, DaemonSet, or StatefulSet.\n\n"+
					"Please split up the service so that there is only one pod controller in %q.", svc.Name, svc.Name)
		}
	}

	// If we don't appear to have any jobs or services, just count it as a
	// service.
	if !hasService && !hasJob {
		hasService = true
	}

	msClient := s.keldaClient.KeldaV1alpha1().Microservices(namespace)
	curr, err := msClient.Get(svc.GetName(), metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return kelda.Microservice{}, errors.WithContext(err, "get microservice")
	}

	var currImageDigests []kelda.ImageDigest
	if err == nil { // the microservice exists
		currImageDigests = curr.Spec.ImageDigests
	}
	imageDigests, err := makeImageDigests(s.kubeClient, namespace, currImageDigests, svc.GetManifests())
	if err != nil {
		return kelda.Microservice{}, errors.WithContext(err, "make image digests")
	}

	annotations := map[string]string{}
	if priorityClassName != "" {
		annotations[annotationKeys.PriorityClass] = priorityClassName
	}

	status := kelda.MicroserviceStatus{}
	if hasService {
		status.ServiceStatus.Phase = kelda.ServiceStarting
	}
	if hasJob {
		status.JobStatus.Phase = kelda.JobStarting
	}
	return kelda.Microservice{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svc.GetName(),
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: kelda.MicroserviceSpec{
			DevMode:      svc.GetDevMode(),
			DevImage:     svc.GetDevImage(),
			Manifests:    svc.GetManifests(),
			HasService:   hasService,
			HasJob:       hasJob,
			ImageDigests: imageDigests,
		},
		Status:    status,
		DevStatus: kelda.DevStatus{},
	}, nil
}

func makeImageDigests(kubeClient kubernetes.Interface, namespace string,
	currDigests []kelda.ImageDigest, manifests []string) ([]kelda.ImageDigest, error) {

	containerInfos, err := update.GetContainerInfosFromManifests(manifests)
	if err != nil {
		return nil, errors.WithContext(err, "get container info")
	}

	var imageDigests []kelda.ImageDigest
	for _, info := range containerInfos {
		currDigest, ok := update.FindDigest(currDigests, info.ControllerName,
			info.ContainerName, info.ImageURL)
		if ok {
			// Use the same digest to avoid updating containers that have already
			// been deployed.
			imageDigests = append(imageDigests, *currDigest)
			continue
		}

		digest, err := updateGetDigestFromContainerInfo(&info, kubeClient, namespace) // nolint: scopelint
		if err != nil {
			// If it fails to retrieve the image digest from the
			// registry, it shouldn't crash.
			log.WithError(err).WithField("controller", info.ControllerName).WithField(
				"container", info.ContainerName).Warn("Failed to retrieve image digest. " +
				"Will deploy with tag.")
			continue
		}
		imageDigests = append(imageDigests, kelda.ImageDigest{
			ControllerName: info.ControllerName,
			ContainerName:  info.ContainerName,
			ImageURL:       info.ImageURL,
			Digest:         digest,
		})
	}
	return imageDigests, nil
}

func tunnelName(tunnel *minion.Tunnel) string {
	return fmt.Sprintf("%s-%d-%d",
		tunnel.GetServiceName(), tunnel.GetLocalPort(), tunnel.GetRemotePort())
}

// Copy the given secret from the Kelda namespace into the target namespace.
func (s *server) copySecret(namespace, name string) error {
	secret, err := s.kubeClient.CoreV1().Secrets(client.KeldaNamespace).Get(
		name, metav1.GetOptions{})
	if err != nil {
		return errors.WithContext(err, "get source secret")
	}

	client := s.kubeClient.CoreV1().Secrets(namespace)
	actual, err := client.Get(secret.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get target secret")
	}

	secret.Namespace = namespace
	secret.UID = types.UID("")
	if kerrors.IsNotFound(err) {
		secret.ResourceVersion = ""
		if _, err = client.Create(secret); err != nil {
			return errors.WithContext(err, "create")
		}
	} else {
		secret.ResourceVersion = actual.ResourceVersion
		if _, err = client.Update(secret); err != nil {
			return errors.WithContext(err, "update")
		}
	}
	return nil
}

func (s *server) createOrUpdateService(ms *kelda.Microservice) error {
	msClient := s.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace)
	curr, err := msClient.Get(ms.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if msExists := err == nil; !msExists {
		if _, err := msClient.Create(ms); err != nil {
			return errors.WithContext(err, "create")
		}
		return nil
	}

	// If the microservice in the cluster is the same, then don't update the
	// Microservice. This avoids voiding out the Microservice's status.
	normalizeMicroserviceSpec(&ms.Spec)
	normalizeMicroserviceSpec(&curr.Spec)
	if reflect.DeepEqual(ms.Spec, curr.Spec) {
		return nil
	}

	// Create a deep copy to avoid mutating the original microservice struct.
	msPatch := ms.DeepCopy()
	msPatch.SpecVersion = curr.SpecVersion + 1

	patch, err := json.Marshal(msPatch)
	if err != nil {
		return errors.WithContext(err, "marshal")
	}

	_, err = msClient.Patch(ms.Name, types.MergePatchType, patch)
	if err != nil {
		return errors.WithContext(err, "patch")
	}
	return nil
}

func normalizeMicroserviceSpec(spec *kelda.MicroserviceSpec) {
	sort.Strings(spec.Manifests)
	sort.Slice(spec.ImageDigests, func(i, j int) bool {
		return spec.ImageDigests[i].Digest < spec.ImageDigests[j].Digest
	})
}

func (s *server) createOrUpdateTunnel(tunnel *kelda.Tunnel) error {
	tunnelsClient := s.keldaClient.KeldaV1alpha1().Tunnels(tunnel.Namespace)
	_, err := tunnelsClient.Get(tunnel.Name, metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	if tunnelExists := err == nil; !tunnelExists {
		if _, err := tunnelsClient.Create(tunnel); err != nil {
			return errors.WithContext(err, "create")
		}
		return nil
	}

	patch, err := json.Marshal(tunnel)
	if err != nil {
		return errors.WithContext(err, "marshal")
	}

	_, err = tunnelsClient.Patch(tunnel.Name, types.MergePatchType, patch)
	if err != nil {
		return errors.WithContext(err, "patch")
	}
	return nil
}

func (s *server) createDevServiceAccount(namespace string,
	imagePullSecrets []core.LocalObjectReference) error {
	saClient := s.kubeClient.CoreV1().ServiceAccounts(namespace)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sa, err := saClient.Get(DevServiceAccountName, metav1.GetOptions{})
		if err != nil && !kerrors.IsNotFound(err) {
			return errors.WithContext(err, "get account")
		}

		if saExists := err == nil; saExists {
			sa.ImagePullSecrets = imagePullSecrets
			_, err = saClient.Update(sa)
			if err != nil {
				// Return the genuine error if err is conflict so that
				// `retry.RetryOnConflict` can retry.
				if kerrors.IsConflict(err) {
					return err
				}
				return errors.WithContext(err, "update account")
			}
		} else {
			_, err = saClient.Create(&core.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DevServiceAccountName,
					Namespace: namespace,
				},
				ImagePullSecrets: imagePullSecrets,
			})
			if err != nil {
				return errors.WithContext(err, "create account")
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	_, err = s.kubeClient.RbacV1().Roles(namespace).Create(&rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kelda-dev-role",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.WithContext(err, "create role")
	}

	_, err = s.kubeClient.RbacV1().RoleBindings(namespace).Create(&rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kelda-dev-role-binding",
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      DevServiceAccountName,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     "kelda-dev-role",
			APIGroup: "rbac.authorization.k8s.io",
		},
	})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.WithContext(err, "create role binding")
	}

	return nil
}

func (s *server) createPriorityClass(namespace string) error {
	_, err := s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})

	if err == nil {
		// The PriorityClass already exists, so there is nothing to do.
		return nil
	}

	// The "Not Found" error is expected when we haven't created the PriorityClass
	// yet. If other errors occur, return them.
	if !kerrors.IsNotFound(err) {
		return errors.WithContext(err, "get")
	}

	// ListOptions only accepts a string, so convert it.
	// Reuse the managed namespace label in order to decrease need to manage keys.
	keldaManagedPriorityClassLabelStr := mapToString(KeldaManagedResourceLabel)

	priorityClasses, err := s.kubeClient.SchedulingV1beta1().PriorityClasses().List(metav1.ListOptions{
		LabelSelector: keldaManagedPriorityClassLabelStr})
	if err != nil {
		return err
	}

	priorityValue := MaxPodPriorityValue

	lastCreated := time.Time{}
	for _, priorityClass := range priorityClasses.Items {
		if priorityClass.CreationTimestamp.Time.After(lastCreated) {
			lastCreated = priorityClass.CreationTimestamp.Time
			priorityValue = priorityClass.Value - 100
		}
	}
	if priorityValue < 0 {
		// Unlikely to happen due to size of the set of available priorities,
		// but we overflow back to max in this case.
		priorityValue = MaxPodPriorityValue
	}

	_, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Create(&scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       priorityValue,
		Description: fmt.Sprintf("This is a PriorityClass for the workspace: %s", namespace),
	})
	return err
}

// Mocked for unit testing.
var serviceAccountCreationTimeout = 1 * time.Minute

func (s *server) addRegcredToDefaultSA(namespace string,
	imagePullSecrets []core.LocalObjectReference) error {

	// Wait for the service account to exist before updating its
	// ImagePullSecrets. It doesn't exist immediately after the namespace is
	// created because Kubernetes is still creating the objects within the
	// namespace.
	saClient := s.kubeClient.CoreV1().ServiceAccounts(namespace)
	ctx, cancel := context.WithTimeout(context.Background(), serviceAccountCreationTimeout)
	defer cancel()
	err := wait.PollUntil(1*time.Second, func() (bool, error) {
		_, err := saClient.Get("default", metav1.GetOptions{})
		switch {
		case err == nil:
			return true, nil
		case kerrors.IsNotFound(err):
			return false, nil
		default:
			// Abort the wait early if the error is something other than the
			// service account not existing.
			return false, err
		}
	}, ctx.Done())

	if err != nil {
		return errors.WithContext(err, "wait for service account to exist")
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sa, err := saClient.Get("default", metav1.GetOptions{})
		if err != nil {
			return errors.WithContext(err, "get account")
		}

		sa.ImagePullSecrets = imagePullSecrets
		_, err = saClient.Update(sa)
		return err
	})
}

func (s *server) LogEvent(ctx context.Context, req *minion.LogEventRequest) (
	*minion.LogEventResponse, error) {
	return &minion.LogEventResponse{
		Error: errors.Marshal(s.logEvent(req)),
	}, nil
}

func (s *server) logEvent(req *minion.LogEventRequest) error {
	time, err := ptypes.Timestamp(req.GetEvent().GetTime())
	if err != nil {
		return errors.WithContext(err, "parse event timestamp")
	}

	additional := map[string]interface{}{}
	if err := json.Unmarshal([]byte(req.GetEvent().GetAdditionalJson()), &additional); err != nil {
		return errors.WithContext(err, "parse event additional")
	}

	// Always log the Kubernetes version or the error in getting it.
	var kubeVersionLog string
	v, err := s.kubeClient.Discovery().ServerVersion()
	if err != nil {
		kubeVersionLog = err.Error()
	} else {
		kubeVersionLog = v.String()
	}
	additional["kubernetes-version"] = kubeVersionLog

	analytics.Log.WithField("event", log.Fields{
		"time":       time,
		"namespace":  req.GetEvent().GetNamespace(),
		"name":       req.GetEvent().GetEvent(),
		"additional": additional,
	}).Info("New client event from deprecated endpoint")
	return nil
}

func (s *server) GetVersion(ctx context.Context, _ *minion.GetVersionRequest) (
	*minion.GetVersionResponse, error) {

	return &minion.GetVersionResponse{
		KeldaVersion: &minion.KeldaVersion{
			Version: version.Version,
		},
	}, nil
}

func (s *server) GetLicense(ctx context.Context, _ *minion.GetLicenseRequest) (
	*minion.GetLicenseResponse, error) {

	var licenseType minion.LicenseTerms_LicenseType
	switch s.license.Terms.Type {
	case config.Customer:
		licenseType = minion.LicenseTerms_CUSTOMER
	case config.Trial:
		licenseType = minion.LicenseTerms_TRIAL
	default:
		return &minion.GetLicenseResponse{
			Error: errors.Marshal(errors.New("unknown license type")),
		}, nil
	}

	expiryTime, err := ptypes.TimestampProto(s.license.Terms.ExpiryTime)
	if err != nil {
		return &minion.GetLicenseResponse{
			Error: errors.Marshal(errors.WithContext(err, "convert expiry timestamp")),
		}, nil
	}

	return &minion.GetLicenseResponse{
		License: &minion.License{
			Terms: &minion.LicenseTerms{
				Customer:   s.license.Terms.Customer,
				Type:       licenseType,
				Seats:      int32(s.license.Terms.Seats),
				ExpiryTime: expiryTime,
			},
		},
	}, nil
}

func mapToString(m map[string]string) string {
	var mappings []string
	for k, v := range m {
		mappings = append(mappings, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(mappings, ",")
}

func (s *server) GetUpdates(ctx context.Context,
	req *minion.GetUpdatesRequest) (*minion.GetUpdatesResponse, error) {
	updates, err := s.getUpdates(req.GetNamespace())
	return &minion.GetUpdatesResponse{
		ServiceUpdates: updates,
		Error:          errors.Marshal(err),
	}, nil
}

func (s *server) getUpdates(namespace string) ([]*minion.ServiceUpdate, error) {
	msClient := s.keldaClient.KeldaV1alpha1().Microservices(namespace)
	svcs, err := msClient.List(metav1.ListOptions{})
	if err != nil {
		return nil, errors.WithContext(err, "list microservices")
	}

	var ret []*minion.ServiceUpdate

	type ServiceUpdateWithError struct {
		serviceUpdate *minion.ServiceUpdate
		err           error
	}

	updChan := make(chan ServiceUpdateWithError, len(svcs.Items))
	var wg sync.WaitGroup
	for _, svc := range svcs.Items {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Skip services in dev mode.
			if svc.Spec.DevMode {
				return
			}

			containerUpdates, err := s.getContainerUpdates(&svc)
			updChan <- ServiceUpdateWithError{
				serviceUpdate: &minion.ServiceUpdate{
					Name:             svc.GetName(),
					ContainerUpdates: containerUpdates,
				},
				err: err,
			}
		}()
	}
	wg.Wait()

	close(updChan)
	for upd := range updChan {
		if upd.err != nil {
			return nil, upd.err
		}
		if len(upd.serviceUpdate.ContainerUpdates) != 0 {
			ret = append(ret, upd.serviceUpdate)
		}
	}

	return ret, nil
}

func (s *server) getContainerUpdates(svc *kelda.Microservice) ([]*minion.ContainerUpdate, error) {
	containerInfos, err := update.GetContainerInfosFromManifests(svc.Spec.Manifests)
	if err != nil {
		return nil, errors.WithContext(err, "get container info")
	}

	var ret []*minion.ContainerUpdate
	for _, info := range containerInfos {
		if update := s.getContainerUpdate(&info, svc); update != nil { // nolint: scopelint
			ret = append(ret, update)
		}
	}

	return ret, nil
}

func (s *server) getContainerUpdate(info *update.ContainerInfo,
	svc *kelda.Microservice) *minion.ContainerUpdate {
	runningDigest, ok := update.FindDigest(svc.Spec.ImageDigests,
		info.ControllerName, info.ContainerName, info.ImageURL)
	if !ok {
		// If the digest is missing, meaning the minion server fails to retrieve
		// it from the registry, it shouldn't crash.
		log.WithField("controller", info.ControllerName).WithField(
			"container", info.ContainerName).Warn("Missing digest override. " +
			"Unable to check for update.")
		return nil
	}
	containerDigest := runningDigest.Digest

	latestDigest, err := updateGetDigestFromContainerInfo(info, s.kubeClient, svc.Namespace)
	if err != nil {
		// If it fails to retrieve the image digest from the registry, it
		// shouldn't crash.
		log.WithError(err).WithField("controller", info.ControllerName).WithField(
			"container", info.ContainerName).Warn("Failed to retrieve image digest. " +
			"Unable to check for update.")
		return nil
	}

	if containerDigest != latestDigest {
		return &minion.ContainerUpdate{
			ControllerName: info.ControllerName,
			ContainerName:  info.ContainerName,
			OldDigest:      containerDigest,
			NewDigest:      latestDigest,
			ImageUrl:       info.ImageURL,
		}
	}
	return nil
}

func (s *server) PerformUpdates(ctx context.Context,
	req *minion.PerformUpdatesRequest) (*minion.PerformUpdatesResponse, error) {
	return &minion.PerformUpdatesResponse{
		Error: errors.Marshal(s.performUpdates(req.GetNamespace(), req.GetServiceUpdates())),
	}, nil
}

func (s *server) performUpdates(namespace string, serviceUpdates []*minion.ServiceUpdate) error {
	msClient := s.keldaClient.KeldaV1alpha1().Microservices(namespace)

	// Update each service specified in serviceUpdates.
	for _, serviceUpdate := range serviceUpdates {
		serviceUpdate := serviceUpdate
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			svc, err := msClient.Get(serviceUpdate.GetName(),
				metav1.GetOptions{})
			if err != nil {
				return errors.WithContext(err, "get microservice")
			}

			// Update each container in the service.
			for _, containerUpdate := range serviceUpdate.ContainerUpdates {
				imageDigest, ok := update.FindDigest(
					svc.Spec.ImageDigests,
					containerUpdate.GetControllerName(),
					containerUpdate.GetContainerName(),
					containerUpdate.GetImageUrl())
				if ok {
					imageDigest.Digest = containerUpdate.GetNewDigest()
				} else {
					svc.Spec.ImageDigests = append(svc.Spec.ImageDigests,
						kelda.ImageDigest{
							ControllerName: containerUpdate.GetControllerName(),
							ContainerName:  containerUpdate.GetContainerName(),
							Digest:         containerUpdate.GetNewDigest(),
							ImageURL:       containerUpdate.GetImageUrl(),
						})
				}
			}

			// Unset the status for the old version of the containers.
			status := kelda.MicroserviceStatus{}
			if svc.Spec.HasService {
				status.ServiceStatus.Phase = kelda.ServiceStarting
			}
			if svc.Spec.HasJob {
				status.JobStatus.Phase = kelda.JobStarting
			}
			svc.Status = status

			svc.SpecVersion++
			_, err = msClient.Update(svc)
			return err
		})
		if err != nil {
			return errors.WithContext(err, "update microservice")
		}
	}

	return nil
}
