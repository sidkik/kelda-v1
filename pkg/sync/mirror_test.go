package sync

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/kelda-inc/kelda/pkg/proto/dev"
)

func TestSnapshotSource(t *testing.T) {
	relFile := randomFile(mockFile{path: "/service/rel-file"})
	relDirFileOne := randomFile(mockFile{path: "/service/rel-dir/file-1"})
	relDirFileTwo := randomFile(mockFile{path: "/service/rel-dir/file-2"})
	relIgnoredFile := randomFile(mockFile{path: "/service/rel-dir/ignored-dir/foo"})
	absFile := randomFile(mockFile{path: "/abs-file"})
	absDirFileOne := randomFile(mockFile{path: "/abs-dir/file-1"})
	absDirFileTwo := randomFile(mockFile{path: "/abs-dir/file-2"})
	absIgnoredFile := randomFile(mockFile{path: "/abs-dir/ignored-file"})

	syncConfig := dev.SyncConfig{
		Rules: []*dev.SyncRule{
			{From: "rel-file", To: "dst1"},
			{From: "rel-dir", To: "dst2", Except: []string{"ignored-dir"}},
			{From: "/abs-file", To: "dst3"},
			{From: "/abs-dir", To: "dst4", Except: []string{"ignored-file"}},
		},
	}

	exp := LocalSnapshot(map[string]SourceFile{
		"rel-file":        toSourceFile("rel-file", relFile),
		"rel-dir/file-1":  toSourceFile("rel-dir/file-1", relDirFileOne),
		"rel-dir/file-2":  toSourceFile("rel-dir/file-2", relDirFileTwo),
		"/abs-file":       toSourceFile("/abs-file", absFile),
		"/abs-dir/file-1": toSourceFile("/abs-dir/file-1", absDirFileOne),
		"/abs-dir/file-2": toSourceFile("/abs-dir/file-2", absDirFileTwo),
	})

	fs = afero.NewMemMapFs()

	localFiles := []mockFile{
		relFile, relDirFileOne, relDirFileTwo, relIgnoredFile,
		absFile, absDirFileOne, absDirFileTwo, absIgnoredFile,
	}
	for _, f := range localFiles {
		assert.NoError(t, f.writeToFs())
	}

	snapshot, err := SnapshotSource(syncConfig, "/service")
	assert.NoError(t, err)
	assert.Equal(t, exp, snapshot)
}

func TestDiffMirror(t *testing.T) {
	matchesSrc := randomFile(mockFile{})
	matchesDst := matchesSrc

	diffModeSrc := randomFile(mockFile{mode: 0644})
	diffModeDst := diffModeSrc
	diffModeDst.mode = 0400

	diffContentsSrc := randomFile(mockFile{contents: "contents"})
	diffContentsDst := diffContentsSrc
	diffContentsDst.contents = "changed contents"

	diffModTimeSrc := randomFile(mockFile{modTime: time.Now()})
	diffModTimeDst := diffModTimeSrc
	diffModTimeDst.modTime = diffModTimeSrc.modTime.Add(-1 * time.Minute)

	local := LocalSnapshot(map[string]SourceFile{
		"matches":       toSourceFile("matches", matchesSrc),
		"diff-mode":     toSourceFile("diff-mode", diffModeSrc),
		"diff-contents": toSourceFile("diff-contents", diffContentsSrc),
		"diff-modtime":  toSourceFile("diff-modtime", diffModTimeSrc),
		"added":         toSourceFile("added", randomFile(mockFile{})),
	})
	mirror := MirrorSnapshot(map[string]MirrorFile{
		"matches":       toMirrorFile("matches", matchesDst),
		"diff-mode":     toMirrorFile("diff-mode", diffModeDst),
		"diff-contents": toMirrorFile("diff-contents", diffContentsDst),
		"diff-modtime":  toMirrorFile("diff-modtime", diffModTimeDst),
		"removed":       toMirrorFile("removed", randomFile(mockFile{})),
	})

	expMirrors := []SourceFile{
		local["added"],
		local["diff-contents"],
		local["diff-mode"],
		local["diff-modtime"],
	}
	expRemoves := []string{
		"removed",
	}

	actualMirrors, actualRemoves := local.Diff(mirror)
	byPath := func(i, j int) bool {
		return actualMirrors[i].SyncSourcePath < actualMirrors[j].SyncSourcePath
	}
	sort.Slice(actualMirrors, byPath)

	assert.Equal(t, expMirrors, actualMirrors)
	assert.Equal(t, expRemoves, actualRemoves)
}

