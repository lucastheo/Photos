package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const (
	maxMetadataScanBytes = 32 * 1024 * 1024
	maxEmbeddedScanBytes = 64 * 1024 * 1024
	maxIFDEntries        = 512
	maxIFDDepth          = 8
)

const (
	tiffTagImageWidth    = 0x0100
	tiffTagImageLength   = 0x0101
	tiffTagMake          = 0x010F
	tiffTagModel         = 0x0110
	tiffTagOrientation   = 0x0112
	tiffTagDateTime      = 0x0132
	tiffTagExifIFD       = 0x8769
	tiffTagGPSIFD        = 0x8825
	exifTagExposureTime  = 0x829A
	exifTagFNumber       = 0x829D
	exifTagISO           = 0x8827
	exifTagDateOriginal  = 0x9003
	exifTagDateDigitized = 0x9004
	exifTagFocalLength   = 0x920A
	exifTagPixelXDim     = 0xA002
	exifTagPixelYDim     = 0xA003
	exifTagLensModel     = 0xA434
	gpsTagLatitudeRef    = 0x0001
	gpsTagLatitude       = 0x0002
	gpsTagLongitudeRef   = 0x0003
	gpsTagLongitude      = 0x0004
)

type tiffParser struct {
	data         []byte
	order        binary.ByteOrder
	visited      map[uint32]bool
	dateFallback string
}

type ifdEntry struct {
	tag       uint16
	fieldType uint16
	count     uint32
	rawVal    []byte
}

func extractJPEGMetadata(f *os.File) (domain.MediaMetadata, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxMetadataScanBytes))
	if err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("read jpeg: %w", err)
	}
	var meta domain.MediaMetadata
	parseJPEGSegments(data, &meta)
	if meta.Width == 0 || meta.Height == 0 {
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr == nil {
			if cfg, _, cfgErr := image.DecodeConfig(f); cfgErr == nil {
				meta.Width = cfg.Width
				meta.Height = cfg.Height
			}
		}
	}
	return meta, nil
}

func parseJPEGSegments(data []byte, meta *domain.MediaMetadata) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return
	}
	pos := 2
	for pos+1 < len(data) {
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			return
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 || marker == 0xDA {
			return
		}
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if pos+2 > len(data) {
			return
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return
		}
		segBody := data[pos+2 : pos+segLen]
		handleJPEGMarker(marker, segBody, meta)
		pos += segLen
	}
}

func handleJPEGMarker(marker byte, body []byte, meta *domain.MediaMetadata) {
	if marker == 0xE1 && len(body) > 6 && bytes.Equal(body[:6], []byte("Exif\x00\x00")) {
		parseTIFFBlock(body[6:], meta)
		return
	}
	if isJPEGSOFMarker(marker) && len(body) >= 5 && meta.Width == 0 && meta.Height == 0 {
		h := int(binary.BigEndian.Uint16(body[1:3]))
		w := int(binary.BigEndian.Uint16(body[3:5]))
		if w > 0 && h > 0 {
			meta.Width = w
			meta.Height = h
		}
	}
}

func isJPEGSOFMarker(marker byte) bool {
	switch marker {
	case 0xC0, 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7, 0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF:
		return true
	default:
		return false
	}
}

func extractRAWMetadata(f *os.File) (domain.MediaMetadata, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxMetadataScanBytes))
	if err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("read raw file: %w", err)
	}
	var meta domain.MediaMetadata
	parseTIFFBlock(data, &meta)
	if jpg, ok := findLargestEmbeddedJPEG(data); ok {
		var jpgMeta domain.MediaMetadata
		parseJPEGSegments(jpg, &jpgMeta)
		mergeMissingMeta(&meta, jpgMeta)
	}
	return meta, nil
}

func extractWebPMetadata(f *os.File) (domain.MediaMetadata, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxMetadataScanBytes))
	if err != nil {
		return domain.MediaMetadata{}, fmt.Errorf("read webp: %w", err)
	}
	var meta domain.MediaMetadata
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return meta, nil
	}
	pos := 12
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkLen := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		bodyStart := pos + 8
		if chunkLen < 0 || bodyStart+chunkLen > len(data) {
			break
		}
		parseWebPChunk(chunkID, data[bodyStart:bodyStart+chunkLen], &meta)
		pos = bodyStart + chunkLen + (chunkLen & 1)
	}
	return meta, nil
}

