package cmd

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// Load the client authentication plugin necessary for connecting to GKE.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/kelda-inc/kelda/cmd/bugtool"
	configCmd "github.com/kelda-inc/kelda/cmd/config"
	deleteCmd "github.com/kelda-inc/kelda/cmd/delete"
	"github.com/kelda-inc/kelda/cmd/dev"
	"github.com/kelda-inc/kelda/cmd/devserver"
	"github.com/kelda-inc/kelda/cmd/login"
	"github.com/kelda-inc/kelda/cmd/logs"
	"github.com/kelda-inc/kelda/cmd/minion"
	"github.com/kelda-inc/kelda/cmd/setup"
	"github.com/kelda-inc/kelda/cmd/ssh"
	"github.com/kelda-inc/kelda/cmd/update"
	"github.com/kelda-inc/kelda/cmd/upgradecli"
	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/cmd/version"
	"github.com/kelda-inc/kelda/pkg/analytics"
	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
	minionClient "github.com/kelda-inc/kelda/pkg/minion/client"
)

// verboseLogKey is the environment variable used to enable verbose logging.
// When it's set to `true`, Debug events are logged, rather than just Info and
// above.
const verboseLogKey = "KELDA_LOG_VERBOSE"

// Execute runs the main CLI process.
func Execute() {
	if os.Getenv(verboseLogKey) == "true" {
		log.SetLevel(log.DebugLevel)
	}

	rootCmd := &cobra.Command{
		Use:          "kelda",
		SilenceUsage: true,

		// The call to rootCmd.Execute prints the error, so we silence errors
		// here to avoid double printing.
		SilenceErrors:    true,
		PersistentPreRun: setupAnalytics,
	}
	rootCmd.AddCommand(
		configCmd.New(),
		bugtool.New(),
		deleteCmd.New(),
		dev.New(),
		devserver.New(),
		login.New(),
		logs.New(),
		minion.New(),
		setup.New(),
		ssh.New(),
		update.New(),
		upgradecli.New(),
		version.New(),
	)

	if err := rootCmd.Execute(); err != nil {
		util.HandleFatalError(err)
	}
}

func setupAnalytics(cmd *cobra.Command, _ []string) {
	analytics.SetSource(cmd.CalledAs())

	// The minion shouldn't connect to itself to get the customer name.
	// Instead, it has special handling within its command implementation that
	// sets the customer name after it parses the license.
	if cmd.CalledAs() != "minion" {
		if customerName, err := getCustomerName(cmd.Annotations); err == nil {
			analytics.SetCustomer(customerName)
		} else {
			log.WithError(err).Debug("Failed to get customer name for analytics")
		}
	}

	if namespace, err := getAnalyticsNamespace(cmd.Annotations); err == nil {
		analytics.SetNamespace(namespace)
	} else {
		log.WithError(err).Debug("Failed to get namespace for analytics")
	}

	if kubeClient, err := getKubeClient(cmd.Annotations); err == nil {
		if kubeVersion, err := kubeClient.Discovery().ServerVersion(); err == nil {
			analytics.SetKubeVersion(kubeVersion.String())
		} else {
			log.WithError(err).Debug("Failed to get Kubernetes version for analytics")
		}
	} else {
		log.WithError(err).Debug("Failed to get Kubernetes client for analytics")
	}
}

func getCustomerName(opts map[string]string) (string, error) {
	minionClient, err := getMinionClient(opts)
	if err != nil {
		return "", errors.WithContext(err, "connect to minion")
	}

	license, err := minionClient.GetLicense()
	if err != nil {
		return "", errors.WithContext(err, "get license")
	}
	return license.Terms.Customer, nil
}

// getMinionClient creates a minion client for the given address if it's specified.
// If it's not specified, it creates a client according to the user's Kelda
// config.
func getMinionClient(opts map[string]string) (minionClient.Client, error) {
	minionAddress, ok := opts[util.AnalyticsMinionAddressKey]
	if ok {
		return minionClient.NewWithAddress(minionAddress)
	}

	userConfig, err := config.ParseUser()
	if err != nil {
		return nil, errors.WithContext(err, "parse user config")
	}

	kubeClient, restConfig, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		return nil, errors.WithContext(err, "get kube client")
	}

	return minionClient.New(kubeClient, restConfig)
}

// getAnalyticsNamespace gets the namespace to use for analytics according to `opts`. If
// the namespace is explicitly set, it uses that. If an environment variable is
// specified, it returns the value of the variable. And by default, it returns the
// value in the user's Kelda config.
func getAnalyticsNamespace(opts map[string]string) (string, error) {
	if ns, ok := opts[util.AnalyticsNamespaceKey]; ok {
		return ns, nil
	}

	if envKey, ok := opts[util.AnalyticsNamespaceEnvKey]; ok {
		return os.Getenv(envKey), nil
	}

	userConfig, err := config.ParseUser()
	if err != nil {
		return "", errors.WithContext(err, "parse user config")
	}
	return userConfig.Namespace, nil
}

func getKubeClient(opts map[string]string) (kubernetes.Interface, error) {
	if _, ok := opts[util.UseInClusterKubeClientKey]; ok {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		return kubernetes.NewForConfig(config)
	}

	userConfig, err := config.ParseUser()
	if err != nil {
		return nil, errors.WithContext(err, "parse user config")
	}

	kubeClient, _, err := util.GetKubeClient(userConfig.Context)
	if err != nil {
		return nil, errors.WithContext(err, "get kube client")
	}
	return kubeClient, nil
}
