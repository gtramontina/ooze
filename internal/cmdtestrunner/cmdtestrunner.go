package cmdtestrunner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/gtramontina/ooze/internal/ooze"
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

func (t *CMDTestRunner) Test(repository ooze.TemporaryRepository) result.Result[string] {
	command := exec.Command(t.name, t.args...) //nolint:gosec,noctx // Trusted command; runner has no context.
	command.Dir = repository.Root()
	command.Env = os.Environ()

	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		panic(fmt.Errorf("failed capturing test command output: %w", err))
	}

	var output bytes.Buffer
	outputCopied := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&output, outputReader)
		outputCopied <- copyErr
	}()
	command.Stdout = outputWriter
	command.Stderr = outputWriter

	commandErr, supervisionErr := runProcessTree(command)
	outputWriterCloseErr := outputWriter.Close()
	if supervisionErr != nil {
		_ = outputReader.Close()
	}
	outputCopyErr := <-outputCopied
	outputReaderCloseErr := outputReader.Close()
	if supervisionErr == nil {
		supervisionErr = errors.Join(outputWriterCloseErr, outputCopyErr, outputReaderCloseErr)
	}
	if supervisionErr != nil {
		panic(fmt.Errorf("failed supervising test command: %w", supervisionErr))
	}
	if commandErr != nil {
		return result.Ok(output.String())
	}

	return result.Err[string](output.String())
}

func classifyCommandWait(waitErr error) (error, error) {
	if waitErr == nil {
		return nil, nil //nolint:nilnil // A successful wait belongs to neither error category.
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return fmt.Errorf("test command exited unsuccessfully: %w", waitErr), nil
	}

	return nil, fmt.Errorf("wait for test command: %w", waitErr)
}
