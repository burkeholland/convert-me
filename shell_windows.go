//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	shellRoot       = `Software\Classes\SystemFileAssociations`
	contextMenuName = "ConvertMe"
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	heifStoreURL    = "ms-windows-store://pdp/?productid=9PMMSR1CGPWG"
)

var shChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

func RegisterContextMenu(executable string) error {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return err
	}

	for _, extension := range []string{".png", ".jpg", ".jpeg", ".bmp", ".tif", ".tiff", ".webp", ".heic", ".heif"} {
		basePath := shellRoot + `\` + extension + `\shell\` + contextMenuName
		key, _, err := registry.CreateKey(registry.CURRENT_USER, basePath, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		if err := key.SetStringValue("MUIVerb", "Convert image"); err != nil {
			key.Close()
			return err
		}
		// An empty value makes Explorer enumerate the commands under this key's
		// nested shell key instead of looking them up in the global CommandStore.
		if err := key.SetStringValue("SubCommands", ""); err != nil {
			key.Close()
			return err
		}
		if err := key.SetStringValue("MultiSelectModel", "Player"); err != nil {
			key.Close()
			return err
		}
		key.Close()

		for _, option := range SupportedFormats() {
			subcommandPath := basePath + `\shell\ConvertMe.` + option.ID
			subcommand, _, err := registry.CreateKey(registry.CURRENT_USER, subcommandPath, registry.ALL_ACCESS)
			if err != nil {
				return err
			}
			if err := subcommand.SetStringValue("MUIVerb", option.Name); err != nil {
				subcommand.Close()
				return err
			}
			if err := subcommand.SetStringValue("MultiSelectModel", "Player"); err != nil {
				subcommand.Close()
				return err
			}
			command, _, err := registry.CreateKey(registry.CURRENT_USER, subcommandPath+`\command`, registry.ALL_ACCESS)
			if err != nil {
				subcommand.Close()
				return err
			}
			commandLine := quickConversionCommand(executable, option.ID)
			if err := command.SetStringValue("", commandLine); err != nil {
				command.Close()
				subcommand.Close()
				return err
			}
			command.Close()
			subcommand.Close()
		}

		customPath := basePath + `\shell\ConvertMe.custom`
		custom, _, err := registry.CreateKey(registry.CURRENT_USER, customPath, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		if err := custom.SetStringValue("MUIVerb", "Custom..."); err != nil {
			custom.Close()
			return err
		}
		if err := custom.SetStringValue("MultiSelectModel", "Player"); err != nil {
			custom.Close()
			return err
		}
		command, _, err := registry.CreateKey(registry.CURRENT_USER, customPath+`\command`, registry.ALL_ACCESS)
		if err != nil {
			custom.Close()
			return err
		}
		if err := command.SetStringValue("", customConversionCommand(executable)); err != nil {
			command.Close()
			custom.Close()
			return err
		}
		command.Close()
		custom.Close()
	}
	notifyShellAssociationsChanged()
	return nil
}

func quickConversionCommand(executable, format string) string {
	return fmt.Sprintf(`"%s" --convert --format %s "%%1"`, executable, format)
}

func customConversionCommand(executable string) string {
	return fmt.Sprintf(`"%s" --custom "%%1"`, executable)
}

func UnregisterContextMenu() error {
	var firstErr error
	for _, extension := range []string{".png", ".jpg", ".jpeg", ".bmp", ".tif", ".tiff", ".webp", ".heic", ".heif"} {
		path := shellRoot + `\` + extension + `\shell\` + contextMenuName
		if err := deleteRegistryTree(registry.CURRENT_USER, path); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	notifyShellAssociationsChanged()
	return firstErr
}

func contextMenuRegistered() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, shellRoot+`\.png\shell\`+contextMenuName, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false
	}
	if err != nil {
		return false
	}
	key.Close()
	return true
}

func notifyShellAssociationsChanged() {
	const (
		shcneAssocChanged = 0x08000000
		shcnfIDList       = 0x0000
	)
	shChangeNotify.Call(shcneAssocChanged, shcnfIDList, 0, 0)
}

func deleteRegistryTree(root registry.Key, path string) error {
	key, err := registry.OpenKey(root, path, registry.READ|registry.WRITE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	subkeys, err := key.ReadSubKeyNames(-1)
	key.Close()
	if err != nil {
		return err
	}
	for _, subkey := range subkeys {
		if err := deleteRegistryTree(root, path+`\`+subkey); err != nil {
			return err
		}
	}
	// Deleting an already-absent key is fine; treat ErrNotExist as success so
	// unregistering a partially-removed tree never reports a spurious failure.
	err = registry.DeleteKey(root, path)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func setLaunchAtLogin(enabled bool, executable string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		err := key.DeleteValue(appName)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}

	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	return key.SetStringValue(appName, fmt.Sprintf(`"%s" --tray`, executable))
}

func shellExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(executable), nil
}

func codecStoreURL() string {
	return heifStoreURL
}
