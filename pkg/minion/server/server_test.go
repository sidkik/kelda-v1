package server

import (
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	scheduling "k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kelda-inc/kelda/pkg/config"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	fakeKelda "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned/fake"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/proto/messages"
	"github.com/kelda-inc/kelda/pkg/proto/minion"
	"github.com/kelda-inc/kelda/pkg/update"
)

func TestCreatePriorityClass(t *testing.T) {
	s := server{kubeClient: fake.NewSimpleClientset()}

	// Test create first PriorityClass at MaxPodPriorityValue.
	namespace := "first"
	exp := scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       MaxPodPriorityValue,
		Description: "This is a PriorityClass for the workspace: first",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err := s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)

	// Manually inject the creation time -- the mock does not auto-populate this
	// Creation time for "first" is recorded for later test.
	firstCreationTime := time.Now()
	actual.CreationTimestamp = metav1.Time{
		Time: firstCreationTime,
	}
	_, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Update(actual)
	assert.NoError(t, err)

	// Create the second and third PriorityClasses.
	namespace = "second"
	exp = scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       MaxPodPriorityValue - 100,
		Description: "This is a PriorityClass for the workspace: second",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)

	// Manually inject the creation time -- the mock does not auto-populate this.
	actual.CreationTimestamp = metav1.Time{
		Time: time.Now(),
	}
	_, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Update(actual)
	assert.NoError(t, err)

	namespace = "third"
	exp = scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       MaxPodPriorityValue - 200,
		Description: "This is a PriorityClass for the workspace: third",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)

	// Manually inject the creation time -- the mock does not auto-populate this.
	actual.CreationTimestamp = metav1.Time{
		Time: time.Now(),
	}
	_, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Update(actual)
	assert.NoError(t, err)

	// Check that recreation of the second PriorityClass results in lower priority.
	namespace = "second"
	err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Delete(namespace, &metav1.DeleteOptions{})
	assert.NoError(t, err)

	exp = scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       MaxPodPriorityValue - 300,
		Description: "This is a PriorityClass for the workspace: second",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)

	// Recreate the first PriorityClass again and ensure it hasn't changed.
	namespace = "first"
	exp = scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              namespace,
			Labels:            KeldaManagedResourceLabel,
			CreationTimestamp: metav1.Time{Time: firstCreationTime},
		},
		Value:       MaxPodPriorityValue,
		Description: "This is a PriorityClass for the workspace: first",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)

	// Test that automatic overflow works.
	_, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Create(&scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "a lot",
			Labels:            KeldaManagedResourceLabel,
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Value:       0,
		Description: "This is a PriorityClass for the workspace: a lot",
	})
	assert.NoError(t, err)

	namespace = "tenth"
	exp = scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: KeldaManagedResourceLabel,
		},
		Value:       MaxPodPriorityValue,
		Description: "This is a PriorityClass for the workspace: tenth",
	}
	assert.NoError(t, s.createPriorityClass(namespace))
	actual, err = s.kubeClient.SchedulingV1beta1().PriorityClasses().Get(namespace, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, *actual)
}

func TestCreateOrUpdateService(t *testing.T) {
	s := server{keldaClient: fakeKelda.NewSimpleClientset()}

	// Test create when it doesn't exist.
	ms := &kelda.Microservice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: kelda.MicroserviceSpec{
			Manifests: []string{"orig"},
		},
	}
	exp := ms.DeepCopy()
	assert.NoError(t, s.createOrUpdateService(ms))

	actual, err := s.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace).
		Get(ms.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.Equal(t, exp, actual)

	// Test update when there's been no change.
	assert.NoError(t, s.createOrUpdateService(ms))

	actual, err = s.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace).
		Get(ms.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, ms, actual)

	// Test update when manifest has changed.
	ms.Spec.Manifests[0] = "updated"
	exp = ms.DeepCopy()
	exp.SpecVersion = 1

	assert.NoError(t, s.createOrUpdateService(ms))

	actual, err = s.keldaClient.KeldaV1alpha1().Microservices(ms.Namespace).
		Get(ms.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, exp, actual)
}

