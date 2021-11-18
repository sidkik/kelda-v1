// +build ci

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/change_workspace"
	"github.com/sidkik/kelda-v1/ci/examples"
	"github.com/sidkik/kelda-v1/ci/guess_dev_command"
	"github.com/sidkik/kelda-v1/ci/magda"
	"github.com/sidkik/kelda-v1/ci/multiple_services"
	"github.com/sidkik/kelda-v1/ci/restart"
	"github.com/sidkik/kelda-v1/ci/sync"
	"github.com/sidkik/kelda-v1/ci/update"
	"github.com/sidkik/kelda-v1/ci/util"

	"github.com/sidkik/kelda-v1/pkg/config"
	minionClient "github.com/sidkik/kelda-v1/pkg/minion/client"
)

type TestFunction func(*testing.T, *util.TestHelper)

const artifactsDir = "/tmp/artifacts"

func TestKelda(t *testing.T) {
	homedir.DisableCache = true

	examplesRepoPath, ok := os.LookupEnv("CI_EXAMPLES_REPO_PATH")
	if !ok {
		t.Error("missing required environment variable CI_EXAMPLES_REPO_PATH")
		return
	}

	ciRootPath, ok := os.LookupEnv("CI_ROOT_PATH")
	if !ok {
		t.Error("missing required environment variable CI_ROOT_PATH")
		return
	}

	noDeleteNsStr, ok := os.LookupEnv("CI_NO_DELETE_NAMESPACE")
	if !ok {
		t.Error("missing required environment variable CI_NO_DELETE_NAMESPACE")
		return
	}

	noDeleteNs := noDeleteNsStr == "true"

	tests := []struct {
		name      string
		workspace string
		testFn    TestFunction
	}{
		{
			name:      "Logs",
			workspace: filepath.Join(examplesRepoPath, "magda/magda-kelda-config/workspace.yaml"),
			testFn:    magda.Test,
		},
		{
			name:      "Restart",
			workspace: filepath.Join(examplesRepoPath, "magda/magda-kelda-config/workspace.yaml"),
			testFn:    restart.Test,
		},
		{
			name:      "MultipleServices",
			workspace: filepath.Join(examplesRepoPath, "magda/magda-kelda-config/workspace.yaml"),
			testFn:    multiple_services.Test,
		},
		{
			name:      "ChangeWorkspace",
			workspace: filepath.Join(examplesRepoPath, "magda/magda-kelda-config/workspace.yaml"),
			testFn:    change_workspace.Test,
		},
		{
			name:      "Update",
			workspace: filepath.Join(ciRootPath, "update/assets/workspace.yaml"),
			testFn:    update.Test,
		},
		{
			name:      "GuessDevCommand",
			workspace: filepath.Join(ciRootPath, "guess_dev_command/assets/workspace.yaml"),
			testFn:    guess_dev_command.Test,
		},
		{
			name:      "FileSync",
			workspace: filepath.Join(ciRootPath, "sync/assets/workspace.yaml"),
			testFn:    sync.Test,
		},
		{
			name: "DjangoPolls",
			workspace: filepath.Join(examplesRepoPath,
				"django-polls/kelda-workspace/workspace.yaml"),
			testFn: examples.ExampleTest{
				DevServices: []string{
					filepath.Join(examplesRepoPath, "django-polls/src"),
				},
				ExpectedServices: []string{"db", "polls", "setup-db"},
				CodeChangeTests: []examples.CodeChangeTest{
					{
						ServicePath:          filepath.Join(examplesRepoPath, "django-polls/src"),
						CodePath:             "polls/templates/polls/index.html",
						FileChange:           examples.DeleteLine(13, 19),
						InitialResponseCheck: keldaAssert.HTTPGetShouldNotContain("http://localhost:8000", "<li>Cats: 0</li>"),
						ChangedResponseCheck: keldaAssert.HTTPGetShouldContain("http://localhost:8000", "<li>Cats: 0</li>"),
					},
				},
			}.Test,
		},
		{
			name: "GolangSockShop",
			workspace: filepath.Join(examplesRepoPath,
				"golang-sock-shop/kelda-config/workspace.yaml"),
			testFn: examples.ExampleTest{
				DevServices: []string{
					filepath.Join(examplesRepoPath, "golang-sock-shop/catalogue"),
				},
				ExpectedServices: []string{
					"carts", "carts-db", "catalogue", "catalogue-db", "front-end",
					"orders", "orders-db", "payment", "queue-master", "rabbitmq",
					"session-db", "shipping", "user", "user-db",
				},
				CodeChangeTests: []examples.CodeChangeTest{
					{
						ServicePath: filepath.Join(examplesRepoPath, "golang-sock-shop/catalogue"),
						CodePath:    "service.go",

						// Uncomment the sample code change.
						FileChange: examples.Replace("\t\t// DEMO: Set the price to always be zero.\n\t\t//", ""),

						InitialResponseCheck: keldaAssert.HTTPGetShouldContain("http://localhost:8081/catalogue", "99.99"),
						ChangedResponseCheck: keldaAssert.HTTPGetShouldNotContain("http://localhost:8081/catalogue", "99.99"),
					},
				},
				Tunnels: []config.Tunnel{
					{ServiceName: "catalogue", LocalPort: 8081, RemotePort: 80},
				},
			}.Test,
		},
		{
			name: "Magda",
			workspace: filepath.Join(examplesRepoPath,
				"magda/magda-kelda-config/workspace.yaml"),
			testFn: examples.ExampleTest{
				DevServices: []string{
					filepath.Join(examplesRepoPath, "magda/magda-web-server"),
				},
				CodeChangeTests: []examples.CodeChangeTest{
					{
						ServicePath: filepath.Join(examplesRepoPath, "magda/magda-web-server"),
						CodePath:    "src/setupIntegrationTest.js",
						FileChange:  examples.Replace("before code change", "after code change"),
						InitialResponseCheck: keldaAssert.HTTPGetShouldEqual(
							"http://localhost:8080"+magda.GetEndpointForMagdaService("web-server"),
							"before code change"),
						ChangedResponseCheck: keldaAssert.HTTPGetShouldEqual(
							"http://localhost:8080"+magda.GetEndpointForMagdaService("web-server"),
							"after code change"),
					},
				},
				ExpectedServices: []string{
					"authorization-api", "combined-db", "content-api", "elasticsearch", "gateway",
					"indexer", "preview-map", "registry-api", "search-api", "web-server",
				},
			}.Test,
		},
		{
			name: "NodeTodo",
			workspace: filepath.Join(examplesRepoPath,
				"node-todo/kelda-workspace/workspace.yaml"),
			testFn: examples.ExampleTest{
				DevServices: []string{
					filepath.Join(examplesRepoPath, "node-todo/web-server"),
				},
				ExpectedServices: []string{"mongodb", "web-server"},
			}.Test,
		},
		{
			name: "NodeTodoWithVolumes",
			workspace: filepath.Join(examplesRepoPath,
				"node-todo-with-volumes/kelda-workspace/workspace.yaml"),
			testFn: examples.ExampleTest{
				ExpectedServices: []string{"mongodb", "web-server"},
			}.Test,
		},
		{
			name: "ReactCalculator",
			workspace: filepath.Join(examplesRepoPath,
				"react-calculator/kelda-config/workspace.yaml"),
			testFn: examples.ExampleTest{
				DevServices: []string{
					filepath.Join(examplesRepoPath, "react-calculator/calculator"),
				},
				ExpectedServices: []string{"calculator"},
			}.Test,
		},
		{
			name: "Kustomize",
			workspace: filepath.Join(examplesRepoPath,
				"kustomize/kelda-config/workspace.yaml"),
			testFn: examples.ExampleTest{
				ExpectedServices: []string{"hello"},
			}.Test,
		},
	}

	require.NoError(t, os.MkdirAll(artifactsDir, 0755))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Try to restore the original workspace path after the test.
			userCfg, err := config.ParseUser()
			if err == nil {
				defer config.WriteUser(userCfg)
			} else {
				log.WithError(err).Warn("Failed to parse current user config. " +
					"The workspace field will not be restored after the tests, " +
					"but we will still run them.")
			}
			userCfg.Workspace = test.workspace
			require.NoError(t, config.WriteUser(userCfg), "update workspace path")

			defer func() {
				path := filepath.Join(artifactsDir, test.name+".tar.gz")
				output, err := exec.Command("kelda", "bug-tool", "--out", path).CombinedOutput()
				if err != nil {
					log.WithError(err).
						WithField("output", string(output)).
						Warn("Failed to create debug archive")
				}
			}()

			if !noDeleteNs {
				log.Info("Deleting test namespace")
				output, err := exec.Command("kelda", "delete").CombinedOutput()
				require.NoError(t, err, "kelda delete: %s", string(output))
			}

			helper, err := util.NewTestHelper(examplesRepoPath, ciRootPath)
			require.NoError(t, err)

			watchCtx, cancelWatch := context.WithCancel(context.Background())
			defer cancelWatch()
			go helper.WatchForErrors(watchCtx, t)

			test.testFn(t, helper)
			t.Run("MinionStatus", func(t *testing.T) { testMinionStatus(t, helper) })
			t.Run("MinionLogs", func(t *testing.T) { testMinionLogs(t, helper) })
			t.Run("CLILogs", func(t *testing.T) { testCLILogs(t, helper) })
		})
	}
}

