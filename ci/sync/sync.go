package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kelda-inc/kelda/ci/util"
	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
)

// syncTestServiceName must match the service name in the test workspace.
const syncTestServiceName = "sync-test"

func Test(t *testing.T, helper *util.TestHelper) {
	t.Run("SyncDestinations", func(t *testing.T) {
		testSyncDestinations(t, helper)
	})
	t.Run("FileChange", func(t *testing.T) {
		testFileChange(t, helper)
	})
	t.Run("ChangeSyncConfig", func(t *testing.T) {
		testChangeSyncConfig(t, helper)
	})
	t.Run("Ignore", func(t *testing.T) {
		testIgnore(t, helper)
	})
}

func testFileChange(t *testing.T, helper *util.TestHelper) {
	testCtx, cancelTest := context.WithCancel(context.Background())

	refFile := randomFile("test-file")
	changedContents := refFile.WithContents("changed contents")
	changedFileMode := refFile.WithMode(os.FileMode(0600))
	changedModTime := refFile.WithModTime(refFile.modTime.Add(1 * time.Minute))

	tests := []struct {
		name   string
		change fsOp
		check  func(*util.TestHelper, string) error
	}{
		{
			name:   "ChangeContents",
			change: createFile(changedContents),
			check:  shouldExist(changedContents),
		},
		{
			name:   "ChangeMode",
			change: createFile(changedFileMode),
			check:  shouldExist(changedFileMode),
		},
		{
			name:   "ChangeModTime",
			change: createFile(changedModTime),
			check:  shouldExist(changedModTime),
		},
		{
			name:   "RemoveFile",
			change: removeFile(refFile.path),
			check:  shouldNotExist(refFile),
		},
	}

	fs, err := newMockFs()
	require.NoError(t, err)

	// Create another file in the directory so that when the test file is
	// removed, we're not left with a completely empty sync.
	// This is necessary because the sync server doesn't restart the process if
	// there aren't any synced files.
	require.NoError(t, createFile(randomFile("other-file"))(fs))

	restartTracker := NewRestartTracker(helper, syncTestServiceName)
	devCfg := config.SyncConfig{
		Version: config.SupportedSyncConfigVersion,
		Name:    syncTestServiceName,
		Sync: []config.SyncRule{
			{From: ".", To: "."},
		},
		Command: []string{"true"},
	}
	require.NoError(t, fs.writeSyncConfig(devCfg))

	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, fs.serviceDir)
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, createFile(refFile)(fs))
			require.NoError(t, helper.WaitUntilSynced(testCtx, fs.serviceDir))

			// Update the restart tracker so that the check below will ignore
			// restarts from before the test changes.
			_, err := restartTracker.HasRestarted()
			require.NoError(t, err)

			require.NoError(t, test.change(fs))
			require.NoError(t, helper.WaitUntilSynced(testCtx, fs.serviceDir))

			assert.NoError(t, test.check(helper, devCfg.Name))

			restarted, err := restartTracker.HasRestarted()
			require.NoError(t, err)
			assert.True(t, restarted, "change should trigger a restart")
		})
	}
}

func testChangeSyncConfig(t *testing.T, helper *util.TestHelper) {
	file1 := randomFile("file1")
	file2 := randomFile("file2")
	file3 := randomFile("file3")
	localFiles := []file{file1, file2, file3}

	restartTracker := NewRestartTracker(helper, syncTestServiceName)
	refSyncConfig := config.SyncConfig{
		Name: syncTestServiceName,
		Sync: []config.SyncRule{
			{From: "file1", To: "."},
			{From: "file2", To: "."},
		},
		Command: []string{"true"},
	}

	tests := []struct {
		name         string
		syncConfig   config.SyncConfig
		expectations []serviceAssertion
	}{
		{
			name: "RemoveSyncRule",
			syncConfig: config.SyncConfig{
				Name: syncTestServiceName,
				Sync: []config.SyncRule{
					{From: "file1", To: "."},
				},
				Command: refSyncConfig.Command,
			},
			expectations: []serviceAssertion{
				shouldExist(file1),
				shouldNotExist(file2),
				shouldNotExist(file3),
			},
		},
		{
			name: "AddSyncRule",
			syncConfig: config.SyncConfig{
				Name: syncTestServiceName,
				Sync: []config.SyncRule{
					{From: "file1", To: "."},
					{From: "file2", To: "."},
					{From: "file3", To: "."},
				},
				Command: refSyncConfig.Command,
			},
			expectations: []serviceAssertion{
				shouldExist(file1),
				shouldExist(file2),
				shouldExist(file3),
			},
		},
		{
			name: "ChangeSyncCommand",
			syncConfig: config.SyncConfig{
				Name:    syncTestServiceName,
				Sync:    refSyncConfig.Sync,
				Command: []string{"echo", "changed command"},
			},
			expectations: []serviceAssertion{
				shouldExist(file1),
				shouldExist(file2),
				shouldNotExist(file3),
			},
		},
	}

	fs, err := newMockFs()
	require.NoError(t, err)
	defer fs.cleanup()

	for _, f := range localFiles {
		require.NoError(t, createFile(f)(fs))
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			testCtx, cancelTest := context.WithCancel(context.Background())

			// Boot with local ref sync config.
			require.NoError(t, fs.writeSyncConfig(refSyncConfig))

			devCtx, cancelDev := context.WithCancel(testCtx)
			waitErr, err := helper.Dev(devCtx, fs.serviceDir)
			require.NoError(t, err, "start kelda dev")
			cancelDev()
			require.NoError(t, <-waitErr)

			require.NoError(t, fs.writeSyncConfig(test.syncConfig))

			// Update the restart tracker so that the check below will ignore
			// restarts from before the test changes.
			_, err = restartTracker.HasRestarted()
			require.NoError(t, err)

			devCtx, cancelDev = context.WithCancel(testCtx)
			waitErr, err = helper.Dev(devCtx, fs.serviceDir)
			require.NoError(t, err, "start kelda dev")
			go func() {
				assert.NoError(t, <-waitErr, "run kelda dev")
				cancelTest()
			}()
			defer cancelDev()

			require.NoError(t, helper.WaitUntilSynced(testCtx, fs.serviceDir))

			restarted, err := restartTracker.HasRestarted()
			require.NoError(t, err)
			assert.True(t, restarted, "change should trigger a restart")

			for _, expectation := range test.expectations {
				assert.NoError(t, expectation(helper, test.syncConfig.Name))
			}
		})
	}
}