func TestCopySecret(t *testing.T) {
	namespace := "namespace"

	appRegistrySecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: client.KeldaNamespace,
			Name:      client.RegistrySecretName,
		},
		Data: map[string][]byte{
			"app": []byte("secret"),
		},
	}

	appRegistrySecretInNamespace := appRegistrySecret
	appRegistrySecretInNamespace.Namespace = namespace

	outdatedAppRegistrySecret := appRegistrySecretInNamespace
	outdatedAppRegistrySecret.Data = map[string][]byte{
		"outdated": []byte("secret"),
	}

	tests := []struct {
		name       string
		client     *fake.Clientset
		expObjects []*corev1.Secret
		expError   error
	}{
		{
			name:   "Application regcred exists",
			client: fake.NewSimpleClientset(&appRegistrySecret),
			expObjects: []*corev1.Secret{
				&appRegistrySecretInNamespace,
			},
			expError: nil,
		},
		{
			name: "Application regcred outdated",
			client: fake.NewSimpleClientset(
				&appRegistrySecret, &outdatedAppRegistrySecret,
			),
			expObjects: []*corev1.Secret{
				&appRegistrySecretInNamespace,
			},
			expError: nil,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := server{kubeClient: test.client}
			err := s.copySecret(namespace, client.RegistrySecretName)
			assert.Equal(t, test.expError, err)

			for _, exp := range test.expObjects {
				actual, err := s.kubeClient.CoreV1().Secrets(exp.Namespace).Get(exp.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, exp, actual)
			}
		})
	}
}

func TestMapToString(t *testing.T) {
	// Go map iteration is in randomized order so we enumerate all possible cases.
	tests := []struct {
		name        string
		input       map[string]string
		expPossible []string
	}{
		{
			name: "Size 1 string map",
			input: map[string]string{
				"salt": "salty",
			},
			expPossible: []string{"salt=salty"},
		},
		{
			name: "Size 2 string map",
			input: map[string]string{
				"salt":  "salty",
				"sugar": "sweet",
			},
			expPossible: []string{
				"salt=salty,sugar=sweet",
				"sugar=sweet,salt=salty",
			},
		},
		{
			name: "Size 3 string map",
			input: map[string]string{
				"salt":  "salty",
				"sugar": "sweet",
				"acid":  "sour",
			},
			expPossible: []string{
				"salt=salty,sugar=sweet,acid=sour",
				"salt=salty,acid=sour,sugar=sweet",
				"sugar=sweet,salt=salty,acid=sour",
				"sugar=sweet,acid=sour,salt=salty",
				"acid=sour,salt=salty,sugar=sweet",
				"acid=sour,sugar=sweet,salt=salty",
			},
		},
	}

	for _, test := range tests {
		assert.Contains(t, test.expPossible, mapToString(test.input))
	}
}