func parseWebPChunk(chunkID string, body []byte, meta *domain.MediaMetadata) {
	switch chunkID {
	case "VP8X":
		if len(body) >= 10 && meta.Width == 0 {
			meta.Width = int(readUint24LE(body[4:7])) + 1
			meta.Height = int(readUint24LE(body[7:10])) + 1
		}
	case "VP8 ":
		if len(body) >= 10 && body[3] == 0x9D && body[4] == 0x01 && body[5] == 0x2A && meta.Width == 0 {
			meta.Width = int(binary.LittleEndian.Uint16(body[6:8]) & 0x3FFF)
			meta.Height = int(binary.LittleEndian.Uint16(body[8:10]) & 0x3FFF)
		}
	case "VP8L":
		if len(body) >= 5 && body[0] == 0x2F && meta.Width == 0 {
			bits := binary.LittleEndian.Uint32(body[1:5])
			meta.Width = int(bits&0x3FFF) + 1
			meta.Height = int((bits>>14)&0x3FFF) + 1
		}
	case "EXIF":
		payload := bytes.TrimPrefix(body, []byte("Exif\x00\x00"))
		parseTIFFBlock(payload, meta)
	}
}

func parsePNGExifChunk(data []byte, meta *domain.MediaMetadata) {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return
	}
	pos := 8
	for pos+12 <= len(data) {
		chunkLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		chunkType := string(data[pos+4 : pos+8])
		if chunkLen < 0 || pos+12+chunkLen > len(data) {
			return
		}
		if chunkType == "eXIf" {
			parseTIFFBlock(data[pos+8:pos+8+chunkLen], meta)
			return
		}
		pos += 12 + chunkLen
	}
}

func parseTIFFBlock(data []byte, meta *domain.MediaMetadata) {
	p, firstIFD, ok := newTIFFParser(data)
	if !ok {
		return
	}
	p.parseIFD0(firstIFD, meta, 0)
	if meta.TakenAt == nil && p.dateFallback != "" {
		meta.TakenAt = parseEXIFTimestamp(p.dateFallback)
	}
}

func parseTIFFAsExifIFD(data []byte, meta *domain.MediaMetadata) {
	p, firstIFD, ok := newTIFFParser(data)
	if !ok {
		return
	}
	p.parseExifIFD(firstIFD, meta)
}

func parseTIFFAsGPSIFD(data []byte, meta *domain.MediaMetadata) {
	p, firstIFD, ok := newTIFFParser(data)
	if !ok {
		return
	}
	p.parseGPSIFD(firstIFD, meta)
}

func newTIFFParser(data []byte) (*tiffParser, uint32, bool) {
	if len(data) < 8 {
		return nil, 0, false
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, 0, false
	}
	magic := order.Uint16(data[2:4])
	if magic != 42 && magic != 0x4F52 {
		return nil, 0, false
	}
	firstIFD := order.Uint32(data[4:8])
	return &tiffParser{
		data:    data,
		order:   order,
		visited: make(map[uint32]bool),
	}, firstIFD, true
}

func (p *tiffParser) readIFD(offset uint32) ([]ifdEntry, bool) {
	if p.visited[offset] || int(offset)+2 > len(p.data) {
		return nil, false
	}
	p.visited[offset] = true
	count := int(p.order.Uint16(p.data[offset : offset+2]))
	if count <= 0 || count > maxIFDEntries {
		return nil, false
	}
	tableStart := int(offset) + 2
	if tableStart+count*12 > len(p.data) {
		return nil, false
	}
	entries := make([]ifdEntry, 0, count)
	for i := range count {
		rec := p.data[tableStart+i*12 : tableStart+(i+1)*12]
		entries = append(entries, ifdEntry{
			tag:       p.order.Uint16(rec[0:2]),
			fieldType: p.order.Uint16(rec[2:4]),
			count:     p.order.Uint32(rec[4:8]),
			rawVal:    rec[8:12],
		})
	}
	return entries, true
}

func (p *tiffParser) parseIFD0(offset uint32, meta *domain.MediaMetadata, depth int) {
	if depth >= maxIFDDepth {
		return
	}
	entries, ok := p.readIFD(offset)
	if !ok {
		return
	}
	for _, e := range entries {
		p.applyIFD0Entry(e, meta, depth)
	}
}

func (p *tiffParser) applyIFD0Entry(e ifdEntry, meta *domain.MediaMetadata, depth int) {
	switch e.tag {
	case tiffTagImageWidth:
		if v, ok := p.uintAt(e, 0); ok && meta.Width == 0 {
			meta.Width = int(v)
		}
	case tiffTagImageLength:
		if v, ok := p.uintAt(e, 0); ok && meta.Height == 0 {
			meta.Height = int(v)
		}
	case tiffTagMake:
		if meta.EXIF.CameraMake == "" {
			meta.EXIF.CameraMake = p.asciiValue(e)
		}
	case tiffTagModel:
		if meta.EXIF.CameraModel == "" {
			meta.EXIF.CameraModel = p.asciiValue(e)
		}
	case tiffTagOrientation:
		if v, ok := p.uintAt(e, 0); ok && v >= 1 && v <= 8 {
			meta.EXIF.Orientation = int(v)
		}
	case tiffTagDateTime:
		if p.dateFallback == "" {
			p.dateFallback = p.asciiValue(e)
		}
	case tiffTagExifIFD:
		if ptr, ok := p.uintAt(e, 0); ok && depth+1 < maxIFDDepth {
			p.parseExifIFD(ptr, meta)
		}
	case tiffTagGPSIFD:
		if ptr, ok := p.uintAt(e, 0); ok && depth+1 < maxIFDDepth {
			p.parseGPSIFD(ptr, meta)
		}
	}
}

