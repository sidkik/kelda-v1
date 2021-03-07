package login

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kelda-inc/kelda/cmd/util"
	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
)

const (
	createTokenEndpoint = "https://o8evqpcw8g.execute-api.us-west-1.amazonaws.com/prod/create-token"
	loginEndpoint       = "https://o8evqpcw8g.execute-api.us-west-1.amazonaws.com/prod/login"
	noAccountError      = "No account exists for %s.\n" +
		"Request Hosted Kelda access at https://kelda.io/request-hosted-kelda-access/"
)

// Response is the structure of the response returned by the login API.
type Response struct {
	Kubeconfig string `json:"kubeconfig"`
	Error      string `json:"error"`
}

// New creates a new `login` command.
func New() *cobra.Command {
	var email string
	var token string
	cmd := &cobra.Command{
		Use: "login",
		Short: "ALPHA. Login to Hosted Kelda. " +
			"Request access at https://kelda.io/request-hosted-kelda-access/",
		Run: func(_ *cobra.Command, _ []string) {
			if err := Main(email, token); err != nil {
				util.HandleFatalError(err)
			}
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email associated with your Hosted Kelda account.")
	cmd.Flags().StringVar(&token, "token", "", "Login token. A token will be generated and emailed "+
		"to you if not supplied.")
	return cmd
}

func Main(email, token string) (err error) {
	if email == "" {
		return errors.NewFriendlyError("Hosted Kelda email is required.\n" +
			"Please provide it with `kelda login -email <email>`")
	}

	// The demo cluster doesn't require authentication.
	if token == "" && email != "demo@kelda.io" {
		if token, err = getToken(email); err != nil {
			return errors.WithContext(err, "get token")
		}
	}

	kubeconfig, err := getKubeconfig(email, token)
	if err != nil {
		return errors.WithContext(err, "get kubeconfig")
	}

	key := IdentifierForEmail(email)
	if err := installKubeconfig(key, kubeconfig); err != nil {
		return errors.WithContext(err, "install credentials")
	}

	if err := setKeldaContext(key); err != nil {
		return errors.WithContext(err, "update kelda context")
	}

	fmt.Println("Successfully logged in.")
	fmt.Println("`kelda dev` will now deploy to your Hosted Kelda cluster.")
	return nil
}

func IdentifierForEmail(email string) string {
	return fmt.Sprintf("hosted-kelda-%s", email)
}

// getToken requests that a token be sent to the user's email, and waits for
// the user to enter it.
func getToken(email string) (string, error) {
	payload := map[string]string{"email": email}
	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return "", errors.WithContext(err, "create payload")
	}

	resp, err := http.Post(createTokenEndpoint, "application/json", bytes.NewBuffer((payloadBytes)))
	if err != nil {
		return "", errors.WithContext(err, "connect to login server")
	}
	defer resp.Body.Close()

	var parsedBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsedBody); err != nil {
		return "", errors.WithContext(err, "failed to parse login body")
	}

	switch resp.StatusCode {
	case http.StatusOK:
		fmt.Println("Login token has been sent to your email.")
		fmt.Printf("Enter here: ")

		token, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", errors.WithContext(err, "read token")
		}
		return strings.TrimRight(token, "\n"), nil
	case 404:
		return "", errors.NewFriendlyError(noAccountError, email)
	default:
		return "", errors.New("server responded with %s (%s)", resp.Status, parsedBody.Error)
	}
}

func getKubeconfig(email, token string) (string, error) {
	payload := map[string]string{"email": email, "token": token}
	payloadBytes, err := json.Marshal(&payload)
	if err != nil {
		return "", errors.WithContext(err, "create payload")
	}

	resp, err := http.Post(loginEndpoint, "application/json", bytes.NewBuffer((payloadBytes)))
	if err != nil {
		return "", errors.WithContext(err, "connect to login server")
	}
	defer resp.Body.Close()

	var parsedBody Response
	if err := json.NewDecoder(resp.Body).Decode(&parsedBody); err != nil {
		return "", errors.WithContext(err, "failed to parse login body")
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return parsedBody.Kubeconfig, nil
	case 401:
		return "", errors.NewFriendlyError("Invalid token. " +
			"You should have received a token in your email.\n" +
			"Note that only the most recently generated token is valid, and tokens expire after 30 minutes.")
	case 404:
		return "", errors.NewFriendlyError(noAccountError, email)
	default:
		return "", errors.New("server responded with %s (%s)", resp.Status, parsedBody.Error)
	}
}

func installKubeconfig(key, toAddStr string) error {
	// Get the current Kubeconfig.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	kubeconfig, err := clientConfig.RawConfig()
	if err != nil {
		return errors.WithContext(err, "get current kubeconfig")
	}

	// Add the new Kubeconfig, using `key` as a unique identifier to avoid
	// clashing with the user's other contexts.
	toAdd, err := clientcmd.Load([]byte(toAddStr))
	if err != nil {
		return errors.WithContext(err, "parse downloaded kubeconfig")
	}

	context, ok := toAdd.Contexts[toAdd.CurrentContext]
	if !ok {
		return errors.New("undefined context in downloaded kubeconfig")
	}

	cluster, ok := toAdd.Clusters[context.Cluster]
	if !ok {
		return errors.New("undefined cluster in downloaded kubeconfig")
	}

	authInfo, ok := toAdd.AuthInfos[context.AuthInfo]
	if !ok {
		return errors.New("undefined cluster in downloaded kubeconfig")
	}

	context.Cluster = key
	context.AuthInfo = key
	kubeconfig.Contexts[key] = context
	kubeconfig.Clusters[key] = cluster
	kubeconfig.AuthInfos[key] = authInfo
	kubeconfig.CurrentContext = key
	if err := clientcmd.ModifyConfig(clientConfig.ConfigAccess(), kubeconfig, false); err != nil {
		return errors.WithContext(err, "write new kubeconfig")
	}
	return nil
}

func setKeldaContext(context string) error {
	currConfig, err := config.ParseUser()
	if err != nil {
		currConfig = config.User{}
	}

	currConfig.Context = context
	return config.WriteUser(currConfig)
}
