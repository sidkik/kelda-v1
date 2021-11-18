package analytics

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/sidkik/kelda-v1/pkg/errors"
	"github.com/sidkik/kelda-v1/pkg/version"
)

func TestAnalyticsLogger(t *testing.T) {
	var postPayloads []interface{}
	httpPost = func(endpoint, contentType string, body io.Reader) (*http.Response, error) {
		assert.Equal(t, endpoint, ddEndpoint)
		assert.Equal(t, contentType, ddContentType)

		bodyBytes, err := ioutil.ReadAll(body)
		assert.NoError(t, err)

		var payload interface{}
		err = json.Unmarshal(bodyBytes, &payload)
		assert.NoError(t, err)

		postPayloads = append(postPayloads, payload)

		respBody := ioutil.NopCloser(bytes.NewBufferString("unused"))
		return &http.Response{Body: respBody}, nil
	}

	mockTime := time.Unix(1569172899, 0).UTC()

	// Force the analytics logger to reinitialize even though we're running in
	// a unit test.
	version.Version = "testing-version"
	Log = newAnalyticsLogger()

	// Only set some tags.
	SetSource("dev")
	SetNamespace("namespace")
	Log.WithFields(logrus.Fields{
		"service": "web",
		"error":   errors.New("wrapped error message"),
	}).WithTime(mockTime).Error("message")
	assert.Len(t, postPayloads, 1)
	assert.Equal(t, postPayloads[0], map[string]interface{}{
		"ddsource":  "dev",
		"ddtags":    "stream:analytics,kelda-version:testing-version,namespace:namespace",
		"message":   "message",
		"service":   "web",
		"error":     "wrapped error message",
		"status":    "error",
		"timestamp": "2019-09-22T17:21:39Z",
	})

	// Test that Panics get converted to Fatal.
	func() {
		defer func() {
			recover()
		}()
		Log.WithTime(mockTime).Panic("Panic!")
	}()
	assert.Len(t, postPayloads, 2)
	assert.Equal(t, postPayloads[1], map[string]interface{}{
		"ddsource":  "dev",
		"ddtags":    "stream:analytics,kelda-version:testing-version,namespace:namespace",
		"message":   "Panic!",
		"status":    "fatal",
		"timestamp": "2019-09-22T17:21:39Z",
	})

	// Set all tags, and log at INFO.
	SetKubeVersion("1.13")
	SetCustomer("test-customer")
	Log.WithFields(logrus.Fields{
		"service": "web",
		"added":   5,
	}).WithTime(mockTime).Info("Synced")
	assert.Len(t, postPayloads, 3)
	assert.Equal(t, postPayloads[2], map[string]interface{}{
		"ddsource": "dev",
		"ddtags": "stream:analytics,kelda-version:testing-version,namespace:namespace," +
			"kube-version:1.13,customer:test-customer",
		"message":   "Synced",
		"service":   "web",
		"added":     float64(5),
		"status":    "info",
		"timestamp": "2019-09-22T17:21:39Z",
	})
}
