package client

//go:generate mockery -dir ../../proto/minion -name KeldaClient

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fakeKube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
	pbMocks "github.com/kelda-inc/kelda/pkg/minion/client/mocks"
	"github.com/kelda-inc/kelda/pkg/proto/minion"
)

func TestNewMinionClient(t *testing.T) {
	tunnel := &kube.Tunnel{
		LocalPort:  12345,
		RemotePort: 294,
	}

	client := grpcClient{}

	getTunnel = func(_ kubernetes.Interface,
		_ *rest.Config, _ string) (*kube.Tunnel, error) {
		return tunnel, nil
	}
	getGrpcClient = func(_ *kube.Tunnel) (grpcClient, error) {
		return client, nil
	}

	var newMinionTests = []struct {
		name      string
		clientSet kubernetes.Interface
		expClient Client
		expError  error
	}{
		{
			name:      "TestMinionDoesntExist",
			clientSet: fakeKube.NewSimpleClientset(),
			expClient: nil,
			expError:  ErrMinionPodNotFound,
		},
		{
			name: "TestMultipleMinionsExist",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{RestartCount: 0},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod-2",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{}}},
						},
					},
				},
			),
			expClient: nil,
			expError:  ErrMultipleMinionPods,
		},
		{
			name: "TestMinionCreating",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ContainerCreating",
								}}},
						},
					},
				},
			),
			expClient: nil,
			expError:  ErrMinionContainerCreating,
		},
		{
			name: "TestMinionTerminated",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{}}},
						},
					},
				},
			),
			expClient: nil,
			expError:  ErrMinionContainerTerminated,
		},
		{
			name: "TestMinionCrashLoopBackOff",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								}}},
						},
					},
				},
			),
			expClient: nil,
			expError:  ErrMinionContainerCrashLoop,
		},
		{
			name: "TestMinionWaitingUnknown",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "Unknown",
								}}},
						},
					},
				},
			),
			expClient: nil,
			expError: errors.NewFriendlyError(
				fmt.Sprintf(MinionContainerWaitingErrTemplate, "Unknown")),
		},
		{
			name: "TestMinionNoContainerStatus",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{},
					},
				},
			),
			expClient: nil,
			expError:  errors.New("no container status found"),
		},
		{
			name: "TestMinionSuccessful",
			clientSet: fakeKube.NewSimpleClientset(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "minion-pod",
						Namespace: "kelda",
						Labels:    map[string]string{"service": "kelda"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{}}},
						},
					},
				},
			),
			expClient: tunneledClient{
				minionTunnel: tunnel,
				grpcClient:   client,
			},
			expError: nil,
		},
	}

	for _, test := range newMinionTests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			mc, err := New(test.clientSet, nil)
			assert.Equal(t, test.expClient, mc)
			assert.Equal(t, test.expError, err)
		})
	}
}

func TestGetLicense(t *testing.T) {
	expiryTime := time.Now().UTC()
	pbExpiryTime, err := ptypes.TimestampProto(expiryTime)
	assert.NoError(t, err)

	tests := []struct {
		name       string
		mockResp   *minion.GetLicenseResponse
		expLicense config.License
		expError   error
	}{
		{
			name: "Customer license",
			mockResp: &minion.GetLicenseResponse{
				License: &minion.License{
					Terms: &minion.LicenseTerms{
						Customer:   "customer",
						Type:       minion.LicenseTerms_CUSTOMER,
						Seats:      5,
						ExpiryTime: pbExpiryTime,
					},
				},
			},
			expLicense: config.License{
				Terms: config.Terms{
					Customer:   "customer",
					Type:       config.Customer,
					Seats:      5,
					ExpiryTime: expiryTime,
				},
			},
		},
		{
			name: "Trial license",
			mockResp: &minion.GetLicenseResponse{
				License: &minion.License{
					Terms: &minion.LicenseTerms{
						Customer:   "customer",
						Type:       minion.LicenseTerms_TRIAL,
						Seats:      1,
						ExpiryTime: pbExpiryTime,
					},
				},
			},
			expLicense: config.License{
				Terms: config.Terms{
					Customer:   "customer",
					Type:       config.Trial,
					Seats:      1,
					ExpiryTime: expiryTime,
				},
			},
		},
		{
			name: "Unknown license type",
			mockResp: &minion.GetLicenseResponse{
				License: &minion.License{
					Terms: &minion.LicenseTerms{
						Customer:   "customer",
						Type:       minion.LicenseTerms_LicenseType(-1),
						Seats:      1,
						ExpiryTime: pbExpiryTime,
					},
				},
			},
			expError: errors.WithContext(errors.New("unknown license type"), "unmarshal license"),
		},
		{
			name: "Server-side error",
			mockResp: &minion.GetLicenseResponse{
				Error: errors.Marshal(errors.New("server-side error")),
			},
			expError: errors.New("server-side error"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			pbClient := &pbMocks.KeldaClient{}
			pbClient.On("GetLicense", mock.Anything, &minion.GetLicenseRequest{}).
				Return(test.mockResp, nil)
			license, err := grpcClient{pbClient: pbClient}.GetLicense()
			assert.Equal(t, test.expError, err)
			assert.Equal(t, test.expLicense, license)
		})
	}
}
