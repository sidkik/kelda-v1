package config

import (
	"path/filepath"

	homedir "github.com/mitchellh/go-homedir"

	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/proto/dev"
	log "github.com/sirupsen/logrus"
)

// SyncConfig contains the configuration that specifies how a service should
// be run when it's in development mode.
type SyncConfig struct {
	Version     string     `json:"version,omitempty"`
	Name        string     `json:"name"` // Required.
	Sync        []SyncRule `json:"sync,omitempty"`
	Command     []string   `json:"command,omitempty"`
	InitCommand []string   `json:"initCommand,omitempty"`
	Image       string     `json:"image,omitempty"`

	// Only populated and consumed by Kelda. Never set by user.
	path string
}

// SyncRule defines how to sync files from the local machine into the
// development container.
type SyncRule struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	Except      []string `json:"except"`
	TriggerInit bool     `json:"triggerInit"`
}

// GetPath returns the filepath that the service was parsed from. A getter
// method is used rather than making the field public so that it can't get set
// by the yaml Unmarshalling.
func (c SyncConfig) GetPath() string {
	return c.path
}

func (c SyncConfig) getVersion() string {
	return c.Version
}

var alwaysIgnored = []string{"kelda.log", "kelda.yaml", ".git", ".DS_Store"}

// InitialSyncConfigVersion is the first version of the Kelda
// sync config. Config files that do not specify a version
// will default to this version.
const InitialSyncConfigVersion = "v1alpha1"

// SupportedSyncConfigVersion is the supported version of the
// Kelda sync config of the current Kelda binary.
const SupportedSyncConfigVersion = "v1alpha1"

// ParseSyncConfig parses the service configuration for `name` in the directory
// `path`.
func ParseSyncConfig(path string) (SyncConfig, error) {
	configPath := filepath.Join(path, "kelda.yaml")
	config := SyncConfig{
		path:    configPath,
		Version: InitialSyncConfigVersion,
	}
	if err := parseConfig(configPath, &config, SupportedSyncConfigVersion); err != nil {
		return SyncConfig{}, errors.WithContext(err, "parse")
	}

	if config.Name == "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = path
			log.WithError(err).Debug("Failed to parse absolute path")
		}
		return SyncConfig{}, errors.NewFriendlyError(
			"The service defined in %q does not have a name set.\n"+
				"The name field in the Sync configuration is required, "+
				"and must match one of the services defined in the "+
				"Workspace configuration.", filepath.Base(absPath))
	}

	var cleanedRules []SyncRule
	for _, rule := range config.Sync {
		// Expand ~'s in the source keys in the sync map.
		from, err := homedir.Expand(rule.From)
		if err != nil {
			return SyncConfig{}, errors.WithContext(err, "expand homedir")
		}

		rule.Except = append(rule.Except, alwaysIgnored...)

		rule.From = filepath.Clean(from)
		rule.To = filepath.Clean(rule.To)
		for i, exception := range rule.Except {
			rule.Except[i] = filepath.Clean(exception)
		}

		cleanedRules = append(cleanedRules, rule)
	}
	config.Sync = cleanedRules

	return config, nil
}

// GetSyncConfigProto returns the sync configuration as the protobuf type.
func (c SyncConfig) GetSyncConfigProto() (syncConfig dev.SyncConfig) {
	for _, rule := range c.Sync {
		syncConfig.Rules = append(syncConfig.Rules, &dev.SyncRule{
			From:        rule.From,
			To:          rule.To,
			Except:      rule.Except,
			TriggerInit: rule.TriggerInit,
		})
	}
	syncConfig.OnSyncCommand = c.Command
	syncConfig.OnInitCommand = c.InitCommand
	return
}
