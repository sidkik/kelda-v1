package restart

import (
	"context"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kelda-inc/kelda/ci/magda"
	"github.com/kelda-inc/kelda/ci/util"

	// Load the client authentication plugin necessary for connecting to GKE.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func Test(t *testing.T, helper *util.TestHelper) {
	testCtx, cancelTest := context.WithCancel(context.Background())

	webServerPath := filepath.Join(helper.ExamplesRepoPath, "magda/magda-web-server")

	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, webServerPath)
	require.NoError(t, err, "start kelda dev")

	log.Info("Killing kelda dev")
	cancelDev()
	require.NoError(t, <-waitErr, "run kelda dev initial")

	log.Info("Reattaching kelda dev")
	devCtx, cancelDev = context.WithCancel(testCtx)
	defer cancelDev()

	waitErr, err = helper.Dev(devCtx, webServerPath)
	require.NoError(t, err, "start kelda dev again")

	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev again")
		cancelTest()
	}()

	t.Run("CodeChange", func(t *testing.T) { magda.TestMagdaCodeChange(testCtx, t, helper, "web-server", webServerPath) })
	t.Run("Logs", func(t *testing.T) { magda.TestLogs(testCtx, t, helper) })
	t.Run("FollowLogs", func(t *testing.T) { magda.TestFollowLogs(testCtx, t, helper) })
}
