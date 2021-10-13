package dev

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	logrusTest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/sidkik/kelda-v1/pkg/proto/dev"
	"github.com/sidkik/kelda-v1/pkg/sync"
	syncMocks "github.com/sidkik/kelda-v1/pkg/sync/client/mocks"
)

func TestSyncOnce(t *testing.T) {
	type test struct {
		name               string
		localSnapshot      sync.LocalSnapshot
		localSnapshotError error
		syncClient         syncMocks.Client
		hasSyncedOnce      bool
		expLogs            []*logrus.Entry
		expError           string
	}

	redFile := sync.SourceFile{
		ContentsPath:   "red",
		SyncSourcePath: "red",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "red",
		},
	}
	localRedSnapshot := sync.LocalSnapshot(map[string]sync.SourceFile{redFile.SyncSourcePath: redFile})

	redMirror := sync.MirrorFile{
		SyncSourcePath: "red",
		FileAttributes: sync.FileAttributes{
			ContentsHash: "red",
		},
	}
	redMirrorSnapshot := sync.MirrorSnapshot{}
	redMirrorSnapshot.Add(redMirror)

	syncConfig := dev.SyncConfig{
		Rules: []*dev.SyncRule{
			{From: redFile.SyncSourcePath, To: redFile.SyncSourcePath},
		},
	}

	testMirror := test{
		name:          "mirror",
		localSnapshot: localRedSnapshot,
		expLogs: []*logrus.Entry{
			{
				Level:   logrus.InfoLevel,
				Data:    logrus.Fields{"service": "test"},
				Message: "Copied 1 files, removed 0.",
			},
		},
	}
	testMirror.syncClient.On("SetTargetVersion", syncConfig, mock.Anything).Return(nil)
	testMirror.syncClient.On("GetMirrorSnapshot").Return(sync.MirrorSnapshot{}, nil)
	testMirror.syncClient.On("Mirror", redFile).Return(nil)
	testMirror.syncClient.On("SyncComplete").Return(nil)

	testRemove := test{
		name:          "remove",
		localSnapshot: map[string]sync.SourceFile{},
		expLogs: []*logrus.Entry{
			{
				Level:   logrus.InfoLevel,
				Data:    logrus.Fields{"service": "test"},
				Message: "Copied 0 files, removed 1.",
			},
		},
	}
	testRemove.syncClient.On("SetTargetVersion", syncConfig, mock.Anything).Return(nil)
	testRemove.syncClient.On("GetMirrorSnapshot").Return(redMirrorSnapshot, nil)
	testRemove.syncClient.On("Remove", redFile.SyncSourcePath).Return(nil)
	testRemove.syncClient.On("SyncComplete").Return(nil)

	testAlreadySyncedFirstTime := test{
		name:          "print a log message when running for the first time and we've already synced",
		localSnapshot: localRedSnapshot,
		expLogs: []*logrus.Entry{
			{
				Level: logrus.InfoLevel,
				Data:  logrus.Fields{"service": "test"},
				Message: "Already synced. A previous run of `kelda dev` " +
					"probably synced the files over already.",
			},
		},
	}
	testAlreadySyncedFirstTime.syncClient.On("SetTargetVersion",
		syncConfig, mock.Anything).Return(nil)
	testAlreadySyncedFirstTime.syncClient.On("GetMirrorSnapshot").Return(
		redMirrorSnapshot, nil)

	testAlreadySyncedSecondTime := test{
		name: "don't print a log message when we've already synced, " +
			"but not running for the first time",
		hasSyncedOnce: true,
		localSnapshot: localRedSnapshot,
	}
	testAlreadySyncedSecondTime.syncClient.On("SetTargetVersion",
		syncConfig, mock.Anything).Return(nil)
	testAlreadySyncedSecondTime.syncClient.On("GetMirrorSnapshot").Return(redMirrorSnapshot, nil)

	testSnapshotSourceError := test{
		name:               "dev.SnapshotSource error halts execution",
		localSnapshotError: assert.AnError,
		expError:           "get local files:",
	}

	testSetVersionError := test{
		name:     "syncClient.SetTargetVersion error halts execution",
		expError: "set target version:",
	}
	testSetVersionError.syncClient.On("SetTargetVersion",
		syncConfig, mock.Anything).Return(assert.AnError)

	testGetMirrorSnapshotError := test{
		name:     "syncClient.GetMirrorSnapshot error halts execution",
		expError: "get mirror files:",
	}
	testGetMirrorSnapshotError.syncClient.On("SetTargetVersion",
		syncConfig, mock.Anything).Return(nil)
	testGetMirrorSnapshotError.syncClient.On("GetMirrorSnapshot").Return(nil, assert.AnError)

	testMirrorError := test{
		name:          "syncClient.Mirror error halts execution",
		localSnapshot: localRedSnapshot,
		expError:      "mirror red:",
	}
	testMirrorError.syncClient.On("SetTargetVersion", syncConfig, mock.Anything).Return(nil)
	testMirrorError.syncClient.On("GetMirrorSnapshot").Return(sync.MirrorSnapshot{}, nil)
	testMirrorError.syncClient.On("Mirror", mock.Anything).Return(assert.AnError)

	testRemoveError := test{
		name:     "syncClient.Remove error halts execution",
		expError: "remove:",
	}
	testRemoveError.syncClient.On("SetTargetVersion", mock.Anything, mock.Anything).Return(nil)
	testRemoveError.syncClient.On("GetMirrorSnapshot").Return(redMirrorSnapshot, nil)
	testRemoveError.syncClient.On("Remove", mock.Anything).Return(assert.AnError)

	// Suppress lint errors about copying the mutex in the mock client since
	// we're only invoking a single reference to the mock.
	tests := []test{testMirror, testRemove, testAlreadySyncedFirstTime, // nolint: vet
		testAlreadySyncedSecondTime, testSnapshotSourceError, // nolint: vet
		testSetVersionError, testGetMirrorSnapshotError, testMirrorError, // nolint: vet
		testRemoveError} // nolint: vet
	for _, test := range tests { // nolint: vet
		test := test // nolint: vet

		t.Run(test.name, func(t *testing.T) {
			servicePath := "/service"
			snapshotSource = func(cfg dev.SyncConfig, relativeTo string) (sync.LocalSnapshot, error) {
				assert.Equal(t, syncConfig, cfg)
				assert.Equal(t, relativeTo, servicePath)
				return test.localSnapshot, test.localSnapshotError
			}

			logger, logHook := logrusTest.NewNullLogger()
			err := syncer{
				service:     "test",
				syncConfig:  syncConfig,
				log:         logger,
				servicePath: servicePath,
			}.syncOnce(&test.syncClient, test.hasSyncedOnce)
			if test.expError == "" {
				assert.NoError(t, err, test.name)
			} else {
				assert.True(t, strings.HasPrefix(err.Error(), test.expError), test.name)
			}
			test.syncClient.AssertExpectations(t)
			assertLogs(t, test.expLogs, logHook.AllEntries(), test.name)
		})
	}
}

func assertLogs(t *testing.T, expLogs, allEntries []*logrus.Entry, msg string) {
	assert.Len(t, allEntries, len(expLogs), msg)
	for i, exp := range expLogs {
		assert.Equal(t, exp.Level, allEntries[i].Level, msg)
		assert.Equal(t, exp.Data, allEntries[i].Data, msg)
		assert.Equal(t, exp.Message, allEntries[i].Message, msg)
	}
}
