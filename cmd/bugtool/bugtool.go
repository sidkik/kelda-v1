package bugtool

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	kelda "github.com/kelda-inc/kelda/pkg/crd/apis/kelda/v1alpha1"
	"github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned/scheme"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/version"
)

var fs = afero.NewOsFs()

// New creates a new `config` command.
func New() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "bug-tool",
		Short: "Generate an archive for Kelda debugging",
		Run:   func(_ *cobra.Command, _ []string) { main(out) },
	}
	cmd.Flags().StringVar(&out, "out", "", "path for archive")
	return cmd
}

func main(out string) {
	tmpdir, err := afero.TempDir(fs, "", "kelda-bug-tool")
	if err != nil {
		err = errors.NewFriendlyError("Failed to create out directory:\n%s", err)
		util.HandleFatalError(err)
	}

	// Wrap defer in a function to handle errors from fs.RemoveAll().
	defer func() {
		err := fs.RemoveAll(tmpdir)
		if err != nil {
			util.HandleFatalError(err)
		}
	}()

	setupInfo(tmpdir)

	if out == "" {
		out = fmt.Sprintf("kelda-bug-info-%s.tar.gz",
			time.Now().Format("Jan_02_2006-15-04-05"))
	}
	if err := tarDirectory(tmpdir, out); err != nil {
		err = errors.NewFriendlyError("Failed to tar:\n%s", err)
		util.HandleFatalError(err)
	}

	msg := `Created bug information archive at '%s'.
Please send it to the Kelda team at 'kevin@kelda.io'.
You may want to edit the archive if your deployment contains sensitive information.
The archive contains:
 * The Kelda CLI logs.
 * The Kelda Minion logs.
 * The logs of all pods in the development namespace.
 * The pod state of all pods in the Kelda and development namespaces.
 * The internal datastructures used by Kelda for tracking Microservice and Tunnel state.
 * The analytics file that tracks usage.
 * The version of the Kelda CLI and Minion.
 * The events logged by Kubernetes.
 * The node state of the Kubernetes cluster.
`
	fmt.Printf(msg, out)
}

func setupInfo(root string) {
	userConfig, err := config.ParseUser()
	if err != nil {
		log.WithError(err).Error("Failed to parse user config")
		return
	}

	if err := setupCLILogs(root, userConfig); err != nil {
		log.WithError(err).Warn("Failed to setup CLI logs")
	}

	kubeClient, restConfig, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		log.WithError(err).Error("Failed to connect to Kubernetes cluster")
		return
	}

	for _, namespace := range []string{minionClient.KeldaNamespace, userConfig.Namespace} {
		err = setupPodLogs(filepath.Join(root, "pod-logs-"+namespace), namespace, kubeClient)
		if err != nil {
			log.WithError(err).WithField("namespace", namespace).Warn("Failed to setup pod logs")
		}

		err = setupPodStatus(filepath.Join(root, "pod-status-"+namespace), namespace, kubeClient)
		if err != nil {
			log.WithError(err).WithField("namespace", namespace).Warn("Failed to setup pod status")
		}
	}

	// Get the events in "default" and "kube-system" as well in case there's
	// relevant debugging information about Node health or core component
	// status.
	eventsNamespaces := []string{minionClient.KeldaNamespace, userConfig.Namespace, "default", "kube-system"}
	for _, namespace := range eventsNamespaces {
		err = setupKubeEvents(filepath.Join(root, "kube-events-"+namespace), namespace, kubeClient)
		if err != nil {
			log.WithError(err).WithField("namespace", namespace).Warn("Failed to setup kube events")
		}
	}

	err = setupNodeStatus(filepath.Join(root, "nodes"), kubeClient)
	if err != nil {
		log.WithError(err).Warn("Failed to setup node status")
	}

	if err := setupCRDs(root, userConfig.Namespace, *restConfig); err != nil {
		log.WithError(err).Warn("Failed to setup CRDs")
	}

	if err := setupVersion(root, kubeClient, restConfig); err != nil {
		log.WithError(err).Warn("Failed to setup version info")
	}

}

func setupCLILogs(root string, userConfig config.User) error {
	if userConfig.Workspace == "" {
		return errors.New("no workspace defined in user config")
	}
	logPath := filepath.Join(filepath.Dir(userConfig.Workspace), "kelda.log")

	logFile, err := fs.Open(logPath)
	if err != nil {
		return errors.WithContext(err, "open log")
	}
	defer logFile.Close()

	outFile, err := fs.Create(filepath.Join(root, "cli.log"))
	if err != nil {
		return errors.WithContext(err, "open destination")
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, logFile); err != nil {
		return errors.WithContext(err, "copy")
	}
	return nil
}

