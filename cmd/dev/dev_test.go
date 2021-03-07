package dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompatibleVersions(t *testing.T) {
	tests := []struct {
		name          string
		remoteVersion string
		localVersion  string
		compatible    bool
	}{
		{
			name:          "Same Version",
			remoteVersion: "0.14.1",
			localVersion:  "0.14.1",
			compatible:    true,
		},
		{
			name:          "same version",
			remoteVersion: "latest",
			localVersion:  "latest",
			compatible:    true,
		},
		{
			name:          "compatible versions",
			remoteVersion: "0.14.2",
			localVersion:  "0.14.1",
			compatible:    true,
		},
		{
			name:          "latest and versioned compatible",
			remoteVersion: "0.14.2",
			localVersion:  "latest",
			compatible:    true,
		},
		{
			name:          "diff versions - minor version",
			remoteVersion: "0.14.0",
			localVersion:  "0.13.0",
			compatible:    false,
		},
		{
			name:          "diff versions - major version",
			remoteVersion: "2.1.0",
			localVersion:  "1.1.0",
			compatible:    false,
		},
		{
			name:          "diff version - no .",
			remoteVersion: "0",
			localVersion:  "1",
			compatible:    true,
		},
		{
			name:          "empty version",
			remoteVersion: "",
			localVersion:  "1",
			compatible:    true,
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.compatible, compatibleVersions(test.remoteVersion, test.localVersion), test.name)
	}
}
