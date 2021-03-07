package microservice

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	minionServer "github.com/kelda-inc/kelda/pkg/minion/server"
	"github.com/kelda-inc/kelda/pkg/version"
)

func TestInject(t *testing.T) {
	trueVal := true
	tests := []struct {
		name     string
		ms       *kelda.Microservice
		manifest string
		exp      *unstructured.Unstructured
		err      error
	}{
		// Add owner information to ConfigMap.
		{
			name: "TestConfigMap",
			ms: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name: "msName",
					UID:  types.UID("msUID"),
				},
			},
			manifest: objectToString(&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Data: map[string]string{
					"foo": "bar",
				},
			}),
			exp: toUnstructured(&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"kelda.io.minion.microservice":            "msName",
						"kelda.io.minion.microserviceSpecVersion": "0",
						"kelda.io.minion.keldaVersion":            version.Version,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         kelda.SchemeGroupVersion.String(),
							Kind:               "Microservice",
							Name:               "msName",
							UID:                types.UID("msUID"),
							Controller:         &trueVal,
							BlockOwnerDeletion: &trueVal,
						},
					},
				},
				Data: map[string]string{
					"foo": "bar",
				},
			}),
		},

		// Add owner information to Deployment and the pods it spawns.
		{
			name: "TestDeployment",
			ms: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name: "msName",
					UID:  types.UID("msUID"),
				},
			},
			manifest: objectToString(&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			}),
			exp: toUnstructured(&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"kelda.io.minion.microservice":            "msName",
						"kelda.io.minion.microserviceSpecVersion": "0",
						"kelda.io.minion.keldaVersion":            version.Version,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         kelda.SchemeGroupVersion.String(),
							Kind:               "Microservice",
							Name:               "msName",
							UID:                types.UID("msUID"),
							Controller:         &trueVal,
							BlockOwnerDeletion: &trueVal,
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"kelda.io.minion.microservice":            "msName",
								"kelda.io.minion.microserviceSpecVersion": "0",
								"kelda.io.minion.keldaVersion":            version.Version,
							},
						},
					},
				},
			}),
		},

		// Modify the spec when it's running in DevMode.
		{
			name: "TestDev",
			ms: &kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "msName",
					UID:       types.UID("msUID"),
					Namespace: "ns",
					Annotations: map[string]string{
						"kelda.io.minion.microservicePriorityClass": "ns",
					},
				},
				Spec: kelda.MicroserviceSpec{
					DevMode:  true,
					DevImage: "devImage",
				},
			},
			manifest: objectToString(&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image:          "image",
									Command:        []string{"command"},
									LivenessProbe:  &corev1.Probe{},
									ReadinessProbe: &corev1.Probe{},
								},
							},
						},
					},
				},
			}),
			exp: toUnstructured(&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: appsv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "ns",
					Annotations: map[string]string{
						"kelda.io.minion.microservice":            "msName",
						"kelda.io.minion.microserviceSpecVersion": "0",
						"kelda.io.minion.keldaVersion":            version.Version,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         kelda.SchemeGroupVersion.String(),
							Kind:               "Microservice",
							Name:               "msName",
							UID:                types.UID("msUID"),
							Controller:         &trueVal,
							BlockOwnerDeletion: &trueVal,
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"kelda.io.minion.microservice":            "msName",
								"kelda.io.minion.microserviceSpecVersion": "0",
								"kelda.io.minion.keldaVersion":            version.Version,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Image: "devImage",
									Command: []string{"sh", "-c",
										"cp /bin-volume/kelda /tmp/kelda && " +
											"/tmp/kelda dev-server msName 0"},
									Env: []corev1.EnvVar{
										{
											Name: "POD_NAME",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "metadata.name",
												},
											},
										},
										{
											Name: "POD_NAMESPACE",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "metadata.namespace",
												},
											},
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "bin-volume",
											MountPath: "/bin-volume",
										},
									},
									LivenessProbe:  nil,
									ReadinessProbe: nil,
								},
							},
							InitContainers: []corev1.Container{
								{
									Name:            "kelda-dev-server",
									Image:           version.KeldaImage,
									ImagePullPolicy: corev1.PullAlways,
									Command:         []string{"cp", "/bin/kelda", "/bin-volume/kelda"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "bin-volume",
											MountPath: "/bin-volume",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "bin-volume",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
							ServiceAccountName: minionServer.DevServiceAccountName,
							PriorityClassName:  "ns",
						},
					},
				},
			}),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			res, err := inject(test.ms, test.manifest)
			assert.Equal(t, test.err, err)
			assert.Equal(t, test.exp, res)
		})
	}
}

