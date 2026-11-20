package media

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"io"
	"math"
	"os"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const (
	macEpochOffsetSeconds = 2082844800
	maxBoxDepth           = 8
	maxISOMetadataBoxSize = 16 * 1024 * 1024
)

type isoExtent struct {
	offset uint64
	length uint64
	method uint8
}

type isoParseState struct {
	meta       domain.MediaMetadata
	exifItemID uint32
	hasExifID  bool
	extents    map[uint32]isoExtent
	idatData   []byte
	maxArea    int64
}

func parseISOBMFFMetadata(f *os.File, format domain.MediaFormat) (domain.MediaMetadata, error) {
	info, err := f.Stat()
	if err != nil {
		return domain.MediaMetadata{}, err
	}
	state := &isoParseState{
		extents: make(map[uint32]isoExtent),
	}
	parseISOBoxes(f, 0, info.Size(), 0, state)
	resolveHEIFExifItem(f, state)

	if (format == domain.FormatCR3 || format == domain.FormatHEIC || format == domain.FormatHEIF) &&
		(state.meta.Width == 0 || state.meta.EXIF.CameraMake == "") {
		fillFromEmbeddedJPEG(f, &state.meta)
	}
	return state.meta, nil
}

func parseISOBoxes(r io.ReaderAt, start, end int64, depth int, state *isoParseState) {
	if depth >= maxBoxDepth {
		return
	}
	pos := start
	for pos+8 <= end {
		boxType, headerLen, boxEnd, ok := readISOBoxHeader(r, pos, end)
		if !ok {
			return
		}
		bodyStart := pos + headerLen
		handleISOBox(r, boxType, bodyStart, boxEnd, depth, state)
		pos = boxEnd
	}
}

func readISOBoxHeader(r io.ReaderAt, pos, limit int64) (string, int64, int64, bool) {
	var hdr [16]byte
	if _, err := r.ReadAt(hdr[:8], pos); err != nil {
		return "", 0, 0, false
	}
	size32 := binary.BigEndian.Uint32(hdr[0:4])
	boxType := string(hdr[4:8])
	headerLen := int64(8)
	var boxSize int64

	switch size32 {
	case 0:
		boxSize = limit - pos
	case 1:
		if pos+16 > limit {
			return "", 0, 0, false
		}
		if _, err := r.ReadAt(hdr[8:16], pos+8); err != nil {
			return "", 0, 0, false
		}
		u64 := binary.BigEndian.Uint64(hdr[8:16])
		if u64 > uint64(limit-pos) {
			return "", 0, 0, false
		}
		boxSize = int64(u64)
		headerLen = 16
	default:
		boxSize = int64(size32)
	}

	if boxSize < headerLen || pos+boxSize > limit {
		return "", 0, 0, false
	}
	return boxType, headerLen, pos + boxSize, true
}

func handleISOBox(r io.ReaderAt, boxType string, bodyStart, boxEnd int64, depth int, state *isoParseState) {
	switch boxType {
	case "moov", "trak", "iprp", "ipco", "udta":
		parseISOBoxes(r, bodyStart, boxEnd, depth+1, state)
	case "meta":
		if bodyStart+4 <= boxEnd {
			parseISOBoxes(r, bodyStart+4, boxEnd, depth+1, state)
		}
	case "uuid":
		if bodyStart+16 <= boxEnd {
			parseISOBoxes(r, bodyStart+16, boxEnd, depth+1, state)
		}
	case "mvhd", "tkhd", "ispe", "iinf", "iloc", "idat", "CMT1", "CMT2", "CMT4", "Exif":
		payload, ok := readBoxPayload(r, bodyStart, boxEnd)
		if !ok {
			return
		}
		applyISOLeafBox(boxType, payload, state)
	}
}

