package media_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucastheo/photos/internal/adapter/media"
	"github.com/lucastheo/photos/internal/domain"
)

func TestEXIFExtractorExifToolVariationsAndFallbacks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bin := t.TempDir()

	writeTestFile(t, root, "no_dims.jpg", encodeTestJPEG(t, 44, 22))
	writeTestFile(t, root, "clip.mp4", buildMP4Fixture(0, 1000, 3250, 320, 240))
	writeTestFile(t, root, "tiny.avif", append(makeISOBox("ftyp", []byte("avif\x00\x00\x00\x00avifmif1")),
		makeFullBox("meta", 0, 0, makeISOBox("iprp", makeISOBox("ipco", makeFullBox("ispe", 0, 0, func() []byte {
			var b bytes.Buffer
			_ = binary.Write(&b, binary.BigEndian, uint32(640))
			_ = binary.Write(&b, binary.BigEndian, uint32(480))
			return b.Bytes()
		}()))))...))
	writeTestFile(t, root, "clip.webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x00})

	// String-formatted exiftool JSON missing ImageWidth/ImageHeight -> native dimension fill.
	stringScript := writeExecScript(t, bin, "exif-strings", `
printf '%s\n' '[{
  "Orientation": "Rotate 90 CW",
  "Make": 123,
  "Model": "TestModel",
  "ISO": "200",
  "FNumber": "f/2.8",
  "ExposureTime": "1/125",
  "FocalLength": "50.0 mm",
  "CreateDate": "2026:02:03 04:05:06+00:00",
  "Duration": "0:01:15"
}]'
`)
	ext := media.NewEXIFExtractor(root).WithExifToolPath(stringScript)
	meta, err := ext.Extract(t.Context(), "no_dims.jpg", domain.FormatJPG)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if meta.Width != 22 || meta.Height != 44 || meta.EXIF.Orientation != 6 {
		t.Fatalf("expected filled + swapped dims 22x44 orient 6, got %dx%d orient %d", meta.Width, meta.Height, meta.EXIF.Orientation)
	}
	if meta.EXIF.CameraMake != "123" || meta.EXIF.ExposureTime != "1/125" || meta.DurationSec != 75 {
		t.Fatalf("unexpected parsed fields: %+v dur=%v", meta.EXIF, meta.DurationSec)
	}
	if meta.EXIF.FocalLengthMM == nil || *meta.EXIF.FocalLengthMM != 50 || meta.EXIF.ISO == nil || *meta.EXIF.ISO != 200 {
		t.Fatalf("unexpected focal/iso: %+v", meta.EXIF)
	}
	if meta.TakenAt == nil || meta.TakenAt.Month() != 2 || meta.TakenAt.Day() != 3 {
		t.Fatalf("unexpected CreateDate TakenAt: %v", meta.TakenAt)
	}

	// Numeric ExposureTime >= 1 and < 1 without exact integer denominator + Rotate 180 / 270 CW.
	rotScript := writeExecScript(t, bin, "exif-rot", `
printf '%s\n' '[{"Orientation": "Rotate 270 CW", "ExposureTime": 2.5, "Duration": "12.5 s"}]'
`)
	metaRot, err := media.NewEXIFExtractor(root).WithExifToolPath(rotScript).Extract(t.Context(), "clip.mp4", domain.FormatMP4)
	if err != nil || metaRot.EXIF.Orientation != 8 || metaRot.EXIF.ExposureTime != "2.5" || metaRot.Width != 240 || metaRot.Height != 320 {
		t.Fatalf("unexpected rotScript output: %+v err=%v", metaRot, err)
	}

	// Empty array or invalid JSON falls back to native parser.
	emptyJSON := writeExecScript(t, bin, "exif-empty", `printf '[]'`)
	metaFallback, err := media.NewEXIFExtractor(root).WithExifToolPath(emptyJSON).Extract(t.Context(), "tiny.avif", domain.FormatAVIF)
	if err != nil || metaFallback.Width != 640 || metaFallback.Height != 480 {
		t.Fatalf("expected AVIF fallback 640x480, got %+v err=%v", metaFallback, err)
	}
	badJSON := writeExecScript(t, bin, "exif-bad", `printf 'not-json'`)
	metaBadJSON, err := media.NewEXIFExtractor(root).WithExifToolPath(badJSON).Extract(t.Context(), "no_dims.jpg", domain.FormatJPG)
	if err != nil || metaBadJSON.Width != 44 {
		t.Fatalf("expected invalid json fallback to 44x22, got %+v err=%v", metaBadJSON, err)
	}

	// Rotate 180 + ExposureTime decimal without integer reciprocal + bad duration string + non-numeric FocalLength.
	miscScript := writeExecScript(t, bin, "exif-misc", `
printf '%s\n' '[{"Orientation": "Rotate 180", "ExposureTime": 0.3, "FocalLength": "unknown", "Duration": "bad:dur:x", "DateTimeOriginal": "0000:00:00 00:00:00"}]'
`)
	metaMisc, err := media.NewEXIFExtractor(root).WithExifToolPath(miscScript).Extract(t.Context(), "no_dims.jpg", domain.FormatJPG)
	if err != nil || metaMisc.EXIF.Orientation != 3 || metaMisc.EXIF.ExposureTime != "0.3" || metaMisc.TakenAt != nil {
		t.Fatalf("unexpected misc script result: %+v err=%v", metaMisc, err)
	}

	// Corrupt PNG returns decode error.
	writeTestFile(t, root, "bad.png", []byte("not-a-png"))
	if _, err := media.NewEXIFExtractor(root).WithExifToolPath("").Extract(t.Context(), "bad.png", domain.FormatPNG); err == nil {
		t.Fatal("expected error for corrupt png")
	}

	webmMeta, err := media.NewEXIFExtractor(root).WithExifToolPath("").Extract(t.Context(), "clip.webm", domain.FormatWebM)
	if err != nil || webmMeta.EXIF.Orientation != 1 {
		t.Fatalf("unexpected webm fallback: %+v err=%v", webmMeta, err)
	}
}

