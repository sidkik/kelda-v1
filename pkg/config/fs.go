package config

import "github.com/spf13/afero"

// fs is used for mock tests. It will be overridden by afero.NewMemMapFs()
// in the tests.
var fs = afero.NewOsFs()
