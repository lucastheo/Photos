package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxEmojiTagRunes      = 16
	variationSelector16   = rune(0xFE0F)
	zeroWidthJoiner       = rune(0x200D)
	combiningEnclosingKey = rune(0x20E3)
	wavingBlackFlag       = rune(0x1F3F4)
	tagCancel             = rune(0xE007F)
)

// NormalizeEmojiTag trims surrounding whitespace, normalizes to Unicode NFC,
// strips trailing U+FE0F variation selectors, and validates that the input is a
// valid emoji tag without plain ASCII text or standalone modifiers.
func NormalizeEmojiTag(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	nfc := norm.NFC.String(trimmed)
	canonical := strings.TrimRight(nfc, "\uFE0F")
	if canonical == "" {
		return "", fmt.Errorf("%w: empty emoji tag", ErrInvalidEmojiTag)
	}
	if utf8.RuneCountInString(canonical) > maxEmojiTagRunes {
		return "", fmt.Errorf("%w: exceeds %d runes", ErrInvalidEmojiTag, maxEmojiTagRunes)
	}
	if !validateEmojiRunes([]rune(canonical)) {
		return "", fmt.Errorf("%w: invalid emoji sequence", ErrInvalidEmojiTag)
	}
	return canonical, nil
}

func validateEmojiRunes(runes []rune) bool {
	for i := 0; i < len(runes); {
		next, ok := consumeEmojiCluster(runes, i)
		if !ok {
			return false
		}
		i = next
	}
	return true
}

func consumeEmojiCluster(runes []rune, start int) (int, bool) {
	if next, ok := consumeKeycap(runes, start); ok {
		return next, true
	}
	r := runes[start]
	if isRegionalIndicator(r) {
		if start+1 < len(runes) && isRegionalIndicator(runes[start+1]) {
			return start + 2, true
		}
		return 0, false
	}
	if r == wavingBlackFlag {
		if next, ok := consumeTagSequence(runes, start); ok {
			return next, true
		}
	}
	if !isEmojiBase(r) {
		return 0, false
	}
	return consumeEmojiFollowers(runes, start+1)
}

func consumeEmojiFollowers(runes []rune, pos int) (int, bool) {
	if pos < len(runes) && runes[pos] == variationSelector16 {
		pos++
	}
	if pos < len(runes) && isEmojiModifier(runes[pos]) {
		pos++
	}
	if pos < len(runes) && runes[pos] == zeroWidthJoiner {
		if pos+1 >= len(runes) {
			return 0, false
		}
		return consumeEmojiCluster(runes, pos+1)
	}
	return pos, true
}

func consumeKeycap(runes []rune, start int) (int, bool) {
	if !isKeycapBase(runes[start]) {
		return 0, false
	}
	if start+1 < len(runes) && runes[start+1] == combiningEnclosingKey {
		return start + 2, true
	}
	if start+2 < len(runes) && runes[start+1] == variationSelector16 && runes[start+2] == combiningEnclosingKey {
		return start + 3, true
	}
	return 0, false
}

func consumeTagSequence(runes []rune, start int) (int, bool) {
	i := start + 1
	for i < len(runes) && runes[i] >= 0xE0020 && runes[i] <= 0xE007E {
		i++
	}
	if i == start+1 || i >= len(runes) || runes[i] != tagCancel {
		return 0, false
	}
	return i + 1, true
}

func isKeycapBase(r rune) bool {
	return (r >= '0' && r <= '9') || r == '#' || r == '*'
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func isEmojiModifier(r rune) bool {
	return r >= 0x1F3FB && r <= 0x1F3FF
}

func isEmojiBase(r rune) bool {
	if r >= 0x1F000 && r <= 0x1FAFF {
		return !isEmojiModifier(r) && !isRegionalIndicator(r)
	}
	return isBMPEmoji(r)
}

func isBMPEmoji(r rune) bool {
	switch {
	case r == 0x00A9 || r == 0x00AE || r == 0x203C || r == 0x2049:
		return true
	case r == 0x2122 || r == 0x2139 || (r >= 0x2194 && r <= 0x2199) || r == 0x21A9 || r == 0x21AA:
		return true
	case (r >= 0x2300 && r <= 0x23FF) || r == 0x24C2:
		return true
	case r == 0x25AA || r == 0x25AB || r == 0x25B6 || r == 0x25C0 || (r >= 0x25FB && r <= 0x25FE):
		return true
	case (r >= 0x2600 && r <= 0x27BF) || r == 0x2934 || r == 0x2935:
		return true
	case (r >= 0x2B05 && r <= 0x2B07) || r == 0x2B1B || r == 0x2B1C || r == 0x2B50 || r == 0x2B55:
		return true
	case r == 0x3030 || r == 0x303D || r == 0x3297 || r == 0x3299:
		return true
	default:
		return false
	}
}
