package tunnel

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	clientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	fakeKelda "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned/fake"
	informerfactory "github.com/kelda-inc/kelda/pkg/crd/client/informers/externalversions"
	"github.com/kelda-inc/kelda/pkg/kube"
)

func TestEnsureTunnelDoesNotExist(t *testing.T) {
	mockWebTunnel := &mockTunnelManager{}
	c := controller{
		tunnels: map[tunnelKey]tunnel{
			{"kevin", "web-server"}: {tunnelManager: mockWebTunnel},
		},
	}

	// Nothing should happen if the tunnel doesn't exist.
	c.ensureTunnelDoesNotExist("dne", "dne")
	assert.Equal(t, map[tunnelKey]tunnel{
		{"kevin", "web-server"}: {tunnelManager: mockWebTunnel},
	}, c.tunnels)
	mockWebTunnel.AssertExpectations(t)

	// If the tunnel exists, then it should be stopped, and deleted from the map.
	mockWebTunnel.On("Stop").Return()
	c.ensureTunnelDoesNotExist("kevin", "web-server")
	assert.Empty(t, c.tunnels)
	mockWebTunnel.AssertExpectations(t)
}

func TestSyncTunnel(t *testing.T) {
	testTunnel := &kelda.Tunnel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-server-8080-80",
			Namespace: "kevin",
		},
		Spec: kelda.TunnelSpec{
			Service:    "web-server",
			LocalPort:  8080,
			RemotePort: 80,
		},
	}

	mockKeldaClient := fakeKelda.NewSimpleClientset()
	informers := informerfactory.NewSharedInformerFactory(mockKeldaClient, 0)
	c := controller{
		tunnels:  map[tunnelKey]tunnel{},
		msLister: informers.Kelda().V1alpha1().Microservices().Lister(),
	}

	// Pod hasn't booted yet. No tunnel should be created.
	c.syncTunnel(testTunnel)
	assert.Empty(t, c.tunnels, "no tunnel should be created")

	// Pod has booted. We should create the tunnel for the first time.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-one-pod",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	ms := &kelda.Microservice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testTunnel.Spec.Service,
			Namespace: testTunnel.Namespace,
		},
		Status: kelda.MicroserviceStatus{
			Actual: []*kube.Object{
				{Object: pod},
			},
		},
	}
	err := informers.Kelda().V1alpha1().Microservices().Informer().GetIndexer().Add(ms)
	assert.NoError(t, err)

	webOneTunnel := &mockTunnelManager{}
	webOneTunnel.On("Start").Return().Once()
	newTunnelManager = func(_ kubernetes.Interface, _ clientset.Interface,
		_ *rest.Config, _ *kelda.Tunnel, podName string) tunnelManager {
		assert.Equal(t, pod.Name, podName)
		return webOneTunnel
	}
	c.syncTunnel(testTunnel)
	// Give time for the webOneTunnel.Start goroutine to start.
	time.Sleep(500 * time.Millisecond)
	webOneTunnel.AssertExpectations(t)

	// Pod is unchanged. We shouldn't change anything.
	c.syncTunnel(testTunnel)
	time.Sleep(500 * time.Millisecond)
	webOneTunnel.AssertExpectations(t)

	// Pod has been rescheduled. We should create a tunnel to the new pod, and
	// destroy the tunnel for the old pod.
	// Because pod is a pointer, we can just update it directly, and the
	// information in the store changes.
	pod.Name = "web-two-pod"
	webTwoTunnel := &mockTunnelManager{}
	webTwoTunnel.On("Start").Return().Once()
	webOneTunnel.On("Stop").Return().Once()
	newTunnelManager = func(_ kubernetes.Interface, _ clientset.Interface,
		_ *rest.Config, _ *kelda.Tunnel, podName string) tunnelManager {
		assert.Equal(t, pod.Name, podName)
		return webTwoTunnel
	}
	c.syncTunnel(testTunnel)
	time.Sleep(500 * time.Millisecond)
	webOneTunnel.AssertExpectations(t)
	webTwoTunnel.AssertExpectations(t)
}

