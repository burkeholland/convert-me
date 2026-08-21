//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"git.sr.ht/~jackmordaunt/go-toast/v2/tmpl"
	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

func showQuickConversionNotification(summary ConversionSummary, format ImageFormat, enabled bool) error {
	notification, show, err := quickConversionNotification(summary, format, enabled)
	if err != nil || !show {
		return err
	}
	if err := toast.SetAppData(toast.AppData{AppID: appName}); err != nil {
		return err
	}

	var xml bytes.Buffer
	if err := tmpl.XMLTemplate.Execute(&xml, notification); err != nil {
		return fmt.Errorf("build notification: %w", err)
	}
	return wintoast.Push(notification.AppID, xml.String(), wintoast.PreferPowershell)
}

func quickConversionNotification(summary ConversionSummary, format ImageFormat, enabled bool) (toast.Notification, bool, error) {
	if summary.Total == 0 {
		return toast.Notification{}, false, errors.New("cannot show a completion notification for an empty conversion")
	}
	hasFailures := summary.Failed > 0 || summary.Canceled > 0
	if !enabled && !hasFailures {
		return toast.Notification{}, false, nil
	}

	title := "Conversion complete"
	body := quickConversionSuccessMessage(summary, format)
	if hasFailures {
		title = "Conversion incomplete"
		body = quickConversionFailureMessage(summary)
	}

	notification := toast.Notification{
		AppID:          appName,
		Title:          title,
		Body:           body,
		Audio:          toast.Silent,
		Duration:       toast.Short,
		ActivationType: toast.Foreground,
	}
	if quickConversionNeedsHEIFCodec(summary) {
		notification.Actions = []toast.Action{{
			Type:      toast.Protocol,
			Content:   "Install HEIF codec",
			Arguments: codecStoreURL(),
		}}
	}
	return notification, true, nil
}

func quickConversionSuccessMessage(summary ConversionSummary, format ImageFormat) string {
	formatName := strings.ToUpper(string(format))
	if option, ok := FormatOptionFor(format); ok {
		formatName = option.Name
	}
	if summary.Completed == 1 && len(summary.Results) == 1 {
		return fmt.Sprintf("Created %s next to the original.", filepath.Base(summary.Results[0].Output))
	}
	return fmt.Sprintf("Converted %d images to %s next to the originals.", summary.Completed, formatName)
}

func quickConversionFailureMessage(summary ConversionSummary) string {
	for _, result := range summary.Results {
		if result.Error != "" {
			return fmt.Sprintf("%d of %d images could not be converted. %s", summary.Failed+summary.Canceled, summary.Total, result.Error)
		}
	}
	return fmt.Sprintf("%d of %d images could not be converted.", summary.Failed+summary.Canceled, summary.Total)
}

func quickConversionNeedsHEIFCodec(summary ConversionSummary) bool {
	for _, result := range summary.Results {
		if strings.Contains(strings.ToLower(result.Error), "heif codec") {
			return true
		}
	}
	return false
}
