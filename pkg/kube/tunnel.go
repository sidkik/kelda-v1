package kube

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/sidkik/kelda-v1/pkg/errors"
)

// Tunnel is a instance of a port-forward between the local machine and a microservice.
type Tunnel struct {
	Namespace, Pod        string
	LocalPort, RemotePort int

	stop chan struct{}
	pf   *portforward.PortForwarder
}

// Run activates the port-forward tunnel itself.
func (t *Tunnel) Run(client rest.Interface, restConfig *rest.Config) error {
	if t.stop == nil {
		t.stop = make(chan struct{})
	}

	if t.LocalPort == 0 {
		randomPort, err := getRandomPort()
		if err != nil {
			return errors.WithContext(err, "get random port")
		}
		t.LocalPort = randomPort
	}

	// Create a SPDY dialer, which will serve as the underlying connection for
	// the port forwarded traffic.
	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return errors.WithContext(err, "create spdy round tripper")
	}

	// Create the URL for the Kubernetes portforwarding endpoint.
	u := client.Post().
		Resource("pods").
		Namespace(t.Namespace).
		Name(t.Pod).
		SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", u)

	ready := make(chan struct{})
	ports := []string{fmt.Sprintf("%d:%d", t.LocalPort, t.RemotePort)}
	t.pf, err = portforward.New(dialer, ports, t.stop, ready,
		ioutil.Discard, ioutil.Discard)
	if err != nil {
		return errors.WithContext(err, "create port forwarder")
	}

	errChan := make(chan error)
	go func() {
		errChan <- t.pf.ForwardPorts()
	}()

	// Block until the tunnel is ready for connections, or exited.
	select {
	case err = <-errChan:
		return errors.WithContext(err, "forwarding ports")
	case <-ready:
		return nil
	}
}

// Close shuts down the tunnel.
func (t *Tunnel) Close() {
	close(t.stop)
}

func getRandomPort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer l.Close()

	_, p, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		return 0, err
	}

	port, err := strconv.Atoi(p)
	if err != nil {
		return 0, err
	}
	return port, nil
}