func readBoxPayload(r io.ReaderAt, start, end int64) ([]byte, bool) {
	size := end - start
	if size <= 0 || size > maxISOMetadataBoxSize {
		return nil, false
	}
	buf := make([]byte, size)
	if _, err := r.ReadAt(buf, start); err != nil {
		return nil, false
	}
	return buf, true
}

func applyISOLeafBox(boxType string, payload []byte, state *isoParseState) {
	switch boxType {
	case "mvhd":
		parseMVHDBox(payload, &state.meta)
	case "tkhd":
		parseTKHDBox(payload, &state.meta)
	case "ispe":
		parseISPEBox(payload, state)
	case "iinf":
		parseIINFBox(payload, state)
	case "iloc":
		parseILOCBox(payload, state)
	case "idat":
		state.idatData = payload
	case "CMT1":
		parseTIFFBlock(payload, &state.meta)
	case "CMT2":
		parseTIFFAsExifIFD(payload, &state.meta)
	case "CMT4":
		parseTIFFAsGPSIFD(payload, &state.meta)
	case "Exif":
		parseExifPayload(payload, &state.meta)
	}
}

func parseMVHDBox(payload []byte, meta *domain.MediaMetadata) {
	if len(payload) < 20 {
		return
	}
	version := payload[0]
	var creationSec uint64
	var timescale uint32
	var duration uint64

	switch version {
	case 0:
		if len(payload) < 24 {
			return
		}
		creationSec = uint64(binary.BigEndian.Uint32(payload[4:8]))
		timescale = binary.BigEndian.Uint32(payload[12:16])
		duration = uint64(binary.BigEndian.Uint32(payload[16:20]))
	case 1:
		if len(payload) < 36 {
			return
		}
		creationSec = binary.BigEndian.Uint64(payload[4:12])
		timescale = binary.BigEndian.Uint32(payload[20:24])
		duration = binary.BigEndian.Uint64(payload[24:32])
	default:
		return
	}

	if timescale > 0 && duration > 0 {
		meta.DurationSec = float64(duration) / float64(timescale)
	}
	if meta.TakenAt == nil && creationSec > macEpochOffsetSeconds {
		ts := time.Unix(int64(creationSec-macEpochOffsetSeconds), 0).UTC()
		if ts.Year() >= 1970 && ts.Year() <= 2200 {
			meta.TakenAt = &ts
		}
	}
}

func parseTKHDBox(payload []byte, meta *domain.MediaMetadata) {
	if len(payload) < 84 || (meta.Width > 0 && meta.Height > 0) {
		return
	}
	version := payload[0]
	widthOff := 76
	if version == 1 {
		widthOff = 88
	} else if version != 0 {
		return
	}
	if len(payload) < widthOff+8 {
		return
	}
	w := int(binary.BigEndian.Uint32(payload[widthOff:widthOff+4]) >> 16)
	h := int(binary.BigEndian.Uint32(payload[widthOff+4:widthOff+8]) >> 16)
	if w > 0 && h > 0 {
		meta.Width = w
		meta.Height = h
	}
}

func parseISPEBox(payload []byte, state *isoParseState) {
	if len(payload) < 12 {
		return
	}
	w := int(binary.BigEndian.Uint32(payload[4:8]))
	h := int(binary.BigEndian.Uint32(payload[8:12]))
	area := int64(w) * int64(h)
	if w > 0 && h > 0 && area >= state.maxArea {
		state.maxArea = area
		state.meta.Width = w
		state.meta.Height = h
	}
}

func parseIINFBox(payload []byte, state *isoParseState) {
	if len(payload) < 6 {
		return
	}
	version := payload[0]
	pos := 6
	count := int(binary.BigEndian.Uint16(payload[4:6]))
	if version > 0 {
		if len(payload) < 8 {
			return
		}
		count = int(binary.BigEndian.Uint32(payload[4:8]))
		pos = 8
	}
	for range count {
		if pos+8 > len(payload) {
			return
		}
		sz := int(binary.BigEndian.Uint32(payload[pos : pos+4]))
		if sz < 12 || pos+sz > len(payload) {
			return
		}
		if string(payload[pos+4:pos+8]) == "infe" {
			parseINFEBox(payload[pos+8:pos+sz], state)
		}
		pos += sz
	}
}

