//go:build !windows

package main

import "errors"

func openFolder(string) error { return errors.New("opening folders is only supported on Windows") }

func revealFiles(string, []string) error {
	return errors.New("showing files is only supported on Windows")
}