func TestGetAndPerformUpdates(t *testing.T) {
	oldDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	newDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	namespace := "namespace"
	serviceName := "service"
	manifests := []string{
		`apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: controller1
spec:
  replicas: 1
  template:
    metadata:
      labels:
        service: web
    spec:
      containers:
      - name: container1
        image: nginx
        imagePullPolicy: IfNotPresent
      - name: container2
        image: apache
        imagePullPolicy: IfNotPresent
      imagePullSecrets:
      - name: regcred
`,
		`apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: controller2
spec:
  replicas: 1
  template:
    metadata:
      labels:
        service: database
    spec:
      containers:
      - name: container1
        image: mariadb
        imagePullPolicy: IfNotPresent
`,
	}
	newestImageDigests := []kelda.ImageDigest{
		{
			ControllerName: "controller1",
			ContainerName:  "container1",
			Digest:         newDigest,
			ImageURL:       "nginx",
		},
		{
			ControllerName: "controller1",
			ContainerName:  "container2",
			Digest:         newDigest,
			ImageURL:       "apache",
		},
		{
			ControllerName: "controller2",
			ContainerName:  "container1",
			Digest:         newDigest,
			ImageURL:       "mariadb",
		},
	}

	updateGetDigestFromContainerInfo = func(*update.ContainerInfo,
		kubernetes.Interface, string) (string, error) {
		return newDigest, nil
	}

	readyStatus := kelda.MicroserviceStatus{
		ServiceStatus: kelda.ServiceStatus{
			Phase: kelda.ServiceReady,
		},
	}
	startingStatus := kelda.MicroserviceStatus{
		ServiceStatus: kelda.ServiceStatus{
			Phase: kelda.ServiceStarting,
		},
	}

	tests := []struct {
		name              string
		inputImageDigests []kelda.ImageDigest
		expUpdates        []*minion.ServiceUpdate
		expStatus         kelda.MicroserviceStatus
	}{
		{
			name:              "no update",
			inputImageDigests: newestImageDigests,
			expUpdates:        nil,
			// The status shouldn't be changed from the default in the test setup.
			expStatus: readyStatus,
		},
		{
			name: "one update",
			inputImageDigests: []kelda.ImageDigest{
				{
					ControllerName: "controller1",
					ContainerName:  "container1",
					Digest:         oldDigest,
					ImageURL:       "nginx",
				},
				{
					ControllerName: "controller1",
					ContainerName:  "container2",
					Digest:         newDigest,
					ImageURL:       "apache",
				},
				{
					ControllerName: "controller2",
					ContainerName:  "container1",
					Digest:         newDigest,
					ImageURL:       "mariadb",
				},
			},
			expUpdates: []*minion.ServiceUpdate{
				{
					Name: serviceName,
					ContainerUpdates: []*minion.ContainerUpdate{
						{
							ControllerName: "controller1",
							ContainerName:  "container1",
							OldDigest:      oldDigest,
							NewDigest:      newDigest,
							ImageUrl:       "nginx",
						},
					},
				},
			},
			expStatus: startingStatus,
		},
		{
			name: "multiple updates",
			inputImageDigests: []kelda.ImageDigest{
				{
					ControllerName: "controller1",
					ContainerName:  "container1",
					Digest:         oldDigest,
					ImageURL:       "nginx",
				},
				{
					ControllerName: "controller1",
					ContainerName:  "container2",
					Digest:         oldDigest,
					ImageURL:       "apache",
				},
				{
					ControllerName: "controller2",
					ContainerName:  "container1",
					Digest:         oldDigest,
					ImageURL:       "mariadb",
				},
			},
			expUpdates: []*minion.ServiceUpdate{
				{
					Name: serviceName,
					ContainerUpdates: []*minion.ContainerUpdate{
						{
							ControllerName: "controller1",
							ContainerName:  "container1",
							OldDigest:      oldDigest,
							NewDigest:      newDigest,
							ImageUrl:       "nginx",
						},
						{
							ControllerName: "controller1",
							ContainerName:  "container2",
							OldDigest:      oldDigest,
							NewDigest:      newDigest,
							ImageUrl:       "apache",
						},
						{
							ControllerName: "controller2",
							ContainerName:  "container1",
							OldDigest:      oldDigest,
							NewDigest:      newDigest,
							ImageUrl:       "mariadb",
						},
					},
				},
			},
			expStatus: startingStatus,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := &server{keldaClient: fakeKelda.NewSimpleClientset()}
			_, err := s.keldaClient.KeldaV1alpha1().Microservices(
				namespace).Create(&kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:    manifests,
					ImageDigests: test.inputImageDigests,
					HasService:   true,
				},
				Status: readyStatus,
			})
			assert.NoError(t, err)
			updates, err := s.getUpdates(namespace)
			assert.Equal(t, test.expUpdates, updates)
			assert.NoError(t, err)
			assert.NoError(t, s.performUpdates(namespace, updates))
			svc, err := s.keldaClient.KeldaV1alpha1().Microservices(
				namespace).Get(serviceName, metav1.GetOptions{})
			assert.NoError(t, err)
			assert.Equal(t, newestImageDigests, svc.Spec.ImageDigests)
			assert.Equal(t, test.expStatus, svc.Status)
		})
	}
}

