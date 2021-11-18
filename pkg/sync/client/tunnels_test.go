package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/sidkik/kelda-v1/pkg/kube"
)

func TestGetSyncTunnelAddress(t *testing.T) {
	counter := 0
	kubeTunnelRun = func(tunnel *kube.Tunnel, _ rest.Interface,
		_ *rest.Config) error {
		tunnel.LocalPort = 10000 + counter
		counter++
		return nil
	}

	kubeClient := &kubernetes.Clientset{}
	restConfig := &rest.Config{}
	namespace1 := "ns1"
	namespace2 := "ns2"
	pod1 := "pod1"
	pod2 := "pod2"

	addr1, err := getSyncTunnelAddress(kubeClient, restConfig, namespace1, pod1)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:10000", addr1)

	addr2, err := getSyncTunnelAddress(kubeClient, restConfig, namespace1, pod2)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:10001", addr2)

	addr3, err := getSyncTunnelAddress(kubeClient, restConfig, namespace2, pod1)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:10002", addr3)

	addr4, err := getSyncTunnelAddress(kubeClient, restConfig, namespace2, pod2)
	assert.NoError(t, err)
	assert.Equal(t, "localhost:10003", addr4)

	addr5, err := getSyncTunnelAddress(kubeClient, restConfig, namespace1, pod1)
	assert.NoError(t, err)
	assert.Equal(t, addr1, addr5)

	addr6, err := getSyncTunnelAddress(kubeClient, restConfig, namespace1, pod2)
	assert.NoError(t, err)
	assert.Equal(t, addr2, addr6)

	addr7, err := getSyncTunnelAddress(kubeClient, restConfig, namespace2, pod1)
	assert.NoError(t, err)
	assert.Equal(t, addr3, addr7)

	addr8, err := getSyncTunnelAddress(kubeClient, restConfig, namespace2, pod2)
	assert.NoError(t, err)
	assert.Equal(t, addr4, addr8)
}
