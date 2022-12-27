package iologger_test

import (
	"bytes"
	"testing"

	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/stretchr/testify/assert"
)

func TestIOLogger(t *testing.T) {
	t.Run("logs the given message with a line break", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)

		logger.Logf("message")

		assert.Equal(t, "message\n", buffer.String())
	})

	t.Run("evaluates printf arguments when logging the given message", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)

		logger.Logf("%s", "message")
		logger.Logf("%d", 1)
		logger.Logf("%.2f", 2.01)

		assert.Equal(t, "message\n1\n2.01\n", buffer.String())
	})
}
