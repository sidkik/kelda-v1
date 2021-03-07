package fswatch

import (
	"sort"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/kelda-inc/fsnotify"
	"github.com/kelda-inc/kelda/pkg/proto/dev"
)

func TestGetPathsToWatch(t *testing.T) {
	serviceDir := "/svc"

	tests := []struct {
		name       string
		dirs       []string
		files      []string
		syncConfig dev.SyncConfig
		expPaths   []string
	}{
		{
			name: "Simple case -- all directories",
			dirs: []string{"/svc/tests", "/svc/src", "/svc/src/ignored",
				"/svc/src/app", "/svc/src/app/controllers"},
			files: []string{"/svc/tests/test.js", "/svc/src/package.json",
				"/svc/src/app/controllers/index.js"},
			syncConfig: dev.SyncConfig{
				Rules: []*dev.SyncRule{
					{From: "src/app", To: "app"},
					{From: "tests", To: "tests"},
				},
			},
			expPaths: []string{"/svc/src/app", "/svc/src/app/controllers",
				"/svc/src/app/controllers/index.js", "/svc/tests", "/svc/tests/test.js"},
		},
		{
			name:  "Watch file",
			dirs:  []string{"/svc/src", "/svc/src/app", "/home/kevin"},
			files: []string{"/svc/src/package.json", "/svc/src/app/index.js", "/home/kevin/.npmrc"},
			syncConfig: dev.SyncConfig{
				Rules: []*dev.SyncRule{
					{From: "src", To: "src"},
					{From: "/home/kevin/.npmrc", To: ".npmrc"},
				},
			},
			expPaths: []string{"/svc/src", "/svc/src/package.json", "/svc/src/app", "/svc/src/app/index.js",
				"/home/kevin", "/home/kevin/.npmrc"},
		},
		{
			name:  "Don't watch ignored paths",
			dirs:  []string{"/svc/src", "/svc/src/app", "/svc/src/node_modules", "/svc/src/node_modules/express"},
			files: []string{"/svc/src/app/index.js", "/svc/src/node_modules/express/index.js"},
			syncConfig: dev.SyncConfig{
				Rules: []*dev.SyncRule{
					{From: "src", To: "src", Except: []string{"node_modules"}},
				},
			},
			expPaths: []string{"/svc/src", "/svc/src/app", "/svc/src/app/index.js"},
		},
	}

	for _, test := range tests {
		fs = afero.NewMemMapFs()
		for _, dir := range test.dirs {
			assert.NoError(t, fs.Mkdir(dir, 0755))
		}
		for _, file := range test.files {
			assert.NoError(t, afero.WriteFile(fs, file, []byte("testfile"), 0644))
		}

		paths, err := getPathsToWatch(test.syncConfig, serviceDir)
		assert.NoError(t, err)

		// Sort for consistency.
		sort.Strings(test.expPaths)
		sort.Strings(paths)
		assert.Equal(t, test.expPaths, paths, test.name)
	}
}

func TestCombineUpdates(t *testing.T) {
	t.Parallel()

	updates := make(chan fsnotify.Event, 1024)
	addEvents := func(num int) {
		for i := 0; i < num; i++ {
			updates <- fsnotify.Event{}
		}
	}

	// Seed with events.
	numUpdates := 100
	addEvents(numUpdates)
	combined := combineUpdates(updates)

	// Assert that the events are being combined.
	numCombined := countEvents(combined)
	assert.True(t, numCombined < numUpdates,
		"expected less combined events (%d) than %d", numCombined, numUpdates)

	// Add more events.
	addEvents(100)
	<-combined
}

func countEvents(c chan struct{}) (n int) {
	// Block until the first event.
	<-c
	n++

	// Count the number of events until there hasn't been any new events in 500
	// milliseconds.
	for {
		select {
		case <-c:
			n++
		case <-time.After(500 * time.Millisecond):
			return n
		}
	}
}