func TestEXIFExtractorNativeWebPVariantsAndTIFFEdgeCases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// WebP lossy "VP8 " + EXIF chunk.
	tiff := buildTIFFFixture(tiffFixtureSpec{
		littleEndian: false,
		orientation:  3,
		makeName:     "WebPMaker",
		modelName:    "W1",
		iso:          320,
		fnumNum:      56,
		fnumDen:      10,
		expNum:       5,
		expDen:       2,
		focalNum:     24,
		focalDen:     1,
		width:        80,
		height:       60,
		dateTimeOrig: "2026-09-10 08:09:10",
	})
	var webpLossy bytes.Buffer
	webpLossy.WriteString("RIFF\x00\x00\x00\x00WEBP")
	vp8Body := []byte{0x00, 0x00, 0x00, 0x9D, 0x01, 0x2A, 0x50, 0x00, 0x3C, 0x00} // 80x60
	webpLossy.WriteString("VP8 ")
	_ = binary.Write(&webpLossy, binary.LittleEndian, uint32(len(vp8Body)))
	webpLossy.Write(vp8Body)
	webpLossy.WriteString("EXIF")
	_ = binary.Write(&webpLossy, binary.LittleEndian, uint32(len(tiff)))
	webpLossy.Write(tiff)
	if len(tiff)%2 == 1 {
		webpLossy.WriteByte(0)
	}
	writeTestFile(t, root, "lossy.webp", webpLossy.Bytes())

	// WebP lossless "VP8L": bits = (W-1) | ((H-1) << 14) -> 120x90 => 119 | (89 << 14)
	bits := uint32(119) | (uint32(89) << 14)
	vp8lBody := make([]byte, 5)
	vp8lBody[0] = 0x2F
	binary.LittleEndian.PutUint32(vp8lBody[1:5], bits)
	var webpLossless bytes.Buffer
	webpLossless.WriteString("RIFF\x00\x00\x00\x00WEBP")
	webpLossless.WriteString("VP8L")
	_ = binary.Write(&webpLossless, binary.LittleEndian, uint32(len(vp8lBody)))
	webpLossless.Write(vp8lBody)
	webpLossless.WriteByte(0)
	writeTestFile(t, root, "lossless.webp", webpLossless.Bytes())

	// TIFF IFD0 fallback ImageWidth, ImageLength, DateTime (0x0132), ExifIFD DateTimeDigitized (0x9004), BYTE/SLONG.
	b := newTIFFBuilder(binary.LittleEndian, "II")
	ifdTIFF := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		b.longEntry(0x0100, 128),
		b.longEntry(0x0101, 96),
		b.asciiEntry(0x0132, "2026:11:12 13:14:15"),
		{tag: 0x0112, fieldType: 1, count: 1, data: []byte{4}},
	})
	writeTestFile(t, root, "ifd0.dng", ifdTIFF)

	// CR3 with NO CMT boxes, only embedded JPEG with EXIF -> tests fillFromEmbeddedJPEG + mergeMissingMeta!
	jpgWithEXIF := injectJPEGAPP1(encodeTestJPEG(t, 48, 36), tiff)
	cr3Fallback := append(makeISOBox("ftyp", []byte("crx \x00\x00\x00\x01crx ")), makeISOBox("mdat", jpgWithEXIF)...)
	writeTestFile(t, root, "fallback.cr3", cr3Fallback)

	// HEIC with extended 64-bit box, iloc v2, infe v3, direct file offset Exif, and size==0 box!
	heicV2 := buildHEICV2FileOffsetFixture(2048, 1536, tiff)
	writeTestFile(t, root, "v2.heic", heicV2)

	ext := media.NewEXIFExtractor(root).WithExifToolPath("")

	lossyMeta, err := ext.Extract(t.Context(), "lossy.webp", domain.FormatWebP)
	if err != nil || lossyMeta.Width != 80 || lossyMeta.Height != 60 || lossyMeta.EXIF.ExposureTime != "2.5" {
		t.Fatalf("unexpected lossy webp: %+v err=%v", lossyMeta, err)
	}
	losslessMeta, err := ext.Extract(t.Context(), "lossless.webp", domain.FormatWebP)
	if err != nil || losslessMeta.Width != 120 || losslessMeta.Height != 90 {
		t.Fatalf("unexpected lossless webp: %+v err=%v", losslessMeta, err)
	}
	dngMeta, err := ext.Extract(t.Context(), "ifd0.dng", domain.FormatDNG)
	if err != nil || dngMeta.Width != 128 || dngMeta.Height != 96 || dngMeta.EXIF.Orientation != 4 || dngMeta.TakenAt == nil {
		t.Fatalf("unexpected ifd0 dng: %+v err=%v", dngMeta, err)
	}
	cr3Meta, err := ext.Extract(t.Context(), "fallback.cr3", domain.FormatCR3)
	if err != nil || cr3Meta.Width != 80 || cr3Meta.Height != 60 || cr3Meta.EXIF.CameraMake != "WebPMaker" {
		t.Fatalf("unexpected cr3 embedded fallback: %+v err=%v", cr3Meta, err)
	}
	if cr3Meta.EXIF.FNumber == nil || math.Abs(*cr3Meta.EXIF.FNumber-5.6) > 0.01 {
		t.Fatalf("expected FNumber 5.6, got %+v", cr3Meta.EXIF.FNumber)
	}
	v2Meta, err := ext.Extract(t.Context(), "v2.heic", domain.FormatHEIC)
	if err != nil || v2Meta.Width != 2048 || v2Meta.Height != 1536 || v2Meta.EXIF.CameraMake != "WebPMaker" {
		t.Fatalf("unexpected v2 heic: %+v err=%v", v2Meta, err)
	}

	// ISO-BMFF size==0 box-to-EOF + direct Exif box + truncated/malformed boxes & TIFF edge cases.
	directExif := append([]byte{0, 0, 0, 0}, makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic"))...)
	directExif = directExif[4:] // clean ftyp
	exifBoxPayload := append([]byte("Exif\x00\x00"), tiff...)
	metaWithExifBox := makeFullBox("meta", 0, 0, makeISOBox("Exif", exifBoxPayload))
	// Append size==0 box header for mdat extending to EOF.
	eofMdat := append([]byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}, encodeTestJPEG(t, 64, 48)...)
	writeTestFile(t, root, "direct.heic", append(append(directExif, metaWithExifBox...), eofMdat...))
	directMeta, err := ext.Extract(t.Context(), "direct.heic", domain.FormatHEIC)
	if err != nil || directMeta.EXIF.CameraMake != "WebPMaker" || directMeta.Width != 80 || directMeta.Height != 60 {
		t.Fatalf("unexpected direct Exif heic: %+v err=%v", directMeta, err)
	}

	// RAW (.nef) with no TIFF dims, embedded JPEG supplies dims + DateTimeDigitized (0x9004).
	digitizedTIFF := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		b.longEntry(0x8769, 64),
	})
	// Place ExifIFD at offset 64 with DateTimeDigitized (0x9004), zero-den rational, SLONG, etc.
	digitizedExif := newTIFFBuilder(binary.LittleEndian, "II")
	digitizedExif.buf = append([]byte{}, digitizedTIFF...)
	for len(digitizedExif.buf) < 64 {
		digitizedExif.buf = append(digitizedExif.buf, 0)
	}
	digitizedExif.writeIFDBlock(64, []rawIFDEntry{
		digitizedExif.asciiEntry(0x9004, "2026:10:15 07:08:09"),
		digitizedExif.rationalEntry(0x829A, [][2]uint32{{1, 0}}), // zero den ignored
		{tag: 0xA002, fieldType: 9, count: 1, data: []byte{77, 0, 0, 0}},
		{tag: 0xA003, fieldType: 9, count: 1, data: []byte{55, 0, 0, 0}},
	})
	nefBytes := append(digitizedExif.buf, encodeTestJPEG(t, 77, 55)...)
	writeTestFile(t, root, "shot.nef", nefBytes)
	nefMeta, err := ext.Extract(t.Context(), "shot.nef", domain.FormatNEF)
	if err != nil || nefMeta.Width != 77 || nefMeta.Height != 55 || nefMeta.TakenAt == nil {
		t.Fatalf("unexpected nef metadata: %+v err=%v", nefMeta, err)
	}

	// Corrupt/truncated JPEG falls back cleanly without panic; truncated WebP stops cleanly.
	writeTestFile(t, root, "trunc.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10, 'E', 'x'})
	if _, err := ext.Extract(t.Context(), "trunc.jpg", domain.FormatJPG); err != nil {
		t.Fatalf("truncated jpeg should not fail: %v", err)
	}
	writeTestFile(t, root, "trunc.webp", []byte("RIFF\x10\x00\x00\x00WEBPBAD!"))
	if _, err := ext.Extract(t.Context(), "trunc.webp", domain.FormatWebP); err != nil {
		t.Fatalf("truncated webp should not fail: %v", err)
	}

	// Additional edge cases: JPEG with padding/RST markers, bad embedded JPEG before real one,
	// ExposureTime integer >= 1 (2/1 -> "2"), invalid date, all TIFF field sizes, and truncated ISO boxes.
	edgeTIFF := buildSingleIFDTIFF(binary.BigEndian, "MM", []rawIFDEntry{
		{tag: 0x010F, fieldType: 2, count: 4, data: []byte("OK\x00\x00")},
		{tag: 0x0112, fieldType: 3, count: 1, data: []byte{0, 1}},
		{tag: 0x8769, fieldType: 4, count: 1, data: []byte{0, 0, 0, 70}},
	})
	eb := newTIFFBuilder(binary.BigEndian, "MM")
	eb.buf = append([]byte{}, edgeTIFF...)
	for len(eb.buf) < 70 {
		eb.buf = append(eb.buf, 0)
	}
	eb.writeIFDBlock(70, []rawIFDEntry{
		eb.rationalEntry(0x829A, [][2]uint32{{2, 1}}),
		eb.asciiEntry(0x9003, "not-a-timestamp"),
		{tag: 0xA434, fieldType: 7, count: 4, data: []byte("L50\x00")},
		{tag: 0xFF01, fieldType: 12, count: 1, data: make([]byte, 8)},
		{tag: 0xFF02, fieldType: 99, count: 1, data: make([]byte, 4)},
	})
	realJPG := encodeTestJPEG(t, 28, 20)
	// Inject 0xFF padding + RST0 + SOS in synthetic JPEG bytes & bogus JPEG before realJPG in a CR3.
	syntheticJPEG := []byte{0xFF, 0xD8, 0xFF, 0xFF, 0xD0, 0xFF, 0xE1, 0x00, byte(8 + len(eb.buf))}
	syntheticJPEG = append(syntheticJPEG, []byte("Exif\x00\x00")...)
	syntheticJPEG = append(syntheticJPEG, eb.buf...)
	syntheticJPEG = append(syntheticJPEG, 0xFF, 0xDA, 0x00, 0x03, 0x00, 0xFF, 0x00, 0xFF, 0xD0, 0xFF, 0xD9)
	writeTestFile(t, root, "markers.jpg", syntheticJPEG)
	mMeta, err := ext.Extract(t.Context(), "markers.jpg", domain.FormatJPG)
	if err != nil || mMeta.EXIF.ExposureTime != "2" || mMeta.TakenAt != nil {
		t.Fatalf("unexpected markers.jpg metadata: %+v err=%v", mMeta, err)
	}

	bogusPrefix := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, 0xFF, 0xD9}
	cr3Mixed := append(makeISOBox("ftyp", []byte("crx \x00\x00\x00\x01crx ")),
		makeISOBox("mdat", append(bogusPrefix, realJPG...))...)
	writeTestFile(t, root, "mixed.cr3", cr3Mixed)
	mixedMeta, err := ext.Extract(t.Context(), "mixed.cr3", domain.FormatCR3)
	if err != nil || mixedMeta.Width != 28 || mixedMeta.Height != 20 {
		t.Fatalf("unexpected mixed.cr3 metadata: %+v err=%v", mixedMeta, err)
	}

	// ISO-BMFF with 4-byte Exif offset payload and malformed truncated iloc/iinf boxes.
	offsetExif := append([]byte{0, 0, 0, 2, 0xAA, 0xBB}, eb.buf...)
	badMeta := makeFullBox("meta", 0, 0, append(
		makeISOBox("Exif", offsetExif),
		append(makeFullBox("iinf", 0, 0, []byte{0, 1}), makeFullBox("iloc", 0, 0, []byte{0})...)...,
	))
	writeTestFile(t, root, "odd.heic", append(makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), badMeta...))
	if _, err := ext.Extract(t.Context(), "odd.heic", domain.FormatHEIC); err != nil {
		t.Fatalf("odd.heic should not fail: %v", err)
	}
}

func TestEXIFMalformedAndBoundaryInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bin := t.TempDir()
	ext := media.NewEXIFExtractor(root).WithExifToolPath("")

	// ExifTool JSON with null/bool/empty/zero values exercises rawString, rawFloat, parseExifToolExposure.
	nullScript := writeExecScript(t, bin, "exif-nulls", `
printf '%s\n' '[{
  "Make": null,
  "Model": true,
  "Orientation": "Unknown",
  "ISO": "",
  "FNumber": null,
  "ExposureTime": null,
  "FocalLength": " ",
  "GPSLatitude": 999,
  "GPSLongitude": -999
}]'`)
	writeTestFile(t, root, "plain.jpg", encodeTestJPEG(t, 20, 10))
	if meta, err := media.NewEXIFExtractor(root).WithExifToolPath(nullScript).Extract(t.Context(), "plain.jpg", domain.FormatJPG); err != nil || meta.Width != 20 {
		t.Fatalf("null json extract failed: %+v err=%v", meta, err)
	}

	// MP4 with audio tkhd (0x0), tkhd v1 (1080x720), mvhd v2 (invalid), short mvhd, truncated 64-bit box, and size=4 box.
	var tkhdV1 bytes.Buffer
	tkhdV1.Write(make([]byte, 84))
	_ = binary.Write(&tkhdV1, binary.BigEndian, uint32(1080)<<16)
	_ = binary.Write(&tkhdV1, binary.BigEndian, uint32(720)<<16)
	audioTkhd := makeFullBox("tkhd", 0, 1, make([]byte, 80))
	badTkhd := makeFullBox("tkhd", 2, 0, make([]byte, 90))
	moov := makeISOBox("moov", append(
		append(makeFullBox("mvhd", 2, 0, make([]byte, 32)), makeFullBox("mvhd", 0, 0, make([]byte, 10))...),
		append(makeISOBox("trak", audioTkhd), append(makeISOBox("trak", badTkhd), makeISOBox("trak", makeFullBox("tkhd", 1, 3, tkhdV1.Bytes()))...)...)...,
	))
	badSizeBox := []byte{0, 0, 0, 4, 'f', 'r', 'e', 'e'}
	trunc64Box := []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0, 0}
	mp4Edge := append(append(makeISOBox("ftyp", []byte("isom\x00\x00\x00\x00isom")), moov...), append(badSizeBox, trunc64Box...)...)
	writeTestFile(t, root, "edge.mp4", mp4Edge)
	mp4Meta, err := ext.Extract(t.Context(), "edge.mp4", domain.FormatMP4)
	if err != nil || mp4Meta.Width != 1080 || mp4Meta.Height != 720 {
		t.Fatalf("unexpected edge.mp4: %+v err=%v", mp4Meta, err)
	}

	// HEIC with infe v1, short Exif box, garbage Exif payload, iloc v0, and malformed JPEG stream.
	badInfe := makeFullBox("infe", 1, 0, make([]byte, 12))
	var iinfBad bytes.Buffer
	_ = binary.Write(&iinfBad, binary.BigEndian, uint16(1))
	iinfBad.Write(badInfe)
	ilocV0 := makeFullBox("iloc", 0, 0, []byte{0x44, 0x40, 0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 4})
	shortExifBox := makeISOBox("Exif", []byte{1, 2, 3})
	badExifBox := makeISOBox("Exif", []byte("garbage-exif-data"))
	metaEdge := makeFullBox("meta", 0, 0, append(
		append(makeFullBox("iinf", 0, 0, iinfBad.Bytes()), ilocV0...),
		append(shortExifBox, badExifBox...)...,
	))
	badEmbedded := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00,
		0xFF, 0xDA, 0x00, 0x03, 0x00, 0xFF, 0xAA, 0x00,
	}
	heicEdge := append(append(makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), metaEdge...), makeISOBox("mdat", badEmbedded)...)
	writeTestFile(t, root, "edge.heic", heicEdge)
	if _, err := ext.Extract(t.Context(), "edge.heic", domain.FormatHEIC); err != nil {
		t.Fatalf("edge.heic failed: %v", err)
	}

	// CR3 with bad CMT2/CMT4 magic + bad cyclic/out-of-bounds TIFF in .dng + GPS with bad rational count.
	badMagicCR3 := makeISOBox("moov", makeISOBox("uuid", append(make([]byte, 16),
		append(makeISOBox("CMT2", []byte("XX000000")), makeISOBox("CMT4", []byte("II\x63\x00\x08\x00\x00\x00"))...)...)))
	writeTestFile(t, root, "badcmt.cr3", append(makeISOBox("ftyp", []byte("crx \x00\x00\x00\x00crx ")), badMagicCR3...))
	if _, err := ext.Extract(t.Context(), "badcmt.cr3", domain.FormatCR3); err != nil {
		t.Fatalf("badcmt.cr3 failed: %v", err)
	}

	// TIFF with cyclic ExifIFD back to 8, out-of-bounds ASCII, wrong fieldTypes for uintAt/rationalAt, bad GPS count.
	tb := newTIFFBuilder(binary.LittleEndian, "II")
	cyclicTIFF := buildSingleIFDTIFF(binary.LittleEndian, "II", []rawIFDEntry{
		{tag: 0x010F, fieldType: 2, count: 10, data: []byte{0xFF, 0xFF, 0, 0}}, // OOB ASCII
		{tag: 0x0112, fieldType: 5, count: 1, data: []byte{0, 0, 0, 0}},        // wrong type for uintAt
		{tag: 0x8769, fieldType: 4, count: 1, data: []byte{8, 0, 0, 0}},        // cyclic back to IFD0
		{tag: 0x8825, fieldType: 4, count: 1, data: []byte{120, 0, 0, 0}},
	})
	tb.buf = append([]byte{}, cyclicTIFF...)
	for len(tb.buf) < 120 {
		tb.buf = append(tb.buf, 0)
	}
	tb.writeIFDBlock(120, []rawIFDEntry{
		{tag: 0x0002, fieldType: 4, count: 1, data: []byte{1, 0, 0, 0}}, // GPS count < 3 & wrong type
	})
	writeTestFile(t, root, "cyclic.dng", tb.buf)
	if _, err := ext.Extract(t.Context(), "cyclic.dng", domain.FormatDNG); err != nil {
		t.Fatalf("cyclic.dng failed: %v", err)
	}

	// Non-WEBP RIFF returns empty metadata cleanly.
	writeTestFile(t, root, "notwebp.webp", []byte("NOTRIFF123456"))
	if _, err := ext.Extract(t.Context(), "notwebp.webp", domain.FormatWebP); err != nil {
		t.Fatalf("notwebp.webp failed: %v", err)
	}

	// Malformed HEIC iloc with uint64-wrapping offset in idat (method=1) and huge file offset (method=0) must not panic.
	for _, tc := range []struct {
		name   string
		method uint16
	}{
		{name: "overflow_idat.heic", method: 1},
		{name: "overflow_file.heic", method: 0},
	} {
		infe := makeFullBox("infe", 2, 0, append([]byte{0x00, 0x07, 0x00, 0x00}, []byte("Exif\x00")...))
		var iinfBuf bytes.Buffer
		_ = binary.Write(&iinfBuf, binary.BigEndian, uint16(1))
		iinfBuf.Write(infe)
		var ilocBuf bytes.Buffer
		ilocBuf.WriteByte(0x84) // offset_size=8, length_size=4
		ilocBuf.WriteByte(0x00) // base_offset_size=0, index_size=0
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint16(1))
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint16(7))
		_ = binary.Write(&ilocBuf, binary.BigEndian, tc.method)
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint16(0))
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint16(1))
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint64(math.MaxUint64-7))
		_ = binary.Write(&ilocBuf, binary.BigEndian, uint32(16))
		overflowMeta := makeFullBox("meta", 0, 0, append(
			append(makeFullBox("iinf", 0, 0, iinfBuf.Bytes()), makeFullBox("iloc", 1, 0, ilocBuf.Bytes())...),
			makeISOBox("idat", make([]byte, 32))...,
		))
		writeTestFile(t, root, tc.name, append(makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic")), overflowMeta...))
		if _, err := ext.Extract(t.Context(), tc.name, domain.FormatHEIC); err != nil {
			t.Fatalf("%s should not fail or panic: %v", tc.name, err)
		}
	}
}

