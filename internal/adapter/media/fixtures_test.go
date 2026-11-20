package media_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

type tiffFixtureSpec struct {
	littleEndian bool
	orientation  uint16
	makeName     string
	modelName    string
	lensModel    string
	iso          uint16
	fnumNum      uint32
	fnumDen      uint32
	expNum       uint32
	expDen       uint32
	focalNum     uint32
	focalDen     uint32
	width        uint32
	height       uint32
	dateTimeOrig string
	latRef       string
	latDMS       [3][2]uint32
	lngRef       string
	lngDMS       [3][2]uint32
	includeGPS   bool
}

func writeTestFile(t *testing.T, root, rel string, data []byte) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", full, err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatalf("write %q: %v", full, err)
	}
	return full
}

func writeExecScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write script %q: %v", p, err)
	}
	return p
}

func makePatternImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / max(1, w-1)),
				G: uint8((y * 255) / max(1, h-1)),
				B: 120,
				A: 255,
			})
		}
	}
	return img
}

func encodeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, makePatternImage(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodeTestPNGWithEXIF(t *testing.T, w, h int, tiff []byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	if err := png.Encode(&raw, makePatternImage(w, h)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if len(tiff) == 0 {
		return raw.Bytes()
	}
	b := raw.Bytes()
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(tiff)))
	chunk.WriteString("eXIf")
	chunk.Write(tiff)
	crc := crc32.ChecksumIEEE(chunk.Bytes()[4:])
	_ = binary.Write(&chunk, binary.BigEndian, crc)

	var out bytes.Buffer
	out.Write(b[:33]) // PNG signature + IHDR chunk (8 + 4 + 4 + 13 + 4 = 33)
	out.Write(chunk.Bytes())
	out.Write(b[33:])
	return out.Bytes()
}

func encodeTestGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, makePatternImage(w, h), nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func injectJPEGAPP1(jpegBytes, tiffBytes []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), tiffBytes...)
	segLen := uint16(len(payload) + 2)
	var app1 bytes.Buffer
	app1.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&app1, binary.BigEndian, segLen)
	app1.Write(payload)

	var out bytes.Buffer
	out.Write(jpegBytes[:2])
	out.Write(app1.Bytes())
	out.Write(jpegBytes[2:])
	return out.Bytes()
}

func buildTIFFFixture(spec tiffFixtureSpec) []byte {
	var order binary.ByteOrder = binary.BigEndian
	endianTag := "MM"
	if spec.littleEndian {
		order = binary.LittleEndian
		endianTag = "II"
	}
	b := newTIFFBuilder(order, endianTag)
	ifd0Off := b.reserveHeader()
	exifPtrIdx, gpsPtrIdx := b.writeIFD0(ifd0Off, spec)
	exifOff := b.writeExifIFD(spec)
	order.PutUint32(b.buf[exifPtrIdx:exifPtrIdx+4], exifOff)
	if spec.includeGPS {
		gpsOff := b.writeGPSIFD(spec)
		order.PutUint32(b.buf[gpsPtrIdx:gpsPtrIdx+4], gpsOff)
	}
	return b.buf
}

func buildSingleIFDTIFF(order binary.ByteOrder, endianTag string, entries []rawIFDEntry) []byte {
	b := newTIFFBuilder(order, endianTag)
	off := b.reserveHeader()
	b.writeIFDBlock(off, entries)
	return b.buf
}

type rawIFDEntry struct {
	tag       uint16
	fieldType uint16
	count     uint32
	data      []byte
}

type tiffBuilder struct {
	order binary.ByteOrder
	tag   string
	buf   []byte
}

func newTIFFBuilder(order binary.ByteOrder, tag string) *tiffBuilder {
	return &tiffBuilder{order: order, tag: tag}
}

func (b *tiffBuilder) reserveHeader() uint32 {
	b.buf = append(b.buf, b.tag...)
	var tmp [6]byte
	b.order.PutUint16(tmp[0:2], 42)
	b.order.PutUint32(tmp[2:6], 8)
	b.buf = append(b.buf, tmp[:]...)
	return 8
}

