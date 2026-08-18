//go:build !windows

package fsrepository

import (
	"fmt"
	"os"
)

func materializeFile(sourcePath, destinationPath string) error {
	err := os.Symlink(sourcePath, destinationPath)
	if err != nil {
		return fmt.Errorf("create symbolic link: %w", err)
	}

	return nil
}
