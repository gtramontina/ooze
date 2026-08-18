//go:build darwin || linux

package cmdtestrunner

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const processGuardianConfigurationFD = 3

var errProcessGuardianCommandArgumentsMissing = errors.New("process guardian command arguments are missing")

type processGuardianCommand struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	Env  []string `json:"env"`
}

func decodeProcessGuardianCommand(file *os.File, command *processGuardianCommand) error {
	decodeErr := json.NewDecoder(file).Decode(command)
	closeErr := file.Close()
	var validationErr error
	if decodeErr == nil && len(command.Args) == 0 {
		validationErr = errProcessGuardianCommandArgumentsMissing
	}

	return errors.Join(decodeErr, closeErr, validationErr)
}

func environmentWithout(environment []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			filtered = append(filtered, variable)
		}
	}

	return filtered
}
