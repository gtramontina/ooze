package ooze

import (
	"errors"
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeManagedAttemptSystemUsesManagedControlBounds(t *testing.T) {
	system, err := newNativeManagedAttemptSystem(newProcessRuntimeShell(1))
	if errors.Is(err, supervision.ErrUnsupportedPlatform) {
		t.Skip("native supervision is unavailable on this operating system")
	}
	require.NoError(t, err, "construct managed attempt system: %v", err)
	require.NotNil(t, system, "managed attempt system returned without its native driver")
	require.NotNil(t, system.driver, "managed attempt system returned without its native driver")
	assert.NotNil(t, system.driver)
}