func TestMakeMicroserviceFromProto(t *testing.T) {
	namespace := "namespace"
	oldDigest := "oldDigest"
	newDigest := "newDigest"
	msName := "msName"
	priorityClassName := namespace

	nginxManifest := `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: nginx-deployment
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: nginx-container
        image: nginx
`

	alpineManifest := `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: alpine-deployment
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: alpine-container
        image: alpine
`

	alpineTwoManifest := `apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: alpine-deployment
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: alpine-container
        image: alpine:2
`

	piJobManifest := `apiVersion: batch/v1
kind: Job
metadata:
  name: pi
spec:
  template:
    spec:
      containers:
      - name: pi
        image: perl
        command: ["perl",  "-Mbignum=bpi", "-wle", "print bpi(2000)"]
        restartPolicy: Never
  backoffLimit: 4
`

	updateGetDigestFromContainerInfo = func(*update.ContainerInfo,
		kubernetes.Interface, string) (string, error) {
		return newDigest, nil
	}

	startingStatus := kelda.MicroserviceStatus{
		ServiceStatus: kelda.ServiceStatus{
			Phase: kelda.ServiceStarting,
		},
	}

	tests := []struct {
		name     string
		in       minion.Service
		currMs   *kelda.Microservice
		expMs    kelda.Microservice
		expError error
	}{
		{
			name: "Create",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{nginxManifest},
			},
			expMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": namespace,
					},
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{nginxManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "nginx-deployment",
							ContainerName:  "nginx-container",
							ImageURL:       "nginx",
							Digest:         newDigest,
						},
					},
				},
				Status: startingStatus,
			},
		},
		{
			name: "UpdateAddManifest",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{nginxManifest, alpineManifest},
			},
			currMs: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{nginxManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "nginx-deployment",
							ContainerName:  "nginx-container",
							Digest:         newDigest,
							ImageURL:       "nginx",
						},
					},
				},
			},
			expMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": namespace,
					},
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{nginxManifest, alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "nginx-deployment",
							ContainerName:  "nginx-container",
							Digest:         newDigest,
							ImageURL:       "nginx",
						},
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         newDigest,
							ImageURL:       "alpine",
						},
					},
				},
				Status: startingStatus,
			},
		},
		{
			name: "UpdateRemoveManifest",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{alpineManifest},
			},
			currMs: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{nginxManifest, alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "nginx-deployment",
							ContainerName:  "nginx-container",
							Digest:         newDigest,
							ImageURL:       "nginx",
						},
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         newDigest,
							ImageURL:       "alpine",
						},
					},
				},
			},
			expMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": namespace,
					},
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         newDigest,
							ImageURL:       "alpine",
						},
					},
				},
				Status: startingStatus,
			},
		},
		{
			name: "ReuseImageDigest",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{alpineManifest},
			},
			currMs: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         oldDigest,
							ImageURL:       "alpine",
						},
					},
				},
			},
			expMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": namespace,
					},
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         oldDigest,
							ImageURL:       "alpine",
						},
					},
				},
				Status: startingStatus,
			},
		},
		{
			name: "UpdateContainerImage",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{alpineTwoManifest},
			},
			currMs: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{alpineManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         "badDigest",
							ImageURL:       "alpine",
						},
					},
				},
			},
			expMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: namespace,
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": namespace,
					},
				},
				Spec: kelda.MicroserviceSpec{
					Manifests:  []string{alpineTwoManifest},
					HasService: true,
					ImageDigests: []kelda.ImageDigest{
						{
							ControllerName: "alpine-deployment",
							ContainerName:  "alpine-container",
							Digest:         newDigest,
							ImageURL:       "alpine:2",
						},
					},
				},
				Status: startingStatus,
			},
		},
		{
			name: "MalformedKubeYAML",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{"invalid YAML"},
			},
			expError: errors.NewFriendlyError(
				"Failed to parse the Kubernetes manifests for service \"msName\".\n" +
					"Please check that the service directory contains " +
					"valid Kubernetes YAML. " +
					"The YAML should be deployable via `kubectl apply -f`.\n\n" +
					"For debugging, the raw error message is shown below:\n" +
					"couldn't get version/kind; json parse error: json: cannot " +
					"unmarshal string into Go value of type struct { APIVersion " +
					`string "json:\"apiVersion,omitempty\""; Kind string ` +
					`"json:\"kind,omitempty\"" }`),
		},
		{
			name: "NoPodControllersDefinedOnDev",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{},
				DevMode:   true,
			},
			expError: errors.NewFriendlyError(
				"Cannot start development on service %q because "+
					"it does not contain a Kubernetes Deployment, DaemonSet or StatefulSet.", msName),
		},
		{
			name: "OnlyJobsOnDev",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{piJobManifest},
				DevMode:   true,
			},
			expError: errors.NewFriendlyError(
				"Cannot start development on service %q. "+
					"Development on Kubernetes Jobs is currently not supported.", msName),
		},
		{
			name: "MultipleDeploymentsOnDev",
			in: minion.Service{
				Name:      msName,
				Manifests: []string{nginxManifest, alpineManifest},
				DevMode:   true,
			},
			expError: errors.NewFriendlyError(
				"Cannot start development on service %q. Kelda only supports "+
					"development for services that have exactly one Deployment, "+
					"DaemonSet, or StatefulSet.\n\n"+
					"Please split up the service so that there is only one pod "+
					"controller in %q.", msName, msName),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := &server{keldaClient: fakeKelda.NewSimpleClientset()}
			if test.currMs != nil {
				_, err := s.keldaClient.KeldaV1alpha1().Microservices(
					namespace).Create(test.currMs)
				assert.NoError(t, err)
			}

			actual, err := s.makeMicroserviceFromProto(namespace, priorityClassName, test.in)
			assert.Equal(t, test.expError, err)
			assert.Equal(t, test.expMs, actual)
		})
	}
}

