//go:build windows

package convert

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// longPath lets the rename work on paths longer than 260 characters.
func longPath(path string) string {
	if len(path) < 240 || strings.HasPrefix(path, `\\?\`) || !filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
	}
	return `\\?\` + path
}

// moveFile renames within one folder. Without replace it fails when the target exists.
// Go's os.Rename is not used here because on Windows it silently replaces the target.
func moveFile(from, to string, replace bool) error {
	source, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	var flags uint32 = windows.MOVEFILE_WRITE_THROUGH
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(source, target, flags)
}

func isAlreadyExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS)
}
