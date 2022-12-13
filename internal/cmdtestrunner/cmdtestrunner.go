package cmdtestrunner

import (
	"os"
	"os/exec"

	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/result"
)

type CMDTestRunner struct {
	name string
	args []string
}

func New(name string, args ...string) *CMDTestRunner {
	return &CMDTestRunner{
		name: name,
		args: args,
	}
}

func (t *CMDTestRunner) Test(repository laboratory.TemporaryRepository) result.Result[string] {
	command := exec.Command(t.name, t.args...) //nolint:gosec
	command.Dir = repository.Root()
	command.Env = os.Environ()

	output, err := command.CombinedOutput()
	if err != nil {
		return result.Ok(string(output))
	}

	return result.Err[string](string(output))
}