func TestUpdateMirrorTracker(t *testing.T) {
	fs = afero.NewMemMapFs()
	tracker := NewMirrorTracker()
	assert.Empty(t, tracker.GetSnapshot())

	assertDoesNotExist := func(path string) {
		exists, err := afero.Exists(fs, path)
		assert.NoError(t, err)
		assert.False(t, exists)
	}

	initialRed := randomFile(mockFile{})
	redMirror := toMirrorFile("red", initialRed)
	initialRed.writeToFs()
	tracker.Mirrored(redMirror)

	blue := randomFile(mockFile{})
	blueMirror := toMirrorFile("blue", blue)
	blue.writeToFs()
	tracker.Mirrored(blueMirror)

	assert.Equal(t, MirrorSnapshot(map[string]MirrorFile{
		blueMirror.SyncSourcePath: blueMirror,
		redMirror.SyncSourcePath:  redMirror,
	}), tracker.GetSnapshot())

	newRed := randomFile(mockFile{})
	redMirror = toMirrorFile("red", newRed)
	newRed.writeToFs()
	tracker.Mirrored(redMirror)

	assert.Equal(t, MirrorSnapshot(map[string]MirrorFile{
		blueMirror.SyncSourcePath: blueMirror,
		redMirror.SyncSourcePath:  redMirror,
	}), tracker.GetSnapshot())
	assertDoesNotExist(initialRed.path)

	tracker.Removed("blue")
	assert.Equal(t, MirrorSnapshot(map[string]MirrorFile{
		redMirror.SyncSourcePath: redMirror,
	}), tracker.GetSnapshot())
	assertDoesNotExist(blue.path)

	getRed, ok := tracker.Get("red")
	assert.True(t, ok)
	assert.Equal(t, getRed, redMirror)

	_, ok = tracker.Get("blue")
	assert.False(t, ok)
}

type mockFile struct {
	path     string
	contents string
	mode     os.FileMode
	modTime  time.Time
}

func (f mockFile) fileAttributes() FileAttributes {
	hasher := sha512.New()
	fmt.Fprintf(hasher, f.contents)
	contentsHash := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

	return FileAttributes{
		ContentsHash: contentsHash,
		Mode:         f.mode,
		ModTime:      f.modTime,
	}
}

func toSourceFile(normalizedPath string, f mockFile) SourceFile {
	return SourceFile{
		SyncSourcePath: normalizedPath,
		ContentsPath:   f.path,
		FileAttributes: f.fileAttributes(),
	}
}

func toMirrorFile(localPath string, f mockFile) MirrorFile {
	return MirrorFile{
		SyncSourcePath: localPath,
		ContentsPath:   f.path,
		FileAttributes: f.fileAttributes(),
	}
}

func (f mockFile) writeToFs() error {
	if err := afero.WriteFile(fs, f.path, []byte(f.contents), f.mode); err != nil {
		return err
	}
	return fs.Chtimes(f.path, time.Now(), f.modTime)
}

func randomFile(overrides mockFile) mockFile {
	if overrides.path == "" {
		overrides.path = strconv.Itoa(rand.Int())
	}

	if overrides.contents == "" {
		overrides.contents = strconv.Itoa(rand.Int())
	}

	if overrides.modTime.IsZero() {
		randomTime := time.Date(2019, 11, 10, rand.Intn(23), rand.Intn(59), rand.Intn(59), 0, time.UTC)
		overrides.modTime = randomTime
	}

	if overrides.mode == 0000 {
		overrides.mode = os.FileMode(0640 | rand.Intn(8))
	}
	return overrides
}
