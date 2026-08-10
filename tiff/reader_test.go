// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tiff

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "image/png"
)

const testdataDir = "../testdata/"

// Read makes *buffer implements io.Reader, so that we can pass one to Decode.
func (*buffer) Read([]byte) (int, error) {
	panic("unimplemented")
}

func load(name string) (image.Image, error) {
	f, err := os.Open(testdataDir + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// TestNoRPS tests decoding an image that has no RowsPerStrip tag. The tag is
// mandatory according to the spec but some software omits it in the case of a
// single strip.
func TestNoRPS(t *testing.T) {
	_, err := load("no_rps.tiff")
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoCompression tests decoding an image that has no Compression tag. This
// tag is mandatory, but most tools interpret a missing value as no
// compression.
func TestNoCompression(t *testing.T) {
	_, err := load("no_compress.tiff")
	if err != nil {
		t.Fatal(err)
	}
}

// TestUnpackBits tests the decoding of PackBits-encoded data.
func TestUnpackBits(t *testing.T) {
	var unpackBitsTests = []struct {
		compressed   string
		uncompressed string
	}{{
		// Example data from Wikipedia.
		"\xfe\xaa\x02\x80\x00\x2a\xfd\xaa\x03\x80\x00\x2a\x22\xf7\xaa",
		"\xaa\xaa\xaa\x80\x00\x2a\xaa\xaa\xaa\xaa\x80\x00\x2a\x22\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa\xaa",
	}}
	for _, u := range unpackBitsTests {
		buf, err := unpackBits(strings.NewReader(u.compressed), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf) != u.uncompressed {
			t.Fatalf("unpackBits: want %x, got %x", u.uncompressed, buf)
		}
	}
}

func TestShortBlockData(t *testing.T) {
	b, err := ioutil.ReadFile("../testdata/bw-uncompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}
	// The bw-uncompressed.tiff image is a 153x55 bi-level image. This is 1 bit
	// per pixel, or 20 bytes per row, times 55 rows, or 1100 bytes of pixel
	// data. 1100 in hex is 0x44c, or "\x4c\x04" in little-endian. We replace
	// that byte count (StripByteCounts-tagged data) by something less than
	// that, so that there is not enough pixel data.
	old := []byte{0x4c, 0x04}
	new := []byte{0x01, 0x01}
	i := bytes.Index(b, old)
	if i < 0 {
		t.Fatal(`could not find "\x4c\x04" byte count`)
	}
	if bytes.Contains(b[i+len(old):], old) {
		t.Fatal(`too many occurrences of "\x4c\x04"`)
	}
	b[i+0] = new[0]
	b[i+1] = new[1]
	if _, err = Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

func TestDecodeInvalidDataType(t *testing.T) {
	b, err := ioutil.ReadFile("../testdata/bw-uncompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// off is the offset of the ImageWidth tag. It is the offset of the overall
	// IFD block (0x00000454), plus 2 for the uint16 number of IFD entries, plus 12
	// to skip the first entry.
	const off = 0x00000454 + 2 + 12*1

	if v := binary.LittleEndian.Uint16(b[off : off+2]); v != tImageWidth {
		t.Fatal(`could not find ImageWidth tag`)
	}
	binary.LittleEndian.PutUint16(b[off+2:], uint16(len(lengths))) // invalid datatype

	if _, err = Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

func compare(t *testing.T, img0, img1 image.Image) {
	t.Helper()
	b0 := img0.Bounds()
	b1 := img1.Bounds()
	if b0.Dx() != b1.Dx() || b0.Dy() != b1.Dy() {
		t.Fatalf("wrong image size: want %s, got %s", b0, b1)
	}
	x1 := b1.Min.X - b0.Min.X
	y1 := b1.Min.Y - b0.Min.Y
	for y := b0.Min.Y; y < b0.Max.Y; y++ {
		for x := b0.Min.X; x < b0.Max.X; x++ {
			c0 := img0.At(x, y)
			c1 := img1.At(x+x1, y+y1)
			r0, g0, b0, a0 := c0.RGBA()
			r1, g1, b1, a1 := c1.RGBA()
			if r0 != r1 || g0 != g1 || b0 != b1 || a0 != a1 {
				t.Fatalf("pixel at (%d, %d) has wrong color: want %v, got %v", x, y, c0, c1)
			}
		}
	}
}

// TestDecode tests that decoding a PNG image and a TIFF image result in the
// same pixel data.
func TestDecode(t *testing.T) {
	img0, err := load("video-001.png")
	if err != nil {
		t.Fatal(err)
	}
	img1, err := load("video-001.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img2, err := load("video-001-strip-64.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img3, err := load("video-001-tile-64x64.tiff")
	if err != nil {
		t.Fatal(err)
	}
	img4, err := load("video-001-16bit.tiff")
	if err != nil {
		t.Fatal(err)
	}

	compare(t, img0, img1)
	compare(t, img0, img2)
	compare(t, img0, img3)
	compare(t, img0, img4)
}

// TestDecodeLZW tests that decoding a PNG image and a LZW-compressed TIFF
// image result in the same pixel data.
func TestDecodeLZW(t *testing.T) {
	img0, err := load("blue-purple-pink.png")
	if err != nil {
		t.Fatal(err)
	}
	img1, err := load("blue-purple-pink.lzwcompressed.tiff")
	if err != nil {
		t.Fatal(err)
	}

	compare(t, img0, img1)
}

// TestEOF tests that decoding a TIFF image returns io.ErrUnexpectedEOF
// when there are no headers or data is empty
func TestEOF(t *testing.T) {
	_, err := Decode(bytes.NewReader(nil))
	if err != io.ErrUnexpectedEOF {
		t.Errorf("Error should be io.ErrUnexpectedEOF on nil but got %v", err)
	}
}

// TestDecodeCCITT tests that decoding a PNG image and a CCITT compressed TIFF
// image result in the same pixel data.
func TestDecodeCCITT(t *testing.T) {
	// TODO Add more tests.
	for _, fn := range []string{
		"bw-gopher",
	} {
		img0, err := load(fn + ".png")
		if err != nil {
			t.Fatal(err)
		}

		img1, err := load(fn + "_ccittGroup3.tiff")
		if err != nil {
			t.Fatal(err)
		}
		compare(t, img0, img1)

		img2, err := load(fn + "_ccittGroup4.tiff")
		if err != nil {
			t.Fatal(err)
		}
		compare(t, img0, img2)
	}
}

// TestDecodeTagOrder tests that a malformed image with unsorted IFD entries is
// correctly rejected.
func TestDecodeTagOrder(t *testing.T) {
	data, err := ioutil.ReadFile("../testdata/video-001.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Swap the first two IFD entries.
	ifdOffset := int64(binary.LittleEndian.Uint32(data[4:8]))
	for i := ifdOffset + 2; i < ifdOffset+14; i++ {
		data[i], data[i+12] = data[i+12], data[i]
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("got nil error, want non-nil")
	}
}

// TestDecompress tests that decoding some TIFF images that use different
// compression formats result in the same pixel data.
func TestDecompress(t *testing.T) {
	var decompressTests = []string{
		"bw-uncompressed.tiff",
		"bw-deflate.tiff",
		"bw-packbits.tiff",
	}
	var img0 image.Image
	for _, name := range decompressTests {
		img1, err := load(name)
		if err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}
		if img0 == nil {
			img0 = img1
			continue
		}
		compare(t, img0, img1)
	}
}

func replace(src []byte, find, repl string) ([]byte, error) {
	removeSpaces := func(r rune) rune {
		if r != ' ' {
			return r
		}
		return -1
	}

	f, err := hex.DecodeString(strings.Map(removeSpaces, find))
	if err != nil {
		return nil, err
	}
	r, err := hex.DecodeString(strings.Map(removeSpaces, repl))
	if err != nil {
		return nil, err
	}
	dst := bytes.Replace(src, f, r, 1)
	if bytes.Equal(dst, src) {
		return nil, errors.New("replacement failed")
	}
	return dst, nil
}

// TestZeroBitsPerSample tests that an IFD with a bitsPerSample of 0 does not
// cause a crash.
// Issue 10711.
func TestZeroBitsPerSample(t *testing.T) {
	b0, err := ioutil.ReadFile(testdataDir + "bw-deflate.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the loaded image to have the problem.
	// 02 01: tag number (tBitsPerSample)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// ?? 00 00 00: value (1 -> 0)
	b1, err := replace(b0,
		"02 01 03 00 01 00 00 00 01 00 00 00",
		"02 01 03 00 01 00 00 00 00 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decode(bytes.NewReader(b1))
	if err == nil {
		t.Fatal("Decode with 0 bits per sample: got nil error, want non-nil")
	}
}

// TestTileTooBig tests that we do not panic when a tile is too big compared to
// the data available.
// Issue 10712
func TestTileTooBig(t *testing.T) {
	b0, err := ioutil.ReadFile(testdataDir + "video-001-tile-64x64.tiff")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the loaded image to have the problem.
	//
	// 42 01: tag number (tTileWidth)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// xx 00 00 00: value (0x40 -> 0x44: a wider tile consumes more data
	// than is available)
	b1, err := replace(b0,
		"42 01 03 00 01 00 00 00 40 00 00 00",
		"42 01 03 00 01 00 00 00 44 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Turn off the predictor, which makes it possible to hit the
	// place with the defect. Without this patch to the image, we run
	// out of data too early, and do not hit the part of the code where
	// the original panic was.
	//
	// 3d 01: tag number (tPredictor)
	// 03 00: data type (short, or uint16)
	// 01 00 00 00: count
	// xx 00 00 00: value (2 -> 1: 2 = horizontal, 1 = none)
	b2, err := replace(b1,
		"3d 01 03 00 01 00 00 00 02 00 00 00",
		"3d 01 03 00 01 00 00 00 01 00 00 00",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decode(bytes.NewReader(b2))
	if err == nil {
		t.Fatal("did not expect nil error")
	}
}

// TestZeroSizedImages tests that decoding does not panic when image dimensions
// are zero, and returns a zero-sized image instead.
// Issue 10393.
func TestZeroSizedImages(t *testing.T) {
	testsizes := []struct {
		w, h int
	}{
		{0, 0},
		{1, 0},
		{0, 1},
		{1, 1},
	}
	for _, r := range testsizes {
		img := image.NewRGBA(image.Rect(0, 0, r.w, r.h))
		var buf bytes.Buffer
		if err := Encode(&buf, img, nil); err != nil {
			t.Errorf("encode w=%d h=%d: %v", r.w, r.h, err)
			continue
		}
		if _, err := Decode(&buf); err != nil {
			t.Errorf("decode w=%d h=%d: %v", r.w, r.h, err)
		}
	}
}

// TestLargeIFDEntry tests that a large IFD entry does not cause Decode to
// panic.
// Issue 10596.
func TestLargeIFDEntry(t *testing.T) {
	testdata := "II*\x00\x08\x00\x00\x00\f\x000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000\x17\x01\x04\x00\x01\x00" +
		"\x00\xc0000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"000000"
	_, err := Decode(strings.NewReader(testdata))
	if err == nil {
		t.Fatal("Decode with large IFD entry: got nil error, want non-nil")
	}
}

// TestInvalidPaletteRef tests that decoding a paletted image whose pixel data
// references a color index past the end of the ColorMap reports an error,
// instead of returning an image whose Pix values are out of range for its
// Palette (indexing that Palette would panic on a later At or ColorIndexAt
// call).
func TestInvalidPaletteRef(t *testing.T) {
	// A minimal little-endian paletted TIFF: one 1x1 uncompressed strip with
	// 8 BitsPerSample, so the single pixel byte is a color index in the range
	// [0, 256), combined with a ColorMap that holds only two colors. The pixel
	// written below indexes entry 255 of that two-entry palette. The ColorMap
	// length and the BitsPerSample value come from independent IFD entries, so
	// nothing else in the decoder rejects this combination.
	const (
		ifdOffset      = 8
		numEntries     = 9
		colorMapOffset = ifdOffset + 2 + numEntries*ifdLen
		pixelOffset    = colorMapOffset + 12
	)

	b := make([]byte, pixelOffset+1)
	copy(b, leHeader)
	binary.LittleEndian.PutUint32(b[4:8], ifdOffset)
	binary.LittleEndian.PutUint16(b[ifdOffset:], numEntries)

	// putEntry writes one 12-byte IFD entry, either with an inline value or,
	// when the value does not fit in four bytes, with an offset to it. Entries
	// must be written in ascending tag order.
	i := ifdOffset + 2
	putEntry := func(tag, datatype uint16, count, value uint32) {
		binary.LittleEndian.PutUint16(b[i:], tag)
		binary.LittleEndian.PutUint16(b[i+2:], datatype)
		binary.LittleEndian.PutUint32(b[i+4:], count)
		if datatype == dtShort && lengths[datatype]*count <= 4 {
			binary.LittleEndian.PutUint16(b[i+8:], uint16(value))
		} else {
			binary.LittleEndian.PutUint32(b[i+8:], value)
		}
		i += ifdLen
	}

	putEntry(tImageWidth, dtShort, 1, 1)
	putEntry(tImageLength, dtShort, 1, 1)
	putEntry(tBitsPerSample, dtShort, 1, 8)
	putEntry(tCompression, dtShort, 1, cNone)
	putEntry(tPhotometricInterpretation, dtShort, 1, pPaletted)
	putEntry(tStripOffsets, dtLong, 1, pixelOffset)
	putEntry(tRowsPerStrip, dtShort, 1, 1)
	putEntry(tStripByteCounts, dtLong, 1, 1)
	// Six shorts: the red, green and blue values of a two-color palette.
	putEntry(tColorMap, dtShort, 6, colorMapOffset)

	// The ColorMap holds all of the red values, then all of the green values,
	// then all of the blue values, so the odd-numbered shorts are the three
	// components of the second (and last) color. Make that color white; the
	// first color stays black.
	for j := 1; j < 6; j += 2 {
		binary.LittleEndian.PutUint16(b[colorMapOffset+2*j:], 0xffff)
	}

	// The one pixel indexes palette entry 255, but the palette has two entries.
	b[pixelOffset] = 0xff

	if _, err := Decode(bytes.NewReader(b)); err != errInvalidColorIndex {
		t.Fatalf("Decode with invalid palette index: got %v, want %v", err, errInvalidColorIndex)
	}
}

// countingReaderAt wraps a byte slice and records the largest single ReadAt
// request made of it, and the total number of bytes requested, so that a test
// can check that a bogus length taken from a TIFF file does not lead to an
// unbounded allocation before the (necessarily failing) read of that data, and
// that repeated reads of the same data are bounded too.
type countingReaderAt struct {
	r          *bytes.Reader
	maxReadLen int
	totalRead  int64
}

func (c *countingReaderAt) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) > c.maxReadLen {
		c.maxReadLen = len(p)
	}
	c.totalRead += int64(len(p))
	return c.r.ReadAt(p, off)
}

// rampReaderAt is an io.ReaderAt that behaves like an arbitrarily long stream
// in which the byte at offset i has the value byte(i), without allocating it.
type rampReaderAt struct{}

func (rampReaderAt) ReadAt(p []byte, off int64) (int, error) {
	for i := range p {
		p[i] = byte(off + int64(i))
	}
	return len(p), nil
}

// ifdEntryOffset returns the offset, within the little-endian TIFF file b, of
// the IFD entry with the given tag.
func ifdEntryOffset(t *testing.T, b []byte, tag int) int {
	t.Helper()
	ifdOffset := int(binary.LittleEndian.Uint32(b[4:8]))
	numItems := int(binary.LittleEndian.Uint16(b[ifdOffset:]))
	for i := 0; i < numItems; i++ {
		off := ifdOffset + 2 + i*ifdLen
		if int(binary.LittleEndian.Uint16(b[off:])) == tag {
			return off
		}
	}
	t.Fatalf("could not find the IFD entry with tag %d", tag)
	return 0
}

// TestSafeReadAt tests that safeReadAt does not allocate a buffer of an
// untrusted size up front, and that reading in chunks still yields the same
// bytes as a single read would.
func TestSafeReadAt(t *testing.T) {
	const data = "0123456789"
	r := strings.NewReader(data)

	// A length that cannot be represented as an int is a read failure, not an
	// enormous allocation.
	if _, err := safeReadAt(r, uint64(math.MaxUint64), 0); err != io.ErrUnexpectedEOF {
		t.Errorf("safeReadAt with an unrepresentable length: got %v, want %v", err, io.ErrUnexpectedEOF)
	}

	// Reading no bytes at the very end of the input is not an error.
	if buf, err := safeReadAt(r, 0, int64(len(data))); err != nil || len(buf) != 0 {
		t.Errorf("safeReadAt of 0 bytes: got %q, %v, want an empty buffer and a nil error", buf, err)
	}

	// Reading more than the 10 bytes the input holds is an error.
	if _, err := safeReadAt(r, 11, 0); err == nil {
		t.Error("safeReadAt past the end of the input: got a nil error, want non-nil")
	}

	// A read that fits is served in one go.
	if buf, err := safeReadAt(r, 4, 2); err != nil || string(buf) != "2345" {
		t.Errorf("safeReadAt of 4 bytes: got %q, %v, want %q and a nil error", buf, err, "2345")
	}

	// A read of more than maxChunkSize bytes is served in chunks, and the
	// result is the concatenation of those chunks.
	const n = maxChunkSize + 5
	buf, err := safeReadAt(rampReaderAt{}, n, 0)
	if err != nil {
		t.Fatalf("safeReadAt of %d bytes: %v", n, err)
	}
	if len(buf) != n {
		t.Fatalf("safeReadAt of %d bytes: got %d bytes", n, len(buf))
	}
	for i := range buf {
		if buf[i] != byte(i) {
			t.Fatalf("safeReadAt of %d bytes: byte %d is %d, want %d", n, i, buf[i], byte(i))
		}
	}

	// A chunked read that runs out of data is an error, and no more than
	// maxChunkSize bytes are read at a time on the way to finding that out.
	c := &countingReaderAt{r: bytes.NewReader([]byte(data))}
	if _, err := safeReadAt(c, n, 0); err == nil {
		t.Error("safeReadAt of a huge length from a short input: got a nil error, want non-nil")
	}
	if c.maxReadLen > maxChunkSize {
		t.Errorf("safeReadAt read %d bytes at once, want at most %d", c.maxReadLen, maxChunkSize)
	}
}

// TestDecodeHugeIFDValueLength tests that an IFD entry that points at an
// enormous amount of data is rejected without first allocating a buffer of
// that size.
func TestDecodeHugeIFDValueLength(t *testing.T) {
	// count is the largest number of dtLong values that gets past the "IFD data
	// too large" check in ifdUint, which is math.MaxInt32 / 4. The data is
	// therefore just under 2 GiB long and, being longer than 4 bytes, is
	// pointed at by the entry instead of being held inside it.
	const count = math.MaxInt32 / 4

	// 8 header bytes, 2 for the entry count, 12 for the one entry and 4 for the
	// offset of the next IFD.
	b := make([]byte, 8+2+ifdLen+4)
	copy(b, leHeader)
	// Offset of the first IFD.
	binary.LittleEndian.PutUint32(b[4:], 8)
	// Number of IFD entries.
	binary.LittleEndian.PutUint16(b[8:], 1)
	// The entry itself: tag, data type, number of values and their offset.
	binary.LittleEndian.PutUint16(b[10:], tStripByteCounts)
	binary.LittleEndian.PutUint16(b[12:], dtLong)
	binary.LittleEndian.PutUint32(b[14:], count)
	binary.LittleEndian.PutUint32(b[18:], 0)

	c := &countingReaderAt{r: bytes.NewReader(b)}
	if _, err := Decode(c); err == nil {
		t.Fatal("got a nil error, want non-nil")
	}
	if c.maxReadLen > maxChunkSize {
		t.Errorf("decoding read %d bytes at once, want at most %d", c.maxReadLen, maxChunkSize)
	}
}

// TestDecodeHugeStripByteCount tests that a StripByteCounts value that is far
// larger than the pixel data actually present is rejected without first
// allocating a buffer of that size.
func TestDecodeHugeStripByteCount(t *testing.T) {
	var w bytes.Buffer
	if err := Encode(&w, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	b := w.Bytes()

	// Claim that the uncompressed pixel data is almost 4 GiB long. The entry
	// holds a single dtLong value, which is 4 bytes, so the value is stored
	// inside the entry rather than being pointed at.
	off := ifdEntryOffset(t, b, tStripByteCounts)
	if v := binary.LittleEndian.Uint16(b[off+2:]); v != dtLong {
		t.Fatalf("StripByteCounts has data type %d, want %d", v, dtLong)
	}
	if v := binary.LittleEndian.Uint32(b[off+4:]); v != 1 {
		t.Fatalf("StripByteCounts holds %d values, want 1", v)
	}
	binary.LittleEndian.PutUint32(b[off+8:], math.MaxUint32)

	// The reader deliberately implements io.ReaderAt, so that it is used as is
	// instead of being wrapped in a *buffer. Decode then reads the pixel data
	// with the StripByteCounts value instead of slicing the buffer.
	c := &countingReaderAt{r: bytes.NewReader(b)}
	if _, err := Decode(c); err == nil {
		t.Fatal("got a nil error, want non-nil")
	}
	if c.maxReadLen > maxChunkSize {
		t.Errorf("decoding read %d bytes at once, want at most %d", c.maxReadLen, maxChunkSize)
	}
}

// TestDecodeShortIFD tests that an IFD header claiming far more entries than
// the file holds is rejected instead of yielding those entries as pixel data.
// The claimed number of entries is a uint16, so ifdLen times it always fits in
// one chunk, but the read of the entries must still fail rather than succeed
// against a truncated file.
func TestDecodeShortIFD(t *testing.T) {
	// 8 header bytes and 2 for the entry count, with no entries at all.
	b := make([]byte, 10)
	copy(b, leHeader)
	// Offset of the first IFD.
	binary.LittleEndian.PutUint32(b[4:], 8)
	// Number of IFD entries.
	binary.LittleEndian.PutUint16(b[8:], math.MaxUint16)

	c := &countingReaderAt{r: bytes.NewReader(b)}
	if _, err := Decode(c); err == nil {
		t.Fatal("got a nil error, want non-nil")
	}
	if c.maxReadLen > maxChunkSize {
		t.Errorf("decoding read %d bytes at once, want at most %d", c.maxReadLen, maxChunkSize)
	}
}

// TestSmallTileSize tests that a tiled image whose tiles are too small is
// rejected. A one pixel wide, zero pixel high tile in an image that is claimed
// to be 2^32-1 pixels wide otherwise makes Decode iterate over more than four
// billion tiles.
func TestSmallTileSize(t *testing.T) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:  uint32(4294967295),
		tImageLength: uint32(0),
		tTileWidth:   uint32(1),
		tTileLength:  uint32(0),
	})
	if _, err := Decode(bytes.NewReader(data)); err != FormatError("tile size is too small") {
		t.Errorf("Decode of a 1x0 tile: got %v, want %v", err, FormatError("tile size is too small"))
	}
}

// TestZeroHeightTiledImage tests that a tiled image of zero height, whose width
// implies hundreds of millions of tiles across, still decodes to an empty image
// and a nil error. Skipping the block loop entirely when there is no block to
// read must not turn a zero-sized image into an error or a nil image.
func TestZeroHeightTiledImage(t *testing.T) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:  uint32(4294967295),
		tImageLength: uint32(0),
		tTileWidth:   uint32(8),
		tTileLength:  uint32(8),
	})
	start := time.Now()
	img, err := Decode(bytes.NewReader(data))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img == nil {
		t.Fatal("Decode returned a nil image and a nil error")
	}
	if got := img.Bounds().Dy(); got != 0 {
		t.Errorf("decoded image height is %d, want 0", got)
	}
	// There are over half a billion tiles across, and none of them down. The
	// block loop is skipped rather than walked over every column of an image
	// that has no rows.
	if elapsed > 30*time.Second {
		t.Errorf("decoding an empty image took %v, want well under 30s", elapsed)
	}
}

// TestExtraSamples tests that a non-RGB image claiming more than one sample per
// pixel is rejected, instead of being decoded as if it had a single one.
func TestExtraSamples(t *testing.T) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:                uint32(8),
		tImageLength:               uint32(8),
		tBitsPerSample:             []uint16{8, 8, 8},
		tPhotometricInterpretation: uint16(pBlackIsZero),
	})
	if _, err := DecodeConfig(bytes.NewReader(data)); err != UnsupportedError("extra samples") {
		t.Errorf("DecodeConfig of a grayscale image with three samples per pixel: got %v, want %v", err, UnsupportedError("extra samples"))
	}
}

// TestOversizedTileData tests that the decompressed data of a tile is limited
// to the size the tile can hold, so that a small image built out of many tiles
// that all point at the same compressed data cannot be made to decompress an
// unbounded amount of it. The image below is 256x256 pixels and a little over
// 64 KiB long, but each of its 1024 tiles decompresses to 64 MiB, or 64 GiB in
// total, if the tile size is not taken into account.
func TestOversizedTileData(t *testing.T) {
	const (
		imageWidth  = 256
		imageHeight = 256
		tileWidth   = 8
		tileLength  = 8
		numTiles    = (imageWidth * imageHeight) / (tileWidth * tileLength)
	)

	// Create a chunk of tile data that decompresses to a large size.
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	zeros := make([]byte, 1024)
	for i := 0; i < 1<<16; i++ {
		if _, err := zw.Write(zeros); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zdata := zbuf.Bytes()

	enc := binary.BigEndian
	data := newTIFF(enc)

	zoff := len(data)
	data = append(data, zdata...)

	// Each tile refers to the same compressed data chunk.
	var tileoffs []uint32
	var tilesizes []uint32
	for i := 0; i < numTiles; i++ {
		tileoffs = append(tileoffs, uint32(zoff))
		tilesizes = append(tilesizes, uint32(len(zdata)))
	}

	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:                uint32(imageWidth),
		tImageLength:               uint32(imageHeight),
		tTileWidth:                 uint32(tileWidth),
		tTileLength:                uint32(tileLength),
		tTileOffsets:               tileoffs,
		tTileByteCounts:            tilesizes,
		tCompression:               uint16(cDeflate),
		tBitsPerSample:             []uint16{16, 16, 16},
		tPhotometricInterpretation: uint16(pRGB),
	})

	// The reader deliberately implements io.ReaderAt, so that the tile data is
	// read through it instead of being sliced out of a *buffer.
	c := &countingReaderAt{r: bytes.NewReader(data)}
	start := time.Now()
	img, err := Decode(c)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got, want := img.Bounds(), image.Rect(0, 0, imageWidth, imageHeight); got != want {
		t.Errorf("decoded image bounds are %v, want %v", got, want)
	}

	// Each tile stops reading once it holds as much data as its 8x8 pixels can
	// use, so only the leading fraction of each compressed chunk is ever
	// inflated. Without the limit this inflates 64 GiB and takes tens of
	// seconds, reading all 64 MiB of compressed data on the way.
	if elapsed > 30*time.Second {
		t.Errorf("decoding took %v, want well under 30s", elapsed)
	}
	if maxRead := int64(numTiles) * int64(len(zdata)) / 4; c.totalRead >= maxRead {
		t.Errorf("decoding read %d bytes of compressed data, want well under %d", c.totalRead, maxRead)
	}
}

// TestReadBuf tests that readBuf reads no more than the given limit, so that a
// small tile cannot be made to yield an unbounded amount of decompressed data,
// and that it reuses the buffer it is given.
func TestReadBuf(t *testing.T) {
	const lim = 10
	src := bytes.Repeat([]byte{'x'}, 4*lim)

	buf, err := readBuf(bytes.NewReader(src), nil, lim)
	if err != nil {
		t.Fatalf("readBuf: %v", err)
	}
	if len(buf) != lim {
		t.Fatalf("readBuf of a %d byte input with a limit of %d: got %d bytes", len(src), lim, len(buf))
	}

	// An input shorter than the limit is read in full.
	buf2, err := readBuf(bytes.NewReader(src[:lim/2]), buf, lim)
	if err != nil {
		t.Fatalf("readBuf: %v", err)
	}
	if len(buf2) != lim/2 {
		t.Fatalf("readBuf of a %d byte input with a limit of %d: got %d bytes", lim/2, lim, len(buf2))
	}
}

// benchmarkDecode benchmarks the decoding of an image.
func benchmarkDecode(b *testing.B, filename string) {
	b.Helper()
	contents, err := ioutil.ReadFile(testdataDir + filename)
	if err != nil {
		b.Fatal(err)
	}
	benchmarkDecodeData(b, contents)
}

func benchmarkDecodeData(b *testing.B, data []byte) {
	b.Helper()
	r := &buffer{buf: data}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Decode(r)
		if err != nil {
			b.Fatal("Decode:", err)
		}
	}
}

