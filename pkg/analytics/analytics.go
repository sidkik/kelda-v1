package analytics

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/sidkik/kelda-v1/pkg/version"
)

var (
	// Log is the global analytics logger. Log events created via this object are
	// automatically pushed into our analytics system.
	Log = newAnalyticsLogger()

	// Optional values for automatically enriching the analytics metadata
	// that's sent to DataDog.
	source      string
	namespace   string
	kubeVersion string
	customer    string

	// Mocked out for unit testing.
	httpPost = http.Post
)

func newAnalyticsLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(ioutil.Discard)

	// Don't actually publish analytics if we weren't compiled from `make`
	// (i.e. we're most likely being called from `go test`), or if we're
	// running a development copy of Kelda.
	if version.Version != version.EmptyValue || strings.HasSuffix(version.Version, "-dev") {
		logger.AddHook(&hook{logrus.AllLevels, analyticsStream})
	}

	return logger
}

const (
	// Documentation: https://docs.datadoghq.com/api/?lang=python#send-logs-over-http
	// https://docs.datadoghq.com/logs/log_collection/?tab=ussite#datadog-logs-endpoints
	ddEndpoint    = "https://http-intake.logs.datadoghq.com/v1/input/30ec6c202c7ef0171de5d489f67d4aeb"
	ddContentType = "application/json"

	analyticsStream = "analytics"
	loggingStream   = "logging"
)

// ddFormatter formats log entries according to DD's preferred format
var ddFormatter = &logrus.JSONFormatter{
	FieldMap: logrus.FieldMap{
		logrus.FieldKeyTime:  "timestamp",
		logrus.FieldKeyLevel: "status",
		logrus.FieldKeyMsg:   "message",
	},
}

// NewHook creates a new hook that forwards log messages to the Kelda analytics
// system.
func NewLogHook() logrus.Hook {
	levels := []logrus.Level{logrus.WarnLevel, logrus.ErrorLevel}
	return &hook{levels, loggingStream}
}

// SetSource sets the source that is automatically added to analytics
// events.
func SetSource(s string) {
	source = s
}

// SetNamespace sets the namespace that is automatically added to analytics
// events.
func SetNamespace(ns string) {
	namespace = ns
}

// SetCustomer sets the customer name that is automatically added to analytics
// events.
func SetCustomer(c string) {
	customer = c
}

// SetKubeVersion sets the Kubernetes version that is automatically added to
// analytics events.
func SetKubeVersion(version string) {
	kubeVersion = version
}

type hook struct {
	levels     []logrus.Level
	streamType string
}

func (h *hook) Levels() []logrus.Level {
	return h.levels
}

func (h *hook) Fire(entry *logrus.Entry) error {
	tags := []string{
		fmt.Sprintf("stream:%s", h.streamType),
		fmt.Sprintf("kelda-version:%s", version.Version),
	}
	if namespace != "" {
		tags = append(tags, fmt.Sprintf("namespace:%s", namespace))
	}
	if kubeVersion != "" {
		tags = append(tags, fmt.Sprintf("kube-version:%s", kubeVersion))
	}
	if customer != "" {
		tags = append(tags, fmt.Sprintf("customer:%s", customer))
	}

	dataCopy := map[string]interface{}{
		"ddsource": source,
		"ddtags":   strings.Join(tags, ","),
	}
	for k, v := range entry.Data {
		dataCopy[k] = v
	}

	// Copy the entry so that when we don't change it when we add
	// DataDog-specific values to Data.
	entryCopy := *entry
	entryCopy.Data = dataCopy

	// DataDog doesn't have a concept of "panic" level, so we treat panics as
	// fatal errors.
	if entry.Level == logrus.PanicLevel {
		entryCopy.Level = logrus.FatalLevel
	}

	jsonBytes, err := ddFormatter.Format(&entryCopy)
	if err != nil {
		logrus.WithError(err).Debug("Failed to marshal log entry for analytics")
		return nil
	}

	resp, err := httpPost(ddEndpoint, ddContentType, bytes.NewReader(jsonBytes))
	if err != nil {
		logrus.WithError(err).Debug("Failed to update analytics")
	} else {
		// Close the body to avoid leaking resources.
		resp.Body.Close()
	}

	// Never return an error because doing so causes the error to be printed
	// directly to `stderr`, which messes up the CUI output:
	// https://github.com/Sirupsen/logrus/issues/116
	return nil
}
