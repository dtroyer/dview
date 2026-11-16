package meta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = "a photo of a fox, (golden hour:1.2)\nsecond line\n" +
	"Negative prompt: blurry, lowres\nwatermark\n" +
	`Steps: 30, Sampler: DPM++ 2M, CFG scale: 7, Seed: 123, Lora hashes: "a: 1, b: 2", Model: sdxl`

// pngWith returns a small PNG with the given chunks inserted after IHDR.
func pngWith(t *testing.T, chunks ...[]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2))))
	b := buf.Bytes()
	out := append([]byte{}, b[:33]...) // signature + IHDR
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, b[33:]...)
}

func chunk(typ string, data []byte) []byte {
	var c bytes.Buffer
	_ = binary.Write(&c, binary.BigEndian, uint32(len(data)))
	c.WriteString(typ)
	c.Write(data)
	_ = binary.Write(&c, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	return c.Bytes()
}

func deflate(b []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

func read(t *testing.T, b []byte) (string, error) {
	t.Helper()
	return Read(bytes.NewReader(b))
}

func TestReadPNG(t *testing.T) {
	tests := map[string][]byte{
		"tEXt": chunk("tEXt", []byte("parameters\x00"+sample)),
		"zTXt": chunk("zTXt", append([]byte("parameters\x00\x00"), deflate([]byte(sample))...)),
		"iTXt": chunk("iTXt", []byte("parameters\x00\x00\x00en\x00\x00"+sample)),
		"iTXt compressed": chunk("iTXt",
			append([]byte("parameters\x00\x01\x00\x00\x00"), deflate([]byte(sample))...)),
	}
	for name, c := range tests {
		t.Run(name, func(t *testing.T) {
			other := chunk("tEXt", []byte("Software\x00something"))
			got, err := read(t, pngWith(t, other, c))
			require.NoError(t, err)
			assert.Equal(t, sample, got)
		})
	}

	t.Run("latin-1 tEXt", func(t *testing.T) {
		got, err := read(t, pngWith(t, chunk("tEXt", []byte("parameters\x00caf\xe9"))))
		require.NoError(t, err)
		assert.Equal(t, "café", got)
	})
	t.Run("none", func(t *testing.T) {
		_, err := read(t, pngWith(t, chunk("tEXt", []byte("Software\x00x"))))
		assert.ErrorIs(t, err, ErrNoMetadata)
	})
}

// tiffWithComment builds a TIFF block whose Exif IFD holds a UserComment.
func tiffWithComment(bo binary.ByteOrder, comment []byte) []byte {
	var b bytes.Buffer
	if bo == binary.LittleEndian {
		b.WriteString("II")
	} else {
		b.WriteString("MM")
	}
	w := func(v any) { _ = binary.Write(&b, bo, v) }
	w(uint16(42))
	w(uint32(8)) // IFD0 at 8
	// IFD0: one entry, the Exif IFD pointer.
	w(uint16(1))
	w(uint16(tagExifIFD))
	w(uint16(4))
	w(uint32(1))
	w(uint32(8 + 2 + 12 + 4)) // Exif IFD right after IFD0
	w(uint32(0))
	// Exif IFD: one entry, UserComment, data after the IFD.
	w(uint16(1))
	w(uint16(tagUserComment))
	w(uint16(7))
	w(uint32(len(comment)))
	w(uint32(26 + 2 + 12 + 4))
	w(uint32(0))
	b.Write(comment)
	return b.Bytes()
}

func utf16Bytes(bo binary.ByteOrder, s string) []byte {
	var b bytes.Buffer
	for _, u := range utf16.Encode([]rune(s)) {
		_ = binary.Write(&b, bo, u)
	}
	return b.Bytes()
}

func jpegWithExif(tiff []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	app0 := []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	b.Write([]byte{0xFF, 0xE0})
	_ = binary.Write(&b, binary.BigEndian, uint16(len(app0)+2))
	b.Write(app0)
	exif := append([]byte("Exif\x00\x00"), tiff...)
	b.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&b, binary.BigEndian, uint16(len(exif)+2))
	b.Write(exif)
	b.Write([]byte{0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9})
	return b.Bytes()
}

func webpWithExif(exif []byte) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	vp8 := []byte{1, 2, 3} // odd size, so the chunk is padded
	body.WriteString("VP8 ")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(vp8)))
	body.Write(vp8)
	body.WriteByte(0)
	body.WriteString("EXIF")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(exif)))
	body.Write(exif)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(body.Len()))
	b.Write(body.Bytes())
	return b.Bytes()
}

func TestReadEXIF(t *testing.T) {
	unicodeBE := append([]byte("UNICODE\x00"), utf16Bytes(binary.BigEndian, sample)...)
	unicodeLE := append([]byte("UNICODE\x00"), utf16Bytes(binary.LittleEndian, sample)...)
	ascii := append([]byte("ASCII\x00\x00\x00"), []byte(sample+"\x00")...)

	tests := map[string][]byte{
		// A1111 writes big-endian UTF-16 even in little-endian EXIF.
		"jpeg II unicode BE": jpegWithExif(tiffWithComment(binary.LittleEndian, unicodeBE)),
		"jpeg MM unicode BE": jpegWithExif(tiffWithComment(binary.BigEndian, unicodeBE)),
		"jpeg II unicode LE": jpegWithExif(tiffWithComment(binary.LittleEndian, unicodeLE)),
		"jpeg ascii":         jpegWithExif(tiffWithComment(binary.LittleEndian, ascii)),
		"webp":               webpWithExif(tiffWithComment(binary.LittleEndian, unicodeBE)),
		"webp Exif prefix": webpWithExif(append([]byte("Exif\x00\x00"),
			tiffWithComment(binary.BigEndian, unicodeBE)...)),
	}
	for name, b := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := read(t, b)
			require.NoError(t, err)
			assert.Equal(t, sample, got)
		})
	}

	t.Run("jpeg without exif", func(t *testing.T) {
		_, err := read(t, []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9})
		assert.ErrorIs(t, err, ErrNoMetadata)
	})
	t.Run("empty comment", func(t *testing.T) {
		_, err := read(t, jpegWithExif(tiffWithComment(binary.LittleEndian, []byte("ASCII\x00\x00\x00"))))
		assert.ErrorIs(t, err, ErrNoMetadata)
	})
}

func TestReadUnsupported(t *testing.T) {
	_, err := read(t, []byte("GIF89a......"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoMetadata)
}

func TestParse(t *testing.T) {
	p := Parse(sample)
	assert.Equal(t, "a photo of a fox, (golden hour:1.2)\nsecond line", p.Prompt)
	assert.Equal(t, "blurry, lowres\nwatermark", p.Negative)
	assert.Equal(t, []Setting{
		{"Steps", "30"},
		{"Sampler", "DPM++ 2M"},
		{"CFG scale", "7"},
		{"Seed", "123"},
		{"Lora hashes", `"a: 1, b: 2"`},
		{"Model", "sdxl"},
	}, p.Settings)
}

func TestParseWithoutSettingsOrNegative(t *testing.T) {
	p := Parse("just a prompt, with: a colon\nand more")
	assert.Equal(t, "just a prompt, with: a colon\nand more", p.Prompt)
	assert.Empty(t, p.Negative)
	assert.Empty(t, p.Settings)
	assert.NotNil(t, p.Settings)

	p = Parse("Steps: 20, Sampler: Euler, CFG scale: 7")
	assert.Empty(t, p.Prompt)
	assert.Len(t, p.Settings, 3)
}