func testMinionStatus(t *testing.T, helper *util.TestHelper) {
	selector := fmt.Sprintf("%s=%s",
		minionClient.MinionLabelSelectorKey,
		minionClient.MinionLabelSelectorValue)
	listOpts := metav1.ListOptions{LabelSelector: selector}
	pods, err := helper.KubeClient.CoreV1().Pods(minionClient.KeldaNamespace).List(listOpts)
	require.NoError(t, err, "list pods")

	for _, pod := range pods.Items {
		assert.Equal(t, corev1.PodRunning, pod.Status.Phase, "pod should be running")
		for _, container := range pod.Status.ContainerStatuses {
			assert.Zero(t, container.RestartCount, "container should not have crashed")
		}
	}
}

func testCLILogs(t *testing.T, helper *util.TestHelper) {
	logPath := filepath.Join(filepath.Dir(helper.User.Workspace), "kelda.log")
	logs, err := ioutil.ReadFile(logPath)
	require.NoError(t, err)
	assertNoErrorOrWarningLogs(t, string(logs))
}

func testMinionLogs(t *testing.T, helper *util.TestHelper) {
	// Get the minion pod.
	selector := fmt.Sprintf("%s=%s",
		minionClient.MinionLabelSelectorKey,
		minionClient.MinionLabelSelectorValue)
	listOpts := metav1.ListOptions{LabelSelector: selector}
	pods, err := helper.KubeClient.CoreV1().Pods(minionClient.KeldaNamespace).List(listOpts)
	require.NoError(t, err, "list pods")
	require.Len(t, pods.Items, 1)

	// Read the minion logs.
	logsReader, err := helper.KubeClient.CoreV1().
		Pods(minionClient.KeldaNamespace).
		GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{}).Stream()
	require.NoError(t, err, "create logs stream")
	defer logsReader.Close()

	logs, err := ioutil.ReadAll(logsReader)
	require.NoError(t, err, "read logs stream")

	assertNoErrorOrWarningLogs(t, string(logs))
}

var logWhitelist = []string{
	"Tunnel crashed. Will recreate.",
	"Failed to update analytics",
}

func assertNoErrorOrWarningLogs(t *testing.T, log string) {
Outer:
	for _, line := range strings.Split(log, "\n") {
		for _, pattern := range logWhitelist {
			if strings.Contains(line, pattern) {
				continue Outer
			}
		}

		assert.NotContains(t, line, "level=warning", "unexpected warning log")
		assert.NotContains(t, line, "level=error", "unexpected error log")
	}
}
