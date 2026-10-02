package convert

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// partialSuffix marks a file that is still being written. A finished conversion is renamed
// to its real name. A leftover after a crash or power cut is safe to delete.
const partialSuffix = ".convertme-part"

// maxStemUnits keeps room in a 255-unit Windows file name for the extension, a " (123)"
// counter and the temporary suffix.
const maxStemUnits = 200

// nameSet remembers the names a batch has already used.
type nameSet map[string]bool

// pathKey is the form in which paths are compared. Windows file names ignore case.
func pathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// stem is a file name without its last extension.
func stem(name string) string {
	trimmed := strings.TrimSuffix(name, filepath.Ext(name))
	if trimmed == "" {
		return name
	}
	return fitStem(trimmed)
}

// fitStem shortens very long names so the output name stays valid. Length is counted in
// UTF-16 units because that is what Windows limits.
func fitStem(value string) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= maxStemUnits {
		return value
	}
	cut := maxStemUnits
	if utf16.IsSurrogate(rune(units[cut-1])) && utf16.IsSurrogate(rune(units[cut])) {
		cut--
	}
	return strings.TrimRight(string(utf16.Decode(units[:cut])), " .")
}

func candidateName(base, extension string, attempt int) string {
	if attempt == 0 {
		return base + extension
	}
	return fmt.Sprintf("%s (%d)%s", base, attempt, extension)
}

func token() string {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(raw)
}

// partialPath is where the engine writes while a conversion is running.
func partialPath(folder, base, extension string) string {
	return filepath.Join(folder, base+extension+"."+token()+partialSuffix)
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// place gives a finished temporary file its final name. A file that is already there is
// only replaced when replace is set, the name is the first choice, and the file is not
// protected. Otherwise the next free "name (n)" is used. The rename itself refuses to
// overwrite, so a file that appears at the last moment is still safe.
func place(temp, folder, base, extension string, replace bool, taken nameSet, protected func(string) bool) (string, error) {
	for attempt := 0; attempt < 10000; attempt++ {
		final := filepath.Join(folder, candidateName(base, extension, attempt))
		key := pathKey(final)
		if taken[key] || protected(final) {
			continue
		}
		err := moveFile(temp, final, replace && attempt == 0)
		if err == nil {
			taken[key] = true
			return final, nil
		}
		if isAlreadyExists(err) {
			continue
		}
		return "", fmt.Errorf("the converted file could not be saved as %s: %w", filepath.Base(final), err)
	}
	return "", errors.New("no free file name was found in the destination folder")
}
