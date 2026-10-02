package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Destination modes.
const (
	DestinationSource = "source"
	DestinationFolder = "folder"
)

// Settings is the small amount the app remembers between runs: the last format chosen for
// each kind of file and where to save. No file names and no history are stored.
type Settings struct {
	Targets           map[Kind]string `json:"targets"`
	DestinationMode   string          `json:"destinationMode"`
	DestinationFolder string          `json:"destinationFolder"`
}

func defaultSettings() Settings {
	targets := make(map[Kind]string, len(defaultTargets))
	for kind, id := range defaultTargets {
		targets[kind] = id
	}
	return Settings{Targets: targets, DestinationMode: DestinationSource}
}

// loadSettings never fails: a missing or damaged file simply means the defaults.
func loadSettings(path string) Settings {
	settings := defaultSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		return settings
	}
	var stored Settings
	if err := json.Unmarshal(data, &stored); err != nil {
		return settings
	}
	for kind, id := range stored.Targets {
		if offers(kind, id) {
			settings.Targets[kind] = id
		}
	}
	if stored.DestinationMode == DestinationFolder && filepath.IsAbs(stored.DestinationFolder) {
		settings.DestinationMode = DestinationFolder
	}
	if filepath.IsAbs(stored.DestinationFolder) {
		settings.DestinationFolder = filepath.Clean(stored.DestinationFolder)
	}
	return settings
}

// saveSettings writes the file next to its final place and renames it over the old one.
func saveSettings(path string, settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}
