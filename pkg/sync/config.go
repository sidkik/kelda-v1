package sync

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/kelda-inc/kelda/pkg/errors"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
)

// Mocked out for unit testing.
var fs = afero.NewOsFs()

// FileAttributes contains some metadata used to compare whether two files are
// equal.
type FileAttributes struct {
	// ContentsHash is the sha512 hash of the contents of the file.
	ContentsHash string

	// Mode is the file mode of the file.
	Mode os.FileMode

	// ModTime is the time of the last file modification.
	ModTime time.Time
}

// Version returns a string representing the contents of the metadata.
func (f FileAttributes) Version() string {
	hasher := sha512.New()
	fmt.Fprintf(hasher, "ContentsHash: %s\n", f.ContentsHash)
	fmt.Fprintf(hasher, "Mode: %#o\n", f.Mode)
	fmt.Fprintf(hasher, "ModTime: %d\n", f.ModTime.UnixNano())
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

// Equal returns whether two files are equal (i.e. whether a sync is necessary).
func (f FileAttributes) Equal(otherFile FileAttributes) bool {
	return f.ContentsHash == otherFile.ContentsHash &&
		f.Mode == otherFile.Mode &&
		f.ModTime.Equal(otherFile.ModTime)
}

// Version represents the version of a set of files. This is used to calculate
// the local version of a set of files, and the running version.
// When used as the running version, we assume that the sync rules in the
// SyncConfig were properly applied to the LocalFiles.
type Version struct {
	SyncConfig dev.SyncConfig
	LocalFiles LocalSnapshot
}

func (v Version) String() string {
	hasher := sha512.New()

	var paths []string
	for _, f := range v.LocalFiles {
		paths = append(paths, f.SyncSourcePath)
	}
	sort.Strings(paths)

	for _, path := range paths {
		fmt.Fprintf(hasher, "%s: %s\n", path, v.LocalFiles[path].Version())
	}

	fmt.Fprintf(hasher, "SyncConfig: %s", VersionSyncConfig(v.SyncConfig))
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

// HashFile returns the sha512 hash of the file at the given path.
func HashFile(path string) (string, error) {
	f, err := fs.Open(path)
	if err != nil {
		return "", errors.WithContext(err, "open")
	}
	defer f.Close()

	hasher := sha512.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", errors.WithContext(err, "read")
	}

	return base64.StdEncoding.EncodeToString(hasher.Sum(nil)), nil
}

// VersionSyncConfig returns a string representing the contents of the sync
// config.
func VersionSyncConfig(cfg dev.SyncConfig) string {
	// Sort the rules before versioning so that the version doesn't depend on
	// the order of the rules.
	var ruleStrings []string
	for _, rule := range cfg.Rules {
		exceptionsCopy := append([]string{}, rule.Except...)
		sort.Strings(exceptionsCopy)

		ruleStr := fmt.Sprintf("From: %s, To: %s, Except: [%s]\n",
			rule.From, rule.To, strings.Join(exceptionsCopy, ", "))
		ruleStrings = append(ruleStrings, ruleStr)
	}
	sort.Strings(ruleStrings)

	hasher := sha512.New()
	fmt.Fprintf(hasher, "Command: [%s]", strings.Join(cfg.OnSyncCommand, ", "))
	fmt.Fprintf(hasher, "InitCommand: [%s]", strings.Join(cfg.OnInitCommand, ", "))
	for _, rule := range ruleStrings {
		fmt.Fprintln(hasher, rule)
	}
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}

// AppliesTo returns whether the given path applies to the sync rule.
// In other words, whether it matches `rule.From`, and isn't ignored.
func AppliesTo(rule dev.SyncRule, path string) bool {
	if _, ok := matchPattern(path, rule.From); !ok {
		return false
	}

	// Ensure that the path doesn't match any of the exclusions.
	for _, exception := range rule.Except {
		excludePattern := filepath.Join(rule.From, exception)
		if _, ok := matchPattern(path, excludePattern); ok {
			return false
		}
	}
	return true
}

// destination returns the path the given file should be synced to.
func destination(rule dev.SyncRule, path string) (string, bool) {
	if !AppliesTo(rule, path) {
		return "", false
	}

	remaining, ok := matchPattern(path, rule.From)
	if !ok {
		return "", false
	}

	if perfectMatch := remaining == ""; perfectMatch {
		dstInfo, err := os.Stat(rule.To)
		if err == nil && dstInfo.IsDir() {
			// E.g. if the path is "index.js", and the rule is "index.js -> .".
			return filepath.Join(rule.To, filepath.Base(path)), true
		}

		// E.g. if the path is "index.js", and the rule is "index.js -> index.js".
		return rule.To, true
	}

	// E.g. if the path is "src/index.js", and the rule is "src -> .".
	return filepath.Join(rule.To, remaining), true
}

// matchPattern returns true if `path` is either an exact match, or a child of
// `pattern`.
// For example, `/foo`, `/foo/bar`, and `/foo/bar/baz` match `/foo`.
// `foo` does not match `/foo` because it's a relative path.
// `foo` matches `.` because they're both relative paths, but `/foo` doesn't
// match `.`.
func matchPattern(path string, pattern string) (remaining string, ok bool) {
	relativePath, err := filepath.Rel(pattern, path)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return "", false
	}

	if relativePath == "." {
		return "", true
	}
	return relativePath, true
}
