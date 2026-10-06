package convert

import (
	"path/filepath"
	"sort"
	"strings"
)

// This file is the single list of what Convert Me reads and writes. The interface, the
// command builder, the README and the tests all read from it, so the app cannot promise a
// format the engine was not built and tested for. Keep it in step with
// scripts/configure-ffmpeg.sh.

// Target is one output format.
type Target struct {
	ID    string
	Label string
	Ext   string
	// Kind is the kind of file the target produces.
	Kind Kind
	// Needs names an encoder that may be missing on some computers. Empty means always available.
	Needs string
}

const needH264 = "h264"

var targetList = []Target{
	{ID: "jpg", Label: "JPG", Ext: ".jpg", Kind: KindImage},
	{ID: "png", Label: "PNG", Ext: ".png", Kind: KindImage},
	{ID: "webp", Label: "WebP", Ext: ".webp", Kind: KindImage},
	{ID: "gif", Label: "GIF", Ext: ".gif", Kind: KindImage},
	{ID: "bmp", Label: "BMP", Ext: ".bmp", Kind: KindImage},
	{ID: "tiff", Label: "TIFF", Ext: ".tiff", Kind: KindImage},
	{ID: "mp3", Label: "MP3", Ext: ".mp3", Kind: KindAudio},
	{ID: "m4a", Label: "M4A", Ext: ".m4a", Kind: KindAudio},
	{ID: "wav", Label: "WAV", Ext: ".wav", Kind: KindAudio},
	{ID: "flac", Label: "FLAC", Ext: ".flac", Kind: KindAudio},
	{ID: "mp4", Label: "MP4", Ext: ".mp4", Kind: KindVideo, Needs: needH264},
	{ID: "mov", Label: "MOV", Ext: ".mov", Kind: KindVideo, Needs: needH264},
	{ID: "mkv", Label: "MKV", Ext: ".mkv", Kind: KindVideo, Needs: needH264},
}

// offered lists, in menu order, the targets available for each kind of source file.
var offered = map[Kind][]string{
	KindImage: {"jpg", "png", "webp", "gif", "bmp", "tiff"},
	KindAudio: {"mp3", "m4a", "wav", "flac"},
	KindVideo: {"mp4", "mov", "mkv", "gif", "mp3", "m4a", "wav", "flac"},
}

var defaultTargets = map[Kind]string{KindImage: "jpg", KindAudio: "mp3", KindVideo: "mp4"}

var kindOrder = []Kind{KindImage, KindVideo, KindAudio}

var kindLabels = map[Kind]string{KindImage: "Images", KindVideo: "Video", KindAudio: "Audio"}

// AudioOnlyGroup is the menu group for audio formats offered for a video source.
const AudioOnlyGroup = "Audio only"

// inputType describes a file extension the app accepts. Listed entries appear in the
// format table. The others are alternative spellings of a listed type.
type inputType struct {
	Kind   Kind
	Label  string
	Listed bool
}

var inputTypes = map[string]inputType{
	".jpg": {KindImage, "JPG", true}, ".jpeg": {KindImage, "JPG", false},
	".jpe": {KindImage, "JPG", false}, ".jfif": {KindImage, "JPG", false},
	".png": {KindImage, "PNG", true}, ".webp": {KindImage, "WebP", true},
	".gif": {KindImage, "GIF", true}, ".bmp": {KindImage, "BMP", true},
	".tiff": {KindImage, "TIFF", true}, ".tif": {KindImage, "TIFF", false},

	".mp3": {KindAudio, "MP3", true}, ".wav": {KindAudio, "WAV", true},
	".flac": {KindAudio, "FLAC", true}, ".m4a": {KindAudio, "M4A", true},
	".aac": {KindAudio, "AAC", true}, ".ogg": {KindAudio, "OGG", true},
	".oga": {KindAudio, "OGG", false}, ".opus": {KindAudio, "Opus", true},
	".wma": {KindAudio, "WMA", true}, ".aiff": {KindAudio, "AIFF", true},
	".aif": {KindAudio, "AIFF", false},

	".mp4": {KindVideo, "MP4", true}, ".m4v": {KindVideo, "M4V", false},
	".mov": {KindVideo, "MOV", true}, ".mkv": {KindVideo, "MKV", true},
	".webm": {KindVideo, "WebM", true}, ".avi": {KindVideo, "AVI", true},
	".wmv": {KindVideo, "WMV", true}, ".mpg": {KindVideo, "MPG", true},
	".mpeg": {KindVideo, "MPG", false}, ".ts": {KindVideo, "TS", true},
	".m2ts": {KindVideo, "TS", false}, ".mts": {KindVideo, "TS", false},
}

