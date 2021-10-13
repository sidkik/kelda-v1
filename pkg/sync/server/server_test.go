package server

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kelda "github.com/sidkik/kelda-v1/pkg/crd/apis/kelda/v1alpha1"
	fakeKelda "github.com/sidkik/kelda-v1/pkg/crd/client/clientset/versioned/fake"
	"github.com/sidkik/kelda-v1/pkg/proto/dev"
	"github.com/sidkik/kelda-v1/pkg/sync"
)

func TestSyncDevStatus(t *testing.T) {
	namespace := "namespace"
	service := "service"
	specVersion := 5
	pod := "pod"

	startingDevStatus := kelda.DevStatus{Pod: pod}

	syncingDevStatus := kelda.DevStatus{
		Pod:            pod,
		RunningVersion: "runningVersion",
		TargetVersion:  "targetVersion",
	}

	tests := []struct {
		name         string
		currMs       kelda.Microservice
		newDevStatus kelda.DevStatus
		expDevStatus kelda.DevStatus
	}{
		{
			name: "FirstRun",
			currMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service,
					Namespace: namespace,
				},
				SpecVersion: specVersion,
			},
			newDevStatus: startingDevStatus,
			expDevStatus: startingDevStatus,
		},
		{
			name: "Update",
			currMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service,
					Namespace: namespace,
				},
				SpecVersion: specVersion,
				DevStatus:   startingDevStatus,
			},
			newDevStatus: syncingDevStatus,
			expDevStatus: syncingDevStatus,
		},
		{
			name: "Unchanged",
			currMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service,
					Namespace: namespace,
				},
				SpecVersion: specVersion,
				DevStatus:   syncingDevStatus,
			},
			newDevStatus: syncingDevStatus,
			expDevStatus: syncingDevStatus,
		},
		{
			name: "SpecVersion",
			currMs: kelda.Microservice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      service,
					Namespace: namespace,
				},
				SpecVersion: specVersion + 1,
				DevStatus:   kelda.DevStatus{Pod: "anotherPod"},
			},
			newDevStatus: syncingDevStatus,
			// The DevStatus isn't updated.
			expDevStatus: kelda.DevStatus{Pod: "anotherPod"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			s := server{
				keldaClient: fakeKelda.NewSimpleClientset(&test.currMs),
				specVersion: specVersion,
				service:     service,
				devStatus:   test.newDevStatus,
				namespace:   namespace,
			}
			require.NoError(t, s.syncDevStatusOnce())

			actual, err := s.keldaClient.KeldaV1alpha1().Microservices(namespace).Get(service, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, test.expDevStatus, actual.DevStatus)
		})
	}
}

func TestCopyFile(t *testing.T) {
	srcPath := "/src/hello/world"
	srcContents := []byte("srcContents")

	dstPath := "/dst/hello/world"

	fs = afero.NewMemMapFs()
	assert.NoError(t, afero.WriteFile(fs, srcPath, srcContents, 0644))
	assert.NoError(t, copyFileImpl(srcPath, dstPath))

	dstContents, err := afero.ReadFile(fs, dstPath)
	assert.NoError(t, err)
	assert.Equal(t, srcContents, dstContents)

	dstFileInfo, err := fs.Stat(dstPath)
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), dstFileInfo.Mode())
}

func TestCopyExecutableFile(t *testing.T) {
	srcPath := "/src/hello/world"
	srcContents := []byte("srcContents")

	dstPath := "/dst/hello/world"

	fs = afero.NewMemMapFs()
	assert.NoError(t, afero.WriteFile(fs, srcPath, srcContents, 0755))
	assert.NoError(t, copyFile(srcPath, dstPath))

	dstContents, err := afero.ReadFile(fs, dstPath)
	assert.NoError(t, err)
	assert.Equal(t, srcContents, dstContents)

	dstFileInfo, err := fs.Stat(dstPath)
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), dstFileInfo.Mode())
}