func (b *tiffBuilder) writeIFD0(offset uint32, spec tiffFixtureSpec) (int, int) {
	entries := []rawIFDEntry{
		b.asciiEntry(0x010F, spec.makeName),
		b.asciiEntry(0x0110, spec.modelName),
		b.shortEntry(0x0112, spec.orientation),
		b.longEntry(0x8769, 0),
	}
	if spec.includeGPS {
		entries = append(entries, b.longEntry(0x8825, 0))
	}
	valOffsets := b.writeIFDBlock(offset, entries)
	exifPtrIdx := valOffsets[3]
	gpsPtrIdx := 0
	if spec.includeGPS {
		gpsPtrIdx = valOffsets[4]
	}
	return exifPtrIdx, gpsPtrIdx
}

func (b *tiffBuilder) writeExifIFD(spec tiffFixtureSpec) uint32 {
	off := uint32(len(b.buf))
	entries := []rawIFDEntry{
		b.rationalEntry(0x829A, [][2]uint32{{spec.expNum, spec.expDen}}),
		b.rationalEntry(0x829D, [][2]uint32{{spec.fnumNum, spec.fnumDen}}),
		b.shortEntry(0x8827, spec.iso),
		b.asciiEntry(0x9003, spec.dateTimeOrig),
		b.rationalEntry(0x920A, [][2]uint32{{spec.focalNum, spec.focalDen}}),
		b.longEntry(0xA002, spec.width),
		b.longEntry(0xA003, spec.height),
		b.asciiEntry(0xA434, spec.lensModel),
	}
	b.writeIFDBlock(off, entries)
	return off
}

func (b *tiffBuilder) writeGPSIFD(spec tiffFixtureSpec) uint32 {
	off := uint32(len(b.buf))
	entries := []rawIFDEntry{
		b.asciiEntry(0x0001, spec.latRef),
		b.rationalEntry(0x0002, spec.latDMS[:]),
		b.asciiEntry(0x0003, spec.lngRef),
		b.rationalEntry(0x0004, spec.lngDMS[:]),
	}
	b.writeIFDBlock(off, entries)
	return off
}

func (b *tiffBuilder) writeIFDBlock(start uint32, entries []rawIFDEntry) []int {
	for len(b.buf) < int(start) {
		b.buf = append(b.buf, 0)
	}
	var cnt [2]byte
	b.order.PutUint16(cnt[:], uint16(len(entries)))
	b.buf = append(b.buf, cnt[:]...)

	entryStart := len(b.buf)
	b.buf = append(b.buf, make([]byte, len(entries)*12+4)...)
	valOffsets := make([]int, len(entries))

	for i, e := range entries {
		rec := entryStart + i*12
		b.order.PutUint16(b.buf[rec:rec+2], e.tag)
		b.order.PutUint16(b.buf[rec+2:rec+4], e.fieldType)
		b.order.PutUint32(b.buf[rec+4:rec+8], e.count)
		if len(e.data) <= 4 {
			copy(b.buf[rec+8:rec+12], e.data)
			valOffsets[i] = rec + 8
			continue
		}
		dataOff := uint32(len(b.buf))
		b.buf = append(b.buf, e.data...)
		b.order.PutUint32(b.buf[rec+8:rec+12], dataOff)
		valOffsets[i] = int(dataOff)
	}
	return valOffsets
}

func (b *tiffBuilder) asciiEntry(tag uint16, val string) rawIFDEntry {
	data := append([]byte(val), 0)
	return rawIFDEntry{tag: tag, fieldType: 2, count: uint32(len(data)), data: data}
}

func (b *tiffBuilder) shortEntry(tag, val uint16) rawIFDEntry {
	var data [2]byte
	b.order.PutUint16(data[:], val)
	return rawIFDEntry{tag: tag, fieldType: 3, count: 1, data: data[:]}
}

func (b *tiffBuilder) longEntry(tag uint16, val uint32) rawIFDEntry {
	var data [4]byte
	b.order.PutUint32(data[:], val)
	return rawIFDEntry{tag: tag, fieldType: 4, count: 1, data: data[:]}
}