func TestEXIFAndPreviewLeadingDashPath(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	bin := t.TempDir()
	cache := t.TempDir()
	sampleJPG := filepath.Join(bin, "sample.jpg")
	if err := os.WriteFile(sampleJPG, encodeTestJPEG(t, 40, 30), 0o644); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, tmp, "-root.jpg", encodeTestJPEG(t, 40, 30))
	writeTestFile(t, tmp, filepath.Join("-lib", "shot.cr3"), []byte("raw"))

	checkExifArg := writeExecScript(t, bin, "exif-dash-check", `
last=""
for a in "$@"; do last="$a"; done
case "$last" in
  ./-*) printf '[{"ImageWidth":40,"ImageHeight":30}]' ;;
  *) echo "unexpected dash arg: $last" >&2; exit 2 ;;
esac`)
	checkRawArg := writeExecScript(t, bin, "raw-dash-check", `
last=""
for a in "$@"; do last="$a"; done
case "$last" in
  ./-*) cat `+sampleJPG+` ;;
  *) echo "unexpected dash arg: $last" >&2; exit 2 ;;
esac`)

	meta, err := media.NewEXIFExtractor(".").WithExifToolPath(checkExifArg).Extract(t.Context(), "-root.jpg", domain.FormatJPG)
	if err != nil || meta.Width != 40 || meta.Height != 30 {
		t.Fatalf("Extract ./-root.jpg = (%+v, %v)", meta, err)
	}
	metaDir, err := media.NewEXIFExtractor("-lib").WithExifToolPath(checkExifArg).Extract(t.Context(), "shot.cr3", domain.FormatCR3)
	if err != nil || metaDir.Width != 40 {
		t.Fatalf("Extract -lib/shot.cr3 = (%+v, %v)", metaDir, err)
	}
	if err := media.NewPreviewExtractor("-lib", cache, 1).WithTools(checkRawArg, "", "").EnsureCache(t.Context(), "shot.cr3", "dashhash", domain.FormatCR3, 1); err != nil {
		t.Fatalf("EnsureCache -lib/shot.cr3 failed: %v", err)
	}
	if entries, err := os.ReadDir(cache); err != nil || len(entries) != 2 || !strings.HasSuffix(entries[0].Name(), ".jpg") {
		t.Fatalf("expected 2 cached JPEGs, got %v (err=%v)", entries, err)
	}
}

