package dev

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"

	configCmd "github.com/sidkik/kelda-v1/cmd/config"
	loginCmd "github.com/sidkik/kelda-v1/cmd/login"
	"github.com/sidkik/kelda-v1/pkg/config"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

var (
	demoLogin   = "demo@kelda.io"
	demoContext = loginCmd.IdentifierForEmail(demoLogin)
)

func setupDemo() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", errors.WithContext(err, "get working directory")
	}
	examplesPath := filepath.Join(wd, "kelda-examples")

	if _, err := os.Stat(examplesPath); err == nil && !os.IsNotExist(err) {
		fmt.Println("kelda-examples repo already exists")
	} else {
		fmt.Println("Cloning the example application from https://github.com/kelda-inc/examples...")
		fmt.Println("This can take a couple minutes on slow internet connections " +
			"due to the number of example applications.")
		fmt.Println()

		_, err = git.PlainClone(examplesPath, false, &git.CloneOptions{
			URL:           "https://github.com/kelda-inc/examples",
			Progress:      os.Stdout,
			Depth:         1,
			ReferenceName: plumbing.Master,
			SingleBranch:  true,
		})
		if err != nil {
			return "", errors.WithContext(err, "clone examples repo")
		}
	}

	fmt.Println()
	fmt.Println("-----------------------------------------------------------------------------")
	fmt.Println()

	fmt.Println("Connecting to the demo Hosted Kelda cluster...")
	fmt.Println()
	if err := loginCmd.Main(demoLogin, ""); err != nil {
		return "", errors.WithContext(err, "login to demo cluster")
	}

	fmt.Println()
	fmt.Println("-----------------------------------------------------------------------------")
	fmt.Println()

	fmt.Println("Running `kelda config` to initialize your local configuration...")
	fmt.Println()
	if err := configCmd.SetupConfig(config.User{
		Context:   demoContext,
		Workspace: filepath.Join(examplesPath, "magda/magda-kelda-config/workspace.yaml"),
	}); err != nil {
		return "", errors.WithContext(err, "run `kelda config`")
	}

	fmt.Println()
	fmt.Println("-----------------------------------------------------------------------------")
	fmt.Println()

	return filepath.Join(examplesPath, "magda/magda-web-server"), nil
}
