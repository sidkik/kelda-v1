package config

import (
	"fmt"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/sidkik/kelda-v1/pkg/errors"
)

func TestParseUser(t *testing.T) {
	out := ".kelda.yaml"
	userEmptyVersion := User{
		Namespace: "default-namespace",
		Context:   "default-context",
		Workspace: "default-workspace",
	}
	userInitialVersion := User{
		Version:   InitialUserConfigVersion,
		Namespace: "default-namespace",
		Context:   "default-context",
		Workspace: "default-workspace",
	}
	userCorrectVersion := User{
		Version:   SupportedUserConfigVersion,
		Namespace: "default-namespace",
		Context:   "default-context",
		Workspace: "default-workspace",
	}
	userIncorrectVersion := User{
		Version:   "incorrect_version",
		Namespace: "default-namespace",
		Context:   "default-context",
		Workspace: "default-workspace",
	}
	userEmptyVersionString, err := yaml.Marshal(userEmptyVersion)
	assert.NoError(t, err)
	userCorrectVersionString, err := yaml.Marshal(userCorrectVersion)
	assert.NoError(t, err)
	userIncorrectVersionString, err := yaml.Marshal(userIncorrectVersion)
	assert.NoError(t, err)

	tests := []struct {
		input     []byte
		expConfig User
		expError  error
	}{
		{
			input:     userEmptyVersionString,
			expConfig: userInitialVersion,
			expError:  nil,
		},
		{
			input:     userCorrectVersionString,
			expConfig: userCorrectVersion,
			expError:  nil,
		},
		{
			input:     userIncorrectVersionString,
			expConfig: User{},
			expError: errors.WithContext(incompatibleVersionError{
				path:   out,
				exp:    SupportedUserConfigVersion,
				actual: userIncorrectVersion.Version,
			}, "parse"),
		},
		{
			input: []byte(fmt.Sprintf(
				"version: %s\nextra: fields", SupportedUserConfigVersion)),
			expError: errors.WithContext(
				errors.NewFriendlyError(parseConfigErrTemplate, out,
					errors.New("error unmarshaling JSON: while decoding JSON: "+
						`json: unknown field "extra"`)),
				"parse"),
		},
		{
			input: []byte(`
version: incorrect_version
extra: fields
`),
			expError: errors.WithContext(incompatibleVersionError{
				path:   out,
				exp:    SupportedUserConfigVersion,
				actual: "incorrect_version",
			}, "parse"),
		},
	}

	fs = afero.NewMemMapFs()
	homedirExpand = func(_ string) (string, error) {
		return out, nil
	}
	for _, test := range tests {
		err := afero.WriteFile(fs, out, test.input, 0644)
		assert.NoError(t, err)
		config, err := ParseUser()
		assert.Equal(t, test.expConfig, config)
		assert.Equal(t, test.expError, err)
	}
}

func TestParseWrittenUser(t *testing.T) {
	fs = afero.NewMemMapFs()
	homedirExpand = func(_ string) (string, error) {
		return ".kelda.yaml", nil
	}

	user := User{
		Namespace: "namespace",
		Context:   "context",
		Workspace: "workspace",
	}

	// Write the user to disk, and assert that we get the same user config when
	// we parse it.
	assert.NoError(t, WriteUser(user))

	parsed, err := ParseUser()
	assert.NoError(t, err)

	user.Version = SupportedUserConfigVersion
	assert.Equal(t, user, parsed)
}
