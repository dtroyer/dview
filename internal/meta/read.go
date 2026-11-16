// Package meta reads and parses the generation metadata that AUTOMATIC1111
// and Forge embed in images.
package meta

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrNoMetadata means the image has no A1111/Forge generation metadata.
var ErrNoMetadata = errors.New("no generation metadata")

// maxText bounds the size of a metadata block, to avoid huge allocations from
// a corrupt file.
const maxText = 16 << 20

// ReadFile returns the raw A1111/Forge "parameters" text embedded in the
// image file at path. The format is detected from the file's content.
func ReadFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return Read(f)
}

// Read returns the raw "parameters" text from a PNG, JPEG or WebP image.
func Read(r io.ReadSeeker) (string, error) {
	var magic [12]byte
	n, err := io.ReadFull(r, magic[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("reading image header: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	m := magic[:n]
	switch {
	case bytes.HasPrefix(m, []byte("\x89PNG\r\n\x1a\n")):
		return readPNG(r)
	case bytes.HasPrefix(m, []byte{0xFF, 0xD8}):
		return readJPEG(r)
	case len(m) == 12 && string(m[0:4]) == "RIFF" && string(m[8:12]) == "WEBP":
		return readWebP(r)
	}
	return "", errors.New("unsupported image format")
}

// readPNG looks for a "parameters" tEXt, zTXt or iTXt chunk. Image data
// chunks are skipped without being read.
func readPNG(r io.ReadSeeker) (string, error) {
	if _, err := r.Seek(8, io.SeekStart); err != nil {
		return "", err
	}
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return "", ErrNoMetadata
			}
			return "", err
		}
		length := binary.BigEndian.Uint32(hdr[0:4])
		typ := string(hdr[4:8])
		switch typ {
		case "tEXt", "zTXt", "iTXt":
			if length > maxText {
				return "", fmt.Errorf("PNG %s chunk too large", typ)
			}
			data := make([]byte, length)
			if _, err := io.ReadFull(r, data); err != nil {
				return "", fmt.Errorf("reading PNG %s chunk: %w", typ, err)
			}
			if text, ok, err := pngText(typ, data); err != nil {
				return "", err
			} else if ok {
				return text, nil
			}
			if _, err := r.Seek(4, io.SeekCurrent); err != nil { // CRC
				return "", err
			}
		case "IEND":
			return "", ErrNoMetadata
		default:
			if _, err := r.Seek(int64(length)+4, io.SeekCurrent); err != nil {
				return "", err
			}
		}
	}
}

// pngText decodes a PNG text chunk and reports whether it is "parameters".
func pngText(typ string, data []byte) (string, bool, error) {
	key, rest, found := bytes.Cut(data, []byte{0})
	if !found || string(key) != "parameters" {
		return "", false, nil
	}
	switch typ {
	case "tEXt":
		return latin1(rest), true, nil
	case "zTXt":
		if len(rest) < 1 {
			return "", true, errors.New("short PNG zTXt chunk")
		}
		text, err := inflate(rest[1:])
		return latin1(text), true, err
	}
	// iTXt: compression flag, method, language\0, translated keyword\0, text.
	if len(rest) < 2 {
		return "", true, errors.New("short PNG iTXt chunk")
	}
	compressed := rest[0] == 1
	rest = rest[2:]
	for range 2 {
		_, after, ok := bytes.Cut(rest, []byte{0})
		if !ok {
			return "", true, errors.New("malformed PNG iTXt chunk")
		}
		rest = after
	}
	if compressed {
		var err error
		if rest, err = inflate(rest); err != nil {
			return "", true, err
		}
	}
	return string(rest), true, nil
}

func inflate(b []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("decompressing PNG text: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, maxText))
	if err != nil {
		return nil, fmt.Errorf("decompressing PNG text: %w", err)
	}
	return out, nil
}

func latin1(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}

// readJPEG looks for an Exif APP1 segment before the image data starts.
func readJPEG(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	if _, err := br.Discard(2); err != nil { // SOI
		return "", err
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return "", ErrNoMetadata
		}
		if b != 0xFF {
			continue
		}
		marker, err := br.ReadByte()
		if err != nil {
			return "", ErrNoMetadata
		}
		switch {
		case marker == 0xFF || marker == 0x00:
			if marker == 0xFF {
				_ = br.UnreadByte()
			}
			continue
		case marker == 0xD9 || marker == 0xDA: // EOI, start of scan
			return "", ErrNoMetadata
		case marker >= 0xD0 && marker <= 0xD7, marker == 0x01: // no length
			continue
		}
		var lenBuf [2]byte
		if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
			return "", ErrNoMetadata
		}
		n := int(binary.BigEndian.Uint16(lenBuf[:])) - 2
		if n < 0 {
			return "", errors.New("malformed JPEG segment")
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(br, data); err != nil {
			return "", fmt.Errorf("reading JPEG segment: %w", err)
		}
		if marker == 0xE1 && bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
			return exifUserComment(data[6:])
		}
	}
}