func BenchmarkDecodeCompressed(b *testing.B)   { benchmarkDecode(b, "video-001.tiff") }
func BenchmarkDecodeUncompressed(b *testing.B) { benchmarkDecode(b, "video-001-uncompressed.tiff") }

func BenchmarkZeroHeightTile(b *testing.B) {
	enc := binary.BigEndian
	data := newTIFF(enc)
	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:  uint32(4294967295),
		tImageLength: uint32(0),
		tTileWidth:   uint32(1),
		tTileLength:  uint32(0),
	})
	benchmarkDecodeData(b, data)
}

func BenchmarkRepeatedOversizedTileData(b *testing.B) {
	const (
		imageWidth  = 256
		imageHeight = 256
		tileWidth   = 8
		tileLength  = 8
		numTiles    = (imageWidth * imageHeight) / (tileWidth * tileLength)
	)

	// Create a chunk of tile data that decompresses to a large size.
	zdata := func() []byte {
		var zbuf bytes.Buffer
		zw := zlib.NewWriter(&zbuf)
		zeros := make([]byte, 1024)
		for i := 0; i < 1<<16; i++ {
			zw.Write(zeros)
		}
		zw.Close()
		return zbuf.Bytes()
	}()

	enc := binary.BigEndian
	data := newTIFF(enc)

	zoff := len(data)
	data = append(data, zdata...)

	// Each tile refers to the same compressed data chunk.
	var tileoffs []uint32
	var tilesizes []uint32
	for i := 0; i < numTiles; i++ {
		tileoffs = append(tileoffs, uint32(zoff))
		tilesizes = append(tilesizes, uint32(len(zdata)))
	}

	data = appendIFD(data, enc, map[uint16]interface{}{
		tImageWidth:                uint32(imageWidth),
		tImageLength:               uint32(imageHeight),
		tTileWidth:                 uint32(tileWidth),
		tTileLength:                uint32(tileLength),
		tTileOffsets:               tileoffs,
		tTileByteCounts:            tilesizes,
		tCompression:               uint16(cDeflate),
		tBitsPerSample:             []uint16{16, 16, 16},
		tPhotometricInterpretation: uint16(pRGB),
	})
	benchmarkDecodeData(b, data)
}

