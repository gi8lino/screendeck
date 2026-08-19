package logging

import (
	"bytes"
	"strings"
	"testing"
)

// TestSetupLogger verifies output formats and debug-level selection.
func TestSetupLogger(t *testing.T) {
	var output bytes.Buffer
	SetupLogger(LogFormatJSON, false, &output).Info("ready", "event", "test")
	if !strings.Contains(output.String(), `"msg":"ready"`) || !strings.Contains(output.String(), `"event":"test"`) {
		t.Fatalf("unexpected JSON log: %s", output.String())
	}

	output.Reset()
	logger := SetupLogger(LogFormatText, true, &output)
	logger.Debug("details")
	if !strings.Contains(output.String(), "level=DEBUG") || !strings.Contains(output.String(), `msg=details`) {
		t.Fatalf("unexpected text log: %s", output.String())
	}
}