func TestIsFatalPodState(t *testing.T) {
	tests := []struct {
		name         string
		pod          corev1.Pod
		nodes        []corev1.Node
		expResult    string
		expCondition bool
	}{
		{
			name: "Normal Condition",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodInitialized,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			nodes:        []corev1.Node{},
			expResult:    "",
			expCondition: false,
		},
		{
			name: "Unschedulable due to untolerated taints",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Key:    "should-be-tolerated",
							Value:  "",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionFalse,
							Reason: corev1.PodReasonUnschedulable,
							Message: "0/1 nodes are available: 1 node(s) had " +
								"taints that the pod didn't tolerate.",
						},
					},
				},
			},
			nodes: []corev1.Node{
				{
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{
								Key:    "node.kubernetes.io/memory-pressure",
								Value:  "",
								Effect: corev1.TaintEffectNoSchedule,
							},
							{
								Key:    "should-be-tolerated",
								Value:  "",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			expResult:    "Cannot schedule Pod due to the following Node taint(s): node.kubernetes.io/memory-pressure.",
			expCondition: true,
		},
		{
			name: "Unschedulable due to some other reason",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.PodScheduled,
							Status:  corev1.ConditionFalse,
							Reason:  corev1.PodReasonUnschedulable,
							Message: "some other reason",
						},
					},
				},
			},
			nodes:        []corev1.Node{},
			expResult:    "some other reason",
			expCondition: true,
		},
		{
			name: "A container is in ImagePullBackOff",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "inelasticsearch",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason:  "ImagePullBackOff",
									Message: "Can't find image",
								},
							},
						},
					},
				},
			},
			nodes:        []corev1.Node{},
			expResult:    "Can't find image",
			expCondition: true,
		},
		{
			name: "A container is in ImagePullBackOff",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "inelasticsearch",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason:  "ImagePullBackOff",
									Message: "Can't find image",
								},
							},
						},
					},
				},
			},
			nodes:        []corev1.Node{},
			expResult:    "Can't find image",
			expCondition: true,
		},
		{
			name: "A container is in CrashLoopBackOff",
			pod: corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "inelasticsearch",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason:  "CrashLoopBackOff",
									Message: "Waiting for 5m",
								},
							},
							LastTerminationState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									Reason:  "Error",
									Message: "Error",
								},
							},
						},
					},
				},
			},
			nodes:        []corev1.Node{},
			expResult:    "CrashLoopBackOff. Termination Reason: Error",
			expCondition: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			res, isFatal := isFatalPodState(&test.pod, test.nodes)
			assert.Equal(t, test.expResult, res)
			assert.Equal(t, test.expCondition, isFatal)
		})
	}
}

func objectToString(obj runtime.Object) string {
	out := bytes.NewBuffer(nil)
	err := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme).Encode(obj, out)
	if err != nil {
		panic(fmt.Errorf("bad test: failed to serialize object: %s", err))
	}
	return out.String()
}

