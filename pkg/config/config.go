package config

import (
	"fmt"
	"os"

	"github.com/ghodss/yaml"
	"github.com/spf13/afero"

	"github.com/sidkik/kelda-v1/pkg/errors"
)

// parseConfigErrTemplate is a template for when the CLI fails to parse yaml
// configuration files. This can happen for a multitude of reasons, including
// extraneous fields and incorrect field types. However, the yaml library
// constructs errors in a way that loses context, and so we can only pass the
// error message on.
const parseConfigErrTemplate = "Configuration file could not be parsed. " +
	"Please review %q.\n" +
	"Common pitfalls include:\n" +
	" - Using the wrong types for fields\n" +
	" - Having extra fields inside the config file\n\n" +
	"For reference, here is the error from the parser:\n" +
	"%s"

type configInterface interface {
	getVersion() string
}

type incompatibleVersionError struct {
	path, exp, actual string
}

func (err incompatibleVersionError) Error() string {
	return err.FriendlyMessage()
}

func (err incompatibleVersionError) FriendlyMessage() string {
	return fmt.Sprintf("The configuration file %q is incompatible "+
		"with this version of Kelda.\n"+
		"Expected version %q, but got %q.", err.path, err.exp, err.actual)
}

func parseConfig(path string, config configInterface, expVersion string) error {
	configBytes, err := afero.ReadFile(fs, path)
	if err != nil {
		if isPathNotFoundError(err) {
			return errors.FileNotFound{Path: path}
		}
		return errors.WithContext(err, "read file")
	}

	err = yaml.Unmarshal(configBytes, config)
	if err != nil {
		return errors.NewFriendlyError(parseConfigErrTemplate, path, err)
	}

	if config.getVersion() != expVersion {
		return incompatibleVersionError{path, expVersion, config.getVersion()}
	}

	// Do a strict unmarshal to check for any extra fields. We do a non-strict
	// unmarshal first so that we can catch version errors before erroring on
	// extra fields.
	err = yaml.UnmarshalStrict(configBytes, config, yaml.DisallowUnknownFields)
	if err != nil {
		return errors.NewFriendlyError(parseConfigErrTemplate, path, err)
	}
	return nil
}

func isPathNotFoundError(err error) bool {
	if fileErr, ok := err.(*os.PathError); ok &&
		fileErr.Op == "open" && fileErr.Err.Error() == "no such file or directory" {
		return true
	}
	return false
}
