package ssh

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/kube"
)

type sshCommand struct {
	keldaClient keldaClientset.Interface
	kubeClient  kubernetes.Interface
	restConfig  rest.Config
	namespace   string
	id          string
}

// New creates a new `ssh` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh SERVICE",
		Short: "Get a shell in a service",
		Run: func(cmd *cobra.Command, args []string) {
			switch len(args) {
			case 0:
				util.HandleFatalError(util.ErrMissingServiceName)
			case 1:
			default:
				util.HandleFatalError(util.ErrTooManyServices)
			}

			userConfig, err := config.ParseUser()
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "parse user config"))
			}

			kubeClient, restConfig, err := util.GetKubeClient(userConfig.Context)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get kube client"))
			}

			keldaClient, err := keldaClientset.NewForConfig(restConfig)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get kelda client"))
			}

			err = sshCommand{
				keldaClient: keldaClient,
				kubeClient:  kubeClient,
				restConfig:  *restConfig,
				namespace:   userConfig.Namespace,
				id:          args[0],
			}.run()
			if err != nil {
				util.HandleFatalError(err)
			}
		},
	}
}

func (cmd sshCommand) run() error {
	podName, containerName, err := util.ResolvePodName(cmd.keldaClient, cmd.namespace, cmd.id)
	if err != nil {
		return errors.WithContext(err, "resolve ID")
	}

	// Put the terminal into raw mode to prevent it echoing characters twice.
	oldState, err := terminal.MakeRaw(0)
	if err != nil {
		return errors.WithContext(err, "set terminal mode")
	}

	defer func() {
		_ = terminal.Restore(0, oldState)
	}()

	execOpts := core.PodExecOptions{
		Container: containerName,
		Command:   []string{"sh"},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}
	streamOpts := remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty:    true,
	}
	return kube.Exec(cmd.kubeClient, &cmd.restConfig, cmd.namespace, podName, execOpts, streamOpts)
}