func buildHEICV2FileOffsetFixture(w, h uint32, tiffBytes []byte) []byte {
	ftyp := makeISOBox("ftyp", []byte("heic\x00\x00\x00\x00heic"))

	var ispeBody bytes.Buffer
	_ = binary.Write(&ispeBody, binary.BigEndian, w)
	_ = binary.Write(&ispeBody, binary.BigEndian, h)
	ipco := makeISOBox("ipco", makeFullBox("ispe", 0, 0, ispeBody.Bytes()))
	iprp := makeISOBox("iprp", ipco)

	// infe version 3 with 4-byte itemID = 7
	var infeBody bytes.Buffer
	_ = binary.Write(&infeBody, binary.BigEndian, uint32(7))
	_ = binary.Write(&infeBody, binary.BigEndian, uint16(0))
	infeBody.WriteString("Exif\x00")
	infe := makeFullBox("infe", 3, 0, infeBody.Bytes())
	var iinfBody bytes.Buffer
	_ = binary.Write(&iinfBody, binary.BigEndian, uint32(1))
	iinfBody.Write(infe)
	iinf := makeFullBox("iinf", 1, 0, iinfBody.Bytes())

	// Build placeholder meta first to calculate exact file offset to mdat payload.
	buildMeta := func(exifOffset uint32) []byte {
		var ilocBody bytes.Buffer
		ilocBody.WriteByte(0x44) // offset=4, len=4
		ilocBody.WriteByte(0x00) // base=0, index=0
		_ = binary.Write(&ilocBody, binary.BigEndian, uint32(1))
		_ = binary.Write(&ilocBody, binary.BigEndian, uint32(7)) // itemID=7
		_ = binary.Write(&ilocBody, binary.BigEndian, uint16(0)) // method=0 (file)
		_ = binary.Write(&ilocBody, binary.BigEndian, uint16(0)) // data_ref
		_ = binary.Write(&ilocBody, binary.BigEndian, uint16(1)) // extentCount
		_ = binary.Write(&ilocBody, binary.BigEndian, exifOffset)
		_ = binary.Write(&ilocBody, binary.BigEndian, uint32(len(tiffBytes)))
		iloc := makeFullBox("iloc", 2, 0, ilocBody.Bytes())
		var m bytes.Buffer
		m.Write(iprp)
		m.Write(iinf)
		m.Write(iloc)
		return makeFullBox("meta", 0, 0, m.Bytes())
	}

	probeMeta := buildMeta(0)
	// Extended 64-bit size box header = 16 bytes for mdat
	mdatOffset := uint32(len(ftyp) + len(probeMeta) + 16)
	meta := buildMeta(mdatOffset)

	var mdat bytes.Buffer
	_ = binary.Write(&mdat, binary.BigEndian, uint32(1)) // extended size
	mdat.WriteString("mdat")
	_ = binary.Write(&mdat, binary.BigEndian, uint64(16+len(tiffBytes)))
	mdat.Write(tiffBytes)

	return append(append(ftyp, meta...), mdat.Bytes()...)
}