func (p *tiffParser) parseExifIFD(offset uint32, meta *domain.MediaMetadata) {
	entries, ok := p.readIFD(offset)
	if !ok {
		return
	}
	for _, e := range entries {
		p.applyExifEntry(e, meta)
	}
}

func (p *tiffParser) applyExifEntry(e ifdEntry, meta *domain.MediaMetadata) {
	switch e.tag {
	case exifTagExposureTime:
		if num, den, ok := p.rationalAt(e, 0); ok && den > 0 && num > 0 {
			meta.EXIF.ExposureTime = formatRationalExposure(num, den)
		}
	case exifTagFNumber:
		if f, ok := p.rationalFloatAt(e, 0); ok && f > 0 {
			meta.EXIF.FNumber = &f
		}
	case exifTagISO:
		if v, ok := p.uintAt(e, 0); ok && v > 0 {
			iso := int(v)
			meta.EXIF.ISO = &iso
		}
	case exifTagDateOriginal:
		if ts := parseEXIFTimestamp(p.asciiValue(e)); ts != nil {
			meta.TakenAt = ts
		}
	case exifTagDateDigitized:
		if meta.TakenAt == nil {
			meta.TakenAt = parseEXIFTimestamp(p.asciiValue(e))
		}
	case exifTagFocalLength:
		if f, ok := p.rationalFloatAt(e, 0); ok && f > 0 {
			meta.EXIF.FocalLengthMM = &f
		}
	case exifTagPixelXDim:
		if v, ok := p.uintAt(e, 0); ok && v > 0 {
			meta.Width = int(v)
		}
	case exifTagPixelYDim:
		if v, ok := p.uintAt(e, 0); ok && v > 0 {
			meta.Height = int(v)
		}
	case exifTagLensModel:
		meta.EXIF.LensModel = p.asciiValue(e)
	}
}

func (p *tiffParser) parseGPSIFD(offset uint32, meta *domain.MediaMetadata) {
	entries, ok := p.readIFD(offset)
	if !ok {
		return
	}
	var latRef, lngRef string
	var latVals, lngVals [3]float64
	var hasLat, hasLng bool

	for _, e := range entries {
		switch e.tag {
		case gpsTagLatitudeRef:
			latRef = p.asciiValue(e)
		case gpsTagLatitude:
			latVals, hasLat = p.dmsRationals(e)
		case gpsTagLongitudeRef:
			lngRef = p.asciiValue(e)
		case gpsTagLongitude:
			lngVals, hasLng = p.dmsRationals(e)
		}
	}
	assignGPSCoordinates(meta, latRef, latVals, hasLat, lngRef, lngVals, hasLng)
}

func assignGPSCoordinates(
	meta *domain.MediaMetadata,
	latRef string,
	latVals [3]float64,
	hasLat bool,
	lngRef string,
	lngVals [3]float64,
	hasLng bool,
) {
	if hasLat {
		lat := dmsToDecimal(latVals)
		lat = applyCoordSign(lat, latRef, "S")
		if lat >= -90 && lat <= 90 {
			meta.EXIF.GPSLatitude = &lat
		}
	}
	if hasLng {
		lng := dmsToDecimal(lngVals)
		lng = applyCoordSign(lng, lngRef, "W")
		if lng >= -180 && lng <= 180 {
			meta.EXIF.GPSLongitude = &lng
		}
	}
}

func (p *tiffParser) dmsRationals(e ifdEntry) ([3]float64, bool) {
	var out [3]float64
	if e.count < 3 {
		return out, false
	}
	for i := range 3 {
		v, ok := p.rationalFloatAt(e, i)
		if !ok {
			return out, false
		}
		out[i] = v
	}
	return out, true
}

func dmsToDecimal(dms [3]float64) float64 {
	return dms[0] + dms[1]/60.0 + dms[2]/3600.0
}

