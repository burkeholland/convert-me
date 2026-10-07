package convert

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
)

// Some pictures are stored with a codec that the engine does not have. Convert Me reads
// those through the codecs of Windows instead: the ones that are part of Windows, and the
// ones the owner of the PC added to it.
//
// HEIC photos are the case this was made for. They are HEVC (H.265) inside. The engine has
// no HEVC decoder, on purpose, and Convert Me does not bring one either. Windows reads
// HEIC photos when Microsoft's HEIF Image Extensions and HEVC Video Extensions are
// installed, so on such a PC Convert Me can read them too, and on any other PC it says
// what is missing.

// SystemImages turns such a picture into one the engine can read.
type SystemImages interface {
	// Export reads the first picture in input, turned and cropped the way it is meant to
	// be shown, and writes it to output as a PNG. When maxSide is above zero the picture
	// is scaled down until neither side is longer than that. The size it returns is the
	// full size of the picture.
	Export(input, output string, maxSide int) (width, height int, err error)
	// Check reads the small HEIC picture that is part of the app. An error means this
	// computer cannot read HEIC photos.
	Check() error
}

// heicCheck is a 64 x 48 HEIC picture made with the encoder of Windows from one of the
// test pictures in scripts\fixtures. scripts\make-heic-fixtures.ps1 makes it.
//
//go:embed heic-check.heic
var heicCheck []byte

// heicBrands are the marks at the start of a file that say "the pictures in here are HEVC".
var heicBrands = map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true, "hevx": true}

// systemImage reports whether a file is a HEIC photo. Like every other picture it is told
// by what is in it, not by its name: a HEIC photo that was renamed to .jpg is still read
// through Windows, and a JPG that was named .heic still goes to the engine.
func systemImage(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	head := make([]byte, 256)
	count, _ := io.ReadFull(file, head)
	return heicContent(head[:count])
}

// heicContent looks at the first box of a file. In a HEIC file that is "ftyp": four bytes
// of length, the word ftyp, the main brand, a version number, and then more brands.
func heicContent(head []byte) bool {
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return false
	}
	end := int(head[0])<<24 | int(head[1])<<16 | int(head[2])<<8 | int(head[3])
	if end > len(head) {
		end = len(head)
	}
	for at := 8; at+4 <= end; at += 4 {
		if at != 12 && heicBrands[string(head[at:at+4])] {
			return true
		}
	}
	return false
}

const (
	heicFormat = "heic"
	heicLabel  = "HEIC"
	// previewSide is the size at which a photo is read while it is inspected. It is enough
	// for the small preview in the list.
	previewSide = 256
	// heicNeeds is shown wherever HEIC cannot be read on this computer.
	heicNeeds = "HEIC photos are read with the HEIF Image Extensions and the HEVC Video Extensions from Microsoft, and this PC does not have both. They are in the Microsoft Store."
	// heicUnreadable is shown for one photo that Windows could not read although it can read HEIC.
	heicUnreadable = "Windows could not read this HEIC photo. It may be damaged, or of a kind that the HEIC support in Windows cannot read."
)

// SystemImageError is a failure to read a picture through Windows, in words for the
// person using the app. Detail holds the technical reason for "Show details".
type SystemImageError struct {
	Message string
	Detail  string
}

func (e *SystemImageError) Error() string { return e.Message }

// systemFailure turns an error from reading a picture through Windows into a message.
func systemFailure(err error) *SystemImageError {
	var known *SystemImageError
	if errors.As(err, &known) {
		return known
	}
	return &SystemImageError{Message: heicUnreadable, Detail: err.Error()}
}

// checkSystemImages finds out at startup whether this computer can read HEIC photos.
func checkSystemImages(system SystemImages) (bool, string) {
	if system == nil {
		return false, heicNeeds
	}
	if err := system.Check(); err != nil {
		return false, heicNeeds
	}
	return true, ""
}

// scaledSize fits a picture inside a square of maxSide pixels and keeps its shape.
func scaledSize(width, height, maxSide int) (int, int) {
	if maxSide <= 0 || (width <= maxSide && height <= maxSide) {
		return width, height
	}
	if width >= height {
		return maxSide, max(1, int(int64(height)*int64(maxSide)/int64(width)))
	}
	return max(1, int(int64(width)*int64(maxSide)/int64(height))), maxSide
}

// checkImageSize applies the same limits as for pictures the engine reads itself.
func checkImageSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("the picture size could not be read")
	}
	if width > maxImageSide || height > maxImageSide || int64(width)*int64(height) > maxImagePixels {
		return fmt.Errorf("this image is %d x %d pixels, which is larger than Convert Me can handle", width, height)
	}
	return nil
}
