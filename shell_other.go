//go:build !windows

package main

func RegisterContextMenu(string) error {
	return nil
}

func UnregisterContextMenu() error {
	return nil
}

func contextMenuRegistered() bool {
	return false
}

func setLaunchAtLogin(bool, string) error {
	return nil
}

func shellExecutable() (string, error) {
	return "", nil
}

func codecStoreURL() string {
	return "https://apps.microsoft.com/detail/9pmmsr1cgpwg"
}
