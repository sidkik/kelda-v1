package fswatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/kelda-inc/fsnotify"
	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
	"github.com/kelda-inc/kelda/pkg/sync"
)

var fs = afero.NewOsFs()

// Watch watches for changes in files tracked by `syncConfig`. It sends an
// event on the returned channel whenever a file within the watched paths
// changes.
// Relative paths are resolved relative to `relativeTo`. For example, if a
// service at `path/to/service` contains the relative path `./src`, we will
// watch `path/to/service/src`.
func Watch(syncConfig dev.SyncConfig, relativeTo string) (chan struct{}, error) {
	pathsToWatch, err := getPathsToWatch(syncConfig, relativeTo)
	if err != nil {
		return nil, errors.WithContext(err, "get paths")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.WithContext(err, "create watcher")
	}

	for _, path := range pathsToWatch {
		if err := watcher.Add(path); err != nil {
			// Close the watcher so that we release the file handlers for the
			// previously added paths.
			if err := watcher.Close(); err != nil {
				log.WithError(err).Warn("Failed to close file watcher")
			}

			return nil, errors.WithContext(err, fmt.Sprintf("watch %q", path))
		}
	}
	return combineUpdates(watcher.Events), nil
}

func combineUpdates(updates <-chan fsnotify.Event) chan struct{} {
	combined := make(chan struct{}, 1)
	go func() {
		for range updates {
			select {
			case combined <- struct{}{}:
			default:
			}
		}
	}()
	return combined
}

func getPathsToWatch(syncConfig dev.SyncConfig, relativeTo string) (paths []string, err error) {
	for _, rule := range syncConfig.Rules {
		path := rule.From
		if !filepath.IsAbs(rule.From) {
			path = filepath.Join(relativeTo, rule.From)
		}

		fi, err := fs.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, errors.FileNotFound{Path: path}
			}
			return nil, errors.WithContext(err, "stat")
		}

		paths = append(paths, path)
		if fi.Mode().IsDir() {
			// Because fsnotify doesn't watch directories recursively, we walk
			// the directory's contents and add all subdirectories and files.
			subpaths, err := getChildren(*rule, path)
			if err != nil {
				return nil, errors.WithContext(err, "get subdirs")
			}
			paths = append(paths, subpaths...)
		} else {
			// If the path is a file, then watch its parent directory as well
			// as the file itself. This way, if the file is removed and
			// re-added we'll notice.
			// This will also cause triggers when other files in the directory
			// are created or removed, but this is fine since the sync will
			// just be a no-op.
			paths = append(paths, filepath.Dir(path))
		}
	}

	return paths, nil
}

func getChildren(rule dev.SyncRule, dir string) (paths []string, err error) {
	err = afero.Walk(fs, dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return errors.WithContext(err, "walk error")
		}

		if path == dir {
			return nil
		}

		// Normalize the path to be relative to `rule.From` before applying the
		// sync rule. This way, `kelda dev` can be run from anywhere on the
		// filesystem.
		relativePath, err := filepath.Rel(dir, path)
		if err != nil || strings.HasPrefix(relativePath, "..") {
			// This shouldn't happen because `path` is always a child of `dir`.
			return errors.WithContext(err, "normalized path")
		}
		normalizedPath := filepath.Join(rule.From, relativePath)

		if sync.AppliesTo(rule, normalizedPath) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
