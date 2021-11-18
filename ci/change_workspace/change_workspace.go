package change_workspace

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/util"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

// Test tests that changes to tunnels and Kubernetes manifests
// are properly deployed after restarting `kelda dev`.
func Test(t *testing.T, helper *util.TestHelper) {
	testCtx, _ := context.WithCancel(context.Background())
	gatewayPath := filepath.Join(helper.ExamplesRepoPath, "magda/magda-gateway")

	// Boot `kelda dev`.
	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, gatewayPath)
	require.NoError(t, err, "start kelda dev")

	log.Info("Updating the workspace tunnels and web deployment version")

	// Update the workspace config to have a tunnel to the web-server.
	// Restore the workspace after the test.
	workspacePath := helper.User.Workspace
	origWorkspace, err := ioutil.ReadFile(workspacePath)
	require.NoError(t, err)
	defer ioutil.WriteFile(workspacePath, origWorkspace, 0644)

	webTunnel := config.Tunnel{
		ServiceName: "web-server",
		LocalPort:   8020,
		RemotePort:  80,
	}
	err = util.AddToWorkspace(workspacePath, []config.Tunnel{webTunnel})
	require.NoError(t, err, "update workspace.yaml")

	// The web service's cluster IP shouldn't change after we change the
	// deployment.
	serviceClient := helper.KubeClient.CoreV1().Services(helper.User.Namespace)
	origService, err := serviceClient.Get("web", metav1.GetOptions{})
	assert.NoError(t, err)

	// Update the web deployment so that it's running a different version of the
	// web-server container.
	// Restore the deployment after the test.
	deploymentPath := filepath.Join(filepath.Dir(workspacePath),
		"web-server", "deployment-web.yaml")
	origDeployment, err := ioutil.ReadFile(deploymentPath)
	require.NoError(t, err)
	defer ioutil.WriteFile(deploymentPath, origDeployment, 0644)

	require.NoError(t, updateDeploymentImage(deploymentPath,
		"gcr.io/magda-221800/magda-web-server:integration-test-v1"), "update deployment")

	// Restart the `kelda dev` process.
	log.Info("Restarting `kelda dev` to pick up the modifications")
	cancelDev()
	require.NoError(t, <-waitErr, "initial kelda dev process")

	devCtx, cancelDev = context.WithCancel(testCtx)
	waitErr, err = helper.Dev(devCtx, gatewayPath)
	require.NoError(t, err, "start kelda dev")
	defer cancelDev()

	go func() {
		assert.NoError(t, <-waitErr, "second kelda dev process")
	}()

	// Test that accessing the newly created tunnel hits the new web-server
	// version.
	log.Info("Checking service version")
	endpoint := fmt.Sprintf("http://localhost:%d/integration-test-web-server", webTunnel.LocalPort)
	result := keldaAssert.HTTPGetShouldEqual(endpoint, "integration test version")()
	assert.NoError(t, result, "access service")

	newService, err := serviceClient.Get("web", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, origService.Spec, newService.Spec)
}

func updateDeploymentImage(path, image string) error {
	deployment, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.WithContext(err, "read")
	}

	// Use a capture group to preserve leading whitespace.
	imageRegex := regexp.MustCompile(`(?m:^(\s*image: )\S*$)`)

	// As a sanity check, check that the regex actually matches something.
	numMatches := len(imageRegex.FindAll(deployment, -1))
	if numMatches != 1 {
		return fmt.Errorf("bad regex. Expected 1 match, got %d", numMatches)
	}

	// Perform the replacement.
	replacement := fmt.Sprintf(`${1}%s`, image)
	newDeployment := imageRegex.ReplaceAll(deployment, []byte(replacement))
	if err := ioutil.WriteFile(path, newDeployment, 0644); err != nil {
		return errors.WithContext(err, "write")
	}
	return nil
}
