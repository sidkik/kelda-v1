package sync

import (
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/sidkik/kelda-v1/pkg/proto/dev"
)

func TestVersion(t *testing.T) {
	refVersion := Version{
		LocalFiles: map[string]SourceFile{
			"foo": {
				SyncSourcePath: "foo",
				FileAttributes: FileAttributes{
					ContentsHash: "foo-hash",
					Mode:         0644,
					ModTime:      time.Now(),
				},
			},
			"/dir/subfile": {
				SyncSourcePath: "/dir/subfile",
				FileAttributes: FileAttributes{
					ContentsHash: "subfile-hash",
					Mode:         0755,
					ModTime:      time.Now(),
				},
			},
		},
		SyncConfig: dev.SyncConfig{
			Rules: []*dev.SyncRule{
				{From: "foo", To: "foo-dst"},
				{From: "dir", To: "dir-dst"},
			},
			OnSyncCommand: []string{"true"},
		},
	}

	tests := []struct {
		name        string
		version     Version
		shouldMatch bool
	}{
		{
			name:        "Matching",
			shouldMatch: true,
			version:     refVersion,
		},

		{
			name:        "IgnoresOrder",
			shouldMatch: true,
			version: Version{
				LocalFiles: refVersion.LocalFiles,
				SyncConfig: dev.SyncConfig{
					Rules: []*dev.SyncRule{
						refVersion.SyncConfig.Rules[1],
						refVersion.SyncConfig.Rules[0],
					},
					OnSyncCommand: refVersion.SyncConfig.OnSyncCommand,
				},
			},
		},

		{
			name:        "DifferentCommand",
			shouldMatch: false,
			version: Version{
				LocalFiles: refVersion.LocalFiles,
				SyncConfig: dev.SyncConfig{
					Rules:         refVersion.SyncConfig.Rules,
					OnSyncCommand: []string{"changed"},
				},
			},
		},

		{
			name:        "DifferentContents",
			shouldMatch: false,
			version: Version{
				LocalFiles: map[string]SourceFile{
					"foo":          refVersion.LocalFiles["foo"],
					"/dir/subfile": withContentsHash(refVersion.LocalFiles["/dir/subfile"], "changed"),
				},
				SyncConfig: refVersion.SyncConfig,
			},
		},

		{
			name:        "DifferentModTime",
			shouldMatch: false,
			version: Version{
				LocalFiles: map[string]SourceFile{
					"foo": refVersion.LocalFiles["foo"],
					"/dir/subfile": withModTime(refVersion.LocalFiles["/dir/subfile"],
						refVersion.LocalFiles["/dir/subfile"].ModTime.Add(1*time.Second)),
				},
				SyncConfig: refVersion.SyncConfig,
			},
		},

		{
			name:        "DifferentMode",
			shouldMatch: false,
			version: Version{
				LocalFiles: map[string]SourceFile{
					"foo":          withMode(refVersion.LocalFiles["foo"], 0600),
					"/dir/subfile": refVersion.LocalFiles["/dir/subfile"],
				},
				SyncConfig: refVersion.SyncConfig,
			},
		},

		{
			name:        "AddedFile",
			shouldMatch: false,
			version: Version{
				LocalFiles: map[string]SourceFile{
					"/dir/subfile": refVersion.LocalFiles["/dir/subfile"],
				},
				SyncConfig: refVersion.SyncConfig,
			},
		},

		{
			name:        "RemovedFile",
			shouldMatch: false,
			version: Version{
				LocalFiles: map[string]SourceFile{
					"foo":          refVersion.LocalFiles["foo"],
					"/dir/subfile": refVersion.LocalFiles["/dir/subfile"],
					"extra": {
						SyncSourcePath: "extra",
						FileAttributes: FileAttributes{
							ContentsHash: "extra-hash",
							Mode:         0400,
							ModTime:      time.Now(),
						},
					},
				},
				SyncConfig: refVersion.SyncConfig,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.shouldMatch {
				assert.Equal(t, refVersion.String(), test.version.String())
			} else {
				assert.NotEqual(t, refVersion.String(), test.version.String())
			}
		})
	}
}