func setupPodLogs(outdir, namespace string, kubeClient kubernetes.Interface) error {
	if err := fs.Mkdir(outdir, 0755); err != nil {
		return errors.WithContext(err, "mkdir")
	}

	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "list pods")
	}

	writeLogs := func(pod, container string, prev bool) error {
		opts := corev1.PodLogOptions{Container: container, Previous: prev}
		stream, err := getLogs(kubeClient, namespace, pod, opts)
		if err != nil {
			return errors.WithContext(err, "initiate logs stream")
		}
		defer stream.Close()

		path := filepath.Join(outdir, fmt.Sprintf("%s-%s", pod, container))
		if prev {
			path += "-crashed"
		}
		out, err := fs.Create(path)
		if err != nil {
			return errors.WithContext(err, "open destination")
		}
		defer out.Close()

		if _, err := io.Copy(out, stream); err != nil {
			return errors.WithContext(err, "write logs stream")
		}
		return nil
	}

	for _, pod := range pods.Items {
		containerToStatus := map[string]corev1.ContainerStatus{}
		for _, status := range pod.Status.ContainerStatuses {
			containerToStatus[status.Name] = status
		}

		containers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
		for _, container := range containers {
			if err := writeLogs(pod.Name, container.Name, false); err != nil {
				return errors.WithContext(err, fmt.Sprintf("write %s logs", pod.Name))
			}

			if status, ok := containerToStatus[container.Name]; ok && status.RestartCount != 0 {
				if err := writeLogs(pod.Name, container.Name, true); err != nil {
					return errors.WithContext(err, fmt.Sprintf("write %s logs", pod.Name))
				}
			}
		}
	}
	return nil
}

var getLogs = getLogsImpl

func getLogsImpl(kubeClient kubernetes.Interface, namespace, pod string,
	opts corev1.PodLogOptions) (io.ReadCloser, error) {

	return kubeClient.CoreV1().
		Pods(namespace).
		GetLogs(pod, &opts).
		Stream()
}

func setupPodStatus(outdir, namespace string, kubeClient kubernetes.Interface) error {
	if err := fs.Mkdir(outdir, 0755); err != nil {
		return errors.WithContext(err, "mkdir")
	}

	pods, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "list pods")
	}

	for _, pod := range pods.Items {
		podBytes, err := yaml.Marshal(pod)
		if err != nil {
			log.WithError(err).WithField("pod", pod.Name).Warn("Failed to marshal pod")
			podBytes = []byte(fmt.Sprintf("%+v\n", pod))
		}

		path := filepath.Join(outdir, pod.Name)
		if err := afero.WriteFile(fs, path, podBytes, 0644); err != nil {
			return errors.WithContext(err, "write")
		}
	}
	return nil
}

func setupNodeStatus(outdir string, kubeClient kubernetes.Interface) error {
	if err := fs.Mkdir(outdir, 0755); err != nil {
		return errors.WithContext(err, "mkdir")
	}

	nodes, err := kubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "list nodes")
	}

	for _, node := range nodes.Items {
		nodeBytes, err := yaml.Marshal(node)
		if err != nil {
			log.WithError(err).WithField("node", node.Name).Warn("Failed to marshal node")
			nodeBytes = []byte(fmt.Sprintf("%+v\n", node))
		}

		path := filepath.Join(outdir, node.Name)
		if err := afero.WriteFile(fs, path, nodeBytes, 0644); err != nil {
			return errors.WithContext(err, "write")
		}
	}
	return nil
}

