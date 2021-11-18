package magda

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/examples"
	"github.com/sidkik/kelda-v1/ci/util"

	// Load the client authentication plugin necessary for connecting to GKE.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func Test(t *testing.T, helper *util.TestHelper) {
	testCtx, cancelTest := context.WithCancel(context.Background())
	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, "--no-sync")
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	t.Run("Logs", func(t *testing.T) { TestLogs(testCtx, t, helper) })
	t.Run("FollowLogs", func(t *testing.T) { TestFollowLogs(testCtx, t, helper) })
}

func TestLogs(ctx context.Context, t *testing.T, helper *util.TestHelper) {
	gatewayEndpoint := GetEndpointForMagdaService("gateway") + "?logsTest"
	testURL := "http://localhost:8080" + gatewayEndpoint
	expLog := fmt.Sprintf("GET %s", gatewayEndpoint)

	// Trigger the log.
	if _, err := http.Get(testURL); !assert.NoError(t, err, "http get") {
		return
	}

	logCtx, _ := context.WithTimeout(ctx, 1*time.Minute)
	stdout, err := helper.Run(logCtx, "logs", "gateway")
	require.NoError(t, err, "kelda logs")

	assert.Contains(t, string(stdout), expLog)
}

func TestFollowLogs(ctx context.Context, t *testing.T, helper *util.TestHelper) {
	gatewayEndpoint := GetEndpointForMagdaService("gateway") + "?followLogsTest"
	testURL := "http://localhost:8080" + gatewayEndpoint
	expLog := fmt.Sprintf("GET %s", gatewayEndpoint)

	logCtx, cancelLogs := context.WithTimeout(ctx, 1*time.Minute)
	defer cancelLogs()
	stdout, waitErr, err := helper.Start(logCtx, "logs", "-f", "gateway")
	require.NoError(t, err, "`kelda logs` failed to start")

	go func() {
		assert.NoError(t, <-waitErr, "`kelda logs` crashed")
	}()

	// Clear stdout.
	stdoutReader := util.NewStreamReader(stdout)
	readCtx, _ := context.WithTimeout(ctx, 30*time.Second)
	_, err = stdoutReader.ReadUntilTimeout(readCtx, 5*time.Second)
	require.NoError(t, err)

	// Trigger the log.
	_, err = http.Get(testURL)
	require.NoError(t, err, "http get")

	// Ensure the log showed up in the logs.
	readCtx, _ = context.WithTimeout(ctx, 30*time.Second)
	newOutput, err := stdoutReader.ReadUntilTimeout(readCtx, 5*time.Second)
	require.NoError(t, err)

	assert.Contains(t, string(newOutput), expLog)
}

func TestMagdaCodeChange(ctx context.Context, t *testing.T, helper *util.TestHelper, serviceName, servicePath string) {
	endpoint := "http://localhost:8080" + GetEndpointForMagdaService(serviceName)
	examples.CodeChangeTest{
		ServicePath:          servicePath,
		CodePath:             "src/setupIntegrationTest.js",
		InitialResponseCheck: keldaAssert.HTTPGetShouldEqual(endpoint, "before code change"),
		ChangedResponseCheck: keldaAssert.HTTPGetShouldEqual(endpoint, "after code change"),
		FileChange:           examples.Replace("before code change", "after code change"),
	}.Test(ctx, t, helper)
}

// GetEndpointForMagdaService returns the endpoint for a special endpoint used
// for the integration tests.
func GetEndpointForMagdaService(service string) string {
	return fmt.Sprintf("/integration-test-%s", service)
}
