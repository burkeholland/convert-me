package convert

import (
	"errors"
	"strings"
	"unicode"
)

const genericFailure = "The conversion did not finish."

// codecNames gives well-known codecs the names people recognise.
var codecNames = map[string]string{
	"av1": "AV1", "hevc": "HEVC (H.265)", "vvc": "VVC (H.266)", "h264": "H.264", "vp9": "VP9",
	"prores_raw": "ProRes RAW", "jpegxl": "JPEG XL", "jpeg2000": "JPEG 2000", "ac4": "AC-4",
	"mpegh_3d_audio": "MPEG-H Audio", "apv": "APV", "cfhd": "CineForm", "dts": "DTS",
}

func codecName(codec string) string {
	if name, ok := codecNames[codec]; ok {
		return name
	}
	if codec == "" {
		return "a codec"
	}
	return strings.ToUpper(codec)
}

// explain turns a failed conversion into a sentence a person can act on. The second value
// is the raw text for "Show details".
func explain(err error) (message, detail string) {
	var failure *ProcessError
	if !errors.As(err, &failure) {
		return sentence(err.Error()), ""
	}
	detail = lastLines(failure.Stderr, 12)
	text := strings.ToLower(failure.Stderr)
	switch {
	case strings.Contains(text, "no space left on device"):
		return "The destination drive is full. Free up some space and try again.", detail
	case strings.Contains(text, "permission denied") || strings.Contains(text, "access is denied"):
		if strings.Contains(text, partialSuffix) {
			return "Windows did not allow Convert Me to save in this folder. Choose another folder.", detail
		}
		return "Windows did not allow Convert Me to read this file.", detail
	case strings.Contains(text, "no such file or directory"):
		return "The file or the destination folder is no longer there.", detail
	case strings.Contains(text, "decoder") && (strings.Contains(text, "not found") || strings.Contains(text, "no decoder")):
		return "This file uses a codec that Convert Me cannot read yet.", detail
	case strings.Contains(text, "h264_mf @") || strings.Contains(text, "could not set output type") ||
		strings.Contains(text, "error while opening encoder"):
		return "The Windows H.264 encoder could not handle this video.", detail
	case strings.Contains(text, "moov atom not found") || strings.Contains(text, "invalid data found") ||
		strings.Contains(text, "end of file") || strings.Contains(text, "truncated") ||
		strings.Contains(text, "error while decoding") || strings.Contains(text, "nothing was written"):
		return "This file looks damaged or incomplete.", detail
	}
	return genericFailure, detail
}

// Sentence makes an internal error read like a sentence.
func Sentence(text string) string { return sentence(text) }

// sentence makes an internal error read like a sentence.
func sentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return genericFailure
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	text = string(runes)
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > 2000 {
		joined = joined[len(joined)-2000:]
	}
	return joined
}
