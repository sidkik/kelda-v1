package update

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sidkik/kelda-v1/cmd/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
	minionClient "github.com/sidkik/kelda-v1/pkg/minion/client"
)

// New creates a new `update` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update the application container images",
		Long: "Checks for updated versions of the Docker images in the application " +
			"according to the upstream Docker registry. " +
			"If it finds newer images, it restarts the out of date containers to " +
			"bring them up to date with the newest versions.",
		Run: func(_ *cobra.Command, _ []string) {
			if err := run(); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
}

func run() error {
	userConfig, err := config.ParseUser()
	if err != nil {
		return errors.WithContext(err, "parse user config")
	}

	kubeClient, restConfig, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		return errors.WithContext(err, "get kube client")
	}

	mc, err := minionClient.New(kubeClient, restConfig)
	if err != nil {
		return errors.WithContext(err, "connect to Kelda server")
	}
	defer mc.Close()

	pp := util.NewProgressPrinter(os.Stdout, "Checking for application "+
		"container updates..")
	go pp.Run()
	updates, err := mc.GetUpdates(userConfig.Namespace)
	pp.StopWithPrint(util.ClearProgress)
	if err != nil {
		return errors.WithContext(err, "get updates")
	}
	if len(updates) == 0 {
		fmt.Println("All images are up to date.")
		return nil
	}

	fmt.Println("The following services are outdated in your development environment:")
	fmt.Println()
	for _, svc := range updates {
		fmt.Printf("\t* %s\n", svc.GetName())
	}
	fmt.Println()

	shouldUpdate, err := util.PromptYesOrNo("Update them now?")
	if err != nil {
		return errors.WithContext(err, "update image prompt")
	}

	if !shouldUpdate {
		fmt.Println("Update aborted.")
		return nil
	}

	pp = util.NewProgressPrinter(os.Stdout, "Performing updates..")
	go pp.Run()
	err = mc.PerformUpdates(userConfig.Namespace, updates)
	pp.StopWithPrint(util.ClearProgress)
	if err != nil {
		return errors.WithContext(err, "perform updates")
	}
	fmt.Println("Update initiated. Check `kelda dev` to track the update progress.")

	return nil
}
