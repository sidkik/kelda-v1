package sync

import (
	log "github.com/sirupsen/logrus"

	"github.com/sidkik/kelda-v1/pkg/proto/dev"
)

// SyncedTracker tracks files that have been synced from the mirror files
// to their final destination according to the user's sync config.
type SyncedTracker map[string]DestinationFile

// DestinationFile contains metadata tracking files that were synced from user machines.
type DestinationFile struct {
	// Path is the path of the synced file on the filesystem after the sync
	// rules have been applied to the source file.
	SyncDestinationPath string

	// SyncSourcePath is the normalized path to the file on the user's machine.
	// This path doesn't exist in the container, but is used to calculate the
	// SyncDestinationPath and the version of the synced files.
	SyncSourcePath string

	FileAttributes

	// SyncRules are the sync rules that were applied to sync this file.
	// Multiple sync rules can apply to the same destination if they have the
	// same source. This pattern is common as a way of setting `triggerInit` on
	// a specific file within a directory.
	SyncRules []*dev.SyncRule
}

// NewSyncedTracker creates a new SyncedTracker.
func NewSyncedTracker() *SyncedTracker {
	return &SyncedTracker{}
}

// Version returns the version of the files that have been synced.
// It assumes that the `syncConfig` was properly applied to the tracked
// SyncSourcePaths.
func (tracker SyncedTracker) Version(syncConfig dev.SyncConfig) Version {
	sourceFiles := LocalSnapshot{}
	for _, f := range tracker {
		sourceFiles[f.SyncSourcePath] = SourceFile{
			SyncSourcePath: f.SyncSourcePath,
			FileAttributes: f.FileAttributes,
		}
	}

	return Version{
		LocalFiles: sourceFiles,
		SyncConfig: syncConfig,
	}
}

// Synced updates SyncedTracker to reflect that `f` was added or updated.
func (tracker SyncedTracker) Synced(f DestinationFile) {
	tracker[f.SyncDestinationPath] = f
}

// Removed updates SyncedTracker to reflect that `path` should no longer be
// tracked. This can happen if the source file was removed on the user's
// machine, or if the sync config was changed so that the source file is no
// longer tracked.
func (tracker SyncedTracker) Removed(path string) {
	delete(tracker, path)
}

// Files returns the tracked files as a list.
func (tracker SyncedTracker) Files() (files []DestinationFile) {
	for _, f := range tracker {
		files = append(files, f)
	}
	return files
}

// Diff returns the file operations necessary to make the tracked files
// compliant with the sync config.
// * Files that are newly added should be copied.
// * Files that have been updated (i.e. have the wrong version) should be resynced.
// * And files that are no longer tracked on the user's machine should be removed.
func (tracker SyncedTracker) Diff(mirror *MirrorTracker, syncConfig dev.SyncConfig) (
	toCopy []DestinationFile, toRemove []string) {

	// Try to match each file that should be synced with what's been synced
	// already.
	// If the file doesn't exist, or has the wrong version, then it needs to be
	// synced.
	expectedFiles := getExpectedFiles(mirror, syncConfig)
	for _, f := range expectedFiles {
		actual, ok := tracker[f.SyncDestinationPath]
		if !ok || !actual.FileAttributes.Equal(f.FileAttributes) {
			toCopy = append(toCopy, f)
		}
	}

	for _, f := range tracker {
		if _, ok := expectedFiles[f.SyncDestinationPath]; !ok {
			toRemove = append(toRemove, f.SyncDestinationPath)
		}
	}
	return
}

func getExpectedFiles(mirror *MirrorTracker, syncConfig dev.SyncConfig) map[string]DestinationFile {
	// Apply the sync config to each local file to decide where it should be
	// copied to.
	desiredFiles := map[string]DestinationFile{}
	for _, f := range mirror.GetSnapshot() {
		for _, syncRule := range syncConfig.Rules {
			dest, ok := destination(*syncRule, f.SyncSourcePath)
			if !ok {
				continue
			}

			desiredFile, ok := desiredFiles[dest]
			if !ok {
				desiredFile = DestinationFile{
					SyncDestinationPath: dest,
					SyncSourcePath:      f.SyncSourcePath,
					FileAttributes:      f.FileAttributes,
				}
			}

			desiredFile.SyncRules = append(desiredFile.SyncRules, syncRule)
			if desiredFile.SyncSourcePath != f.SyncSourcePath {
				log.WithFields(log.Fields{
					"rules":   desiredFile.SyncRules,
					"dstPath": dest,
				}).Warn("Two paths map to the same file. Ignoring the latter file.")
			}

			desiredFiles[dest] = desiredFile
		}
	}

	return desiredFiles
}
