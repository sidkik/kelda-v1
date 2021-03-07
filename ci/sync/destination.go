package sync

import (
	"context"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kelda-inc/kelda/ci/util"
	"github.com/kelda-inc/kelda/pkg/config"
)

type syncTest struct {
	name                   string
	rules                  []config.SyncRule
	preExistingRemoteFiles []remoteFile
	expSyncs               []expSync
}

type remoteFile struct {
	path         string
	shouldChange bool
}

type expSync struct {
	local  string
	remote string
}

func testSyncDestinations(t *testing.T, helper *util.TestHelper) {
	tests := []syncTest{
		{
			name: "FileToFile",
			rules: []config.SyncRule{
				{From: "rel-file", To: "rel-file-1"},
				{From: "rel-dir/file", To: "rel-file-2"},
				{From: "/abs-file", To: "rel-file-3"},
				{From: "to-abs-dest", To: "/abs-file"},
				{From: "to-existing-file", To: "existing-file"},
			},
			expSyncs: []expSync{
				{local: "rel-file", remote: "rel-file-1"},
				{local: "rel-dir/file", remote: "rel-file-2"},
				{local: "/abs-file", remote: "rel-file-3"},
				{local: "to-abs-dest", remote: "/abs-file"},
				{local: "to-existing-file", remote: "existing-file"},
			},
			preExistingRemoteFiles: []remoteFile{
				{path: "existing-file", shouldChange: true},
			},
		},
		{
			name: "FileToDir",
			rules: []config.SyncRule{
				{From: "to-root", To: "/"},
				{From: "to-existing", To: "existing-dir"},
				{From: "/abs-file", To: "new-dir/new-file"},
				{From: "to-curr-dir", To: "."},
			},
			expSyncs: []expSync{
				{local: "to-root", remote: "/to-root"},
				{local: "to-existing", remote: "existing-dir/to-existing"},
				{local: "/abs-file", remote: "new-dir/new-file"},
				{local: "to-curr-dir", remote: "to-curr-dir"},
			},
			preExistingRemoteFiles: []remoteFile{
				{path: "existing-dir/existing-file", shouldChange: false},
			},
		},
		{
			name: "FromDir",
			rules: []config.SyncRule{
				{From: "rel-dir", To: "rel-dir-dest"},
				{From: "to-existing-dir", To: "existing-dir"},
				{From: "/abs-dir", To: "/abs-dir"},
			},
			expSyncs: []expSync{
				{local: "rel-dir/file1", remote: "rel-dir-dest/file1"},
				{local: "rel-dir/file2", remote: "rel-dir-dest/file2"},
				{local: "rel-dir/subdir/file3", remote: "rel-dir-dest/subdir/file3"},

				{local: "to-existing-dir/file1", remote: "existing-dir/file1"},
				{local: "to-existing-dir/file2", remote: "existing-dir/file2"},
				{local: "to-existing-dir/subdir/file3", remote: "existing-dir/subdir/file3"},

				{local: "/abs-dir/file1", remote: "/abs-dir/file1"},
				{local: "/abs-dir/file2", remote: "/abs-dir/file2"},
				{local: "/abs-dir/subdir/file3", remote: "/abs-dir/subdir/file3"},
			},
			preExistingRemoteFiles: []remoteFile{
				{path: "existing-dir/existing-file", shouldChange: false},
			},
		},
		{
			name: "FromCurrDir",
			rules: []config.SyncRule{
				{From: ".", To: "."},
				{From: ".", To: "new-dir"},
			},
			expSyncs: []expSync{
				{local: "file1", remote: "file1"},
				{local: "file2", remote: "file2"},
				{local: "subdir/file3", remote: "subdir/file3"},

				{local: "file1", remote: "new-dir/file1"},
				{local: "file2", remote: "new-dir/file2"},
				{local: "subdir/file3", remote: "new-dir/subdir/file3"},
			},
		},
		{
			name: "FromParentDir",
			rules: []config.SyncRule{
				{From: "../file", To: "file-dst"},
				{From: "../dir", To: "dir-dst"},
			},
			expSyncs: []expSync{
				{local: "../file", remote: "file-dst"},
				{local: "../dir/file1", remote: "dir-dst/file1"},
				{local: "../dir/file2", remote: "dir-dst/file2"},
			},
		},
		{
			name: "FromHomeDir",
			rules: []config.SyncRule{
				{From: "~/file", To: "file-dst"},
				{From: "~/dir", To: "dir-dst"},
			},
			expSyncs: []expSync{
				{local: "~/file", remote: "file-dst"},
				{local: "~/dir/file1", remote: "dir-dst/file1"},
				{local: "~/dir/file2", remote: "dir-dst/file2"},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			runSyncTest(t, helper, test)
		})
	}
}

