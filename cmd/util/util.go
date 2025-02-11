package util

import (
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sidkik/kelda-v1/pkg/config"
	keldaClientset "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned"
	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/kube"
)

const (
	// AnalyticsMinionAddressKey is the annotation key used by Kelda commands to denote
	// what address should be used for updating analytics.
	AnalyticsMinionAddressKey = "analyticsMinionAddress"

	// AnalyticsNamespaceKey is the annotation key used to hardcode a analytics
	// namespace.
	AnalyticsNamespaceKey = "analyticsNamespace"

	// AnalyticsNamespaceEnvKey is the annotation key used to set the analytics
	// namespace to an environment variable that's read at runtime.
	AnalyticsNamespaceEnvKey = "analyticsNamespaceEnv"

	// UseInClusterKubeClientKey is the annotation used to make analytics look
	// up information via the in cluster client, rather than the user's Kelda
	// config.
	UseInClusterKubeClientKey = "useInClusterKubeClient"

	// ServiceNotExistsTemplate is an error message template shown when the
	// requested service doesn't exist.
	ServiceNotExistsTemplate = "Service %q does not exist. " +
		"Check the `kelda dev` UI for a list of possible service names."

	// ContextNotExistsTemplate is an error message template shown when the context
	// in the user configuration file doesn't exist.
	ContextNotExistsTemplate = "Context %q does not exist. Check the " +
		"output of `kubectl config get-contexts` for a list of possible contexts " +
		"and use `kelda config` to configure an available context."
)

