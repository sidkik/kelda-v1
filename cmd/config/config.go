package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sidkik/kelda-v1/cmd/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

// Mocked for unit testing.
var (
	stdout              io.Writer = os.Stdout
	stdin               io.Reader = os.Stdin
	guessDefaults                 = guessDefaultsImpl
	parseUserConfig               = config.ParseUser
	stat                          = os.Stat
	getWorkingDirectory           = os.Getwd
	loadKubeconfig                = clientcmd.NewDefaultClientConfigLoadingRules().Load
	getCurrentUser                = user.Current
)

// New creates a new `config` command.
func New() *cobra.Command {
	var cliOpts config.User
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Setup the Kelda user configuration",
		Run: func(_ *cobra.Command, _ []string) {
			if err := SetupConfig(cliOpts); err != nil {
				err = errors.NewFriendlyError("Failed to setup configuration:\n%s", err)
				util.HandleFatalError(err)
			}
		},
	}
	cmd.Flags().StringVar(&cliOpts.Context, "context", "",
		"Set the context in the config. "+
			"Optional: If not set, `kelda config` will interactively prompt.")
	cmd.Flags().StringVar(&cliOpts.Namespace, "namespace", "",
		"Set the namespace in the config. "+
			"Optional: If not set, `kelda config` will interactively prompt.")
	cmd.Flags().StringVar(&cliOpts.Workspace, "workspace", "",
		"Set the workspace in the config. "+
			"Optional: If not set, `kelda config` will interactively prompt.")

	// Setup the commands for querying the contents of the user config.
	type getterSpec struct {
		use, short string
		fn         func(config.User) string
	}

	getters := []getterSpec{
		{
			use:   "get-namespace",
			short: "Get the currently configured development namespace",
			fn:    func(cfg config.User) string { return cfg.Namespace },
		},
		{
			use:   "get-context",
			short: "Get the currently configured kubectl context",
			fn:    func(cfg config.User) string { return cfg.Context },
		},
	}
	for _, getter := range getters {
		getter := getter
		cmd.AddCommand(&cobra.Command{
			Use:   getter.use,
			Short: getter.short,
			Run: func(_ *cobra.Command, _ []string) {
				cfg, err := parseUserConfig()
				if err != nil {
					err = errors.WithContext(err, "read config")
					util.HandleFatalError(err)
				}

				fmt.Fprintln(stdout, getter.fn(cfg))
			},
		})
	}

	return cmd
}

func SetupConfig(cliOpts config.User) error {
	cfg, err := generateConfig(cliOpts)
	if err != nil {
		return errors.WithContext(err, "generate config")
	}

	if err := config.WriteUser(cfg); err != nil {
		return errors.WithContext(err, "write config")
	}

	path, err := config.GetUserConfigPath()
	if err != nil {
		return errors.WithContext(err, "get user config path")
	}

	fmt.Printf("Wrote config to %s\n", path)
	return nil
}

func namespaceValidationFn(ns string) (string, bool) {
	// Ensure Compliance with DNS-1123.
	// 1) Must be lowercase alphanumeric.
	// 2) The `-` character can also be used in any interior character
	//    of the string.
	// 3) Max of 63 characters.
	dns1123MaxLen := 63

	if ns == "kelda" {
		return "The `kelda` namespace is reserved for the Kelda minion. " +
			"Please pick another namespace.", false
	}

	if len(ns) > dns1123MaxLen {
		return "The namespace name must not be more than 63 characters. " +
			"Please pick another namespace.", false
	}

	re := regexp.MustCompile(`^[-a-z0-9]*$`)
	if !strings.HasPrefix(ns, "-") && !strings.HasSuffix(ns, "-") &&
		re.MatchString(ns) {
		return "", true
	}

	return "This namespace contains invalid characters. " +
		"Please pick another namespace that only  " +
		"uses the following characters:\n" +
		"1) lowercase letters (a-z) \n" +
		"2) numbers (0-9) \n" +
		"3) - \n" +
		"Please ensure that your chosen namespace " +
		"does not start or end with the `-` character.", false

}

type prompt struct {
	helpString, prompt, defaultAnswer, currAnswer string
	field                                         *string
	validationFn                                  func(string) (string, bool)
}