func TestAddRegcredToDefaultSA(t *testing.T) {
	serviceAccountCreationTimeout = 2 * time.Second
	imagePullSecrets := []corev1.LocalObjectReference{
		{Name: client.RegistrySecretName},
	}
	namespace := "namespace"

	tests := []struct {
		name        string
		mockObjects []runtime.Object
		expError    error
		expObjects  []corev1.ServiceAccount
	}{
		{
			name: "Service account does not exist",
			expError: errors.WithContext(
				errors.New("timed out waiting for the condition"),
				"wait for service account to exist"),
		},
		{
			name: "Service account exists",
			mockObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: namespace,
					},
				},
			},
			expObjects: []corev1.ServiceAccount{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: namespace,
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
		{
			name: "Service account exists with secrets",
			mockObjects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: namespace,
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
			expObjects: []corev1.ServiceAccount{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default",
						Namespace: namespace,
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := server{kubeClient: fake.NewSimpleClientset(test.mockObjects...)}
			err := s.addRegcredToDefaultSA(namespace, imagePullSecrets)
			assert.Equal(t, test.expError, err)
			for _, obj := range test.expObjects {
				saClient := s.kubeClient.CoreV1().ServiceAccounts(namespace)
				sa, err := saClient.Get(obj.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, obj, *sa)
			}
		})
	}
}