type byteOrder interface {
	binary.ByteOrder
}

// appendUint16 appends v to b in enc's byte order. binary.AppendByteOrder and
// its AppendUintNN methods are not available in the Go version this package
// targets, so the encoding is done with a scratch buffer instead.
func appendUint16(enc byteOrder, b []byte, v uint16) []byte {
	var buf [2]byte
	enc.PutUint16(buf[:], v)
	return append(b, buf[:]...)
}

// appendUint32 appends v to b in enc's byte order.
func appendUint32(enc byteOrder, b []byte, v uint32) []byte {
	var buf [4]byte
	enc.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

// newTIFF returns the TIFF header.
func newTIFF(enc byteOrder) []byte {
	b := []byte{0, 0, 0, 42, 0, 0, 0, 0}
	switch enc.Uint16([]byte{1, 0}) {
	case 0x1:
		b[0], b[1] = 'I', 'I'
	case 0x100:
		b[0], b[1] = 'M', 'M'
	default:
		panic("odd byte order")
	}
	return b
}

// appendIFD appends an IFD to the TIFF in b,
// updating the IFD location in the header.
func appendIFD(b []byte, enc byteOrder, entries map[uint16]interface{}) []byte {
	var tags []uint16
	for tag := range entries {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i] < tags[j]
	})

	var ifd []byte
	for _, tag := range tags {
		ifd = appendUint16(enc, ifd, tag)
		switch v := entries[tag].(type) {
		case uint16:
			ifd = appendUint16(enc, ifd, dtShort)
			ifd = appendUint32(enc, ifd, 1)
			ifd = appendUint16(enc, ifd, v)
			ifd = appendUint16(enc, ifd, v)
		case uint32:
			ifd = appendUint16(enc, ifd, dtLong)
			ifd = appendUint32(enc, ifd, 1)
			ifd = appendUint32(enc, ifd, v)
		case []uint16:
			ifd = appendUint16(enc, ifd, dtShort)
			ifd = appendUint32(enc, ifd, uint32(len(v)))
			switch len(v) {
			case 0:
				ifd = appendUint32(enc, ifd, 0)
			case 1:
				ifd = appendUint16(enc, ifd, v[0])
				ifd = appendUint16(enc, ifd, v[1])
			default:
				ifd = appendUint32(enc, ifd, uint32(len(b)))
				for _, e := range v {
					b = appendUint16(enc, b, e)
				}
			}
		case []uint32:
			ifd = appendUint16(enc, ifd, dtLong)
			ifd = appendUint32(enc, ifd, uint32(len(v)))
			switch len(v) {
			case 0:
				ifd = appendUint32(enc, ifd, 0)
			case 1:
				ifd = appendUint32(enc, ifd, v[0])
			default:
				ifd = appendUint32(enc, ifd, uint32(len(b)))
				for _, e := range v {
					b = appendUint32(enc, b, e)
				}
			}
		default:
			panic(fmt.Errorf("unhandled type %T", v))
		}
	}

	enc.PutUint32(b[4:8], uint32(len(b)))
	b = appendUint16(enc, b, uint16(len(entries)))
	b = append(b, ifd...)
	b = appendUint32(enc, b, 0)
	return b
}