func runSyncTest(t *testing.T, helper *util.TestHelper, test syncTest) {
	testCtx, cancelTest := context.WithCancel(context.Background())

	fs, err := newMockFs()
	require.NoError(t, err)
	defer fs.cleanup()

	// Convert the absolute paths in the test to be relative to the mock filesystem.
	for i, rule := range test.rules {
		if filepath.IsAbs(rule.From) {
			rule.From = fs.chroot(rule.From)
		}
		test.rules[i] = rule
	}

	// Boot Kelda with an empty dev config.
	log.Info("Doing an initial boot with an empty dev config to trigger pod creation")
	devCfg := config.SyncConfig{
		Version: config.SupportedSyncConfigVersion,
		Name:    syncTestServiceName,
		Command: []string{"true"},
	}
	require.NoError(t, fs.writeSyncConfig(devCfg))

	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, fs.serviceDir)
	require.NoError(t, err, "start kelda dev")
	cancelDev()
	require.NoError(t, <-waitErr)

	// Setup the test files.
	log.Info("Setting up pod filesystem with pre existing files")
	remoteFiles := map[string]file{}
	for _, toCreate := range test.preExistingRemoteFiles {
		f := randomFile(toCreate.path)
		remoteFiles[f.path] = f
		require.NoError(t, createRemoteFile(helper, devCfg.Name, f))
	}

	log.Info("Setting up the local files")
	localFiles := map[string]file{}
	for _, expFile := range test.expSyncs {
		// Don't recreate the file if we already created it.
		if _, ok := localFiles[expFile.local]; ok {
			continue
		}

		f := randomFile(expFile.local)
		require.NoError(t, createFile(f)(fs))
		localFiles[f.path] = f
	}

	// Start Kelda again to sync the files.
	log.Info("Starting Kelda again with the sync config. It should sync the local files.")
	devCfg.Sync = test.rules
	require.NoError(t, fs.writeSyncConfig(devCfg))

	devCtx, cancelDev = context.WithCancel(testCtx)
	waitErr, err = helper.Dev(devCtx, fs.serviceDir)
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	require.NoError(t, helper.WaitUntilSynced(testCtx, fs.serviceDir))

	// Check that the expected files were changed.
	log.Info("Checking that the sync completed correctly.")
	for _, f := range test.expSyncs {
		remoteFile, exists, err := getRemoteFile(helper, devCfg.Name, f.remote)
		require.NoError(t, err)
		require.True(t, exists)

		expFile := localFiles[f.local].WithPath(f.remote)
		assert.Equal(t, expFile, remoteFile)
	}

	for _, f := range test.preExistingRemoteFiles {
		remoteFile, exists, err := getRemoteFile(helper, devCfg.Name, f.path)
		require.NoError(t, err)
		require.True(t, exists)

		if f.shouldChange {
			assert.NotEqual(t, remoteFiles[f.path], remoteFile)
		} else {
			assert.Equal(t, remoteFiles[f.path], remoteFile)
		}
	}
}

func testIgnore(t *testing.T, helper *util.TestHelper) {
	testCtx, cancelTest := context.WithCancel(context.Background())

	devCfg := config.SyncConfig{
		Version: config.SupportedSyncConfigVersion,
		Name:    syncTestServiceName,
		Sync: []config.SyncRule{
			{
				From:   "src",
				To:     "dst",
				Except: []string{"ignored-dir", "ignored-file"},
			},
		},
		Command: []string{"true"},
	}

	// Create the local files.
	fs, err := newMockFs()
	require.NoError(t, err)
	defer fs.cleanup()

	require.NoError(t, fs.writeSyncConfig(devCfg))
	require.NoError(t, createFile(randomFile("src/should-sync"))(fs))
	require.NoError(t, createFile(randomFile("src/ignored-file"))(fs))
	require.NoError(t, createFile(randomFile("src/ignored-dir/ignored-subfile"))(fs))

	// Start Kelda.
	devCtx, cancelDev := context.WithCancel(testCtx)
	waitErr, err := helper.Dev(devCtx, fs.serviceDir)
	require.NoError(t, err, "start kelda dev")
	go func() {
		assert.NoError(t, <-waitErr, "run kelda dev")
		cancelTest()
	}()
	defer cancelDev()

	require.NoError(t, helper.WaitUntilSynced(testCtx, fs.serviceDir))

	// Make sure that only `should-sync` synced.
	syncedFiles, err := helper.Exec(devCfg.Name, []string{"find", "dst"})
	require.NoError(t, err)

	expSyncedFiles := `dst
dst/should-sync
`
	assert.Equal(t, expSyncedFiles, syncedFiles)
}
