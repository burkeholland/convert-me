//go:build !windows

package main

import "image"

func heicCodecAvailable() bool {
	return true
}

func encodeHEICFile(string, image.Image, int) error {
	return ErrHEICEncodeUnsupported
}