func TestAppliesTo(t *testing.T) {
	tests := []struct {
		rule dev.SyncRule
		path string
		exp  bool
	}{
		{
			rule: dev.SyncRule{From: "src-file", To: "dst-file"},
			path: "src-file",
			exp:  true,
		},
		{
			rule: dev.SyncRule{From: "src-dir", To: "dst-dir", Except: []string{"ignoreme"}},
			path: "src-dir/file",
			exp:  true,
		},
		{
			rule: dev.SyncRule{From: ".", To: "dst-dir"},
			path: "src-file",
			exp:  true,
		},
		{
			rule: dev.SyncRule{From: "src-dir", To: "dst-dir", Except: []string{"ignoreme"}},
			path: "src-dir/ignoreme",
			exp:  false,
		},
		{
			rule: dev.SyncRule{From: "src-file", To: "dst-file"},
			path: "another-file",
			exp:  false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.exp, AppliesTo(test.rule, test.path))
	}
}

func TestDestination(t *testing.T) {
	fs = afero.NewMemMapFs()
	assert.NoError(t, fs.Mkdir("existing-dir", 0755))

	tests := []struct {
		rule   dev.SyncRule
		path   string
		expDst string
		expOK  bool
	}{
		{
			rule:   dev.SyncRule{From: "src-file", To: "dst-file"},
			path:   "src-file",
			expDst: "dst-file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: "src-dir", To: "dst-dir"},
			path:   "src-dir/file",
			expDst: "dst-dir/file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: "src-file", To: "/"},
			path:   "src-file",
			expDst: "/src-file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: "src-dir", To: "/"},
			path:   "src-dir/file",
			expDst: "/file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: "src-dir", To: "existing-dir"},
			path:   "src-dir/file",
			expDst: "existing-dir/file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: ".", To: "existing-dir"},
			path:   "src-dir/file",
			expDst: "existing-dir/src-dir/file",
			expOK:  true,
		},
		{
			rule:   dev.SyncRule{From: ".", To: "."},
			path:   "src-dir/file",
			expDst: "src-dir/file",
			expOK:  true,
		},
		{
			rule:  dev.SyncRule{From: "src-dir", To: "dst-dir", Except: []string{"ignoreme"}},
			path:  "src-dir/ignoreme",
			expOK: false,
		},
		{
			rule:  dev.SyncRule{From: "src-file", To: "dst-file"},
			path:  "another-file",
			expOK: false,
		},
	}
	for _, test := range tests {
		dst, ok := destination(test.rule, test.path)
		assert.Equal(t, test.expOK, ok)
		assert.Equal(t, test.expDst, dst)
	}
}

func TestHashFile(t *testing.T) {
	fs = afero.NewMemMapFs()

	assert.NoError(t, afero.WriteFile(fs, "red", []byte("red"), 0644))
	assert.NoError(t, afero.WriteFile(fs, "another-red", []byte("red"), 0644))
	assert.NoError(t, afero.WriteFile(fs, "blue", []byte("blue"), 0644))

	redHash, err := HashFile("red")
	assert.NoError(t, err)

	anotherRedHash, err := HashFile("another-red")
	assert.NoError(t, err)

	blueHash, err := HashFile("blue")
	assert.NoError(t, err)

	assert.Equal(t, redHash, anotherRedHash)
	assert.NotEqual(t, redHash, blueHash)
}

func withContentsHash(f SourceFile, hash string) SourceFile {
	f.FileAttributes.ContentsHash = hash
	return f
}

func withMode(f SourceFile, mode os.FileMode) SourceFile {
	f.FileAttributes.Mode = mode
	return f
}

func withModTime(f SourceFile, t time.Time) SourceFile {
	f.FileAttributes.ModTime = t
	return f
}