func parseINFEBox(body []byte, state *isoParseState) {
	if len(body) < 12 {
		return
	}
	version := body[0]
	var itemID uint32
	var typeOff int
	switch version {
	case 2:
		itemID = uint32(binary.BigEndian.Uint16(body[4:6]))
		typeOff = 8
	case 3:
		if len(body) < 14 {
			return
		}
		itemID = binary.BigEndian.Uint32(body[4:8])
		typeOff = 10
	default:
		return
	}
	if string(body[typeOff:typeOff+4]) == "Exif" {
		state.exifItemID = itemID
		state.hasExifID = true
	}
}

func parseILOCBox(payload []byte, state *isoParseState) {
	if len(payload) < 8 {
		return
	}
	version := payload[0]
	offsetSize := int(payload[4] >> 4)
	lengthSize := int(payload[4] & 0x0F)
	baseSize := int(payload[5] >> 4)
	indexSize := 0
	if version == 1 || version == 2 {
		indexSize = int(payload[5] & 0x0F)
	}
	pos := 6
	itemCountU, nextPos, ok := readILOCUint(payload, pos, version)
	if !ok {
		return
	}
	itemCount := int(itemCountU)
	pos = nextPos
	for range itemCount {
		next, ok := parseILOCItem(payload, pos, version, offsetSize, lengthSize, baseSize, indexSize, state)
		if !ok {
			return
		}
		pos = next
	}
}

func parseILOCItem(
	payload []byte,
	pos int,
	version byte,
	offsetSize, lengthSize, baseSize, indexSize int,
	state *isoParseState,
) (int, bool) {
	itemID, pos, ok := readILOCUint(payload, pos, version)
	if !ok {
		return 0, false
	}
	var method uint8
	if version == 1 || version == 2 {
		if pos+2 > len(payload) {
			return 0, false
		}
		method = payload[pos+1] & 0x0F
		pos += 2
	}
	if pos+2+baseSize+2 > len(payload) {
		return 0, false
	}
	pos += 2
	baseOffset := readUintN(payload[pos : pos+baseSize])
	pos += baseSize
	extentCount := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2
	return readILOCExtents(payload, pos, extentCount, indexSize, offsetSize, lengthSize, baseOffset, method, itemID, state)
}

