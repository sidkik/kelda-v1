package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/sidkik/kelda-v1/cmd/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	kelda "github.com/sidkik/kelda-v1/pkg/crd/apis/kelda/v1alpha1"
	keldaClientset "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned"
	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/kube"
	"github.com/sidkik/kelda-v1/pkg/sync"
)

// TestHelper contains methods commonly used during integration tests.
type TestHelper struct {
	KubeClient       kubernetes.Interface
	KeldaClient      keldaClientset.Interface
	User             config.User
	ExamplesRepoPath string
	CIRootPath       string
	restConfig       rest.Config
}

// NewTestHelper creates a new TestHelper.
func NewTestHelper(examplesRepoPath, ciRootPath string) (*TestHelper, error) {
	user, err := config.ParseUser()
	if err != nil {
		return nil, err
	}

	kubeClient, restConfig, err := util.GetKubeClient(user.Context)
	if err != nil {
		return nil, err
	}

	keldaClient, err := keldaClientset.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &TestHelper{
		KubeClient:       kubeClient,
		KeldaClient:      keldaClient,
		User:             user,
		ExamplesRepoPath: examplesRepoPath,
		CIRootPath:       ciRootPath,
		restConfig:       *restConfig,
	}, nil
}

// Start starts the given Kelda command. It returns a thread-safe reader for
// the stdout output, and a channel for obtaining any errors after starting the
// command, and any errors from starting the command.
func (helper *TestHelper) Start(ctx context.Context, args ...string) (
	io.Reader, chan error, error) {

	cmd := exec.Command("kelda", args...)

	stdoutReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	stderr := bytes.NewBuffer(nil)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	errChan := make(chan error)
	go func() {
		waitErr := make(chan error)
		go func() {
			waitErr <- cmd.Wait()
			close(waitErr)
		}()

		defer close(errChan)
		select {
		case <-ctx.Done():
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				errChan <- errors.WithContext(err, "kill")
				return
			}
			<-waitErr
		case err := <-waitErr:
			errChan <- fmt.Errorf("crashed (%s): stderr: %s", err, stderr)
		}
	}()
	return stdoutReader, errChan, nil
}

// Run runs the given Kelda command, and returns its stdout.
func (helper *TestHelper) Run(ctx context.Context, command ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "kelda", command...).Output()
}

// Dev runs `kelda dev` with the given arguments, and waits until the
// development environment is ready for use.
func (helper *TestHelper) Dev(ctx context.Context, args ...string) (chan error, error) {
	log.Info("Starting kelda dev")
	cmd := append([]string{"dev", "--no-gui"}, args...)
	stdout, cmdErr, startErr := helper.Start(ctx, cmd...)
	if startErr != nil {
		return nil, errors.WithContext(startErr, "start")
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, 10*time.Minute)
	defer cancelWait()

	devEnvErr := make(chan error, 1)
	go func() {
		defer close(devEnvErr)

		// The following escape sequence is printed by `kelda dev` after the
		// CreateWorkspace request has completed in order to clear the status
		// message. Conveniently, we can also use it as a unique identifier for
		// telling when the CRDs are updated, and the statuses in the cluster
		// are accurate.
		crdsCreated := []byte(util.ClearProgress)
		log.Info("Waiting for development environment to finish initial setup")
		if err := waitForOutput(waitCtx, stdout, crdsCreated); err != nil {
			devEnvErr <- errors.WithContext(err, "initial setup")
			return
		}

		log.Info("Waiting for everything to boot")
		if err := helper.WaitUntilReady(waitCtx); err != nil {
			devEnvErr <- errors.WithContext(err, "full setup")
			return
		}
	}()

	select {
	// If `kelda dev` crashes.
	case err := <-cmdErr:
		return nil, errors.WithContext(err, "kelda dev crashed")

	// If we're done waiting for the development environment to be ready.
	case err := <-devEnvErr:
		// In a race between `cmdErr`, and `devEnvErr`, prefer `cmdErr`
		// (e.g. if the wait fails because `kelda dev` crashed).
		select {
		// Give any crash errors time to propagate through the goroutines.
		// When `kelda dev` crashes, its `stdout` gets closed immediately,
		// which causes an error in `devEnvErr` before we see the exit error in
		// `cmdErr`.
		case <-time.After(5 * time.Second):
		case err := <-cmdErr:
			return nil, errors.WithContext(err, "kelda dev crashed")
		}

		if err != nil {
			return nil, errors.WithContext(err, "wait for ready")
		}
		return cmdErr, nil
	}
}

