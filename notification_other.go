//go:build !windows

package main

func showQuickConversionNotification(ConversionSummary, ImageFormat, bool) error {
	return nil
}