func TestManageChildOnce(t *testing.T) {
	// Setup the restartChild mock.
	type call struct {
		syncCommand, initCommand []string
		shouldInit               bool
	}
	restartChildCalled := false
	var restartChildError error
	var lastCall call
	s := server{
		restartChild: func(syncCommand, initCommand []string, shouldInit bool) error {
			restartChildCalled = true
			lastCall = call{syncCommand, initCommand, shouldInit}
			return restartChildError
		},
		syncedFiles:   sync.NewSyncedTracker(),
		mirroredFiles: sync.NewMirrorTracker(),
	}

	copyFile = func(src, dst string) error { return nil }
	removeFile = func(path string) error { return nil }

	// Initial run before any files have been synced. Nothing should happen.
	syncConfig := dev.SyncConfig{}
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.False(t, restartChildCalled)

	// Sync over some application files.
	restartChildCalled = false
	syncConfig = dev.SyncConfig{
		Rules: []*dev.SyncRule{
			{
				From: "src",
				To:   "dest",
			},
			{
				From:        "package.json",
				To:          "package.json",
				TriggerInit: true,
			},
		},
		OnSyncCommand: []string{"on", "sync", "command"},
		OnInitCommand: []string{"on", "init", "command"},
	}
	s.mirroredFiles.Mirrored(sync.MirrorFile{
		ContentsPath:   "/tmp/index-file",
		SyncSourcePath: "src/index.js",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "index.js",
		},
	})
	s.mirroredFiles.Mirrored(sync.MirrorFile{
		ContentsPath:   "/tmp/test-file",
		SyncSourcePath: "src/test.js",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "test.js",
		},
	})
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.True(t, restartChildCalled)
	assert.Equal(t, call{syncConfig.OnSyncCommand, syncConfig.OnInitCommand, false}, lastCall)

	// Run manageChildOnce again without any changes. Nothing should happen.
	restartChildCalled = false
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.False(t, restartChildCalled)

	// Remove the test file.
	s.mirroredFiles.Removed("src/test.js")
	restartChildCalled = false
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.True(t, restartChildCalled)
	assert.Equal(t, call{syncConfig.OnSyncCommand, syncConfig.OnInitCommand, false}, lastCall)

	// Sync over the package.json, which triggers the init command, but the init command fails.
	s.mirroredFiles.Mirrored(sync.MirrorFile{
		ContentsPath:   "/tmp/package-mirror",
		SyncSourcePath: "package.json",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "package.json",
		},
	})
	restartChildCalled = false
	restartChildError = assert.AnError
	assert.NotNil(t, s.manageChildOnce(syncConfig))
	assert.True(t, restartChildCalled)
	assert.Equal(t, call{syncConfig.OnSyncCommand, syncConfig.OnInitCommand, true}, lastCall)

	// Run manageChildOnce again, but let the init command succeed.
	restartChildCalled = false
	restartChildError = nil
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.True(t, restartChildCalled)
	assert.Equal(t, call{syncConfig.OnSyncCommand, syncConfig.OnInitCommand, true}, lastCall)

	// Run manageChildOnce again when there's no change.
	restartChildCalled = false
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.False(t, restartChildCalled)

	// Sync over more application files.
	s.mirroredFiles.Mirrored(sync.MirrorFile{
		ContentsPath:   "/tmp/another-file",
		SyncSourcePath: "src/another.js",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "another.js",
		},
	})
	restartChildCalled = false
	assert.NoError(t, s.manageChildOnce(syncConfig))
	assert.True(t, restartChildCalled)
	assert.Equal(t, call{syncConfig.OnSyncCommand, syncConfig.OnInitCommand, false}, lastCall)
}

func TestRestartChildEmptyCommand(t *testing.T) {
	// Test errors when the commands aren't long enough.
	s := server{}
	err := s.restartChildImpl(nil, []string{"init", "command"}, false)
	assert.NotNil(t, err)

	err = s.restartChildImpl([]string{"child", "command"}, nil, true)
	assert.NotNil(t, err)

	// It's ok if the init command is empty if it's not going to be run.
	startCommand = func(cmd *exec.Cmd) error {
		return nil
	}
	err = s.restartChildImpl([]string{"child", "command"}, nil, false)
	assert.NoError(t, err)
}

func TestRestartChild(t *testing.T) {
	childCommand := []string{"child", "command"}
	childPid := 100
	initCommand := []string{"init", "command"}
	s := server{}

	// Run the child command first without the init command.
	startCalled := false
	startCommand = func(cmd *exec.Cmd) error {
		startCalled = true
		assert.Equal(t, childCommand, cmd.Args)
		cmd.Process = &os.Process{Pid: childPid}
		return nil
	}
	err := s.restartChildImpl(childCommand, initCommand, false)
	assert.NoError(t, err)
	assert.True(t, startCalled)

	// Restart the child, and run the init command.
	killCalled := false
	kill = func(pid int, sig syscall.Signal) error {
		killCalled = true
		assert.Equal(t, syscall.SIGKILL, sig)
		assert.Equal(t, -childPid, pid)
		return nil
	}

	waitCalled := false
	waitCommand = func(cmd *exec.Cmd) error {
		waitCalled = true
		assert.Equal(t, childPid, cmd.Process.Pid)
		return nil
	}

	runCalled := false
	runCommand = func(cmd *exec.Cmd) error {
		runCalled = true
		assert.Equal(t, initCommand, cmd.Args)
		return nil
	}

	// No need to redefine the startCommand handler from above, just reset the
	// startCalled variable.
	startCalled = false
	err = s.restartChildImpl(childCommand, initCommand, true)
	assert.NoError(t, err)
	assert.True(t, killCalled)
	assert.True(t, waitCalled)
	assert.True(t, runCalled)
	assert.True(t, startCalled)
}

func TestTruncateSlice(t *testing.T) {
	tests := []struct {
		name   string
		slc    []string
		length int
		exp    []string
	}{
		{
			name:   "NoTruncate",
			slc:    []string{"foo", "bar"},
			length: 5,
			exp:    []string{"foo", "bar"},
		},
		{
			name:   "NoTruncateExactLength",
			slc:    []string{"foo", "bar"},
			length: 2,
			exp:    []string{"foo", "bar"},
		},
		{
			name:   "Truncate",
			slc:    []string{"foo", "bar"},
			length: 1,
			exp:    []string{"foo", "... 1 more ..."},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.exp, truncateSlice(test.slc, test.length))
		})
	}
}
