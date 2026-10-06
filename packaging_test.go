package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"convertme/internal/convert"
)

// The File Explorer command is shown for every file type the app can read, except these:
// .ts and .mts are far more often TypeScript source than video.
var notInExplorerMenu = map[string]bool{".ts": true, ".mts": true}

func TestPackageManifestMatchesTheApp(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("packaging", "msix", "AppxManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(data)

	var want []string
	for _, extension := range convert.InputExtensions() {
		if !notInExplorerMenu[extension] {
			want = append(want, extension)
		}
	}
	var got []string
	for _, match := range regexp.MustCompile(`<desktop5:ItemType Type="([^"]*)">`).FindAllStringSubmatch(manifest, -1) {
		got = append(got, match[1])
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the manifest shows the command for\n%v\nbut the app reads\n%v", got, want)
	}

	header, err := os.ReadFile(filepath.Join("shellext", "clsid.h"))
	if err != nil {
		t.Fatal(err)
	}
	class := regexp.MustCompile(`CONVERTME_COMMAND_CLSID "([0-9A-F]{8}(?:-[0-9A-F]{4}){3}-[0-9A-F]{12})"`).FindSubmatch(header)
	if class == nil {
		t.Fatal("shellext\\clsid.h does not define the class id")
	}
	ids := regexp.MustCompile(`(?:\bId|Clsid)="([0-9A-Fa-f]{8}(?:-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12})"`).FindAllStringSubmatch(manifest, -1)
	if len(ids) != len(want)+1 {
		t.Errorf("the manifest names a class id %d times, want %d", len(ids), len(want)+1)
	}
	for _, id := range ids {
		if id[1] != string(class[1]) {
			t.Errorf("the manifest names class id %s, but the command is %s", id[1], class[1])
		}
	}

	for _, part := range []string{
		`Version="` + convert.Version + `.0"`,
		`Executable="ConvertMe.exe"`,
		`Path="ConvertMeCommand.dll"`,
	} {
		if !strings.Contains(manifest, part) {
			t.Errorf("the manifest does not contain %s", part)
		}
	}
	// The package must not claim any file type, and must not ask for more than it needs.
	for _, part := range []string{"fileTypeAssociation", "internetClient", "broadFileSystemAccess", "windows.startupTask"} {
		if strings.Contains(manifest, part) {
			t.Errorf("the manifest must not contain %s", part)
		}
	}
}
