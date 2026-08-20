package logging

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetupLogger verifies output formats and debug-level selection.
func TestSetupLogger(t *testing.T) {
	var output bytes.Buffer
	SetupLogger(LogFormatJSON, false, &output).Info("ready",
		"event", "test",
	)
	assert.True(t, strings.Contains(output.String(), `"msg":"ready"`))
	assert.True(t, strings.Contains(output.String(), `"event":"test"`))

	output.Reset()
	logger := SetupLogger(LogFormatText, true, &output)
	logger.Debug("details")
	assert.True(t, strings.Contains(output.String(), "level=DEBUG"))
	assert.True(t, strings.Contains(output.String(), `msg=details`))
}