// waitForOutput blocks until `expOutput` is written to `reader`, or `ctx` has
// expired.
func waitForOutput(ctx context.Context, reader io.Reader, expOutput []byte) error {
	actualOutput := bytes.NewBuffer(nil)
	streamReader := NewStreamReader(reader)
	for {
		select {
		case <-ctx.Done():
			return errors.New("cancelled")
		case r := <-streamReader.Read(ctx):
			if r.Error != nil {
				return errors.WithContext(r.Error, "read")
			}
			if _, err := actualOutput.Write(r.Bytes); err != nil {
				return errors.WithContext(err, "copy")
			}

			if bytes.Contains(actualOutput.Bytes(), expOutput) {
				return nil
			}
		}
	}
}

// WaitUntilReady blocks until all the Tunnels and Microservices in the
// Workspace are ready.
func (helper *TestHelper) WaitUntilReady(ctx context.Context) error {
	msClient := helper.KeldaClient.KeldaV1alpha1().Microservices(helper.User.Namespace)
	tunnelsClient := helper.KeldaClient.KeldaV1alpha1().Tunnels(helper.User.Namespace)
	isReady := func() bool {
		msList, err := msClient.List(metav1.ListOptions{})
		if err != nil {
			log.WithError(err).Error("List microservices")
			return false
		}

		tunnelsList, err := tunnelsClient.List(metav1.ListOptions{})
		if err != nil {
			log.WithError(err).Error("List tunnels")
			return false
		}

		for _, ms := range msList.Items {
			if ms.Spec.HasService && ms.Status.ServiceStatus.Phase != kelda.ServiceReady {
				return false
			}
			if ms.Spec.HasJob && ms.Status.JobStatus.Phase != kelda.JobCompleted {
				return false
			}
		}

		for _, tunnel := range tunnelsList.Items {
			if tunnel.Status.Phase != kelda.TunnelUp {
				return false
			}
		}

		return true
	}

	tunnelTrigger, err := helper.TunnelTrigger()
	if err != nil {
		return errors.WithContext(err, "watch tunnels")
	}

	msTrigger, err := helper.MicroserviceTrigger()
	if err != nil {
		return errors.WithContext(err, "watch microservices")
	}

	if !TestWithRetry(ctx, combineTriggers(tunnelTrigger, msTrigger), isReady) {
		return errors.New("never became ready")
	}
	return nil
}

func (helper *TestHelper) Exec(service string, command []string) (string, error) {
	podName, containerName, err := util.ResolvePodName(helper.KeldaClient, helper.User.Namespace, service)
	if err != nil {
		return "", errors.WithContext(err, "resolve service")
	}

	var outputBuffer bytes.Buffer
	execOpts := corev1.PodExecOptions{
		Container: containerName,
		Command:   command,
		Stdout:    true,
	}
	streamOpts := remotecommand.StreamOptions{
		Stdout: &outputBuffer,
	}

	err = kube.Exec(helper.KubeClient, &helper.restConfig, helper.User.Namespace, podName, execOpts, streamOpts)
	if err != nil {
		return "", errors.WithContext(err, "exec")
	}

	output, err := ioutil.ReadAll(&outputBuffer)
	if err != nil {
		return "", errors.WithContext(err, "read output")
	}
	return string(output), nil
}

var ErrNeverSynced = errors.New("never synced")

// WaitUntilSynced blocks until the local files in `servicePath` are running in
// the development environment.
func (helper *TestHelper) WaitUntilSynced(ctx context.Context, servicePath string) error {
	ws, err := config.ParseWorkspace(nil, helper.User.Workspace, "")
	if err != nil {
		return errors.WithContext(err, "read")
	}

	serviceCfg, err := config.ParseSyncConfig(servicePath)
	if err != nil {
		return errors.WithContext(err, "parse service")
	}

	msTrigger, err := helper.MicroserviceTrigger()
	if err != nil {
		return errors.WithContext(err, "watch microservices")
	}

	syncCfg := serviceCfg.GetSyncConfigProto()
	if len(syncCfg.OnSyncCommand) == 0 {
		svc, ok := ws.GetService(serviceCfg.Name)
		if !ok {
			return errors.WithContext(err, "get service")
		}

		syncCfg.OnSyncCommand, err = svc.GetDevCommand()
		if err != nil {
			return errors.WithContext(err, "get dev command")
		}
	}

	snapshot, err := sync.SnapshotSource(syncCfg, servicePath)
	if err != nil {
		return errors.WithContext(err, "snapshot")
	}
	expVer := sync.Version{syncCfg, snapshot}.String()

	msClient := helper.KeldaClient.KeldaV1alpha1().Microservices(helper.User.Namespace)
	isSynced := func() bool {
		service, err := msClient.Get(serviceCfg.Name, metav1.GetOptions{})
		if err != nil {
			log.WithError(err).Error("Failed to get service")
			return false
		}

		return service.DevStatus.RunningVersion == expVer
	}
	if !TestWithRetry(ctx, msTrigger, isSynced) {
		return ErrNeverSynced
	}
	return nil
}

