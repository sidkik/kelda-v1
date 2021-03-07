package version

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
	"github.com/kelda-inc/kelda/pkg/version"
)

// New creates a new `version` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the local and remote version of Kelda.",
		Long: "Print the local version of Kelda and the version running\n" +
			"on the minion server, as a git commit hash.",
		Run: func(_ *cobra.Command, args []string) {
			if err := run(); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
}

func run() error {
	fmt.Printf("local version:  %s\n", version.Version)

	minionContext, err := getMinionContext()
	if err != nil {
		return errors.WithContext(err, "get minion context")
	}

	kubeClient, restConfig, err := util.GetKubeClient(minionContext)
	if err != nil {
		return errors.WithContext(err, "get kube client")
	}

	mc, err := minionClient.New(kubeClient, restConfig)
	if err != nil {
		return errors.WithContext(err, "connect to Kelda server")
	}
	defer mc.Close()

	remoteVersion, err := mc.GetVersion()
	if err != nil {
		return errors.WithContext(err, "get remote version")
	}

	fmt.Printf("minion version: %s\n", remoteVersion)
	return nil
}

func getMinionContext() (string, error) {
	userConfig, userConfigErr := config.ParseUser()
	if userConfigErr == nil {
		return userConfig.Context, nil
	}

	kubeCfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return "", errors.WithContext(err, "load kubeconfig")
	}

	log.WithError(userConfigErr).Debugf(
		"Failed to get minion context from %s. "+
			"Falling back to the current Kubeconfig context (%s)",
		config.UserConfigPath, kubeCfg.CurrentContext)
	return kubeCfg.CurrentContext, nil
}
