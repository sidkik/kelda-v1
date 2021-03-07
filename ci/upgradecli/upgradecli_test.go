// +build upgradetest

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgrade(t *testing.T) {
	gopath := os.Getenv("GOPATH")
	keldaPath := filepath.Join(gopath, "/bin/kelda")

	// Test upgrading Kelda.
	require.NoError(t, os.Rename(filepath.Join(gopath, "/bin/kelda-min-version"), keldaPath))
	_, err := runKeldaUpgradeCli(t)
	require.NoError(t, err)
	clientVersion, minionVersion := getVersions(t)
	require.Equal(t, minionVersion, clientVersion)

	// Test downgrading Kelda.
	require.NoError(t, os.Rename(filepath.Join(gopath, "/bin/kelda-max-version"), keldaPath))
	_, err = runKeldaUpgradeCli(t)
	require.NoError(t, err)
	clientVersion, minionVersion = getVersions(t)
	require.Equal(t, minionVersion, clientVersion)

	// Test when versions are equal that no command was executed.
	command, err := runKeldaUpgradeCli(t)
	require.NoError(t, err)
	require.Equal(t, minionVersion, clientVersion)
	require.Equal(t, "", command)
}

// getVersions returns the Kelda CLI and Minion version.
func getVersions(t *testing.T) (string, string) {
	cmd := exec.Command("kelda", "version")
	stdout, err := cmd.CombinedOutput()
	require.NoError(t, err)
	stdoutStr := string(stdout)

	ownVer := regexp.MustCompile(`local version:\s+([0-9]+\.[0-9]+\.[0-9])`)
	match := ownVer.FindAllStringSubmatch(stdoutStr, 1)
	require.NotNil(t, match)
	ownVerStr := match[0][1]

	minionVer := regexp.MustCompile(`minion version:\s+([0-9]+\.[0-9]+\.[0-9])`)
	match = minionVer.FindAllStringSubmatch(stdoutStr, 1)
	require.NotNil(t, match)
	minionVerStr := match[0][1]

	return ownVerStr, minionVerStr
}

// runKeldaUpgradeCli executes `kelda upgrade-cli` and returns the
// executed post-install command.
func runKeldaUpgradeCli(t *testing.T) (string, error) {
	cmd := exec.Command("kelda", "upgrade-cli")
	cmd.Stdin = bytes.NewBuffer([]byte("y\n"))
	stdout, err := cmd.CombinedOutput()
	stdoutStr := string(stdout)
	require.NoError(t, err, stdoutStr)

	// Execute the post-install copy command if one exists.
	// CI should ensure that `sudo` is not required to install.
	pattern := regexp.MustCompile(`(cp) (\./kelda) (\/\S*kelda)`)
	match := pattern.FindAllStringSubmatch(stdoutStr, 1)
	if len(match) == 0 {
		// No post-install command.
		return "", nil
	} else if len(match[0]) == 4 {
		// Found post-install command.
		fullCmd := match[0][0]
		program := match[0][1]
		args := match[0][2:4]

		cmd = exec.Command(program, args...)
		err := cmd.Run()
		require.NoError(t, err)
		return fullCmd, nil
	}

	// Not supposed to happen.
	return "", fmt.Errorf("invalid post-install command: %s", match)
}
