package dev

import (
	"os"
	"os/signal"

	"github.com/sirupsen/logrus"

	keldaClientset "github.com/kelda-inc/kelda/pkg/crd/client/clientset/versioned"
)

// noOutputGUI implements a headless GUI that's used during integration tests.
// It doesn't print anything to the screen, and just blocks until `kelda dev`
// is killed.
type noOutputGUI struct{}

func (gui noOutputGUI) Run(_ keldaClientset.Interface, _ string) error {
	// Just wait for Ctrl-C.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	return nil
}

func (gui noOutputGUI) GetLogger() *logrus.Logger {
	return logrus.StandardLogger()
}