// generateConfig interacts with the user to decide what the user's desired
// configuration is.
// It makes best guesses at reasonable defaults, and allows users to explicitly
// override them if desired.
func generateConfig(cliOpts config.User) (config.User, error) {
	defaults := guessDefaults()
	currConfig, err := parseUserConfig()
	if err != nil {
		currConfig = config.User{}
		log.WithError(err).Debug("Failed to read current config")
	}

	cfg := cliOpts
	var prompts []prompt
	if cliOpts.Context == "" {
		prompts = append(prompts, prompt{
			helpString: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.",
			prompt:        "Development context",
			defaultAnswer: defaults.Context,
			currAnswer:    currConfig.Context,
			field:         &cfg.Context,
		})
	}

	if cliOpts.Namespace == "" {
		prompts = append(prompts, prompt{
			helpString: "Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.",
			prompt:        "Development namespace",
			defaultAnswer: defaults.Namespace,
			currAnswer:    currConfig.Namespace,
			field:         &cfg.Namespace,
			validationFn:  namespaceValidationFn,
		})
	}

	if cliOpts.Workspace == "" {
		prompts = append(prompts, prompt{
			helpString: "Enter the path to the Kelda workspace configuration " +
				"(which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.",
			prompt:        "Path to Kelda Workspace file",
			defaultAnswer: defaults.Workspace,
			currAnswer:    currConfig.Workspace,
			field:         &cfg.Workspace,
		})
	}

	for _, prompt := range prompts {
		var resp string
		for {
			resp, err = promptUser(prompt.helpString, prompt.prompt,
				prompt.defaultAnswer, prompt.currAnswer)
			if err != nil {
				return config.User{}, errors.WithContext(err, "read response")
			}

			if prompt.validationFn == nil {
				break
			}

			validationErr, ok := prompt.validationFn(resp)
			if ok {
				break
			}

			fmt.Fprintln(stdout, validationErr)
		}

		*prompt.field = resp
	}

	return cfg, nil
}

// guessDefaults tries to guess reasonable defaults for the fields in the user
// config.
func guessDefaultsImpl() (cfg config.User) {
	if namespace, err := guessNamespace(); err == nil {
		cfg.Namespace = namespace
	} else {
		log.WithError(err).Info("Failed to guess namespace")
	}

	if context, err := getCurrentContext(); err == nil {
		cfg.Context = context
	} else {
		log.WithError(err).Info("Failed to guess context")
	}

	if workspace, err := guessWorkspace(); err == nil {
		cfg.Workspace = workspace
	} else {
		log.WithError(err).Info("Failed to guess workspace")
	}

	return cfg
}

func sanitizeNamespace(original string) (sanitized string) {
	sanitized = strings.ToLower(original)
	noInvalidChar := regexp.MustCompile(`[^-a-z0-9]`)
	sanitized = noInvalidChar.ReplaceAllString(sanitized, "")
	noLeadingOrTrailingHyphen := regexp.MustCompile(`^-*(.*?)-*$`)
	sanitized = noLeadingOrTrailingHyphen.ReplaceAllString(sanitized, "$1")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	// As a sanity check, make sure the sanitized namespace passes the namespace
	// validation. This should never fail unless there's a bug in the sanitization
	// logic above.
	if _, ok := namespaceValidationFn(sanitized); !ok {
		return ""
	}
	return sanitized
}

func guessNamespace() (string, error) {
	currConfig, err := parseUserConfig()
	if err == nil && currConfig.Namespace != "" {
		return currConfig.Namespace, nil
	}
	user, err := getCurrentUser()
	if err != nil {
		return "", errors.WithContext(err, "get current user")
	}
	return sanitizeNamespace(user.Username), nil
}

func getCurrentContext() (string, error) {
	cfg, err := loadKubeconfig()
	if err != nil {
		return "", errors.WithContext(err, "load kubeconfig")
	}
	return cfg.CurrentContext, nil
}

// guessWorkspace returns the path to the workspace.yaml in the current
// directory if it exists.
func guessWorkspace() (string, error) {
	currDir, err := getWorkingDirectory()
	if err != nil {
		return "", errors.WithContext(err, "get current directory")
	}

	path := filepath.Join(currDir, "workspace.yaml")
	if _, err := stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errors.WithContext(err, "stat")
	}
	return path, nil
}

func promptUser(helpString, prompt, defaultAnswer, currAnswer string) (string, error) {
	// Display a new line at the end to separate different fields to make it
	// look clearer.
	defer fmt.Fprintln(stdout)

	options := []string{}
	if defaultAnswer != "" {
		options = append(options, defaultAnswer)
	}
	if currAnswer != "" && currAnswer != defaultAnswer {
		options = append(options, currAnswer)
	}
	options = append(options, "(Enter manually)")

	fmt.Fprintln(stdout, helpString+"\n"+prompt+":")

	stdinReader := bufio.NewReader(stdin)

	if nOptions := len(options); nOptions > 1 {
		// defaultAnswer or currAnswer exists.
		fmt.Fprintln(stdout)
		for i, option := range options {
			if i == 0 {
				option = fmt.Sprintf("%s (recommended)", option)
			}
			fmt.Fprintf(stdout, "\t%d. %s\n", i+1, option)
		}
		fmt.Fprintln(stdout)

		for {
			fmt.Fprintf(stdout, "Please choose one [1-%d]: ", nOptions)
			choiceStr, err := stdinReader.ReadString('\n')
			if err != nil {
				return "", err
			}

			var choice int
			choiceStr = strings.TrimRight(choiceStr, "\n")

			// Default to the first choice if user doesn't enter anything.
			if choiceStr == "" {
				choice = 1
			} else {
				choice, err = strconv.Atoi(choiceStr)
				if err != nil || choice < 1 || choice > nOptions {
					// Try again if the input is invalid.
					continue
				}
			}

			if choice == nOptions {
				// Enter manually.
				break
			}

			return options[choice-1], nil
		}
	}

	fmt.Fprint(stdout, "Please enter manually: ")
	resp, err := stdinReader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimRight(resp, "\n"), nil
}