func (b *tiffBuilder) rationalEntry(tag uint16, pairs [][2]uint32) rawIFDEntry {
	data := make([]byte, len(pairs)*8)
	for i, p := range pairs {
		b.order.PutUint32(data[i*8:i*8+4], p[0])
		b.order.PutUint32(data[i*8+4:i*8+8], p[1])
	}
	return rawIFDEntry{tag: tag, fieldType: 5, count: uint32(len(pairs)), data: data}
}

func makeISOBox(boxType string, payload []byte) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(len(payload)+8))
	out.WriteString(boxType)
	out.Write(payload)
	return out.Bytes()
}

func makeFullBox(boxType string, version byte, flags uint32, body []byte) []byte {
	var payload bytes.Buffer
	payload.WriteByte(version)
	payload.WriteByte(byte(flags >> 16))
	payload.WriteByte(byte(flags >> 8))
	payload.WriteByte(byte(flags))
	payload.Write(body)
	return makeISOBox(boxType, payload.Bytes())
}

func buildCR3Fixture(t *testing.T, spec tiffFixtureSpec, thumbJPG, previewJPG []byte) []byte {
	t.Helper()
	b := newTIFFBuilder(binary.LittleEndian, "II")
	cmt1 := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		b.asciiEntry(0x010F, spec.makeName),
		b.asciiEntry(0x0110, spec.modelName),
		b.shortEntry(0x0112, spec.orientation),
	})
	cmt2 := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		b.rationalEntry(0x829A, [][2]uint32{{spec.expNum, spec.expDen}}),
		b.rationalEntry(0x829D, [][2]uint32{{spec.fnumNum, spec.fnumDen}}),
		b.shortEntry(0x8827, spec.iso),
		b.asciiEntry(0x9003, spec.dateTimeOrig),
		b.rationalEntry(0x920A, [][2]uint32{{spec.focalNum, spec.focalDen}}),
		b.longEntry(0xA002, spec.width),
		b.longEntry(0xA003, spec.height),
		b.asciiEntry(0xA434, spec.lensModel),
	})
	cmt4 := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		b.asciiEntry(0x0001, spec.latRef),
		b.rationalEntry(0x0002, spec.latDMS[:]),
		b.asciiEntry(0x0003, spec.lngRef),
		b.rationalEntry(0x0004, spec.lngDMS[:]),
	})

	var uuidBody bytes.Buffer
	uuidBody.Write([]byte{0x85, 0xc0, 0xb6, 0x87, 0x82, 0x0f, 0x11, 0xe0, 0x81, 0x11, 0xf4, 0xce, 0x46, 0x2b, 0x6a, 0x48})
	uuidBody.Write(makeISOBox("CMT1", cmt1))
	uuidBody.Write(makeISOBox("CMT2", cmt2))
	uuidBody.Write(makeISOBox("CMT4", cmt4))

	ftyp := makeISOBox("ftyp", []byte("crx \x00\x00\x00\x01crx isom"))
	moov := makeISOBox("moov", makeISOBox("uuid", uuidBody.Bytes()))
	var mdatBody bytes.Buffer
	mdatBody.Write(thumbJPG)
	mdatBody.Write([]byte("PADDATA"))
	mdatBody.Write(previewJPG)
	mdat := makeISOBox("mdat", mdatBody.Bytes())

	return append(append(ftyp, moov...), mdat...)
}

