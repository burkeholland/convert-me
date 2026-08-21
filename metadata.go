package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
)

var errNoJPEGExif = errors.New("no JPEG EXIF metadata")

func compatibleMetadata(input string, output ImageFormat, preserve bool) ([]byte, error) {
	if !preserve || (output != FormatJPEG && output != FormatWebP) || FormatFromPath(input) != FormatJPEG {
		return nil, nil
	}
	metadata, err := readJPEGExif(input)
	if errors.Is(err, errNoJPEGExif) {
		return nil, nil
	}
	return metadata, err
}

func readJPEGExif(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, errNoJPEGExif
	}

	offset := 2
	for offset+4 <= len(data) {
		if data[offset] != 0xff {
			offset++
			continue
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			break
		}
		marker := data[offset]
		offset++
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(data) {
			break
		}
		segmentLength := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if segmentLength < 2 || offset+segmentLength > len(data) {
			break
		}
		segment := data[offset+2 : offset+segmentLength]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return append([]byte(nil), segment...), nil
		}
		offset += segmentLength
	}
	return nil, errNoJPEGExif
}