func (p *tiffParser) entryBytes(e ifdEntry) ([]byte, bool) {
	unitSize := tiffTypeSize(e.fieldType)
	if unitSize == 0 || e.count == 0 {
		return nil, false
	}
	total64 := uint64(unitSize) * uint64(e.count)
	if total64 > uint64(len(p.data)) {
		return nil, false
	}
	total := int(total64)
	if total <= 4 {
		return e.rawVal[:total], true
	}
	off := int(p.order.Uint32(e.rawVal))
	if off < 0 || off+total > len(p.data) {
		return nil, false
	}
	return p.data[off : off+total], true
}

func tiffTypeSize(fieldType uint16) int {
	switch fieldType {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11:
		return 4
	case 5, 10, 12:
		return 8
	default:
		return 0
	}
}

func (p *tiffParser) asciiValue(e ifdEntry) string {
	raw, ok := p.entryBytes(e)
	if !ok {
		return ""
	}
	if idx := bytes.IndexByte(raw, 0); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(string(raw))
}

func (p *tiffParser) uintAt(e ifdEntry, index int) (uint32, bool) {
	raw, ok := p.entryBytes(e)
	if !ok {
		return 0, false
	}
	switch e.fieldType {
	case 1:
		if index >= len(raw) {
			return 0, false
		}
		return uint32(raw[index]), true
	case 3:
		off := index * 2
		if off+2 > len(raw) {
			return 0, false
		}
		return uint32(p.order.Uint16(raw[off : off+2])), true
	case 4, 9:
		off := index * 4
		if off+4 > len(raw) {
			return 0, false
		}
		return p.order.Uint32(raw[off : off+4]), true
	default:
		return 0, false
	}
}

func (p *tiffParser) rationalAt(e ifdEntry, index int) (uint32, uint32, bool) {
	raw, ok := p.entryBytes(e)
	if !ok || (e.fieldType != 5 && e.fieldType != 10) {
		return 0, 0, false
	}
	off := index * 8
	if off+8 > len(raw) {
		return 0, 0, false
	}
	num := p.order.Uint32(raw[off : off+4])
	den := p.order.Uint32(raw[off+4 : off+8])
	if den == 0 {
		return 0, 0, false
	}
	return num, den, true
}

func (p *tiffParser) rationalFloatAt(e ifdEntry, index int) (float64, bool) {
	num, den, ok := p.rationalAt(e, index)
	if !ok {
		return 0, false
	}
	val := float64(num) / float64(den)
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, false
	}
	return val, true
}

func formatRationalExposure(num, den uint32) string {
	g := gcdUint32(num, den)
	n, d := num/g, den/g
	if d == 1 {
		return fmt.Sprintf("%d", n)
	}
	if n < d {
		return fmt.Sprintf("%d/%d", n, d)
	}
	return formatExposureFloat(float64(n) / float64(d))
}

func gcdUint32(a, b uint32) uint32 {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func parseEXIFTimestamp(raw string) *time.Time {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "0000:00:00") {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006:01:02 15:04:05-07:00",
		"2006:01:02 15:04:05.000",
		"2006:01:02 15:04:05",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

func readUint24LE(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

func mergeMissingMeta(dst *domain.MediaMetadata, src domain.MediaMetadata) {
	if dst.Width == 0 {
		dst.Width = src.Width
	}
	if dst.Height == 0 {
		dst.Height = src.Height
	}
	if dst.TakenAt == nil {
		dst.TakenAt = src.TakenAt
	}
	if dst.EXIF.Orientation == 0 {
		dst.EXIF.Orientation = src.EXIF.Orientation
	}
	if dst.EXIF.CameraMake == "" {
		dst.EXIF.CameraMake = src.EXIF.CameraMake
	}
	if dst.EXIF.CameraModel == "" {
		dst.EXIF.CameraModel = src.EXIF.CameraModel
	}
	if dst.EXIF.LensModel == "" {
		dst.EXIF.LensModel = src.EXIF.LensModel
	}
	mergeMissingExposure(dst, src)
}

func mergeMissingExposure(dst *domain.MediaMetadata, src domain.MediaMetadata) {
	if dst.EXIF.ISO == nil {
		dst.EXIF.ISO = src.EXIF.ISO
	}
	if dst.EXIF.FNumber == nil {
		dst.EXIF.FNumber = src.EXIF.FNumber
	}
	if dst.EXIF.ExposureTime == "" {
		dst.EXIF.ExposureTime = src.EXIF.ExposureTime
	}
	if dst.EXIF.FocalLengthMM == nil {
		dst.EXIF.FocalLengthMM = src.EXIF.FocalLengthMM
	}
	if dst.EXIF.GPSLatitude == nil {
		dst.EXIF.GPSLatitude = src.EXIF.GPSLatitude
	}
	if dst.EXIF.GPSLongitude == nil {
		dst.EXIF.GPSLongitude = src.EXIF.GPSLongitude
	}
}
