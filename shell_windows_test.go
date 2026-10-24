//go:build windows

package main

import "testing"

func TestArchitectureFromPackageFullName(t *testing.T) {
	for name, want := range map[string]string{
		"BurkeHolland.ConvertMe_1.2.1.0_x64__b2yzzbag0614y":   "x64",
		"BurkeHolland.ConvertMe_1.2.2.0_arm64__b2yzzbag0614y": "arm64",
		"BurkeHolland.ConvertMe_1.2.2.0_ARM64__b2yzzbag0614y": "arm64",
		"not a package name": "",
		"":                   "",
	} {
		if got := architectureFromPackageFullName(name); got != want {
			t.Errorf("architectureFromPackageFullName(%q) = %q, want %q", name, got, want)
		}
	}
}
