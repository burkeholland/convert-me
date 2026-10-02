//go:build !windows

package convert

import (
	"errors"
	"os"
)

// moveFile is only used on Windows in the shipped app. This version keeps the package
// buildable elsewhere: a hard link fails when the target exists, which gives the same
// "never overwrite" behaviour.
func moveFile(from, to string, replace bool) error {
	if replace {
		return os.Rename(from, to)
	}
	if err := os.Link(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}

func isAlreadyExists(err error) bool {
	return errors.Is(err, os.ErrExist)
}