var microserviceErrorWhitelist = []string{
	// These errors get thrown during the boot of the node-todo-with-volumes
	// example application.
	"pod has unbound immediate PersistentVolumeClaims",
	"pod has unbound PersistentVolumeClaims",
	"persistentvolumeclaim \"mongo-pvc\" not found",
}

// WatchForErrors watches for any errors in development environment.
func (helper *TestHelper) WatchForErrors(ctx context.Context, t *testing.T) {
	msClient := helper.KeldaClient.KeldaV1alpha1().Microservices(helper.User.Namespace)
	msWatcher, err := msClient.Watch(metav1.ListOptions{})
	if err != nil {
		t.Errorf("watch microservices: %s", err)
		return
	}
	defer msWatcher.Stop()

	updates := msWatcher.ResultChan()
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if !(update.Type == watch.Added || update.Type == watch.Modified) {
				break
			}

			// Ensure that there aren't any transient errors while booting.
			ms := update.Object.(*kelda.Microservice)
			if ms.Status.MetaStatus.Phase == kelda.MetaStatusSyncFailed ||
				ms.Status.MetaStatus.Phase == kelda.MetaDeployFailed ||
				ms.Status.JobStatus.Phase == kelda.JobFailed {
				t.Errorf("microservice %s in failed state: %+v", ms.Name, ms.Status)
			}

			if ms.Status.ServiceStatus.Phase == kelda.ServiceFailed {
				var isWhitelisted bool
				for _, whitelistedError := range microserviceErrorWhitelist {
					if strings.Contains(ms.Status.ServiceStatus.Message, whitelistedError) {
						isWhitelisted = true
						break
					}
				}

				if !isWhitelisted {
					t.Errorf("microservice %s in failed state: %+v", ms.Name, ms.Status)
				}
			}
		}
	}
}

// MicroserviceTrigger returns a channel that triggers each time there's a
// change in the Microservice CRDs.
func (helper *TestHelper) MicroserviceTrigger() (chan struct{}, error) {
	msClient := helper.KeldaClient.KeldaV1alpha1().Microservices(helper.User.Namespace)
	msWatcher, err := msClient.Watch(metav1.ListOptions{})
	if err != nil {
		return nil, errors.WithContext(err, "watch")
	}
	return kubeWatcherToGeneric(msWatcher), nil
}

// TunnelTrigger returns a channel that triggers each time there's a
// change in the Tunnel CRDs.
func (helper *TestHelper) TunnelTrigger() (chan struct{}, error) {
	tunnelsClient := helper.KeldaClient.KeldaV1alpha1().Tunnels(helper.User.Namespace)
	tunnelWatcher, err := tunnelsClient.Watch(metav1.ListOptions{})
	if err != nil {
		return nil, errors.WithContext(err, "watch")
	}
	return kubeWatcherToGeneric(tunnelWatcher), nil
}

func combineTriggers(x, y chan struct{}) chan struct{} {
	combined := make(chan struct{})
	go func() {
		for {
			select {
			case <-x:
			case <-y:
			}

			select {
			case combined <- struct{}{}:
			default:
			}
		}
	}()
	return combined
}

func kubeWatcherToGeneric(watcher watch.Interface) chan struct{} {
	updates := make(chan struct{})
	go func() {
		for range watcher.ResultChan() {
			select {
			case updates <- struct{}{}:
			default:
			}
		}
	}()
	return updates
}

func TestWithRetry(ctx context.Context, trigger chan struct{}, test func() bool) bool {
	maxSleepTime := 30 * time.Second
	sleepTime := 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return test()
		case <-time.After(sleepTime):
			sleepTime *= 2
			if sleepTime > maxSleepTime {
				sleepTime = maxSleepTime
			}
		case <-trigger:
		}

		if test() {
			return true
		}
	}
}

func AddToWorkspace(path string, tunnels []config.Tunnel) error {
	rawWorkspace, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.WithContext(err, "read")
	}

	var workspaceWithoutServiceManifests config.Workspace
	if err := yaml.Unmarshal(rawWorkspace, &workspaceWithoutServiceManifests); err != nil {
		return errors.WithContext(err, "parse")
	}

	curr, err := config.ParseWorkspace(nil, path, "")
	if err != nil {
		return errors.WithContext(err, "read")
	}

	// Unset extra metadata that gets added during parsing, but doesn't belong
	// in the raw user-defined config.
	curr.Services = workspaceWithoutServiceManifests.Services

	curr.Tunnels = append(curr.Tunnels, tunnels...)

	yamlBytes, err := yaml.Marshal(curr)
	if err != nil {
		return errors.WithContext(err, "marshal")
	}

	if err := ioutil.WriteFile(path, yamlBytes, 0644); err != nil {
		return errors.WithContext(err, "write")
	}
	return nil
}
