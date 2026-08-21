//go:build windows

package main

import (
	"testing"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
)

func TestQuickConversionNotificationMessagesUseCompletedCount(t *testing.T) {
	single := ConversionSummary{
		Total:     1,
		Completed: 1,
		Results:   []FileResult{{Output: `C:\Images\photo.jpg`, Status: "completed"}},
	}
	if got, want := quickConversionSuccessMessage(single, FormatJPEG), "Created photo.jpg next to the original."; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	multiple := ConversionSummary{Total: 2, Completed: 2}
	if got, want := quickConversionSuccessMessage(multiple, FormatJPEG), "Converted 2 images to JPEG next to the originals."; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestMissingHEIFCodecNotificationIncludesStoreAction(t *testing.T) {
	summary := ConversionSummary{
		Total:  1,
		Failed: 1,
		Results: []FileResult{{
			Input:  `C:\Images\photo.png`,
			Status: "failed",
			Error:  "encode photo.heic: HEIF codec is not installed",
		}},
	}

	notification, show, err := quickConversionNotification(summary, FormatHEIC, false)
	if err != nil {
		t.Fatal(err)
	}
	if !show {
		t.Fatal("failure notifications must be shown even when success notifications are disabled")
	}
	if notification.Title != "Conversion incomplete" {
		t.Fatalf("unexpected notification title %q", notification.Title)
	}
	if len(notification.Actions) != 1 {
		t.Fatalf("expected one Store action, got %d", len(notification.Actions))
	}
	action := notification.Actions[0]
	if action.Type != toast.Protocol || action.Content != "Install HEIF codec" || action.Arguments != codecStoreURL() {
		t.Fatalf("unexpected Store action: %+v", action)
	}
}

func TestDisabledSuccessNotificationIsSkipped(t *testing.T) {
	notification, show, err := quickConversionNotification(
		ConversionSummary{Total: 1, Completed: 1},
		FormatJPEG,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if show {
		t.Fatalf("expected notification to be skipped, got %+v", notification)
	}
}
