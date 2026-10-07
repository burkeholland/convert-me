//go:build !windows

package convert

import "errors"

type noSystemImages struct{}

func newSystemImages() SystemImages { return noSystemImages{} }

func (noSystemImages) Export(input, output string, maxSide int) (int, int, error) {
	return 0, 0, errors.New("HEIC photos can only be read on Windows")
}

func (noSystemImages) Check() error {
	return errors.New("HEIC photos can only be read on Windows")
}
