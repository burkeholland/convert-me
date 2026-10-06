package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Files can be named on the command line in two ways. Plain arguments are what "Open
// with" and files dropped on ConvertMe.exe produce. A file list is what the File Explorer
// command of the packaged version writes (shellext\ConvertMeCommand.cpp), because a
// selection can hold far more names than a command line has room for.
const (
	listFlag = "--files-from"

	// A file list is only read, and afterwards removed, when both its name and its first
	// line say that it is one. Any other file named after the flag is left alone.
	listHeader     = "Convert Me file list 1"
	listNamePrefix = "convertme-files-"
	listNameSuffix = ".txt"

	maxListBytes = 8 << 20
	maxListPaths = 5000
)

// launchPaths returns the files the command line asks for, in the order they are named.
func launchPaths(args []string, workingDirectory string) []string {
	var paths []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == listFlag:
			if i+1 < len(args) {
				i++
				paths = append(paths, readFileList(args[i])...)
			}
		case strings.HasPrefix(args[i], listFlag+"="):
			paths = append(paths, readFileList(strings.TrimPrefix(args[i], listFlag+"="))...)
		default:
			paths = append(paths, absolutePaths(args[i:i+1], workingDirectory)...)
		}
	}
	return paths
}

// readFileList reads a file list and removes it. The list is UTF-8 text: the header line,
// then one full path on each line.
func readFileList(name string) []string {
	base := strings.ToLower(filepath.Base(name))
	if !filepath.IsAbs(name) || !strings.HasPrefix(base, listNamePrefix) || !strings.HasSuffix(base, listNameSuffix) {
		return nil
	}
	file, err := os.Open(name)
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxListBytes+1))
	file.Close()
	if err != nil || len(data) > maxListBytes {
		return nil
	}
	lines := strings.Split(string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))), "\n")
	if strings.TrimRight(lines[0], "\r") != listHeader {
		return nil
	}
	os.Remove(name)

	var paths []string
	for _, line := range lines[1:] {
		line = strings.TrimRight(line, "\r")
		if len(paths) == maxListPaths {
			break
		}
		if line == "" || !utf8.ValidString(line) || strings.ContainsRune(line, 0) || !filepath.IsAbs(line) {
			continue
		}
		paths = append(paths, filepath.Clean(line))
	}
	return paths
}