func getRemoteFile(helper *util.TestHelper, service, path string) (file, bool, error) {
	existsCmd := fmt.Sprintf("if [ -f %q ]; then echo exists; else echo does not exist; fi", path)
	existsOutput, err := helper.Exec(service, []string{"sh", "-c", existsCmd})
	if err != nil {
		return file{}, false, errors.WithContext(err, "check whether exists")
	}

	switch strings.TrimSuffix(existsOutput, "\n") {
	case "exists":
	case "does not exist":
		return file{}, false, nil
	default:
		return file{}, false, fmt.Errorf("unexpected output in exists check: %s", existsOutput)
	}

	contents, err := helper.Exec(service, []string{"cat", path})
	if err != nil {
		return file{}, false, errors.WithContext(err, "cat")
	}

	fileInfoStr, err := helper.Exec(service, []string{"stat", "-c", "%a %Y", path})
	if err != nil {
		return file{}, false, errors.WithContext(err, "ls")
	}

	infoParts := strings.Split(strings.TrimSuffix(fileInfoStr, "\n"), " ")
	if len(infoParts) != 2 {
		return file{}, false, fmt.Errorf("unexpected output in stat: %s", fileInfoStr)
	}

	octalMode, err := strconv.ParseUint(infoParts[0], 8, 32)
	if err != nil {
		return file{}, false, errors.WithContext(err, "parse file mode")
	}

	parsedModTime, err := strconv.Atoi(infoParts[1])
	if err != nil {
		return file{}, false, errors.WithContext(err, "parse mod time")
	}

	return file{
		path:     path,
		contents: contents,
		mode:     os.FileMode(octalMode),
		modTime:  time.Unix(int64(parsedModTime), 0).UTC(),
	}, true, nil
}

func createRemoteFile(helper *util.TestHelper, service string, f file) error {
	mkdirCmd := []string{"mkdir", "-p", filepath.Dir(f.path)}
	if _, err := helper.Exec(service, mkdirCmd); err != nil {
		return errors.WithContext(err, "mkdir")
	}

	writeCmd := fmt.Sprintf("printf %q > %q", f.contents, f.path)
	if _, err := helper.Exec(service, []string{"sh", "-c", writeCmd}); err != nil {
		return errors.WithContext(err, "write")
	}

	chmodCmd := []string{"chmod", fmt.Sprintf("%#o", f.mode), f.path}
	if _, err := helper.Exec(service, chmodCmd); err != nil {
		return errors.WithContext(err, "chmod")
	}

	// Touch expects times in the format "YYYYMMDDhhmm.SS".
	timeFormat := "200601021504.05"
	touchCmd := []string{"touch", "-m", "-t", f.modTime.Format(timeFormat), f.path}
	if _, err := helper.Exec(service, touchCmd); err != nil {
		return errors.WithContext(err, "touch")
	}
	return nil
}

type serviceAssertion func(helper *util.TestHelper, service string) error

func shouldExist(exp file) func(*util.TestHelper, string) error {
	return func(helper *util.TestHelper, service string) error {
		actual, exists, err := getRemoteFile(helper, service, exp.path)
		if err != nil {
			return errors.WithContext(err, "get remote file")
		}

		if !exists {
			return fmt.Errorf("file %q does not exist", exp.path)
		}

		if actual != exp {
			return fmt.Errorf("Expected file %v, got %v", exp, actual)
		}
		return nil
	}
}

func shouldNotExist(exp file) func(*util.TestHelper, string) error {
	return func(helper *util.TestHelper, service string) error {
		_, exists, err := getRemoteFile(helper, service, exp.path)
		if err != nil {
			return errors.WithContext(err, "get remote file")
		}

		if exists {
			return fmt.Errorf("file %q exists", exp.path)
		}
		return nil
	}
}