// readOrder fixes the order of the "reads" column in the format table.
var readOrder = map[Kind][]string{
	KindImage: {"JPG", "PNG", "WebP", "GIF", "BMP", "TIFF"},
	KindAudio: {"MP3", "WAV", "FLAC", "M4A", "AAC", "OGG", "Opus", "WMA", "AIFF"},
	KindVideo: {"MP4", "MOV", "MKV", "WebM", "AVI", "WMV", "MPG", "TS"},
}

// imageFormats maps the engine's name for a still image format to its label. Images are
// identified by content, so a PNG saved with a .jpg name is still handled as a PNG.
var imageFormats = map[string]string{
	"png_pipe": "PNG", "jpeg_pipe": "JPG", "bmp_pipe": "BMP",
	"tiff_pipe": "TIFF", "webp_pipe": "WebP", "gif": "GIF",
}

func targetByID(id string) (Target, bool) {
	for _, target := range targetList {
		if target.ID == id {
			return target, true
		}
	}
	return Target{}, false
}

func offers(kind Kind, id string) bool {
	for _, candidate := range offered[kind] {
		if candidate == id {
			return true
		}
	}
	return false
}

// KnownExtension reports whether a path has an extension the app accepts.
func KnownExtension(path string) bool {
	_, ok := inputTypes[strings.ToLower(filepath.Ext(path))]
	return ok
}

// DialogPattern is the file filter for the native "choose files" dialog.
func DialogPattern() string {
	extensions := make([]string, 0, len(inputTypes))
	for extension := range inputTypes {
		extensions = append(extensions, "*"+extension)
	}
	sort.Strings(extensions)
	return strings.Join(extensions, ";")
}

// InputExtensions lists every file extension the app accepts, in alphabetical order.
func InputExtensions() []string {
	extensions := make([]string, 0, len(inputTypes))
	for extension := range inputTypes {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)
	return extensions
}

// FormatTable is the honest "reads and writes" table.
func FormatTable() []FormatRow {
	rows := make([]FormatRow, 0, len(kindOrder))
	for _, kind := range kindOrder {
		row := FormatRow{Kind: kind, Label: kindLabels[kind], Reads: append([]string{}, readOrder[kind]...)}
		for _, id := range offered[kind] {
			target, _ := targetByID(id)
			if kind == KindVideo && target.Kind == KindAudio {
				continue
			}
			row.Writes = append(row.Writes, target.Label)
		}
		rows = append(rows, row)
	}
	return rows
}

// sourceLabel is the short format name shown for an input file.
func sourceLabel(path string, media Media) string {
	if label, ok := imageFormats[media.Format]; ok {
		return label
	}
	extension := strings.ToLower(filepath.Ext(path))
	if known, ok := inputTypes[extension]; ok {
		return known.Label
	}
	return strings.ToUpper(strings.TrimPrefix(extension, "."))
}

func options(kind Kind, caps Capabilities) []Option {
	list := make([]Option, 0, len(offered[kind]))
	for _, id := range offered[kind] {
		target, _ := targetByID(id)
		option := Option{ID: id, Label: target.Label, Available: true}
		if kind == KindVideo && target.Kind == KindAudio {
			option.Group = AudioOnlyGroup
		}
		if target.Needs == needH264 && !caps.H264 {
			option.Available = false
			option.Reason = caps.H264Reason
		}
		list = append(list, option)
	}
	return list
}

// firstAvailable picks the preferred target when it works on this computer and otherwise
// the first one that does.
func firstAvailable(kind Kind, preferred string, caps Capabilities) string {
	list := options(kind, caps)
	for _, option := range list {
		if option.ID == preferred && option.Available {
			return preferred
		}
	}
	for _, option := range list {
		if option.Available {
			return option.ID
		}
	}
	return preferred
}