func buildHEICFixture(w, h uint32, tiffBytes, embeddedPreview []byte) []byte {
	ftyp := makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heicmif1"))

	var ispeSmall, ispeLarge bytes.Buffer
	_ = binary.Write(&ispeSmall, binary.BigEndian, uint32(160))
	_ = binary.Write(&ispeSmall, binary.BigEndian, uint32(120))
	_ = binary.Write(&ispeLarge, binary.BigEndian, w)
	_ = binary.Write(&ispeLarge, binary.BigEndian, h)
	ipco := makeISOBox("ipco", append(
		makeFullBox("ispe", 0, 0, ispeSmall.Bytes()),
		makeFullBox("ispe", 0, 0, ispeLarge.Bytes())...,
	))
	iprp := makeISOBox("iprp", ipco)

	infe := makeFullBox("infe", 2, 0, append([]byte{0x00, 0x02, 0x00, 0x00}, []byte("Exif\x00")...))
	var iinfBody bytes.Buffer
	_ = binary.Write(&iinfBody, binary.BigEndian, uint16(1))
	iinfBody.Write(infe)
	iinf := makeFullBox("iinf", 0, 0, iinfBody.Bytes())

	exifPayload := append([]byte{0, 0, 0, 6}, append([]byte("Exif\x00\x00"), tiffBytes...)...)
	var ilocBody bytes.Buffer
	ilocBody.WriteByte(0x44) // offset_size=4, length_size=4
	ilocBody.WriteByte(0x00) // base_offset_size=0, index_size=0
	_ = binary.Write(&ilocBody, binary.BigEndian, uint16(1))
	_ = binary.Write(&ilocBody, binary.BigEndian, uint16(2)) // item_ID=2
	_ = binary.Write(&ilocBody, binary.BigEndian, uint16(1)) // construction_method=1 (idat)
	_ = binary.Write(&ilocBody, binary.BigEndian, uint16(0)) // data_ref_idx
	_ = binary.Write(&ilocBody, binary.BigEndian, uint16(1)) // extent_count=1
	_ = binary.Write(&ilocBody, binary.BigEndian, uint32(0)) // offset in idat
	_ = binary.Write(&ilocBody, binary.BigEndian, uint32(len(exifPayload)))
	iloc := makeFullBox("iloc", 1, 0, ilocBody.Bytes())
	idat := makeISOBox("idat", exifPayload)

	var metaBody bytes.Buffer
	metaBody.Write(iprp)
	metaBody.Write(iinf)
	metaBody.Write(iloc)
	metaBody.Write(idat)
	meta := makeFullBox("meta", 0, 0, metaBody.Bytes())
	mdat := makeISOBox("mdat", embeddedPreview)

	return append(append(ftyp, meta...), mdat...)
}

func buildMP4Fixture(version byte, timescale uint32, duration uint64, w, h uint32) []byte {
	ftyp := makeISOBox("ftyp", []byte("isom\x00\x00\x02\x00isommp41"))

	var mvhdBody bytes.Buffer
	if version == 0 {
		_ = binary.Write(&mvhdBody, binary.BigEndian, uint32(3800000000)) // creation
		_ = binary.Write(&mvhdBody, binary.BigEndian, uint32(3800000000)) // mod
		_ = binary.Write(&mvhdBody, binary.BigEndian, timescale)
		_ = binary.Write(&mvhdBody, binary.BigEndian, uint32(duration))
		mvhdBody.Write(make([]byte, 80))
	} else {
		_ = binary.Write(&mvhdBody, binary.BigEndian, uint64(3800000000))
		_ = binary.Write(&mvhdBody, binary.BigEndian, uint64(3800000000))
		_ = binary.Write(&mvhdBody, binary.BigEndian, timescale)
		_ = binary.Write(&mvhdBody, binary.BigEndian, duration)
		mvhdBody.Write(make([]byte, 80))
	}
	mvhd := makeFullBox("mvhd", version, 0, mvhdBody.Bytes())

	var tkhdBody bytes.Buffer
	tkhdBody.Write(make([]byte, 72))
	_ = binary.Write(&tkhdBody, binary.BigEndian, w<<16)
	_ = binary.Write(&tkhdBody, binary.BigEndian, h<<16)
	tkhd := makeFullBox("tkhd", 0, 3, tkhdBody.Bytes())
	trak := makeISOBox("trak", tkhd)

	moov := makeISOBox("moov", append(mvhd, trak...))
	mdat := makeISOBox("mdat", []byte("videopayload"))
	return append(append(ftyp, moov...), mdat...)
}