func TestTunnelManager(t *testing.T) {
	t.Parallel()

	testTunnel := kelda.Tunnel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-server-8080-80",
			Namespace: "kevin",
		},
		Spec: kelda.TunnelSpec{
			Service:    "web-server",
			LocalPort:  8080,
			RemotePort: 80,
		},
	}

	type runTunnelMock struct {
		err             error
		calls           int
		stopped         bool
		sendReadySignal chan struct{}
		triggerExit     chan struct{}
		assertStatus    func(exp kelda.TunnelStatus, msg string)
	}
	setupTest := func() (tunnelManagerImpl, *runTunnelMock) {
		// Make a copy of the tunnel so that assignments to it `updateStatus`
		// don't affect the global copy.
		tunnelCopy := testTunnel
		tm := tunnelManagerImpl{
			keldaClient: fakeKelda.NewSimpleClientset(&tunnelCopy),
			stop:        make(chan struct{}),
			stopped:     make(chan struct{}),
			spec:        &tunnelCopy,
		}

		assertStatus := func(exp kelda.TunnelStatus, msg string) {
			// Check the phase with an expotential backoff since we don't know
			// when the tunnel status will update.
			sleepTime := 100 * time.Millisecond
			var actual kelda.TunnelStatus
			for i := 0; i < 5; i++ {
				tunnel, err := tm.keldaClient.KeldaV1alpha1().
					Tunnels(tunnelCopy.Namespace).
					Get(tunnelCopy.Name, metav1.GetOptions{})
				assert.NoError(t, err, msg)

				actual = tunnel.Status
				if actual == exp {
					break
				}

				time.Sleep(sleepTime)
				sleepTime *= 2
			}
			assert.Equal(t, exp, actual, msg)
		}

		mock := runTunnelMock{
			sendReadySignal: make(chan struct{}),
			triggerExit:     make(chan struct{}),
			assertStatus:    assertStatus,
		}

		tm.runTunnelOnce = func(stop, ready chan struct{}) error {
			mock.calls++

			select {
			case <-stop:
				mock.stopped = true
				return mock.err
			case <-mock.sendReadySignal:
				close(ready)
			case <-mock.triggerExit:
				return mock.err
			}

			select {
			case <-stop:
				mock.stopped = true
				return mock.err
			case <-mock.triggerExit:
				return mock.err
			}
		}
		return tm, &mock
	}

	upStatus := kelda.TunnelStatus{Phase: kelda.TunnelUp}
	crashedStatus := kelda.TunnelStatus{Phase: kelda.TunnelCrashed, Message: "error"}
	exitedStatus := kelda.TunnelStatus{Phase: kelda.TunnelCrashed}

	// Test happy path.
	tm, mock := setupTest()
	go tm.Start()
	mock.sendReadySignal <- struct{}{}
	mock.assertStatus(upStatus, "tunnel status should be up once ready")
	tm.Stop()
	mock.assertStatus(exitedStatus, "tunnel status should be down once stopped")
	assert.True(t, mock.stopped, "tunnel should be explicitly stopped")
	assert.Equal(t, 1, mock.calls)

	// Test tunnel recreation.
	tm, mock = setupTest()
	go tm.Start()

	// Simulate a crash.
	mock.err = errors.New(crashedStatus.Message)
	mock.triggerExit <- struct{}{}
	mock.assertStatus(crashedStatus, "tunnel status should be down when crashed")

	// Simulate a recovery
	mock.err = nil
	mock.sendReadySignal <- struct{}{}
	mock.assertStatus(upStatus, "tunnel status should be up once ready")

	// Simulate another crash.
	mock.err = errors.New(crashedStatus.Message)
	mock.triggerExit <- struct{}{}
	mock.assertStatus(crashedStatus, "tunnel status should be down when crashed")

	// Simulate another recovery.
	mock.err = nil
	mock.sendReadySignal <- struct{}{}
	mock.assertStatus(upStatus, "tunnel status should be up once ready")

	tm.Stop()
	mock.assertStatus(exitedStatus, "tunnel status should be down once stopped")
	assert.Equal(t, 3, mock.calls, "tunnel should be recreated")
}
