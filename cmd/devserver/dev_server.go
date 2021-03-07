package devserver

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"github.com/kelda-inc/kelda/cmd/util"
	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
	syncServer "github.com/kelda-inc/kelda/pkg/sync/server"
)

// New creates a new `dev-server` command.
func New() *cobra.Command {
	minionAddress := fmt.Sprintf("minion.kelda.svc.cluster.local:%d", minionClient.DefaultPort)
	return &cobra.Command{
		Use: "dev-server <service> <spec version>",
		Short: "The interface between Kelda and the in-progress code in the " +
			"cluster. This command should not be used directly.",
		Args:   cobra.MinimumNArgs(2),
		Hidden: true,
		Annotations: map[string]string{
			util.AnalyticsMinionAddressKey: minionAddress,
			util.AnalyticsNamespaceEnvKey:  "POD_NAMESPACE",
			util.UseInClusterKubeClientKey: "true",
		},
		Run: func(_ *cobra.Command, args []string) {
			service := args[0]
			specVersion, err := strconv.Atoi(args[1])
			if err != nil {
				err := errors.WithContext(err,
					fmt.Sprintf("spec version (%s) must be an int", args[1]))
				util.HandleFatalError(err)
			}
			pod := os.Getenv("POD_NAME")
			namespace := os.Getenv("POD_NAMESPACE")

			config, err := rest.InClusterConfig()
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get kubeconfig"))
			}

			keldaClient, err := keldaClientset.NewForConfig(config)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "get Kelda client"))
			}

			err = syncServer.Run(keldaClient, namespace, service, pod, specVersion)
			if err != nil {
				util.HandleFatalError(errors.WithContext(err, "run sync server"))
			}
		},
	}
}
