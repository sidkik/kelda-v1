package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"testing"

	logrusTest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
)

func TestPromptUser(t *testing.T) {
	tests := []struct {
		name                                                 string
		helpString, prompt, defaultAnswer, currAnswer, stdin string
		expPrompt, expResult                                 string
	}{
		{
			name:          "No default or current answer",
			helpString:    "explanation",
			prompt:        "prompt",
			defaultAnswer: "",
			currAnswer:    "",
			stdin:         "user input\n",
			expPrompt: "explanation\n" +
				"prompt:\n" +
				"Please enter manually: \n",
			expResult: "user input",
		},
		{
			name:          "No default answer only, chose current answer",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "",
			currAnswer:    "current answer",
			stdin:         "1\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. current answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expResult: "current answer",
		},
		{
			name:          "No default answer only, enter manually",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "",
			currAnswer:    "current answer",
			stdin: "2\n" +
				"user input\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. current answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: " +
				"Please enter manually: \n",
			expResult: "user input",
		},
		{
			name:          "No current answer only, chose default answer",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "",
			stdin:         "1\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expResult: "default answer",
		},
		{
			name:          "No current answer only, enter manually",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "",
			stdin: "2\n" +
				"user input\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: " +
				"Please enter manually: \n",
			expResult: "user input",
		},
		{
			name:          "Same default answer and current answer, chose default answer",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "default answer",
			stdin:         "1\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expResult: "default answer",
		},
		{
			name:          "Same default answer and current answer, enter manually",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "default answer",
			stdin: "2\n" +
				"user input",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: " +
				"Please enter manually: \n",
			expResult: "user input",
		},
		{
			name:          "Different default answer and current answer, chose default answer",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "current answer",
			stdin:         "1\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. current answer\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n",
			expResult: "default answer",
		},
		{
			name:          "Empty response -- pick default",
			helpString:    "help",
			prompt:        "prompt",
			defaultAnswer: "one",
			currAnswer:    "two",
			stdin:         "\n",
			expPrompt: "help\n" +
				"prompt:\n" +
				"\n" +
				"\t1. one (recommended)\n" +
				"\t2. two\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n",
			expResult: "one",
		},
		{
			name:          "Different default answer and current answer, chose current answer",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "current answer",
			stdin:         "2\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. current answer\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n",
			expResult: "current answer",
		},
		{
			name:          "Different default answer and current answer, enter manually",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "current answer",
			stdin: "3\n" +
				"user input\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. current answer\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please enter manually: \n",
			expResult: "user input",
		},
		{
			name:          "Invalid input",
			helpString:    "different explanation",
			prompt:        "different prompt",
			defaultAnswer: "default answer",
			currAnswer:    "current answer",
			stdin: "invalid input\n" +
				"1\n",
			expPrompt: "different explanation\n" +
				"different prompt:\n" +
				"\n" +
				"\t1. default answer (recommended)\n" +
				"\t2. current answer\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please choose one [1-3]: \n",
			expResult: "default answer",
		},
	}

	type promptUserResult struct {
		resp string
		err  error
	}
	for _, test := range tests {
		// Setup mocks.
		out := bytes.NewBuffer(nil)
		stdinReader, stdinWriter := io.Pipe()
		stdout = out
		stdin = stdinReader

		// Start the promptUser function.
		resultChan := make(chan promptUserResult)
		go func() {
			resp, err := promptUser(test.helpString, test.prompt,
				test.defaultAnswer, test.currAnswer)
			resultChan <- promptUserResult{resp, err}
		}()

		// Provide the user input.
		fmt.Fprintln(stdinWriter, test.stdin)

		// Check that promptUser behaved as expected.
		result := <-resultChan
		assert.NoError(t, result.err, test.name)
		assert.Equal(t, test.expResult, result.resp, test.name)

		// Test the prompt after `promptUser` has exited so that we can be sure
		// we're not testing before `promptUser` has a chance to print to stdout.
		assert.Equal(t, test.expPrompt, out.String(), test.name)
	}
}