var (
	// The credentials required to connect to the demo cluster. The account was
	// generated by the make-kubeconfig.sh script, and matches the kubeconfig
	// in the examples repo.
	demoClusterConfig = rest.Config{
		Host: "https://35.188.52.10",
		BearerToken: "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL" +
			"3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3Bh" +
			"Y2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmF" +
			"tZSI6ImtlbGRhLXVzZXItdG9rZW4tdG5tZGIiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2" +
			"NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoia2VsZGEtdXNlciIsImt1YmVybmV0ZXMua" +
			"W8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6ImQ2YjVjYTZmLWJlYWMt" +
			"MTFlOS05ZTQyLTQyMDEwYTgwMDAwOCIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDp" +
			"kZWZhdWx0OmtlbGRhLXVzZXIifQ.FRl0mi-0lSm4ILDNiVufaN1U8RCxHXKGd-5euzf931j" +
			"SoNcpZxUsm44Bp7796W6GAfnFHGzSp_rrzasIrYpMkchZP8vkpTtiSGD8sNez4aJNl_X9C4" +
			"vXkagLTOCsEYV46DrQTFl9zu-aLAyeOxqjVtgpeT4FI3SXJtmLhwXy5tVImJNsflkyUX8Lh" +
			"qcCfHwKVEfcLvQYYOvPanwnCNH8QQnSgOTbEyoUWyZ7nKO1jrIdTtTPT6sxPteepY8ISMFi" +
			"uSOPZ95JQ3kuHMqYhQGBaU6tfJXDPNKBIC5qpfhFfzco5yVNZQDI_T8PF9feQwH91kVjRpm" +
			"6kt7T-jsDIe3uDg",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: mustDecodeBase64("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSU" +
				"RERENDQWZTZ0F3SUJBZ0lSQUx3UXE3RXBmRHlFM285QjF4NHlNOVF3RFFZSktvWklodmNOQ" +
				"VFFTEJRQXcKTHpFdE1Dc0dBMVVFQXhNa05ESmxOMkk0TURrdFlUTXlPUzAwTkRrd0xXSXla" +
				"RFF0WWpObFpUTmxOV0V5WlRZegpNQjRYRFRFNU1EZ3hNekU1TVRNeE4xb1hEVEkwTURneE1" +
				"USXdNVE14TjFvd0x6RXRNQ3NHQTFVRUF4TWtOREpsCk4ySTRNRGt0WVRNeU9TMDBORGt3TF" +
				"dJeVpEUXRZak5sWlRObE5XRXlaVFl6TUlJQklqQU5CZ2txaGtpRzl3MEIKQVFFRkFBT0NBU" +
				"ThBTUlJQkNnS0NBUUVBaWdJK2VXNExtSUdJa05hODBWRXJibkZyMkRSbHkxRnlUclpRbmto" +
				"QQpzRThoeU9mMHpCbUlCWDhBQ3JCaFlSUlc5OVNQK3ZrRG5yNy9JS3oxZWZKdmFYMWpCSTJ" +
				"4WEdPSHFoSnBhWmVqCmgrbWFOODI5aG4ydHQ5a20xeFowT25aVmlhRnN6dUZMTUxyK1hwSF" +
				"FOd1JNeDFROGVjZWl2b29TNnR5UytuRDQKU2NqQy9Md0pGdjVTd0NoOHNlM00wMjFJek1RQ" +
				"khYN2daYnp4SUh6Wkt1czNNVGUzTTl3c0lnT255TGYxT29FRgp0L05xbFgrNlNGS0lVdE5R" +
				"b1dPRXYzZ1gzWWU2RzMvV2c0N3owVmcwamVhMlRNNWhudEhTZDlPb0RNR3U5MXBSCk5yYVM" +
				"rNmk4c1hsL2lsL3NpSTZUVXZacGZlMlFvZzZrK0ZSaUd2K3lhN3ZTZndJREFRQUJveU13SV" +
				"RBT0JnTlYKSFE4QkFmOEVCQU1DQWdRd0R3WURWUjBUQVFIL0JBVXdBd0VCL3pBTkJna3Foa" +
				"2lHOXcwQkFRc0ZBQU9DQVFFQQpnR2VUUzZBVTdFVGk3bVo5RjFGY3RMald5azI0d3U4d25r" +
				"bWVOVExuWFVXOVZFOUdnd2dJSGswRVFPcVNjOTdKCnU2c202UHpEcTRSTFJUVUR3RGZmdXk" +
				"xZnhmek84U2xGMmI2WktLZitqYnhSQXdzMmdPU3hiT3ZxTWdLVC9uUG8KSnM1ZUlKSnlGZn" +
				"p0UlYwTmxuYng1am1SMnRmZUtGZlI3MzNWQ0FHdXpqSkdrMFJEb1pJV0plaDNIOXQyQU9ne" +
				"Ap5aktFNXp6Z3IvR3JXZTZsU1J6SHdaTE1EYXpiUkNUcG5hZFhDZjZFVUExV0hYZDM4ZHpK" +
				"N3NJcnRFcnlmcE8wCk1kU0lHNWJmellIZXQzc203WU03OWNHUzRQdHVWbS9ESDhmSmNuTi8" +
				"xdkdacllDa1QyUTJKVlN5WnBJU2JJMzgKMmVMS0RyN1pnTWlFTFBvMTlwK1lCUT09Ci0tLS" +
				"0tRU5EIENFUlRJRklDQVRFLS0tLS0K"),
		},
	}

	// ErrMultiplePodsRunning represents a failure to resolve a service to a
	// single pod because it has multiple pods running. This can happen when
	// Kubernetes is transitioning between Deployment versions, and hasn't
	// terminated the previous pod before starting the new one.
	ErrMultiplePodsRunning = errors.NewFriendlyError("Multiple pods are running.\n" +
		"This is likely because Kubernetes is transitioning between states, " +
		"and should resolve itself eventually.")

	// ErrMultiplePodsSpecified represents a failure to resolve a service to a
	// single pod because the service definition defines multiple pods. This
	// can happen when a service has multiple Deployments, or if the Deployment
	// has a Replicas count higher than one.
	ErrMultiplePodsSpecified = errors.NewFriendlyError("Service defines multiple pods, " +
		"which isn't supported by Kelda. To resolve, either:\n" +
		"- Contact your administrator to split the service up\n" +
		"- Or use `kubectl` directly.")

	// ErrMissingServiceName is used by commands that expect users to provide a
	// service name.
	ErrMissingServiceName = errors.NewFriendlyError("A service name is required. " +
		"Check the `kelda dev` UI for a list of possible " +
		"service names.")

	// ErrTooManyServices is used by commands that expect users to provide
	// exactly one service name.
	ErrTooManyServices = errors.NewFriendlyError("Only one service name at a time is supported.")
)

