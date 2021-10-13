package client

import (
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/kube"
)

type tunnelKey struct {
	namespace string
	pod       string
}

// tunnels maps a tunnelKey to its sync tunnel.
var tunnels = make(map[tunnelKey]*kube.Tunnel)

// tunnelsMux is a mutex lock for tunnels access.
var tunnelsMux sync.Mutex

// kubeTunnelRun is a mock function to be overridden in tests.
var kubeTunnelRun = func(tunnel *kube.Tunnel, restClient rest.Interface,
	restConfig *rest.Config) error {
	return tunnel.Run(restClient, restConfig)
}

// getSyncTunnelAddress gets the local address of the sync tunnel for the
// specified dev pod. If the tunnel doesn't exist, it'll try to create one.
func getSyncTunnelAddress(kubeClient kubernetes.Interface,
	restConfig *rest.Config, namespace string, pod string) (string, error) {
	tunnelsMux.Lock()
	defer tunnelsMux.Unlock()

	key := tunnelKey{
		namespace: namespace,
		pod:       pod,
	}
	tunnel, ok := tunnels[key]
	if !ok {
		restClient := kubeClient.CoreV1().RESTClient()
		tunnel = &kube.Tunnel{
			Namespace:  namespace,
			Pod:        pod,
			RemotePort: 9001,
		}
		if err := kubeTunnelRun(tunnel, restClient, restConfig); err != nil {
			return "", errors.WithContext(err,
				fmt.Sprintf("setup sync tunnel for pod %q", pod))
		}
		tunnels[key] = tunnel
	}
	return fmt.Sprintf("localhost:%d", tunnel.LocalPort), nil
}

// CloseAllSyncTunnels closes all the sync tunnels that have been created.
func CloseAllSyncTunnels() {
	tunnelsMux.Lock()
	defer tunnelsMux.Unlock()

	for _, tunnel := range tunnels {
		tunnel.Close()
	}
	tunnels = make(map[tunnelKey]*kube.Tunnel)
}
