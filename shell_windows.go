//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                        = windows.NewLazySystemDLL("shell32.dll")
	ole32                          = windows.NewLazySystemDLL("ole32.dll")
	user32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procSHParseDisplayName         = shell32.NewProc("SHParseDisplayName")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
	procILFree                     = shell32.NewProc("ILFree")
	procCoInitializeEx             = ole32.NewProc("CoInitializeEx")
	procCoUninitialize             = ole32.NewProc("CoUninitialize")
	procAllowSetForegroundWindow   = user32.NewProc("AllowSetForegroundWindow")
	procGetCurrentPackageFullName  = kernel32.NewProc("GetCurrentPackageFullName")
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	asfwAny                 = 0xFFFFFFFF
)

// letOpenWindowComeForward lets a Convert Me window that is already open move in front
// of other windows. Windows only allows that to the program the user has just started.
// This process is that program, so it passes the permission on before it hands its
// files to the open window.
func letOpenWindowComeForward() {
	procAllowSetForegroundWindow.Call(asfwAny)
}

// hasPackageIdentity says whether this process runs as part of an installed package (the
// Store version or the test package). Only then does File Explorer show the
// Convert with Convert Me entry. A process without a package gets
// APPMODEL_ERROR_NO_PACKAGE; a packaged one is told its zero-length buffer is too small.
func hasPackageIdentity() bool {
	if procGetCurrentPackageFullName.Find() != nil {
		return false
	}
	var length uint32
	status, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
	return status == uintptr(windows.ERROR_INSUFFICIENT_BUFFER)
}

// hasExplorerEntry says whether File Explorer really shows Convert with Convert Me. The
// command is a DLL built for the architecture of the package. File Explorer on an Arm PC
// does not load the one from an x64 package, so that package has no entry there.
func hasExplorerEntry() bool {
	if !hasPackageIdentity() {
		return false
	}
	return !(packageArchitecture() == "x64" && windowsIsArm64())
}

// packageArchitecture reads the architecture from the full name of this process's package.
func packageArchitecture() string {
	var length uint32
	procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)), 0)
	if length == 0 || length > 1024 {
		return ""
	}
	buffer := make([]uint16, length)
	status, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&length)),
		uintptr(unsafe.Pointer(&buffer[0])))
	if status != 0 {
		return ""
	}
	return architectureFromPackageFullName(windows.UTF16ToString(buffer))
}

// architectureFromPackageFullName takes Name_Version_Architecture_ResourceId_PublisherId.
func architectureFromPackageFullName(name string) string {
	parts := strings.Split(name, "_")
	if len(parts) != 5 {
		return ""
	}
	return strings.ToLower(parts[2])
}

// windowsIsArm64 is true on an Arm PC, also for this x64 program running under emulation.
func windowsIsArm64() bool {
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err != nil {
		return false
	}
	return native == imageFileMachineArm64
}

const imageFileMachineArm64 = 0xAA64

// openFolder opens a folder in Explorer. The "explore" verb can only browse, so even a
// path that somehow pointed at a program would never be started.
func openFolder(folder string) error {
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("the folder %s is no longer there", folder)
	}
	verb, err := windows.UTF16PtrFromString("explore")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(folder)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}

// revealFiles opens a folder in Explorer with the given files selected. It uses the shell
// API with parsed item identifiers, so no command line is ever built from file names.
func revealFiles(folder string, files []string) error {
	if len(files) == 0 {
		return errors.New("no files to select")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if result == 0 || result == 1 {
		defer procCoUninitialize.Call()
	}

	parent, err := parseShellItem(folder)
	if err != nil {
		return err
	}
	defer procILFree.Call(parent)
	children := make([]uintptr, 0, len(files))
	defer func() {
		for _, child := range children {
			procILFree.Call(child)
		}
	}()
	for _, file := range files {
		if child, err := parseShellItem(file); err == nil {
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		return errors.New("the files could not be located")
	}
	status, _, _ := procSHOpenFolderAndSelectItems.Call(parent, uintptr(len(children)),
		uintptr(unsafe.Pointer(&children[0])), 0)
	if int32(status) < 0 {
		return fmt.Errorf("Explorer could not show the files (0x%08x)", uint32(status))
	}
	return nil
}

func parseShellItem(path string) (uintptr, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var item uintptr
	status, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&item)), 0, 0)
	if int32(status) < 0 || item == 0 {
		return 0, fmt.Errorf("the path could not be located (0x%08x)", uint32(status))
	}
	return item, nil
}