func TestNamespaceValidation(t *testing.T) {
	invalidCharacterPrompt := "This namespace contains invalid characters. " +
		"Please pick another namespace that only  " +
		"uses the following characters:\n" +
		"1) lowercase letters (a-z) \n" +
		"2) numbers (0-9) \n" +
		"3) - \n" +
		"Please ensure that your chosen namespace " +
		"does not start or end with the `-` character."
	invalidLenPrompt := "The namespace name must not be more than 63 characters. " +
		"Please pick another namespace."
	tests := []struct {
		name          string
		input         string
		expInputValid bool
		expPrompt     string
	}{
		{
			name:          "invalid - . in middle",
			input:         "dev.test",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - . in beginning",
			input:         ".devtest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - . at end",
			input:         "devtest.",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - interior spaces",
			input:         "dev test",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - space at beginning",
			input:         " devtest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - space at end",
			input:         "devtest ",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - capital letter in middle",
			input:         "devAest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - capital letter in beginning",
			input:         "Devtest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - capital letter at end",
			input:         "devtesT",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - special character in middle",
			input:         "dev!est",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - special character at beginning",
			input:         "!evtest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - special character at end",
			input:         "devtes!",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - `-` at end",
			input:         "devtes-",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "invalid - `-` at beginning",
			input:         "-evtest",
			expInputValid: false,
			expPrompt:     invalidCharacterPrompt,
		},
		{
			name:          "valid - `-` in middle",
			input:         "dev-test",
			expInputValid: true,
			expPrompt:     "",
		},
		{
			name:          "valid - example name",
			input:         "devtest1",
			expInputValid: true,
			expPrompt:     "",
		},
		{
			name:          "valid - example name w/ `-`",
			input:         "devtest-123",
			expInputValid: true,
			expPrompt:     "",
		},
		{
			name: "invalid - name too long",
			//The namespace must be shorter than 63 characters.
			input:         strings.Repeat("xy", 32),
			expInputValid: false,
			expPrompt:     invalidLenPrompt,
		},
	}

	for _, test := range tests {
		prompt, ok := namespaceValidationFn(test.input)
		assert.Equal(t, ok, test.expInputValid, test.name)
		assert.Equal(t, prompt, test.expPrompt, test.name)
	}
}

func TestNamespaceSanitization(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expOutput string
	}{
		{
			name:      "already valid 1",
			input:     "qwerty",
			expOutput: "qwerty",
		},
		{
			name:      "already valid 2",
			input:     "123qwerty789",
			expOutput: "123qwerty789",
		},
		{
			name:      "already valid 3",
			input:     "123-qwerty-789",
			expOutput: "123-qwerty-789",
		},
		{
			name:      "convert to lowercase",
			input:     "123-QwerTY-789",
			expOutput: "123-qwerty-789",
		},
		{
			name:      "remove garbage characters after lowercase conversion",
			input:     "!@#qW-er&*()ty",
			expOutput: "qw-erty",
		},
		{
			name:      "remove leading hyphen",
			input:     "-qwer-ty",
			expOutput: "qwer-ty",
		},
		{
			name:      "remove trailing hyphen",
			input:     "qwer-ty-",
			expOutput: "qwer-ty",
		},
		{
			name:      "remove leading and trailing hyphen",
			input:     "-qwer-ty-",
			expOutput: "qwer-ty",
		},
		{
			name:      "leading hyphen after invalid characters",
			input:     "!@#-qwerty",
			expOutput: "qwerty",
		},
		{
			name:      "truncate after removal",
			input:     "--abcdefghijklm^^^^^nopqrstuvwxyzabcdefghijklmnopqrstuvwxyz-Abcdefghijkl--",
			expOutput: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz-abcdefghij",
		},
		{
			name:      "single character",
			input:     "q",
			expOutput: "q",
		},
	}

	for _, test := range tests {
		sanitized := sanitizeNamespace(test.input)
		assert.Equal(t, sanitized, test.expOutput, test.name)
	}
}

