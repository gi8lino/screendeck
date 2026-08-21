package logging

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetupLogger verifies logger format and level selection.
func TestSetupLogger(t *testing.T) {
	t.Run("JSON info", func(t *testing.T) {
		var output bytes.Buffer
		SetupLogger(LogFormatJSON, false, &output).Info("ready",
			"event", "test",
		)

		assert.Contains(t, output.String(), `"msg":"ready"`)
		assert.Contains(t, output.String(), `"event":"test"`)
	})

	t.Run("text debug", func(t *testing.T) {
		var output bytes.Buffer
		SetupLogger(LogFormatText, true, &output).Debug("details")

		assert.Contains(t, output.String(), "level=DEBUG")
		assert.Contains(t, output.String(), `msg=details`)
	})
}
