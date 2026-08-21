package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const appName = "ConvertMe"

func defaultSettings() Settings {
	return Settings{
		JPEGQuality:         88,
		WebPQuality:         82,
		PreserveMetadata:    true,
		LaunchAtLogin:       true,
		ShowNotifications:   true,
		ExplorerIntegration: true,
	}
}

func normalizeSettings(settings Settings) Settings {
	defaults := defaultSettings()
	if settings.JPEGQuality < 1 || settings.JPEGQuality > 100 {
		settings.JPEGQuality = defaults.JPEGQuality
	}
	if settings.WebPQuality < 1 || settings.WebPQuality > 100 {
		settings.WebPQuality = defaults.WebPQuality
	}
	return settings
}

func settingsFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, appName, "settings.json"), nil
}

func loadSettings() (Settings, error) {
	path, err := settingsFilePath()
	if err != nil {
		return Settings{}, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	return normalizeSettings(settings), nil
}

func saveSettings(settings Settings) error {
	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(normalizeSettings(settings), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
