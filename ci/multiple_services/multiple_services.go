package multiple_services

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keldaAssert "github.com/kelda-inc/kelda/ci/assert"
	"github.com/kelda-inc/kelda/ci/examples"
	"github.com/kelda-inc/kelda/ci/magda"
	"github.com/kelda-inc/kelda/ci/util"
)

func Test(t *testing.T, helper *util.TestHelper) {
	testCtx, _ := context.WithCancel(context.Background())

	webServerPath := filepath.Join(helper.ExamplesRepoPath, "magda/magda-web-server")
	gatewayPath := filepath.Join(helper.ExamplesRepoPath, "magda/magda-gateway")

	type service struct{ name, path string }
	type test struct {
		name          string
		shouldSync    []service
		shouldNotSync []service
	}

	getServicePaths := func(svcs []service) (paths []string) {
		for _, svc := range svcs {
			paths = append(paths, svc.path)
		}
		return
	}

	runTest := func(test test) {
		t.Run(test.name, func(t *testing.T) {
			devCtx, cancelDev := context.WithCancel(testCtx)
			waitErr, err := helper.Dev(devCtx, getServicePaths(test.shouldSync)...)
			require.NoError(t, err, "start kelda dev")

			// Wait for tunnels to point at any new versions of services.
			// When the Gateway switches from dev to regular mode in the
			// JustWebServer test, the gateway gets restarted, so it's possible
			// for the tunnel status to be Up, but for it to be not actually
			// usable since it's still pointing at the previous pod.
			// We sleep here to give Kelda time to switch the tunnel to the
			// right pod.
			time.Sleep(30 * time.Second)

			for _, svc := range test.shouldSync {
				svc := svc
				testName := fmt.Sprintf("%s-Syncs", svc.name)
				t.Run(testName, func(t *testing.T) {
					magda.TestMagdaCodeChange(testCtx, t, helper, svc.name, svc.path)
				})
			}

			for _, svc := range test.shouldNotSync {
				svc := svc
				testName := fmt.Sprintf("%s-DoesNotSync", svc.name)
				t.Run(testName, func(t *testing.T) {
					testCodeNotSynced(testCtx, t, helper, svc.name, svc.path)
				})
			}

			cancelDev()
			assert.NoError(t, <-waitErr, "run kelda dev")
		})
	}

	gateway := service{"gateway", gatewayPath}
	webServer := service{"web-server", webServerPath}

	runTest(test{
		name:          "JustGateway",
		shouldSync:    []service{gateway},
		shouldNotSync: []service{webServer},
	})

	runTest(test{
		name:       "GatewayAndWebServer",
		shouldSync: []service{gateway, webServer},
	})

	runTest(test{
		name:          "JustWebServer",
		shouldSync:    []service{webServer},
		shouldNotSync: []service{gateway},
	})
}

func testCodeNotSynced(ctx context.Context, t *testing.T, helper *util.TestHelper, serviceName, servicePath string) {
	var testEndpoint = "http://localhost:8080/integration-test-" + serviceName
	const defaultCode = "before code change"
	var testPath = filepath.Join(servicePath, "src/setupIntegrationTest.js")

	// Restore the test file after the test.
	origContents, err := ioutil.ReadFile(testPath)
	require.NoError(t, err, "read")
	defer func() {
		require.NoError(t, ioutil.WriteFile(testPath, origContents, 0644), "restore")
	}()

	log.Info("Modifying file")
	require.NoError(t, examples.ModifyFile(testPath, examples.Replace(defaultCode, "should not be synced")))

	log.Info("Check that code doesn't sync")
	waitCtx, _ := context.WithTimeout(ctx, 30*time.Second)
	assert.Equal(t, util.ErrNeverSynced, helper.WaitUntilSynced(waitCtx, servicePath))

	log.Info("Checking code wasn't deployed")
	waitCtx, _ = context.WithTimeout(ctx, 1*time.Minute)
	notDeployed := func() bool {
		if err := keldaAssert.HTTPGetShouldEqual(testEndpoint, defaultCode)(); err != nil {
			log.WithError(err).Error("Unexpected response from service")
			return false
		}
		return true
	}
	if !util.TestWithRetry(waitCtx, nil, notDeployed) {
		t.Error("Unexpected response from service")
	}
}
