package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
)

// A SourceFile is a file that exists on the user's machine.
type SourceFile struct {
	// ContentsPath is the path to the file that can be opened by the Kelda
	// process. It may be either a relative path or an absolute path.
	ContentsPath string

	// SyncSourcePath is the normalized path in terms of the rules in the
	// SyncConfig.
	// For example, a file would have a ContentsPath of `/service/index.js` and
	// SyncSourcePath of `index.js` if the file is being synced by a SyncConfig
	// at `/service/kelda.yaml` with a rule.From of `index.js`.
	SyncSourcePath string

	// FileAttributes contains metadata that's used for comparing equality
	// between SourceFiles and MirrorFiles.
	FileAttributes
}

// A MirrorFile is a copy of a SourceFile that lives in the container rather
// than on the user's machine.
// The SyncSourcePath and FileAttributes of a properly mirrored file are
// exactly equal.
// The ContentsPath may differ since the file is living on a different filesystem.
type MirrorFile SourceFile

// MirrorSnapshot is a collection of all mirrored files.
type MirrorSnapshot map[string]MirrorFile

// Add updates the MirrorSnapshot.
func (mirror MirrorSnapshot) Add(f MirrorFile) {
	mirror[f.SyncSourcePath] = f
}

// Marshal converts the MirrorSnapshot into the protobuf format.
func (mirror MirrorSnapshot) Marshal() (dev.MirrorSnapshot, error) {
	files := map[string]*dev.MirrorFile{}
	for path, f := range mirror {
		modTime, err := ptypes.TimestampProto(f.ModTime)
		if err != nil {
			return dev.MirrorSnapshot{}, errors.WithContext(err, "marshal modtime timestamp")
		}

		files[path] = &dev.MirrorFile{
			SyncSourcePath: f.SyncSourcePath,
			FileAttributes: &dev.FileAttributes{
				ContentsHash: f.ContentsHash,
				Mode:         uint32(f.Mode),
				ModTime:      modTime,
			},
		}
	}
	return dev.MirrorSnapshot{Files: files}, nil
}

// UnmarshalMirrorSnapshot parses the protobuf version of the mirror into the
// go type that's used in the rest of the code.
func UnmarshalMirrorSnapshot(pbSnapshot dev.MirrorSnapshot) (MirrorSnapshot, error) {
	snapshot := MirrorSnapshot{}
	for _, f := range pbSnapshot.GetFiles() {
		modTime, err := ptypes.Timestamp(f.GetFileAttributes().GetModTime())
		if err != nil {
			return MirrorSnapshot{}, errors.WithContext(err, "parse modtime")
		}

		snapshot.Add(MirrorFile{
			SyncSourcePath: f.GetSyncSourcePath(),
			FileAttributes: FileAttributes{
				ContentsHash: f.GetFileAttributes().GetContentsHash(),
				ModTime:      modTime,
				Mode:         os.FileMode(f.GetFileAttributes().GetMode()),
			},
		})
	}
	return snapshot, nil
}

// LocalSnapshot is a collection of all source files.
type LocalSnapshot map[string]SourceFile

// MirrorTracker tracks the files that have been mirrored from the user's
// local machine to the container.
type MirrorTracker struct {
	files MirrorSnapshot
	lock  sync.Mutex
}

// NewMirrorTracker returns a new MirrorTracker.
func NewMirrorTracker() *MirrorTracker {
	return &MirrorTracker{files: MirrorSnapshot{}}
}

// Mirrored updates the information for f.
func (tracker *MirrorTracker) Mirrored(f MirrorFile) {
	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	oldFile, ok := tracker.files[f.SyncSourcePath]
	tracker.files[f.SyncSourcePath] = f

	if ok {
		tracker.cleanupStaleFile(oldFile.ContentsPath)
	}
}

// Mirrored updates the tracker to reflect that `path` was removed from the
// user's machine.
func (tracker *MirrorTracker) Removed(path string) {
	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	oldFile, ok := tracker.files[path]
	delete(tracker.files, path)
	if ok {
		tracker.cleanupStaleFile(oldFile.ContentsPath)
	}
}

func (tracker *MirrorTracker) cleanupStaleFile(path string) {
	if err := fs.Remove(path); err != nil {
		log.WithError(err).WithField("path", path).Warn(
			"Failed to clean up stale staged file from disk. This won't affect future syncs.")
	}
}

func (tracker *MirrorTracker) GetSnapshot() MirrorSnapshot {
	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	// Copy the underlying snapshot because maps are reference types.
	// If we didn't copy the snapshot, changes to one snapshot would affect the
	// other, and accesses wouldn't be threadsafe.
	snapshotCopy := MirrorSnapshot{}
	for k, v := range tracker.files {
		snapshotCopy[k] = v
	}
	return snapshotCopy
}

func (tracker *MirrorTracker) Get(localPath string) (MirrorFile, bool) {
	tracker.lock.Lock()
	defer tracker.lock.Unlock()

	f, ok := tracker.files[localPath]
	return f, ok
}

// Diff returns the files that need to be create, updated, or removed from the
// file mirror.
func (local LocalSnapshot) Diff(mirror MirrorSnapshot) (toMirror []SourceFile, toRemove []string) {
	for _, exp := range local {
		curr, ok := mirror[exp.SyncSourcePath]
		if !ok || !curr.FileAttributes.Equal(exp.FileAttributes) {
			toMirror = append(toMirror, exp)
		}
	}

	for _, curr := range mirror {
		if _, ok := local[curr.SyncSourcePath]; !ok {
			toRemove = append(toRemove, curr.SyncSourcePath)
		}
	}
	return
}

// SnapshotSource returns the information on the files that are tracked by the
// syncConfig.
func SnapshotSource(syncConfig dev.SyncConfig, relativeTo string) (LocalSnapshot, error) {
	files := LocalSnapshot{}
	for _, rule := range syncConfig.Rules {
		toSnapshot := rule.From
		if !filepath.IsAbs(rule.From) {
			toSnapshot = filepath.Join(relativeTo, rule.From)
		}

		fi, err := fs.Stat(toSnapshot)
		if err != nil {
			return nil, errors.WithContext(err, "open path")
		}

		if fi.IsDir() {
			rule := rule
			err := afero.Walk(fs, toSnapshot, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				if fi.IsDir() {
					return nil
				}

				relativePath, err := filepath.Rel(toSnapshot, path)
				if err != nil || strings.HasPrefix(relativePath, "..") {
					return errors.WithContext(err, "normalized path")
				}
				normalizedPath := filepath.Join(rule.From, relativePath)

				if !AppliesTo(*rule, normalizedPath) {
					return nil
				}

				contentsHash, err := HashFile(path)
				if err != nil {
					return err
				}

				files[normalizedPath] = SourceFile{
					SyncSourcePath: normalizedPath,
					ContentsPath:   path,
					FileAttributes: FileAttributes{
						ContentsHash: contentsHash,
						ModTime:      fi.ModTime(),
						Mode:         fi.Mode(),
					},
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			contentsHash, err := HashFile(toSnapshot)
			if err != nil {
				return nil, errors.WithContext(err,
					fmt.Sprintf("version path %q", toSnapshot))
			}

			files[rule.From] = SourceFile{
				SyncSourcePath: rule.From,
				ContentsPath:   toSnapshot,
				FileAttributes: FileAttributes{
					ContentsHash: contentsHash,
					ModTime:      fi.ModTime(),
					Mode:         fi.Mode(),
				},
			}
		}
	}
	return files, nil
}