func TestGenerateConfig(t *testing.T) {
	tests := []struct {
		name                string
		cliOpts             config.User
		defaults            config.User
		mockParseUserConfig func() (config.User, error)
		inputs              []string
		expPrompt           string
		expConfig           config.User
	}{
		{
			name: "Initial setup -- ~/.kelda.yaml doesn't exist yet",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{}, errors.FileNotFound{}
			},
			inputs: []string{"1\n", "1\n", "1\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
		},
		{
			name: "When ~/.kelda.yaml exists, prefer default values",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{
					Context:   "current-context",
					Namespace: "current-namespace",
					Workspace: "current-workspace",
				}, nil
			},
			inputs: []string{"1\n", "1\n", "1\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. current-context\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. current-namespace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. current-workspace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
		},
		{
			name: "When ~/.kelda.yaml exists, prefer its values",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{
					Context:   "current-context",
					Namespace: "current-namespace",
					Workspace: "current-workspace",
				}, nil
			},
			inputs: []string{"2\n", "2\n", "2\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. current-context\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. current-namespace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. current-workspace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n",
			expConfig: config.User{
				Context:   "current-context",
				Namespace: "current-namespace",
				Workspace: "current-workspace",
			},
		},
		{
			name: "User input has ultimate precedence",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{
					Context:   "current-context",
					Namespace: "current-namespace",
					Workspace: "current-workspace",
				}, nil
			},
			inputs: []string{"3\nuser-context\n", "3\nuser-namespace\n", "3\nuser-workspace\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. current-context\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please enter manually: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. current-namespace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please enter manually: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. current-workspace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please enter manually: \n",
			expConfig: config.User{
				Context:   "user-context",
				Namespace: "user-namespace",
				Workspace: "user-workspace",
			},
		},
		{
			name: "Combination of all three inputs",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{
					Namespace: "current-namespace",
					Workspace: "current-workspace",
				}, nil
			},
			inputs: []string{"1\n", "2\n", "3\nuser-workspace\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. current-namespace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. current-workspace\n" +
				"\t3. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-3]: " +
				"Please enter manually: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "current-namespace",
				Workspace: "user-workspace",
			},
		},
		{
			name: "Namespace cannot be named kelda",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{}, errors.FileNotFound{}
			},
			inputs: []string{"1\n", "2\n", "kelda\n", "1\n", "1\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: Please enter manually: \n" +
				"The `kelda` namespace is reserved for the Kelda minion. Please pick another namespace.\n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
		},
		{
			name: "Namespace cannot contain invalid characters (.)",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{}, errors.FileNotFound{}
			},
			inputs: []string{"1\n", "2\n", "dev.test\n", "1\n", "1\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: Please enter manually: \n" +
				"This namespace contains invalid characters. " +
				"Please pick another namespace that only  " +
				"uses the following characters:\n" +
				"1) lowercase letters (a-z) \n" +
				"2) numbers (0-9) \n" +
				"3) - \n" +
				"Please ensure that your chosen namespace " +
				"does not start or end with the `-` character.\n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
		},
		{
			name: "Namespace is longer than 63 characters",
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{}, errors.FileNotFound{}
			},
			inputs: []string{"1\n", "2\n", fmt.Sprintf("%s\n", strings.Repeat("a", 64)), "1\n", "1\n"},
			expPrompt: "Enter the `kubectl` context for the development cluster.\n" +
				"This cluster should be running the Kelda minion.\n" +
				"It defaults to the current context.\n" +
				"Development context:\n" +
				"\n" +
				"\t1. default-context (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: Please enter manually: \n" +
				"The namespace name must not be more than 63 characters. " +
				"Please pick another namespace.\n" +
				"Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n" +
				"Enter the path to the Kelda workspace configuration (which describes how to boot your application).\n" +
				"It defaults to the configuration located in the current directory.\n" +
				"Path to Kelda Workspace file:\n" +
				"\n" +
				"\t1. default-workspace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expConfig: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
		},
		{
			name: "Some fields set explicitly with CLI flags",
			cliOpts: config.User{
				Context:   "cli-context",
				Workspace: "cli-workspace",
			},
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{}, errors.FileNotFound{}
			},
			inputs: []string{"1\n"},
			expPrompt: "Enter the namespace to use for development.\n" +
				"Your deployment will be isolated from other developers based on this namespace.\n" +
				"Any string is valid as long as it's unique within the Kubernetes cluster.\n" +
				"Development namespace:\n" +
				"\n" +
				"\t1. default-namespace (recommended)\n" +
				"\t2. (Enter manually)\n" +
				"\n" +
				"Please choose one [1-2]: \n",
			expConfig: config.User{
				Context:   "cli-context",
				Namespace: "default-namespace",
				Workspace: "cli-workspace",
			},
		},
		{
			name: "All fields set explicitly with CLI flags",
			cliOpts: config.User{
				Context:   "cli-context",
				Namespace: "cli-namespace",
				Workspace: "cli-workspace",
			},
			defaults: config.User{
				Context:   "default-context",
				Namespace: "default-namespace",
				Workspace: "default-workspace",
			},
			mockParseUserConfig: func() (config.User, error) {
				return config.User{
					Context:   "curr-context",
					Namespace: "curr-namespace",
					Workspace: "curr-workspace",
				}, nil
			},
			expConfig: config.User{
				Context:   "cli-context",
				Namespace: "cli-namespace",
				Workspace: "cli-workspace",
			},
		},
	}

	type generateConfigResult struct {
		cfg config.User
		err error
	}

	for _, test := range tests {
		test := test

		// Setup mocks.
		out := bytes.NewBuffer(nil)
		stdinReader, stdinWriter := io.Pipe()
		stdout = out
		stdin = stdinReader
		guessDefaults = func() config.User { return test.defaults }
		parseUserConfig = test.mockParseUserConfig

		// Start the generateConfig function.
		resultChan := make(chan generateConfigResult)
		go func() {
			resp, err := generateConfig(test.cliOpts)
			resultChan <- generateConfigResult{resp, err}
		}()

		// Provide the user input.
		for _, input := range test.inputs {
			fmt.Fprint(stdinWriter, input)
		}

		// Check that generateConfig behaved as expected.
		result := <-resultChan
		assert.NoError(t, result.err, test.name)
		assert.Equal(t, test.expConfig, result.cfg, test.name)

		// Test the prompt after `generateConfig` has exited so that we can be sure
		// we're not testing before `generateConfig` has a chance to print to stdout.
		assert.Equal(t, test.expPrompt, out.String(), test.name)
	}
}

