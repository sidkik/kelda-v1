package update

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	dockerClient "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/util"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

const (
	testUpdateImageURL      = "gcr.io/magda-221800/test-update"
	testUpdateImage1URL     = testUpdateImageURL + ":1"
	testUpdateImage2URL     = testUpdateImageURL + ":2"
	testUpdateImageStageURL = testUpdateImageURL + ":stage"

	testUpdate1Contents = "update test 1\n"
	testUpdate2Contents = "update test 2\n"

	updateEndpoint = "http://localhost:8081"
)

const runIn = "circle-1"

func Test(t *testing.T, helper *util.TestHelper) {
	if helper.User.Namespace != runIn {
		t.Skipf("Skipping this test because it will only run in namespace %q, "+
			"but the current namespace is %q", runIn, helper.User.Namespace)
	}

	testCtx, cancelTest := context.WithCancel(context.Background())

	registryAuth, err := getDockerRegistryAuth()
	require.NoError(t, err)

	dClient, err := dockerClient.NewEnvClient()
	require.NoError(t, err)

	log.Infof("Retagging %s as %s", testUpdateImage1URL, testUpdateImageStageURL)
	dockerRetag(t, dClient, registryAuth, testUpdateImage1URL, testUpdateImageStageURL)

	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, "--no-sync")
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	log.Info("Running `kelda update` for the first time. There should be no update.")
	runKeldaUpdate(t, false)
	assertUpdateHTTP(t, helper, testUpdate1Contents)

	log.Infof("Retagging %s as %s", testUpdateImage2URL, testUpdateImageStageURL)
	dockerRetag(t, dClient, registryAuth, testUpdateImage2URL, testUpdateImageStageURL)

	log.Info("Running `kelda update` for the second time. There should be an update.")
	runKeldaUpdate(t, true)
	assertUpdateHTTP(t, helper, testUpdate2Contents)

	log.Info("Running `kelda update` for the third time. There should be no update.")
	runKeldaUpdate(t, false)
	assertUpdateHTTP(t, helper, testUpdate2Contents)
}

// getDockerRegistryAuth returns a Docker authentication header value based on the
// GCLOUD_SERVICE_KEY environment variable.
func getDockerRegistryAuth() (string, error) {
	base64ServiceKey, ok := os.LookupEnv("GCLOUD_SERVICE_KEY")
	if !ok {
		return "", errors.New("GCLOUD_SERVICE_KEY is required")
	}

	serviceKey, err := base64.StdEncoding.DecodeString(base64ServiceKey)
	if err != nil {
		return "", errors.WithContext(err, "base64 decode")
	}

	authJSON, err := json.Marshal(dockerTypes.AuthConfig{
		Username: "_json_key",
		Password: string(serviceKey),
	})
	if err != nil {
		return "", errors.WithContext(err, "encode json")
	}

	return base64.URLEncoding.EncodeToString(authJSON), nil
}

func assertUpdateHTTP(t *testing.T, helper *util.TestHelper, exp string) {
	ctx, _ := context.WithTimeout(context.Background(), 2*time.Minute)
	if !assert.NoError(t, helper.WaitUntilReady(ctx)) {
		return
	}

	// Wait for tunnels to point at the new version of the test-update image.
	// When the images changes, the pod name changes, so we sleep here to give
	// Kelda time to switch the tunnel to the right pod.
	time.Sleep(30 * time.Second)

	assert.NoError(t, keldaAssert.HTTPGetShouldEqual(updateEndpoint, exp)())
}

func runKeldaUpdate(t *testing.T, expUpdate bool) {
	cmd := exec.Command("kelda", "update")
	cmd.Stdin = bytes.NewBuffer([]byte("y\n"))
	stdout, err := cmd.CombinedOutput()
	require.NoError(t, err)

	stdoutStr := string(stdout)
	if expUpdate {
		require.Contains(t, stdoutStr, "Update initiated. "+
			"Check `kelda dev` to track the update progress.")
	} else {
		require.Contains(t, stdoutStr, "All images are up to date.")
	}
}

func dockerRetag(t *testing.T, client dockerClient.ImageAPIClient, registryAuth, oldRef, newRef string) {
	ctx := context.Background()

	// Ignore any errors because the images might not exist.
	log.Infof("Removing %s and %s", oldRef, newRef)
	removeOptions := dockerTypes.ImageRemoveOptions{Force: true}
	client.ImageRemove(ctx, oldRef, removeOptions)
	client.ImageRemove(ctx, newRef, removeOptions)

	log.Infof("Pulling %s", oldRef)
	stdout, err := client.ImagePull(ctx, oldRef, dockerTypes.ImagePullOptions{RegistryAuth: registryAuth})
	require.NoError(t, err)
	// Read until EOF to wait for the pull to finish.
	_, err = ioutil.ReadAll(stdout)
	require.NoError(t, err)
	stdout.Close()

	log.Infof("Tagging %s as %s", oldRef, newRef)
	require.NoError(t, client.ImageTag(ctx, oldRef, newRef))

	log.Infof("Pushing %s", newRef)
	stdout, err = client.ImagePush(ctx, newRef, dockerTypes.ImagePushOptions{RegistryAuth: registryAuth})
	require.NoError(t, err)
	// Read until EOF to wait for the push to finish.
	_, err = ioutil.ReadAll(stdout)
	require.NoError(t, err)
	stdout.Close()
}
