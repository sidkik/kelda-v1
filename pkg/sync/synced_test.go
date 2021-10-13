package sync

import (
	"sort"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/sidkik/kelda-v1/pkg/proto/dev"
)

func TestSyncedVersion(t *testing.T) {
	tracker := NewSyncedTracker()

	redFile := randomFile(mockFile{path: "red-src"})
	syncedRedFile := toDestinationFile(redFile, "red-dest")
	tracker.Synced(syncedRedFile)

	blueFile := randomFile(mockFile{path: "blue-src"})
	syncedBlueFile := toDestinationFile(blueFile, "blue-dest")
	tracker.Synced(syncedBlueFile)

	tracker.Synced(toDestinationFile(blueFile, "blue-dest-2"))

	syncCfg := dev.SyncConfig{
		Rules: []*dev.SyncRule{
			{From: "red-src", To: "red-dest"},
			{From: "blue-src", To: "blue-dest"},
			{From: "blue-src", To: "blue-dest-2"},
		},
	}
	syncedVersion := tracker.Version(syncCfg)
	localVersion := Version{
		LocalFiles: map[string]SourceFile{
			"red-src":  toSourceFile("red-src", redFile),
			"blue-src": toSourceFile("blue-src", blueFile),
		},
		SyncConfig: syncCfg,
	}
	assert.Equal(t, localVersion.String(), syncedVersion.String())

	// Simulate a local change.
	localVersion.LocalFiles["red-src"] = toSourceFile("red-src", randomFile(mockFile{}))
	assert.NotEqual(t, localVersion.String(), syncedVersion.String())
}

func TestDiffSynced(t *testing.T) {
	fs = afero.NewMemMapFs()

	syncedTracker := NewSyncedTracker()
	mirrorTracker := NewMirrorTracker()

	syncConfig := dev.SyncConfig{
		Rules: []*dev.SyncRule{
			{From: "red-src", To: "red-dest"},
			{From: "blue-src", To: "blue-dest"},
			{From: "blue-src", To: "blue-dest-2"},
		},
	}

	assertDiff := func(expCopies []DestinationFile, expRemoves []string) {
		toCopy, toRemove := syncedTracker.Diff(mirrorTracker, syncConfig)

		sort.Strings(toRemove)
		assert.Equal(t, expRemoves, toRemove)

		byPath := func(i, j int) bool {
			return toCopy[i].SyncDestinationPath < toCopy[j].SyncDestinationPath
		}
		sort.Slice(toCopy, byPath)
		assert.Equal(t, expCopies, toCopy)
	}

	redFile := randomFile(mockFile{path: "red-src"})
	blueFile := randomFile(mockFile{path: "blue-src"})

	// The red file was added locally.
	mirrorTracker.Mirrored(toMirrorFile("red-src", redFile))
	expSynced := []DestinationFile{
		toDestinationFile(redFile, "red-dest", syncConfig.Rules[0]),
	}
	assertDiff(expSynced, nil)
	syncedTracker.Synced(expSynced[0])
	assert.Len(t, syncedTracker.Files(), 1)

	// The blue file was added locally.
	mirrorTracker.Mirrored(toMirrorFile("blue-src", blueFile))
	expSynced = []DestinationFile{
		toDestinationFile(blueFile, "blue-dest", syncConfig.Rules[1]),
		toDestinationFile(blueFile, "blue-dest-2", syncConfig.Rules[2]),
	}
	assertDiff(expSynced, nil)
	syncedTracker.Synced(expSynced[0])
	syncedTracker.Synced(expSynced[1])
	assert.Len(t, syncedTracker.Files(), 3)

	// There's a new version of the red file.
	redFile.contents = "new contents"
	mirrorTracker.Mirrored(toMirrorFile("red-src", redFile))
	expSynced = []DestinationFile{
		toDestinationFile(redFile, "red-dest", syncConfig.Rules[0]),
	}
	assertDiff(expSynced, nil)
	syncedTracker.Synced(expSynced[0])
	assert.Len(t, syncedTracker.Files(), 3)

	// The blue file was removed.
	mirrorTracker.Removed("blue-src")
	assertDiff(nil, []string{
		"blue-dest",
		"blue-dest-2",
	})
	syncedTracker.Removed("blue-dest")
	syncedTracker.Removed("blue-dest-2")
	assert.Len(t, syncedTracker.Files(), 1)
}

func toDestinationFile(f mockFile, dest string, syncRules ...*dev.SyncRule) DestinationFile {
	return DestinationFile{
		SyncDestinationPath: dest,
		SyncSourcePath:      f.path,
		FileAttributes:      f.fileAttributes(),
		SyncRules:           syncRules,
	}
}
