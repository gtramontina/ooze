package ooze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicSimulationOwnsItsPackageBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), "simulation"), entry.Name())
	}

	packageEntries, err := os.ReadDir(filepath.Join("internal", "simulation"))
	require.NoError(t, err)
	assert.NotEmpty(t, packageEntries)
}