func TestInjectImageDigests(t *testing.T) {
	ms := &kelda.Microservice{
		Spec: kelda.MicroserviceSpec{
			ImageDigests: []kelda.ImageDigest{
				{
					ControllerName: "controller1",
					ContainerName:  "container1",
					Digest:         "digest1",
					ImageURL:       "nginx:alpine",
				},
				{
					ControllerName: "controller1",
					ContainerName:  "container2",
					Digest:         "digest2",
					ImageURL:       "apache",
				},
				{
					ControllerName: "controller2",
					ContainerName:  "container1",
					Digest:         "digest3",
					ImageURL:       "ignored",
				},
				{
					ControllerName: "controller2",
					ContainerName:  "container2",
					Digest:         "digest4",
					ImageURL:       "ignored",
				},
			},
		},
	}
	controllerName := "controller1"

	tests := []struct {
		name         string
		inputPodSpec *corev1.PodSpec
		expPodSpec   *corev1.PodSpec
	}{
		{
			name: "inject none",
			inputPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "wrong-container",
						Image: "nginx:alpine",
					},
				},
			},
			expPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "wrong-container",
						Image: "nginx:alpine",
					},
				},
			},
		},
		{
			name: "inject one",
			inputPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container1",
						Image: "nginx:alpine",
					},
					{
						Name:  "wrong-container",
						Image: "apache",
					},
				},
			},
			expPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container1",
						Image: "nginx@digest1",
					},
					{
						Name:  "wrong-container",
						Image: "apache",
					},
				},
			},
		},
		{
			name: "inject multiple",
			inputPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container1",
						Image: "nginx:alpine",
					},
					{
						Name:  "container2",
						Image: "apache",
					},
					{
						Name:  "wrong-container",
						Image: "caddy",
					},
				},
			},
			expPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container1",
						Image: "nginx@digest1",
					},
					{
						Name:  "container2",
						Image: "apache@digest2",
					},
					{
						Name:  "wrong-container",
						Image: "caddy",
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			injectImageDigests(ms, controllerName, test.inputPodSpec)
			assert.Equal(t, *test.inputPodSpec, *test.expPodSpec)
		})
	}
}

func TestGroupManifests(t *testing.T) {
	// Manifests for tests
	deploymentManifest := `apiVersion: extensions/v1beta1
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
`
	daemonSetManifest := `apiVersion: extensions/v1beta1
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
`
	goodPVCManifest := `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: myclaim
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 8Gi
  storageClassName: slow
  selector:
    matchLabels:
      release: "stable"
    matchExpressions:
      - {key: environment, operator: In, values: [dev]}
`

	malformedPVCManifest := `apiVersion: v1
metadata:
  name: myclaim
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 8Gi
  storageClassName: slow
  selector:
    matchLabels:
      release: "stable"
    matchExpressions:
      - {key: environment, operator: In, values: [dev]}
`
	// Manifest error message
	expectedManifestErrorMsg := "Object 'Kind' is missing in 'apiVersion: v1\nmetadata:\n  name: myclaim\nspec:\n  " +
		"accessModes:\n    - ReadWriteOnce\n  volumeMode: Filesystem\n  resources:\n    requests:\n      storage: 8Gi\n  " +
		"storageClassName: slow\n  selector:\n    matchLabels:\n      release: \"stable\"\n    matchExpressions:\n      " +
		"- {key: environment, operator: In, values: [dev]}\n'"

	tests := []struct {
		name      string
		manifests []string
		expected  [][]string
		err       error
	}{
		{
			name: "Testing for only the first manifest group",
			manifests: []string{
				deploymentManifest,
				daemonSetManifest,
			},
			expected: [][]string{
				{
					deploymentManifest,
					daemonSetManifest,
				},
				{},
			},
			err: nil,
		},

		{
			name: "Test for both manifest groups",
			manifests: []string{
				deploymentManifest,
				daemonSetManifest,
				goodPVCManifest,
			},
			expected: [][]string{
				{
					deploymentManifest,
					daemonSetManifest,
				},
				{
					goodPVCManifest,
				},
			},
			err: nil,
		},
		{
			name:      "Test for only second group manifests",
			manifests: []string{goodPVCManifest},
			expected: [][]string{
				{},
				{goodPVCManifest},
			},
			err: nil,
		},
		{
			name:      "Test for malformed manifests",
			manifests: []string{malformedPVCManifest},
			expected:  nil,
			err:       errors.New(expectedManifestErrorMsg),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			res, err := sortManifests(test.manifests)

			if err != nil {
				assert.Equal(t, test.err.Error(), err.Error())
			} else {
				assert.Equal(t, test.err, err)
			}

			assert.Equal(t, test.expected, res)
		})
	}
}

func toUnstructured(obj runtime.Object) *unstructured.Unstructured {
	res, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: res}
}
