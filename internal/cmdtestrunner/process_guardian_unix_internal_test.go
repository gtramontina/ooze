//go:build darwin || linux

package cmdtestrunner

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeProcessGuardianCommandRejectsMissingArguments(t *testing.T) {
	configurationReader, configurationWriter, err := os.Pipe()
	require.NoError(t, err)

	configuration := processGuardianCommand{
		Path: "test-command",
		Args: nil,
		Dir:  "",
		Env:  nil,
	}
	require.NoError(t, json.NewEncoder(configurationWriter).Encode(configuration))
	require.NoError(t, configurationWriter.Close())

	var command processGuardianCommand
	err = decodeProcessGuardianCommand(configurationReader, &command)

	require.ErrorIs(t, err, errProcessGuardianCommandArgumentsMissing)
}
