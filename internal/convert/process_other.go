//go:build !windows

package convert

import "os/exec"

func configureProcess(*exec.Cmd) {}

func adoptProcess(*exec.Cmd) {}
