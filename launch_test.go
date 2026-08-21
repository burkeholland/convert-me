package main

import "testing"

func TestParseLaunchRequest(t *testing.T) {
	request := ParseLaunchRequest([]string{
		"--convert",
		"--format", "jpg",
		`C:\Photos\one.png`,
		`C:\Photos\ONE.PNG`,
		`C:\Photos\two.webp`,
	})

	if request.Mode != "convert" {
		t.Fatalf("expected convert mode, got %q", request.Mode)
	}
	if request.Format != "jpeg" {
		t.Fatalf("expected jpeg format, got %q", request.Format)
	}
	if len(request.Files) != 2 {
		t.Fatalf("expected duplicate paths to be removed, got %d files", len(request.Files))
	}
}

func TestSupportedFormatsExcludeGIF(t *testing.T) {
	if NormalizeFormat("gif") != "" {
		t.Fatal("GIF should not be included in v1")
	}
	for _, format := range SupportedFormats() {
		if format.ID == "gif" {
			t.Fatal("GIF should not be included in the format catalog")
		}
	}
}

func TestExplorerCommandsUseSelectedFilePlaceholder(t *testing.T) {
	executable := `C:\Program Files\ConvertMe\convert-me.exe`
	if got, want := quickConversionCommand(executable, "jpeg"), `"C:\Program Files\ConvertMe\convert-me.exe" --convert --format jpeg "%1"`; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got, want := customConversionCommand(executable), `"C:\Program Files\ConvertMe\convert-me.exe" --custom "%1"`; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
