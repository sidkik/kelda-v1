package sync

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	homedir "github.com/mitchellh/go-homedir"

	"github.com/kelda-inc/kelda/pkg/config"
	"github.com/kelda-inc/kelda/pkg/errors"
)

type file struct {
	path     string
	contents string
	mode     os.FileMode
	modTime  time.Time
}

func (f file) WithPath(path string) file {
	f.path = path
	return f
}

func (f file) WithContents(contents string) file {
	f.contents = contents
	return f
}

func (f file) WithMode(mode os.FileMode) file {
	f.mode = mode
	return f
}

func (f file) WithModTime(modTime time.Time) file {
	f.modTime = modTime
	return f
}

func randomFile(path string) file {
	randomTime := time.Date(2019, 11, 10, rand.Intn(23), rand.Intn(59), rand.Intn(59), 0, time.UTC)
	return file{
		path:     path,
		contents: strconv.Itoa(rand.Int()),
		mode:     os.FileMode(0640 | rand.Intn(8)),
		modTime:  randomTime,
	}
}

// mockFs contains helper methods for creating temporary file structures for
// testing.
type mockFs struct {
	root       string
	chrootDir  string
	serviceDir string
	homeDir    string

	originalHomeDir string
}

type fsOp func(mockFs) error

func newMockFs() (mockFs, error) {
	root, err := ioutil.TempDir("", "kelda-sync-test")
	if err != nil {
		return mockFs{}, errors.WithContext(err, "make chroot dir")
	}

	chrootDir := filepath.Join(root, "chroot")
	if err := os.Mkdir(chrootDir, 0755); err != nil {
		return mockFs{}, errors.WithContext(err, "make chroot directory")
	}

	serviceDir := filepath.Join(root, "service-dir")
	if err := os.Mkdir(serviceDir, 0755); err != nil {
		return mockFs{}, errors.WithContext(err, "make service directory")
	}

	mockHomeDir := filepath.Join(root, "home")
	if err := os.Mkdir(mockHomeDir, 0755); err != nil {
		return mockFs{}, errors.WithContext(err, "make home directory")
	}

	currHomeDir, err := homedir.Dir()
	if err != nil {
		return mockFs{}, errors.WithContext(err, "get current homedir")
	}

	for _, toLink := range []string{"~/.kube", "~/.config/gcloud", config.UserConfigPath} {
		if len(toLink) == 0 || toLink[0] != '~' {
			return mockFs{}, fmt.Errorf("%q is not a file in the homedir", toLink)
		}

		src := filepath.Join(currHomeDir, toLink[1:])
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}

		dst := filepath.Join(mockHomeDir, toLink[1:])
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return mockFs{}, errors.WithContext(err, "make parent")
		}

		if err := os.Symlink(src, dst); err != nil {
			return mockFs{}, errors.WithContext(err, "link user config")
		}
	}

	originalHomeDir := os.Getenv("HOME")
	os.Setenv("HOME", mockHomeDir)

	return mockFs{
		root:            root,
		chrootDir:       chrootDir,
		serviceDir:      serviceDir,
		homeDir:         mockHomeDir,
		originalHomeDir: originalHomeDir,
	}, nil
}

func (fs mockFs) getMockPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		return filepath.Join(fs.homeDir, path[1:])
	}

	if filepath.IsAbs(path) {
		return fs.chroot(path)
	}
	return filepath.Join(fs.serviceDir, path)
}

func (fs mockFs) cleanup() error {
	os.Setenv("HOME", fs.originalHomeDir)
	return os.RemoveAll(fs.root)
}

func (fs mockFs) chroot(path string) string {
	return filepath.Join(fs.chrootDir, path)
}

func (fs mockFs) writeSyncConfig(devConfig config.SyncConfig) error {
	yamlBytes, err := yaml.Marshal(devConfig)
	if err != nil {
		return errors.WithContext(err, "marshal")
	}

	devCfgPath := filepath.Join(fs.serviceDir, "kelda.yaml")
	return ioutil.WriteFile(devCfgPath, yamlBytes, 0644)
}

func createFile(toCreate file) fsOp {
	return func(fs mockFs) error {
		toCreate.path = fs.getMockPath(toCreate.path)

		parent := filepath.Dir(toCreate.path)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return errors.WithContext(err, "make parent")
		}

		f, err := os.Create(toCreate.path)
		if err != nil {
			return errors.WithContext(err, "create")
		}
		defer f.Close()

		_, err = io.Copy(f, bytes.NewReader([]byte(toCreate.contents)))
		if err != nil {
			return errors.WithContext(err, "write")
		}

		if err := os.Chmod(toCreate.path, toCreate.mode); err != nil {
			return errors.WithContext(err, "chmod")
		}

		if err := os.Chtimes(toCreate.path, time.Now(), toCreate.modTime); err != nil {
			return errors.WithContext(err, "chtimes")
		}
		return nil
	}
}

func removeFile(f string) fsOp {
	return func(fs mockFs) error {
		return os.Remove(fs.getMockPath(f))
	}
}