func setupCRDs(root string, namespace string, restConfig rest.Config) error {
	for _, dir := range []string{"crds", "crds/tunnels", "crds/microservices"} {
		if err := fs.Mkdir(filepath.Join(root, dir), 0755); err != nil {
			return errors.WithContext(err, "mkdir")
		}
	}

	// Use the rest client so that the output isn't affect by serialization
	// into the concrete Go struct.
	restConfig.GroupVersion = &kelda.SchemeGroupVersion
	restConfig.APIPath = "/apis"
	restConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	restClient, err := rest.RESTClientFor(&restConfig)
	if err != nil {
		return errors.WithContext(err, "create Kubernetes REST client")
	}

	writeCRD := func(path string, crd []byte) error {
		if prettyCRD, err := yaml.JSONToYAML(crd); err == nil {
			crd = prettyCRD
		} else {
			log.WithError(err).Warn("Failed to convert CRD JSON to YAML")
		}

		return afero.WriteFile(fs, path, crd, 0644)
	}

	var msList kelda.MicroserviceList
	err = restClient.Get().
		Namespace(namespace).
		Resource("microservices").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do().
		Into(&msList)
	if err != nil {
		return errors.WithContext(err, "list microservices")
	}

	for _, ms := range msList.Items {
		rawMs, err := restClient.Get().
			Namespace(namespace).
			Resource("microservices").
			Name(ms.Name).
			VersionedParams(&metav1.GetOptions{}, scheme.ParameterCodec).
			Do().Raw()
		if err != nil {
			return errors.WithContext(err, "get microservice")
		}

		out := filepath.Join(root, "crds", "microservices", ms.Name)
		if err := writeCRD(out, rawMs); err != nil {
			return errors.WithContext(err, "write microservice")
		}
	}

	var tunnelList kelda.TunnelList
	err = restClient.Get().
		Namespace(namespace).
		Resource("tunnels").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do().
		Into(&tunnelList)
	if err != nil {
		return errors.WithContext(err, "list tunnels")
	}

	for _, tunnel := range tunnelList.Items {
		rawTunnel, err := restClient.Get().
			Namespace(namespace).
			Resource("tunnels").
			Name(tunnel.Name).
			VersionedParams(&metav1.GetOptions{}, scheme.ParameterCodec).
			Do().Raw()
		if err != nil {
			return errors.WithContext(err, "get tunnel")
		}

		out := filepath.Join(root, "crds", "tunnels", tunnel.Name)
		if err := writeCRD(out, rawTunnel); err != nil {
			return errors.WithContext(err, "write tunnel")
		}
	}

	return nil
}

func setupVersion(root string, kubeClient kubernetes.Interface, restConfig *rest.Config) error {
	outdir := filepath.Join(root, "version")
	if err := fs.Mkdir(outdir, 0755); err != nil {
		return errors.WithContext(err, "mkdir")
	}

	localOut, err := fs.Create(filepath.Join(outdir, "local"))
	if err != nil {
		return errors.WithContext(err, "create")
	}
	defer localOut.Close()
	fmt.Fprintf(localOut, "local version:  %s\n", version.Version)

	mc, err := minionClient.New(kubeClient, restConfig)
	if err != nil {
		return errors.WithContext(err, "connect to Kelda server")
	}
	defer mc.Close()

	remoteVersion, err := mc.GetVersion()
	if err != nil {
		return errors.WithContext(err, "get remote version")
	}

	minionOut, err := fs.Create(filepath.Join(outdir, "minion"))
	if err != nil {
		return errors.WithContext(err, "create")
	}
	defer minionOut.Close()
	fmt.Fprintf(minionOut, "minion version: %s\n", remoteVersion)

	return nil
}

func setupKubeEvents(path, namespace string, kubeClient kubernetes.Interface) error {
	events, err := kubeClient.CoreV1().Events(namespace).List(metav1.ListOptions{})
	if err != nil {
		return errors.WithContext(err, "list events")
	}

	eventsBytes, err := yaml.Marshal(events)
	if err != nil {
		log.WithError(err).WithField("events", events).Warn("Failed to marshal events")
		eventsBytes = []byte(fmt.Sprintf("%+v\n", events))
	}

	if err := afero.WriteFile(fs, path, eventsBytes, 0644); err != nil {
		return errors.WithContext(err, "write")
	}
	return nil
}

func tarDirectory(src, outPath string) error {
	out, err := fs.Create(outPath)
	if err != nil {
		return errors.WithContext(err, "open destination")
	}
	defer out.Close()

	gzw := gzip.NewWriter(out)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	return afero.Walk(fs, src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return errors.WithContext(err, fmt.Sprintf("make header %s", file))
		}

		relPath, err := filepath.Rel(src, file)
		if err != nil {
			return errors.WithContext(err, fmt.Sprintf("get relative path of %s to %s", file, src))
		}

		header.Name = filepath.Join("kelda-bug-info", relPath)
		if err := tw.WriteHeader(header); err != nil {
			return errors.WithContext(err, fmt.Sprintf("write %s header", file))
		}

		// Only write contents if it's a file (i.e. not a directory).
		if !fi.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(file)
		if err != nil {
			return errors.WithContext(err, fmt.Sprintf("open %s", file))
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return errors.WithContext(err, fmt.Sprintf("open %s", file))
		}
		return nil
	})
}
