package logs

import (
	"bytes"
	"context"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForwardLogs(t *testing.T) {
	service := "gateway"
	expFirstLog := "2019-11-07T13:00:00Z Hello."
	expSecondLog := "badtimestamp World."
	logsStream := ioutil.NopCloser(bytes.NewBuffer(
		[]byte(expFirstLog + "\n" + expSecondLog + "\n")))

	output := make(chan rawLogLine, 8)
	forwardLogs(output, service, logsStream)

	firstLog := <-output
	secondLog := <-output
	select {
	case <-output:
		t.Error("There shouldn't be any more messages")
	default:
	}

	assert.Equal(t, firstLog.message, expFirstLog)
	assert.Equal(t, firstLog.fromService, service)
	assert.Equal(t, secondLog.message, expSecondLog)
	assert.Equal(t, firstLog.fromService, service)
}

func TestPrintLogs(t *testing.T) {
	// Mock out stdout so that we can capture its output below.
	mockStdout := chanWriter(make(chan []byte, 8))
	stdout = mockStdout

	expectedOutput := "\x1b[35mgateway\x1b[0m › This should get printed first.\n" +
		"\x1b[33mweb-server\x1b[0m › This should get printed second.\n"

	// Seed the logs channel with two messages in the wrong order.
	rawLogs := make(chan rawLogLine, 8)
	rawLogs <- rawLogLine{
		fromService: "web-server",
		message:     "2019-11-07T13:00:00Z This should get printed second.",
	}
	rawLogs <- rawLogLine{
		fromService: "gateway",
		message:     "2019-11-07T12:00:00Z This should get printed first.",
	}
	close(rawLogs)

	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)
	go printLogs(ctx, rawLogs, false)

	// Wait for the desired output to appear, or for the timeout to expire.
	var printedOutput []byte
	for {
		select {
		case out := <-mockStdout:
			printedOutput = append(printedOutput, out...)
		case <-ctx.Done():
			assert.Equal(t, expectedOutput, string(printedOutput))
			return
		}

		if string(printedOutput) == expectedOutput {
			return
		}
	}
}

// chanWriter provides an io.Writer interface for writing to a channel.
type chanWriter chan []byte

func (w chanWriter) Write(p []byte) (int, error) {
	cpy := make([]byte, len(p))
	copy(cpy, p)
	w <- cpy
	return len(p), nil
}
