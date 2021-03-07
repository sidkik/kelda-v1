package config

import (
	"path/filepath"

	"github.com/ghodss/yaml"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/afero"

	"github.com/kelda-inc/kelda/pkg/errors"
)

const (
	// KeldaDemoContext is the name of the context that connect to the
	// Kelda-maintained demo cluster. If this context is set, the user's local
	// kubeconfig is ignored.
	KeldaDemoContext = "kelda-demo-cluster"

	// UserConfigPath is the default path to the Kelda user config.
	UserConfigPath = "~/.kelda.yaml"

	// InitialUserConfigVersion is the first version of the Kelda
	// user config. Config files that do not specify a version
	// will default to this version.
	InitialUserConfigVersion = "v1alpha1"

	// SupportedUserConfigVersion is the supported version of the
	// Kelda user config of the current Kelda binary.
	SupportedUserConfigVersion = "v1alpha1"
)

// User contains configuration used to identify the user.
type User struct {
	Version   string `json:"version,omitempty"`
	Namespace string `json:"namespace"`
	Context   string `json:"context"`
	Workspace string `json:"workspace"`
}

func (u User) getVersion() string {
	return u.Version
}

// homedirExpand will be overridden in mock tests
var homedirExpand = homedir.Expand

// ParseUser attempts to parse the User stored in the default path.
func ParseUser() (User, error) {
	path, err := GetUserConfigPath()
	if err != nil {
		return User{}, errors.WithContext(err, "expand config path")
	}

	config := User{Version: InitialUserConfigVersion}
	if err := parseConfig(path, &config, SupportedUserConfigVersion); err != nil {
		if _, ok := err.(errors.FileNotFound); ok {
			return User{}, errors.NewFriendlyError("The Kelda user config "+
				"file doesn't exist at %q. Please run `kelda config` in your "+
				"workspace directory to create the user config file.", path)
		}
		return User{}, errors.WithContext(err, "parse")
	}

	config.Workspace, err = homedir.Expand(config.Workspace)
	if err != nil {
		return User{}, errors.WithContext(err, "expand workspace path")
	}

	// Evaluate relative paths relative to the config path.
	if config.Workspace != "" && !filepath.IsAbs(config.Workspace) {
		config.Workspace = filepath.Join(filepath.Dir(path), config.Workspace)
	}
	return config, nil
}

// WriteUser writes the given user config to disk.
func WriteUser(cfg User) error {
	cfg.Version = SupportedUserConfigVersion
	path, err := GetUserConfigPath()
	if err != nil {
		return errors.WithContext(err, "expand config path")
	}

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.WithContext(err, "marshal")
	}

	if err := afero.WriteFile(fs, path, yamlBytes, 0644); err != nil {
		return errors.WithContext(err, "write")
	}
	return nil
}

// Get the path to the user's global Kelda configuration. This path is
// expanded, so it can be directly passed to file operations.
func GetUserConfigPath() (string, error) {
	return homedirExpand(UserConfigPath)
}