func readILOCUint(payload []byte, pos int, version byte) (uint32, int, bool) {
	if version < 2 {
		if pos+2 > len(payload) {
			return 0, 0, false
		}
		return uint32(binary.BigEndian.Uint16(payload[pos : pos+2])), pos + 2, true
	}
	if pos+4 > len(payload) {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(payload[pos : pos+4]), pos + 4, true
}

func readILOCExtents(
	payload []byte,
	pos, extentCount, indexSize, offsetSize, lengthSize int,
	baseOffset uint64,
	method uint8,
	itemID uint32,
	state *isoParseState,
) (int, bool) {
	extSize := indexSize + offsetSize + lengthSize
	if pos+extentCount*extSize > len(payload) {
		return 0, false
	}
	for i := range extentCount {
		cur := pos + i*extSize + indexSize
		extOff := readUintN(payload[cur : cur+offsetSize])
		extLen := readUintN(payload[cur+offsetSize : cur+offsetSize+lengthSize])
		if i == 0 && extLen > 0 && extOff <= math.MaxUint64-baseOffset {
			state.extents[itemID] = isoExtent{offset: baseOffset + extOff, length: extLen, method: method}
		}
	}
	return pos + extentCount*extSize, true
}

func readUintN(b []byte) uint64 {
	var out uint64
	for _, v := range b {
		out = (out << 8) | uint64(v)
	}
	return out
}

func resolveHEIFExifItem(r io.ReaderAt, state *isoParseState) {
	if !state.hasExifID {
		return
	}
	ext, ok := state.extents[state.exifItemID]
	if !ok || ext.length == 0 || ext.length > maxISOMetadataBoxSize {
		return
	}
	savedW, savedH := state.meta.Width, state.meta.Height
	if ext.method == 1 {
		idatLen := uint64(len(state.idatData))
		if ext.offset <= idatLen && ext.length <= idatLen-ext.offset {
			parseExifPayload(state.idatData[ext.offset:ext.offset+ext.length], &state.meta)
		}
	} else if ext.offset <= math.MaxInt64 {
		buf := make([]byte, ext.length)
		if _, err := r.ReadAt(buf, int64(ext.offset)); err == nil {
			parseExifPayload(buf, &state.meta)
		}
	}
	if state.maxArea > 0 && savedW > 0 && savedH > 0 {
		state.meta.Width = savedW
		state.meta.Height = savedH
	}
}

func parseExifPayload(payload []byte, meta *domain.MediaMetadata) {
	if len(payload) < 8 {
		return
	}
	if string(payload[:2]) == "II" || string(payload[:2]) == "MM" {
		parseTIFFBlock(payload, meta)
		return
	}
	if bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
		parseTIFFBlock(payload[6:], meta)
		return
	}
	tiffOff := int(binary.BigEndian.Uint32(payload[:4]))
	start := 4 + tiffOff
	if start >= 4 && start < len(payload) {
		parseTIFFBlock(payload[start:], meta)
		return
	}
	if idx := bytes.Index(payload, []byte("Exif\x00\x00")); idx >= 0 {
		parseTIFFBlock(payload[idx+6:], meta)
	}
}

func fillFromEmbeddedJPEG(f *os.File, meta *domain.MediaMetadata) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEmbeddedScanBytes))
	if err != nil {
		return
	}
	jpg, ok := findLargestEmbeddedJPEG(data)
	if !ok {
		return
	}
	var jpgMeta domain.MediaMetadata
	parseJPEGSegments(jpg, &jpgMeta)
	mergeMissingMeta(meta, jpgMeta)
}

func findLargestEmbeddedJPEG(data []byte) ([]byte, bool) {
	var best []byte
	var bestArea int64

	for i := 0; i+3 < len(data); i++ {
		if data[i] != 0xFF || data[i+1] != 0xD8 || data[i+2] != 0xFF {
			continue
		}
		end, ok := findJPEGStreamEnd(data, i)
		if !ok {
			continue
		}
		candidate := data[i:end]
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(candidate))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			i = end - 1
			continue
		}
		area := int64(cfg.Width) * int64(cfg.Height)
		if area > bestArea {
			bestArea = area
			best = candidate
		}
		i = end - 1
	}
	return best, len(best) > 0
}

func findJPEGStreamEnd(data []byte, start int) (int, bool) {
	pos := start + 2
	for pos+1 < len(data) {
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			return 0, false
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 {
			return pos, true
		}
		if marker == 0xDA {
			return scanJPEGEntropyToEOI(data, pos)
		}
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if pos+2 > len(data) {
			return 0, false
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return 0, false
		}
		pos += segLen
	}
	return 0, false
}

func scanJPEGEntropyToEOI(data []byte, pos int) (int, bool) {
	if pos+2 > len(data) {
		return 0, false
	}
	sosLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	if sosLen < 2 || pos+sosLen > len(data) {
		return 0, false
	}
	pos += sosLen
	for pos+1 < len(data) {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		next := data[pos+1]
		if next == 0xD9 {
			return pos + 2, true
		}
		if next == 0x00 || (next >= 0xD0 && next <= 0xD7) || next == 0xFF {
			pos += 2
			continue
		}
		pos += 2
	}
	return 0, false
}