func TestGuessDefaults(t *testing.T) {
	tests := []struct {
		name                string
		parseUserConfig     func() (config.User, error)
		stat                func(string) (os.FileInfo, error)
		getWorkingDirectory func() (string, error)
		loadKubeconfig      func() (*clientcmdapi.Config, error)
		getCurrentUser      func() (*user.User, error)
		expCfg              config.User
		expLogs             []string
	}{
		{
			name: "Success case",
			parseUserConfig: func() (config.User, error) {
				return config.User{}, nil
			},
			getWorkingDirectory: func() (string, error) {
				return "wd", nil
			},
			stat: func(path string) (os.FileInfo, error) {
				assert.Equal(t, "wd/workspace.yaml", path)
				return nil, nil
			},
			loadKubeconfig: func() (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{CurrentContext: "context"}, nil
			},
			getCurrentUser: func() (*user.User, error) {
				return &user.User{Username: "username"}, nil
			},
			expCfg: config.User{
				Namespace: "username",
				Workspace: "wd/workspace.yaml",
				Context:   "context",
			},
		},
		{
			name: "Current namespace takes precedence",
			parseUserConfig: func() (config.User, error) {
				return config.User{Namespace: "curr-ns"}, nil
			},
			getWorkingDirectory: func() (string, error) {
				return "wd", nil
			},
			stat: func(path string) (os.FileInfo, error) {
				assert.Equal(t, "wd/workspace.yaml", path)
				return nil, nil
			},
			loadKubeconfig: func() (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{CurrentContext: "context"}, nil
			},
			getCurrentUser: func() (*user.User, error) {
				return &user.User{Username: "username"}, nil
			},
			expCfg: config.User{
				Namespace: "curr-ns",
				Workspace: "wd/workspace.yaml",
				Context:   "context",
			},
		},
		{
			name: "Failure case",
			parseUserConfig: func() (config.User, error) {
				return config.User{}, nil
			},
			getWorkingDirectory: func() (string, error) {
				return "", errors.New("error")
			},
			stat: func(path string) (os.FileInfo, error) {
				return nil, errors.New("error")
			},
			loadKubeconfig: func() (*clientcmdapi.Config, error) {
				return nil, errors.New("error")
			},
			getCurrentUser: func() (*user.User, error) {
				return nil, errors.New("error")
			},
			expCfg: config.User{
				Namespace: "",
				Workspace: "",
				Context:   "",
			},
			expLogs: []string{
				"Failed to guess namespace",
				"Failed to guess context",
				"Failed to guess workspace",
			},
		},
		{
			name: "Don't log error when workspace file doesn't exist",
			parseUserConfig: func() (config.User, error) {
				return config.User{}, nil
			},
			getWorkingDirectory: func() (string, error) {
				return "wd", nil
			},
			stat: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			loadKubeconfig: func() (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{CurrentContext: "context"}, nil
			},
			getCurrentUser: func() (*user.User, error) {
				return &user.User{Username: "username"}, nil
			},
			expCfg: config.User{
				Namespace: "username",
				Context:   "context",
			},
		},
	}

	for _, test := range tests {
		// Setup mocks.
		parseUserConfig = test.parseUserConfig
		stat = test.stat
		getWorkingDirectory = test.getWorkingDirectory
		loadKubeconfig = test.loadKubeconfig
		getCurrentUser = test.getCurrentUser
		logHook := logrusTest.NewGlobal()

		assert.Equal(t, test.expCfg, guessDefaultsImpl(), test.name)
		assert.Len(t, logHook.Entries, len(test.expLogs), test.name)
		for i, log := range test.expLogs {
			assert.Equal(t, log, logHook.Entries[i].Message, test.name)
		}
	}
}

func TestGetters(t *testing.T) {
	configCmd := New()
	namespaceCmd, _, err := configCmd.Find([]string{"get-namespace"})
	assert.NoError(t, err)
	contextCmd, _, err := configCmd.Find([]string{"get-context"})
	assert.NoError(t, err)

	expNamespace := "namespace"
	expContext := "context"
	parseUserConfig = func() (config.User, error) {
		return config.User{
			Namespace: expNamespace,
			Context:   expContext,
		}, nil
	}

	out := bytes.NewBuffer(nil)
	stdout = out

	namespaceCmd.Run(nil, nil)
	contextCmd.Run(nil, nil)
	assert.Equal(t, fmt.Sprintf("%s\n%s\n", expNamespace, expContext), out.String())
}