func TestSetupNamespace(t *testing.T) {
	namespace := "namespace"
	license := config.License{
		Terms: config.Terms{
			Type: config.Trial,
		},
	}
	tests := []struct {
		name          string
		mockObjects   []runtime.Object
		expError      error
		expMessages   []*messages.Message
		expNamespaces []corev1.Namespace
	}{
		{
			name: "DoesNotExist",
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
				},
			},
		},
		{
			name: "AlreadyExists",
			mockObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
		},
		{
			name: "Terminating",
			mockObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceTerminating,
					},
				},
			},
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceTerminating,
					},
				},
			},
			expError: errNamespaceTerminating,
		},
		{
			name: "GracePeriod",
			mockObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},

				// This namespace shouldn't be counted.
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "non-kelda",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
			expMessages: []*messages.Message{
				{
					Type: messages.Message_WARNING,
					Text: "Application seats in use exceeds licensed seats: " +
						"2 seats used of 1 purchased.\n" +
						"Entering grace period. Please contact your Kelda administrator to expand your license.",
				},
			},
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: KeldaManagedResourceLabel,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "non-kelda",
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
		},
		{
			name: "ExceededSeats",
			mockObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace-1",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace-2",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
			expError: errors.WithContext(
				errors.NewFriendlyError("Application seats in use exceeds grace threshold: "+
					"3 seats used of 1 purchased.\n"+
					"Please contact your Kelda administrator to expand your license."),
				"failed license verification"),
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace-1",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "another-namespace-2",
						Labels: KeldaManagedResourceLabel,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
		},
		{
			name: "NonKeldaNamespace",
			mockObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
			expError: errors.NewFriendlyError(errNonKeldaManagedNamespace, namespace),
			expNamespaces: []corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
					Status: corev1.NamespaceStatus{
						Phase: corev1.NamespaceActive,
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := server{
				license:    license,
				kubeClient: fake.NewSimpleClientset(test.mockObjects...),
			}

			msgs, err := s.setupNamespace(namespace)
			assert.Equal(t, test.expError, err)
			assert.Equal(t, test.expMessages, msgs)

			actualNamespaces := map[string]corev1.Namespace{}
			actualNamespacesResp, err := s.kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
			assert.NoError(t, err)

			for _, ns := range actualNamespacesResp.Items {
				actualNamespaces[ns.Name] = ns
			}

			// Check that each expected namespace is present.
			for _, expNs := range test.expNamespaces {
				actualNs, ok := actualNamespaces[expNs.Name]
				assert.True(t, ok)
				assert.Equal(t, expNs, actualNs)
				delete(actualNamespaces, expNs.Name)
			}

			// Check that there aren't any namespaces not in the
			// `expNamespaces` list.
			assert.Empty(t, actualNamespaces)
		})
	}
}

func TestGetLicense(t *testing.T) {
	expiryTime := time.Now().UTC()
	pbExpiryTime, err := ptypes.TimestampProto(expiryTime)
	assert.NoError(t, err)

	tests := []struct {
		name    string
		license config.License
		expResp *minion.GetLicenseResponse
	}{
		{
			name: "Customer license",
			license: config.License{
				Terms: config.Terms{
					Customer:   "customer",
					Type:       config.Customer,
					Seats:      5,
					ExpiryTime: expiryTime,
				},
			},
			expResp: &minion.GetLicenseResponse{
				License: &minion.License{
					Terms: &minion.LicenseTerms{
						Customer:   "customer",
						Type:       minion.LicenseTerms_CUSTOMER,
						Seats:      5,
						ExpiryTime: pbExpiryTime,
					},
				},
			},
		},
		{
			name: "Trial license",
			license: config.License{
				Terms: config.Terms{
					Customer:   "customer",
					Type:       config.Trial,
					Seats:      1,
					ExpiryTime: expiryTime,
				},
			},
			expResp: &minion.GetLicenseResponse{
				License: &minion.License{
					Terms: &minion.LicenseTerms{
						Customer:   "customer",
						Type:       minion.LicenseTerms_TRIAL,
						Seats:      1,
						ExpiryTime: pbExpiryTime,
					},
				},
			},
		},
		{
			name: "Unknown license type",
			license: config.License{
				Terms: config.Terms{
					Type: config.LicenseType(-1),
				},
			},
			expResp: &minion.GetLicenseResponse{
				Error: errors.Marshal(errors.New("unknown license type")),
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := server{license: test.license}
			resp, err := s.GetLicense(nil, nil)
			assert.NoError(t, err)
			assert.Equal(t, test.expResp, resp)
		})
	}
}
