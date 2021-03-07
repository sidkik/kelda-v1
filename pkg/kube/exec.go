package kube

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/kelda-inc/kelda/pkg/errors"
)

// Exec runs a command in a Kubernetes pod.
func Exec(kubeClient kubernetes.Interface, restConfig *rest.Config, namespace, pod string,
	execOpts corev1.PodExecOptions, streamOpts remotecommand.StreamOptions) error {
	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		SubResource("exec").
		Name(pod).
		Namespace(namespace).
		VersionedParams(&execOpts, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return errors.WithContext(err, "setup remote shell")
	}

	err = exec.Stream(streamOpts)
	if err != nil {
		return errors.WithContext(err, "stream")
	}
	return nil
}