// ioReader wraps an io.Reader to hide any io.ReaderAt implementation,
// forcing the tiff package to use the buffer code path. It records the
// largest read it was asked for, so that a test can check that the buffer
// fills in bounded chunks instead of one giant allocation.
type ioReader struct {
	io.Reader
	maxRead int
}

func (r *ioReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	return r.Reader.Read(p)
}

// TestDecodeOOMIFDOffset tests that a TIFF with an IFD offset of 0xFFFFFFFF
// does not cause an out-of-memory panic in buffer.fill.
func TestDecodeOOMIFDOffset(t *testing.T) {
	for _, endian := range []struct {
		name   string
		header []byte
	}{
		{"little-endian", []byte{'I', 'I', 42, 0, 0xff, 0xff, 0xff, 0xff}},
		{"big-endian", []byte{'M', 'M', 0, 42, 0xff, 0xff, 0xff, 0xff}},
	} {
		t.Run(endian.name, func(t *testing.T) {
			r := &ioReader{Reader: bytes.NewReader(endian.header)}
			if _, err := Decode(r); err == nil {
				t.Error("Decode with IFD offset 0xFFFFFFFF: got nil error, want non-nil")
			}
			if r.maxRead > fillChunkSize {
				t.Errorf("Decode with IFD offset 0xFFFFFFFF: largest read was %d bytes, want at most %d", r.maxRead, fillChunkSize)
			}
			r = &ioReader{Reader: bytes.NewReader(endian.header)}
			if _, err := DecodeConfig(r); err == nil {
				t.Error("DecodeConfig with IFD offset 0xFFFFFFFF: got nil error, want non-nil")
			}
			if r.maxRead > fillChunkSize {
				t.Errorf("DecodeConfig with IFD offset 0xFFFFFFFF: largest read was %d bytes, want at most %d", r.maxRead, fillChunkSize)
			}
		})
	}
}
