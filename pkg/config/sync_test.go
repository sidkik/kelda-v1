package config

import (
	"fmt"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/sidkik/kelda-v1/pkg/errors"
)

func TestParseSyncConfig(t *testing.T) {
	path := "."
	out := "kelda.yaml"
	name := "test_service"

	tests := []struct {
		name      string
		input     []byte
		expConfig SyncConfig
		expError  error
	}{
		{
			name:  "EmptyVersion",
			input: mustMarshal(SyncConfig{Name: name}),
			expConfig: SyncConfig{
				Version: InitialSyncConfigVersion,
				Name:    name,
				path:    out,
			},
			expError: nil,
		},
		{
			name: "CorrectVersion",
			input: mustMarshal(SyncConfig{
				Version: SupportedSyncConfigVersion,
				Name:    name,
			}),
			expConfig: SyncConfig{
				Version: SupportedSyncConfigVersion,
				Name:    name,
				path:    out,
			},
			expError: nil,
		},
		{
			name: "IncorrectVersion",
			input: mustMarshal(SyncConfig{
				Version: "incorrect_version",
				Name:    name,
			}),
			expConfig: SyncConfig{},
			expError: errors.WithContext(incompatibleVersionError{
				path:   out,
				exp:    SupportedSyncConfigVersion,
				actual: "incorrect_version",
			}, "parse"),
		},
		{
			name: "EmptyName",
			input: mustMarshal(SyncConfig{
				Version: SupportedSyncConfigVersion,
			}),
			expError: errors.NewFriendlyError(
				"The service defined in \"config\" does not have a name set.\n" +
					"The name field in the Sync configuration is required, " +
					"and must match one of the services defined in the " +
					"Workspace configuration."),
		},
		{
			name: "ExtraFields",
			input: []byte(fmt.Sprintf(
				"version: %s\nextra: fields", SupportedSyncConfigVersion)),
			expError: errors.WithContext(
				errors.NewFriendlyError(parseConfigErrTemplate, out,
					errors.New("error unmarshaling JSON: while decoding JSON: "+
						`json: unknown field "extra"`)),
				"parse"),
		},
		{
			name: "IncorrectVersionAndExtraFields",
			input: []byte(`
version: incorrect_version
extra: fields
`),
			expError: errors.WithContext(incompatibleVersionError{
				path:   out,
				exp:    SupportedSyncConfigVersion,
				actual: "incorrect_version",
			}, "parse"),
		},
	}

	fs = afero.NewMemMapFs()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := afero.WriteFile(fs, out, test.input, 0644)
			assert.NoError(t, err)
			config, err := ParseSyncConfig(path)
			assert.Equal(t, test.expConfig, config)
			assert.Equal(t, test.expError, err)
		})
	}
}

func mustMarshal(cfg interface{}) []byte {
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		panic(fmt.Errorf("bad test input, unable to marshal to yaml: %s", err))
	}
	return yamlBytes
}
