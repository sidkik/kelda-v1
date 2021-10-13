package examples

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

// ExampleTest is used for testing our example applications.
type ExampleTest struct {
	// The services that we expect to get deployed from the Workspace.
	ExpectedServices []string

	// The services that should be run in development mode.
	DevServices []string

	// Additional tunnels to add to the Workspace.
	Tunnels []config.Tunnel

	CodeChangeTests []CodeChangeTest
}

// CodeChangeTest tests that Kelda properly syncs files by modifying a file,
// and checking the HTTP response.
type CodeChangeTest struct {
	// The path to the directory containing the service to test.
	ServicePath string

	// The path to the file that we're going to change.
	CodePath string

	// The modification that should be made to CodePath.
	FileChange FileModifier

	// The expected response before the code is changed.
	InitialResponseCheck keldaAssert.Assertion

	// The expected response after the code is changed.
	ChangedResponseCheck keldaAssert.Assertion
}

func (test ExampleTest) Test(t *testing.T, helper *util.TestHelper) {
	// Update the workspace config to deploy the additional tunnels.
	// Restore the workspace after the test.
	workspacePath := helper.User.Workspace
	origWorkspace, err := ioutil.ReadFile(workspacePath)
	require.NoError(t, err)
	defer ioutil.WriteFile(workspacePath, origWorkspace, 0644)
	require.NoError(t, util.AddToWorkspace(workspacePath, test.Tunnels))

	devArgs := test.DevServices
	if len(test.DevServices) == 0 {
		devArgs = []string{"--no-sync"}
	}

	testCtx, cancelTest := context.WithCancel(context.Background())
	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, devArgs...)
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	for _, codeChangeTest := range test.CodeChangeTests {
		t.Run("CodeChange", func(t *testing.T) {
			codeChangeTest.Test(testCtx, t, helper)
		})
	}

	t.Run("ExpectedServices", func(t *testing.T) {
		msList, err := helper.KeldaClient.KeldaV1alpha1().
			Microservices(helper.User.Namespace).
			List(metav1.ListOptions{})
		require.NoError(t, err)

		expServicesMap := map[string]struct{}{}
		for _, svc := range test.ExpectedServices {
			expServicesMap[svc] = struct{}{}
		}

		for _, actualSvc := range msList.Items {
			if _, ok := expServicesMap[actualSvc.Name]; !ok {
				t.Errorf("Unexpected service: %s", actualSvc.Name)
			}
			delete(expServicesMap, actualSvc.Name)
		}

		for svc := range expServicesMap {
			t.Errorf("Expected service %q but it wasn't booted", svc)
		}
	})

	t.Run("CanLog", func(t *testing.T) {
		for _, svc := range test.ExpectedServices {
			_, err = helper.Run(testCtx, "logs", svc)
			require.NoErrorf(t, err, "kelda logs %s", svc)
		}
	})
}

func (test CodeChangeTest) Test(ctx context.Context, t *testing.T, helper *util.TestHelper) {
	// Restore the test file after the test.
	codePath := filepath.Join(test.ServicePath, test.CodePath)
	origContents, err := ioutil.ReadFile(codePath)
	require.NoError(t, err, "read")
	defer func() {
		require.NoError(t, ioutil.WriteFile(codePath, origContents, 0644), "restore")

		// Block until the service restarts after the sync. This way,
		// subsequent tests that depend on this service won't fail while Kelda
		// syncs the code update.
		// An example of this is in the MultipleServices/GatewayAndWebServer test.
		// The web-server depends on the gateway to forward the GET request
		// used by the test, but because the web-server test is run immediately
		// after the gateway test, it was possible for the gateway to be
		// unresponsive during the web-server test.
		waitCtx, _ := context.WithTimeout(ctx, 1*time.Minute)
		assert.NoError(t, helper.WaitUntilSynced(waitCtx, test.ServicePath))
	}()

	// Give a generous timeout for the first sync since it might install
	// dependencies.
	log.Info("Waiting for initial code to sync")
	waitCtx, _ := context.WithTimeout(ctx, 3*time.Minute)
	assert.NoError(t, helper.WaitUntilSynced(waitCtx, test.ServicePath))

	// Wait for the service to start listening (it takes some time for the
	// service to actually initialize and bind to the port within the container).
	time.Sleep(20 * time.Second)

	log.Info("Running initial check")
	assert.NoError(t, test.InitialResponseCheck())

	log.Info("Modifying file")
	require.NoError(t, ModifyFile(codePath, test.FileChange))

	log.Info("Waiting for new code to sync")
	waitCtx, _ = context.WithTimeout(ctx, 1*time.Minute)
	assert.NoError(t, helper.WaitUntilSynced(waitCtx, test.ServicePath))

	// Wait for it to be ready.
	waitCtx, _ = context.WithTimeout(ctx, 1*time.Minute)
	assert.NoError(t, helper.WaitUntilReady(waitCtx))

	// Wait for the service to start listening.
	time.Sleep(20 * time.Second)

	log.Info("Checking new code was deployed")
	waitCtx, _ = context.WithTimeout(ctx, 1*time.Minute)
	deployed := func() bool {
		err := test.ChangedResponseCheck()
		if err != nil {
			log.WithError(err).Error("Modified code isn't deployed. Will retry.")
			return false
		}
		return true
	}
	if !util.TestWithRetry(waitCtx, nil, deployed) {
		t.Error("Code wasn't deployed")
	}
}

type FileModifier func(string) string

func ModifyFile(path string, modFn FileModifier) error {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.WithContext(err, "read")
	}

	err = ioutil.WriteFile(path, []byte(modFn(string(f))), 0644)
	if err != nil {
		return errors.WithContext(err, "write")
	}
	return nil
}

func Replace(currStr, newStr string) FileModifier {
	return func(f string) string {
		return strings.Replace(f, currStr, newStr, -1)
	}
}

func DeleteLine(linesToDelete ...int) FileModifier {
	return func(f string) string {
		var resultLines []string
		for i, line := range strings.Split(f, "\n") {
			var shouldSkip bool
			for _, toDelete := range linesToDelete {
				if i+1 == toDelete {
					shouldSkip = true
					break
				}
			}
			if shouldSkip {
				continue
			}

			resultLines = append(resultLines, line)
		}

		return strings.Join(resultLines, "\n")
	}
}
