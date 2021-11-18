package guess_dev_command

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keldaAssert "github.com/sidkik/kelda-v1/ci/assert"
	"github.com/sidkik/kelda-v1/ci/examples"
	"github.com/sidkik/kelda-v1/ci/util"
)

// Test tests that Kelda properly enters development mode for
// services that have a command specified in the service kelda.yaml, but no
// command specified in the workspace deployment.
// This mirrors the setup used by SupplyPike.
func Test(t *testing.T, helper *util.TestHelper) {
	testCtx, cancelTest := context.WithCancel(context.Background())
	servicePath := filepath.Join(helper.CIRootPath, "guess_dev_command/assets")

	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, servicePath)
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	endpoint := "http://localhost:3000"
	preChange := "Hello World!"
	postChange := "After code change"
	t.Run("CodeChange", func(t *testing.T) {
		examples.CodeChangeTest{
			ServicePath:          servicePath,
			CodePath:             "app.js",
			FileChange:           examples.Replace(preChange, postChange),
			InitialResponseCheck: keldaAssert.HTTPGetShouldEqual(endpoint, preChange),
			ChangedResponseCheck: keldaAssert.HTTPGetShouldEqual(endpoint, postChange),
		}.Test(testCtx, t, helper)
	})
}
