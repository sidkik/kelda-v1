package delete

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
)

const deadlineSeconds = 120

// New creates a new `delete` command.
func New() *cobra.Command {
	return &cobra.Command{
		Use: "delete",
		Short: "Delete the development environment. " +
			"Does not affect other developers.",
		Run: func(cmd *cobra.Command, args []string) {
			userConfig, err := config.ParseUser()
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "parse user config"))
			}

			if err := deleteDevEnv(userConfig); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
}

func deleteDevEnv(userConfig config.User) error {
	kubeClient, _, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		return errors.WithContext(err, "get kube client")
	}

	msg := fmt.Sprintf("Deleting namespace '%s'...", userConfig.Namespace)
	pp := util.NewProgressPrinter(os.Stdout, msg)
	go pp.Run()

	err = kubeClient.CoreV1().Namespaces().Delete(userConfig.Namespace, nil)
	if kerrors.IsNotFound(err) {
		pp.Stop()
		fmt.Println("\nNamespace doesn't exist. Nothing to do.")
		return nil
	}

	// Detect PriorityClass availability and delete them if they exist.
	serverGroups, _, err := kubeClient.Discovery().ServerGroupsAndResources()
	if err != nil {
		return errors.WithContext(err, "get groups")
	}
	for _, serverGroup := range serverGroups {
		if serverGroup.Name != "scheduling.k8s.io" {
			continue
		}
		for _, serverVersion := range serverGroup.Versions {
			if serverVersion.Version == "v1beta1" {
				err = kubeClient.SchedulingV1beta1().PriorityClasses().Delete(
					userConfig.Namespace, nil)
				if err != nil {
					return errors.WithContext(err, "delete priorityclass")
				}

				priorityClassDeleted := func() bool {
					_, err := kubeClient.SchedulingV1beta1().PriorityClasses().Get(
						userConfig.Namespace, metav1.GetOptions{})
					return kerrors.IsNotFound(err)
				}

				if !waitUntilTrue(priorityClassDeleted) {
					return errors.New("priorityclass deletion exceeded grace period")
				}
			}
		}
	}

	defer pp.Stop()

	namespaceDeleted := func() bool {
		_, err := kubeClient.CoreV1().Namespaces().Get(userConfig.Namespace, metav1.GetOptions{})
		return kerrors.IsNotFound(err)
	}

	if !waitUntilTrue(namespaceDeleted) {
		return errors.New("namespace deletion exceeded grace period")
	}

	return nil
}

func waitUntilTrue(f func() bool) bool {
	deadlineExceeded := time.After(deadlineSeconds * time.Second)
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()

	for {
		select {
		case <-deadlineExceeded:
			return false
		case <-poll.C:
			if f() {
				return true
			}
		}
	}
}