// ClearProgress clears the progress printer output so any errors are easier
// to read when they're printed to stdout.
// The `\033` character denotes the start of the control sequence, the
// `[2K` clears the text on the current line, and the `\r` moves the cursor
// back to the beginning of the line.
const ClearProgress = "\033[2K\r"

// GetKubeClient creates a Kubernetes config and client for a given kubeconfig context.
func GetKubeClient(context string) (kubernetes.Interface, *rest.Config, error) {
	if context == "" {
		return nil, nil, errors.New("context is required. Set it with `kelda config`")
	}

	// Use the hardcoded demo cluster credentials if the user specifies the
	// demo context. Otherwise, get the credentials from the user's local
	// kubeconfig.
	var restConfig *rest.Config
	if context == config.KeldaDemoContext {
		restConfig = &demoClusterConfig
	} else {
		var err error
		restConfig, err = GetKubeConfig(context).ClientConfig()
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "context") && strings.Contains(msg, "does not exist") {
				return nil, nil, errors.NewFriendlyError(ContextNotExistsTemplate, context)
			}
			return nil, nil, errors.WithContext(err, "get config")
		}
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, errors.WithContext(err, "new client")
	}
	return client, restConfig, nil
}

// GetKubeConfig parses the ClientConfig stored in the local system for `context`.
func GetKubeConfig(context string) clientcmd.ClientConfig {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: context}
	return clientcmd.
		NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
}

// ResolvePodName resolves a service name to a pod name.
func ResolvePodName(kc keldaClientset.Interface,
	namespace, id string) (string, string, error) {
	msList, err := kc.KeldaV1alpha1().Microservices(namespace).List(metav1.ListOptions{})
	if err != nil {
		return "", "", errors.WithContext(err, "get list of services")
	}

	var serviceList []string
	ind := -1
	for i, ms := range msList.Items {
		if ms.ObjectMeta.Name == id {
			ind = i
			break
		}
		serviceList = append(serviceList, ms.ObjectMeta.Name)
	}
	if ind == -1 {
		sort.Strings(serviceList)
		return "", "", errors.NewFriendlyError("Service %q does not exist. "+
			"Only the following services are currently deployed: [%s]",
			id, strings.Join(serviceList, ", "))
	}
	ms := msList.Items[ind]

	pods := kube.SelectPods(ms.Status.Actual, false)
	switch len(pods) {
	case 0:
		return "", "", errors.New("no matching pods")
	case 1:
		return pods[0].Name, pods[0].Spec.Containers[0].Name, nil
	}

	if countPodSpecs(ms.Status.Actual) > 1 {
		return "", "", ErrMultiplePodsSpecified
	}
	return "", "", ErrMultiplePodsRunning
}

func countPodSpecs(objectTrees []*kube.Object) (numSpecs int32) {
	for _, objectTree := range objectTrees {
		switch obj := objectTree.Object.(type) {
		case *appsv1.Deployment:
			numSpecs += *obj.Spec.Replicas
		case *appsv1.StatefulSet:
			numSpecs += *obj.Spec.Replicas
		case *appsv1.DaemonSet:
			numSpecs += obj.Status.DesiredNumberScheduled
		}
		numSpecs += countPodSpecs(objectTree.Children)
	}
	return
}

// HandleFatalError handles errors that are severe enough to terminate the
// program.
func HandleFatalError(err error) {
	fmt.Fprintln(os.Stderr, errors.GetPrintableMessage(err))
	os.Exit(1)
}

// HandlePanic catches panics and logs them to analytics. It's meant to be
// invoked in a `defer` statement for goroutines that might panic.
func HandlePanic() {
	if r := recover(); r != nil {
		panic(r)
		// Don't report analytics.
		//analytics.Log.WithField("call stack", getStackTrace()).Panic(r)
	}
}

// PromptYesOrNo prompts the specified string and let the user choose whether
// they want or don't want to proceed.
func PromptYesOrNo(prompt string) (bool, error) {
	for {
		fmt.Print(prompt + " (y/N) ")
		var resp string
		fmt.Scanln(&resp)

		// Trim the newline from the input.
		resp = strings.TrimRight(resp, "\n")
		switch resp {
		case "", "n", "N":
			return false, nil
		case "y", "Y":
			return true, nil
		}
	}
}

func mustDecodeBase64(encoded string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic(err)
	}
	return decoded
}
