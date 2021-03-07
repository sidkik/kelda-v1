package bugtool

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fakeKube "k8s.io/client-go/kubernetes/fake"

	"github.com/kelda-inc/kelda/pkg/config"
)

type file struct {
	path, contents string
}

func TestSetupCLILogs(t *testing.T) {
	tests := []struct {
		name       string
		root       string
		userConfig config.User
		mockFiles  []file
		expFiles   []file
		expError   error
	}{
		{
			name:       "Log exists",
			root:       "root",
			userConfig: config.User{Workspace: "magda/workspace.yml"},
			mockFiles:  []file{{"magda/kelda.log", "log contents"}},
			expFiles:   []file{{"root/cli.log", "log contents"}},
		},
		{
			name:       "Log doesn't exist",
			userConfig: config.User{Workspace: "magda/workspace.yml"},
			expError:   errors.New("open log: open magda/kelda.log: file does not exist"),
		},
		{
			name:     "No workspace",
			expError: errors.New("no workspace defined in user config"),
		},
	}

	for _, test := range tests {
		fs = afero.NewMemMapFs()
		assert.NoError(t, setupFiles(test.mockFiles))
		err := setupCLILogs(test.root, test.userConfig)
		if test.expError == nil {
			assert.NoError(t, err, test.name)
		} else {
			assert.EqualError(t, err, test.expError.Error(), test.name)
		}
		assertFiles(t, test.expFiles, test.name)
	}
}

func TestSetupPodLogs(t *testing.T) {
	fs = afero.NewMemMapFs()
	newReadCloser := func(str string) io.ReadCloser {
		return ioutil.NopCloser(bytes.NewBuffer([]byte(str)))
	}
	getLogs = func(_ kubernetes.Interface, _, pod string, opts corev1.PodLogOptions) (
		io.ReadCloser, error) {

		if pod == "minion-pod" && !opts.Previous {
			return newReadCloser("logs from running minion pod"), nil
		}

		if pod == "minion-pod" && opts.Previous {
			return newReadCloser("logs from crashed minion pod"), nil
		}

		if pod == "terminating-minion-pod" && !opts.Previous {
			return newReadCloser("logs from terminated minion pod"), nil
		}

		panic("unreached")
	}
	clientset := fakeKube.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "minion-pod",
				Namespace: "kelda",
				Labels:    map[string]string{"service": "kelda"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "minion"},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "minion", RestartCount: 5},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "terminating-minion-pod",
				Namespace: "kelda",
				Labels:    map[string]string{"service": "kelda"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "minion"},
				},
			},
		},
	)
	assert.NoError(t, setupPodLogs("root/minion-logs", "kelda", clientset))

	expFiles := []file{
		{"root/minion-logs/minion-pod-minion", "logs from running minion pod"},
		{"root/minion-logs/minion-pod-minion-crashed", "logs from crashed minion pod"},
		{"root/minion-logs/terminating-minion-pod-minion", "logs from terminated minion pod"},
	}
	assertFiles(t, expFiles, "setupPodLogs should create log files")
}

func TestSetupPodStatus(t *testing.T) {
	fs = afero.NewMemMapFs()
	clientset := fakeKube.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-1",
				Namespace: "dev",
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{RestartCount: 5},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-2",
				Namespace: "dev",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ignored-pod",
				Namespace: "ignored",
			},
		},
	)
	assert.NoError(t, setupPodStatus("root/pod-status", "dev", clientset))

	expFiles := []file{
		{
			"root/pod-status/pod-1",
			`metadata:
  creationTimestamp: null
  name: pod-1
  namespace: dev
spec:
  containers: null
status:
  containerStatuses:
  - image: ""
    imageID: ""
    lastState: {}
    name: ""
    ready: false
    restartCount: 5
    state: {}
`,
		},
		{
			"root/pod-status/pod-2",
			`metadata:
  creationTimestamp: null
  name: pod-2
  namespace: dev
spec:
  containers: null
status: {}
`,
		},
	}
	assertFiles(t, expFiles, "setupPodStatus should create status files")
}

func TestSetupKubeEvents(t *testing.T) {
	fs = afero.NewMemMapFs()
	clientset := fakeKube.NewSimpleClientset(
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-event",
				Namespace: "kelda",
			},
			Message: "pod event message",
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-event",
				Namespace: "default",
			},
			Message: "node event message",
		},
	)
	assert.NoError(t, setupKubeEvents("kelda-events", "kelda", clientset))
	assert.NoError(t, setupKubeEvents("node-events", "default", clientset))

	expFiles := []file{
		{
			"kelda-events",
			`items:
- eventTime: null
  firstTimestamp: null
  involvedObject: {}
  lastTimestamp: null
  message: pod event message
  metadata:
    creationTimestamp: null
    name: pod-event
    namespace: kelda
  reportingComponent: ""
  reportingInstance: ""
  source: {}
metadata: {}
`,
		},
		{
			"node-events",
			`items:
- eventTime: null
  firstTimestamp: null
  involvedObject: {}
  lastTimestamp: null
  message: node event message
  metadata:
    creationTimestamp: null
    name: node-event
    namespace: default
  reportingComponent: ""
  reportingInstance: ""
  source: {}
metadata: {}
`,
		},
	}
	assertFiles(t, expFiles, "setupKubeEvents should create event files")
}

func TestSetupNodes(t *testing.T) {
	fs = afero.NewMemMapFs()
	clientset := fakeKube.NewSimpleClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
			Status: corev1.NodeStatus{
				Phase: corev1.NodeRunning,
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
			Status: corev1.NodeStatus{
				Phase: corev1.NodeRunning,
			},
		},
	)
	assert.NoError(t, setupNodeStatus("root/nodes", clientset))

	expFiles := []file{
		{
			"root/nodes/node-1",
			`metadata:
  creationTimestamp: null
  name: node-1
spec: {}
status:
  daemonEndpoints:
    kubeletEndpoint:
      Port: 0
  nodeInfo:
    architecture: ""
    bootID: ""
    containerRuntimeVersion: ""
    kernelVersion: ""
    kubeProxyVersion: ""
    kubeletVersion: ""
    machineID: ""
    operatingSystem: ""
    osImage: ""
    systemUUID: ""
  phase: Running
`,
		},
		{
			"root/nodes/node-2",
			`metadata:
  creationTimestamp: null
  name: node-2
spec: {}
status:
  daemonEndpoints:
    kubeletEndpoint:
      Port: 0
  nodeInfo:
    architecture: ""
    bootID: ""
    containerRuntimeVersion: ""
    kernelVersion: ""
    kubeProxyVersion: ""
    kubeletVersion: ""
    machineID: ""
    operatingSystem: ""
    osImage: ""
    systemUUID: ""
  phase: Running
`,
		},
	}
	assertFiles(t, expFiles, "setupNodeStatus should create status files")
}

func setupFiles(files []file) error {
	for _, f := range files {
		if err := afero.WriteFile(fs, f.path, []byte(f.contents), 0644); err != nil {
			return err
		}
	}
	return nil
}

func assertFiles(t *testing.T, files []file, msg string) {
	for _, f := range files {
		contents, err := afero.ReadFile(fs, f.path)
		assert.NoError(t, err, msg)
		assert.Equal(t, f.contents, string(contents), msg)
	}
}
