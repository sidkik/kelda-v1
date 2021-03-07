package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	fakeExtension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	fakeKube "k8s.io/client-go/kubernetes/fake"

	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
)

func TestCreateObjects(t *testing.T) {
	// Test the initial deployment doesn't error.
	fakeExtensionClient := fakeExtension.NewSimpleClientset()
	fakeKubeClient := fakeKube.NewSimpleClientset()
	assert.NoError(t, createObjects(fakeKubeClient, fakeExtensionClient, ""))

	// Test updating doesn't error.
	assert.NoError(t, createObjects(fakeKubeClient, fakeExtensionClient, ""))
}

func TestReadLicense(t *testing.T) {
	getRandomName = func(_ int) string { return "name" }
	type file struct {
		path, contents string
	}

	tests := []struct {
		name        string
		licensePath string
		kubeClient  kubernetes.Interface
		mockFiles   []file
		expLicense  string
		expError    string
	}{
		{
			name: "FirstInstallSuccess",
			mockFiles: []file{
				{"license", "license-contents"},
			},
			licensePath: "license",
			expLicense:  "license-contents",
		},
		{
			name:       "FirstInstallNoLicense",
			kubeClient: fakeKube.NewSimpleClientset(),
			expLicense: "eyJMaWNlbnNlIjoiZXlKVVpYSnRjeUk2ZXlKRGRYTjBiMjFsY2lJNklt" +
				"NWhiV1VpTENKVWVYQmxJam94TENKVFpXRjBjeUk2TUN3aVJYaHdhWEo1VkdsdFpT" +
				"STZJakF3TURFdE1ERXRNREZVTURBNk1EQTZNREJhSW4xOSIsIlNpZ25hdHVyZSI6" +
				"bnVsbCwiVmVyc2lvbiI6InYxYWxwaGEyIn0=",
		},
		{
			name: "UpgradeChangeLicense",
			mockFiles: []file{
				{"license", "newLicense"},
			},
			licensePath: "license",
			kubeClient: fakeKube.NewSimpleClientset(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      licenseConfigMapName,
						Namespace: minionClient.KeldaNamespace,
					},
					Data: map[string]string{
						licenseKeyName: "oldLicense",
					},
				},
			),
			expLicense: "newLicense",
		},
		{
			name: "UpgradeNoLicense",
			kubeClient: fakeKube.NewSimpleClientset(
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      licenseConfigMapName,
						Namespace: minionClient.KeldaNamespace,
					},
					Data: map[string]string{
						licenseKeyName: "oldLicense",
					},
				},
			),
			expLicense:  "oldLicense",
			licensePath: "",
		},
		{
			name:        "BadLicensePath",
			licensePath: "dne",
			kubeClient:  fakeKube.NewSimpleClientset(),
			expError:    "License file does not exist. Aborting.",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fs = afero.NewMemMapFs()
			for _, f := range test.mockFiles {
				err := afero.WriteFile(fs, f.path, []byte(f.contents), 0644)
				assert.NoError(t, err)
			}

			res, err := getLicense(test.kubeClient, test.licensePath)
			assert.Equal(t, test.expLicense, res)

			if test.expError != "" {
				assert.EqualError(t, err, test.expError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsMinionReady(t *testing.T) {
	tests := []struct {
		name     string
		objects  []runtime.Object
		expReady bool
		expError error
	}{
		{
			name: "Ready",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   1,
						UpdatedReplicas: 1,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			expReady: true,
			expError: nil,
		},
		{
			name: "OldReadyNewBooting",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        2,
						ReadyReplicas:   1,
						UpdatedReplicas: 1,
					},
				},
			},
			expReady: false,
			expError: nil,
		},
		{
			name: "NotCreated",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   1,
						UpdatedReplicas: 0,
					},
				},
			},
			expReady: false,
			expError: nil,
		},
		{
			name: "Failed",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   0,
						UpdatedReplicas: 1,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: corev1.PodStatus{
						Phase:   corev1.PodFailed,
						Message: "message",
					},
				},
			},
			expReady: false,
			expError: errors.New("pod failed: message"),
		},
		{
			name: "Unknown",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   0,
						UpdatedReplicas: 1,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: corev1.PodStatus{
						Phase:   corev1.PodUnknown,
						Message: "message",
					},
				},
			},
			expReady: false,
			expError: errors.New("pod unknown: message"),
		},
		{
			name: "Unschedulable",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   0,
						UpdatedReplicas: 1,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Message: "no nodes available to schedule pods",
							},
						},
					},
				},
			},
			expReady: false,
			expError: errors.New("The Kelda minion server is unschedulable " +
				"because no nodes are available in the cluster. Please check " +
				"`kubectl -n kelda describe pod kelda` for more information."),
		},
		{
			name: "BadImage",
			objects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: appsv1.DeploymentStatus{
						Replicas:        1,
						ReadyReplicas:   0,
						UpdatedReplicas: 1,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: minionClient.KeldaNamespace,
						Name:      "kelda",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason: "ImagePullBackOff",
									},
								},
							},
						},
					},
				},
			},
			expReady: false,
			expError: errors.New("The cluster failed to pull the Kelda minion image. " +
				"Please check `kubectl -n kelda describe pod kelda` for more information."),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fakeClient := fakeKube.NewSimpleClientset(test.objects...)
			ready, err := isMinionReady(fakeClient)
			assert.Equal(t, test.expReady, ready)

			if test.expError == nil {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, test.expError.Error())
			}
		})
	}
}

func TestHashString(t *testing.T) {
	fooHashOne, err := hashString("foo")
	assert.NoError(t, err)

	fooHashTwo, err := hashString("foo")
	assert.NoError(t, err)

	barHash, err := hashString("bar")
	assert.NoError(t, err)

	assert.Equal(t, fooHashOne, fooHashTwo)
	assert.NotEqual(t, fooHashOne, barHash)
}
