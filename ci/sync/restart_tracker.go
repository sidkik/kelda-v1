package sync

import (
	"context"
	"strings"

	"github.com/kelda-inc/kelda/ci/util"
	"github.com/kelda-inc/kelda/pkg/errors"
)

// restartTracker keeps track of whether the dev process in the container has
// restarted.
type restartTracker struct {
	service     string
	numRestarts int
	helper      *util.TestHelper
}

func NewRestartTracker(helper *util.TestHelper, service string) restartTracker {
	return restartTracker{
		service: service,
		helper:  helper,
	}
}

// HasRestarted returns whether or not the dev process has restarted since the
// last called to `HasRestarted`.
func (t *restartTracker) HasRestarted() (bool, error) {
	logs, err := t.helper.Run(context.Background(), "logs", t.service)
	if err != nil {
		return false, errors.WithContext(err, "get logs")
	}

	oldRestartCount := t.numRestarts
	t.numRestarts = strings.Count(string(logs), "Restarting due to change..")
	switch {
	case t.numRestarts > oldRestartCount:
		return true, nil
	case t.numRestarts == oldRestartCount:
		return false, nil
	default:
		return false, errors.New("number of restarts decreased. The pod may have restarted.")
	}
}