// readWebP looks for an EXIF chunk in a RIFF WebP file.
func readWebP(r io.ReadSeeker) (string, error) {
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return "", err
	}
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return "", ErrNoMetadata
		}
		size := binary.LittleEndian.Uint32(hdr[4:8])
		padded := int64(size) + int64(size&1)
		if string(hdr[0:4]) != "EXIF" {
			if _, err := r.Seek(padded, io.SeekCurrent); err != nil {
				return "", err
			}
			continue
		}
		if size > maxText {
			return "", errors.New("WebP EXIF chunk too large")
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return "", fmt.Errorf("reading WebP EXIF chunk: %w", err)
		}
		return exifUserComment(bytes.TrimPrefix(data, []byte("Exif\x00\x00")))
	}
}

const (
	tagExifIFD     = 0x8769
	tagUserComment = 0x9286
)

// exifUserComment returns the UserComment from a TIFF-format EXIF block.
func exifUserComment(tiff []byte) (string, error) {
	if len(tiff) < 8 {
		return "", errors.New("short EXIF data")
	}
	var bo binary.ByteOrder
	switch string(tiff[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return "", errors.New("malformed EXIF data")
	}

	// findTag returns the value bytes of a tag in the IFD at offset.
	findTag := func(offset uint32, tag uint16) ([]byte, bool) {
		if int64(offset)+2 > int64(len(tiff)) {
			return nil, false
		}
		count := int(bo.Uint16(tiff[offset:]))
		for i := range count {
			e := int(offset) + 2 + i*12
			if e+12 > len(tiff) {
				return nil, false
			}
			if bo.Uint16(tiff[e:]) != tag {
				continue
			}
			typ, n := bo.Uint16(tiff[e+2:]), bo.Uint32(tiff[e+4:])
			size := int64(n) * typeSize(typ)
			if size <= 4 {
				return tiff[e+8 : e+8+int(size)], true
			}
			start := int64(bo.Uint32(tiff[e+8:]))
			if start+size > int64(len(tiff)) {
				return nil, false
			}
			return tiff[start : start+size], true
		}
		return nil, false
	}

	ptr, ok := findTag(bo.Uint32(tiff[4:8]), tagExifIFD)
	if !ok || len(ptr) != 4 {
		return "", ErrNoMetadata
	}
	comment, ok := findTag(bo.Uint32(ptr), tagUserComment)
	if !ok || len(comment) < 8 {
		return "", ErrNoMetadata
	}
	text := decodeUserComment(comment[:8], comment[8:], bo)
	if strings.TrimSpace(text) == "" {
		return "", ErrNoMetadata
	}
	return text, nil
}

func typeSize(typ uint16) int64 {
	switch typ {
	case 3, 8: // SHORT, SSHORT
		return 2
	case 4, 9, 11: // LONG, SLONG, FLOAT
		return 4
	case 5, 10, 12: // RATIONAL, SRATIONAL, DOUBLE
		return 8
	}
	return 1 // BYTE, ASCII, UNDEFINED, ...
}

// decodeUserComment decodes an EXIF UserComment from its 8-byte character
// code prefix. A1111 writes "UNICODE" comments as big-endian UTF-16; other
// tools use the file's byte order, so the order is guessed from where the
// zero bytes of ASCII characters fall.
func decodeUserComment(code, data []byte, bo binary.ByteOrder) string {
	switch strings.TrimRight(string(code), "\x00") {
	case "UNICODE":
		data = data[:len(data)&^1]
		if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
			bo, data = binary.BigEndian, data[2:]
		} else if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
			bo, data = binary.LittleEndian, data[2:]
		} else {
			var evenZero, oddZero int
			for i := 0; i+1 < len(data); i += 2 {
				if data[i] == 0 {
					evenZero++
				}
				if data[i+1] == 0 {
					oddZero++
				}
			}
			if evenZero > oddZero {
				bo = binary.BigEndian
			} else if oddZero > evenZero {
				bo = binary.LittleEndian
			}
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = bo.Uint16(data[i*2:])
		}
		return strings.TrimRight(string(utf16.Decode(units)), "\x00")
	default: // ASCII, undefined, or anything else
		return strings.TrimRight(latin1(data), "\x00")
	}
}
