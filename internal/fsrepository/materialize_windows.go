//go:build windows

package fsrepository

import (
	"fmt"
	"io"
	"os"
)

func materializeFile(sourcePath, destinationPath string) error {
	return materializeFileUsing(sourcePath, destinationPath, os.Link)
}

func materializeFileUsing(sourcePath, destinationPath string, createHardLink func(string, string) error) error {
	err := createHardLink(sourcePath, destinationPath)
	if err == nil {
		return nil
	}

	return copyFile(sourcePath, destinationPath)
}

func copyFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	destination, err := os.OpenFile(destinationPath, flags, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	_, err = io.Copy(destination, source)
	if err != nil {
		_ = destination.Close()
		_ = os.Remove(destinationPath)

		return fmt.Errorf("copy source: %w", err)
	}

	err = destination.Close()
	if err != nil {
		_ = os.Remove(destinationPath)

		return fmt.Errorf("close destination: %w", err)
	}

	return nil
}
