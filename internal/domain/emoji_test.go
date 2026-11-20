package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lucastheo/photos/internal/domain"
)

func TestNormalizeEmojiTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		// Valid canonicalizations & emoji classes
		{name: "heart with FE0F", raw: "❤️", want: "\u2764"},
		{name: "heart without FE0F", raw: "\u2764", want: "\u2764"},
		{name: "whitespace trimmed", raw: "  ❤️ \n\t", want: "\u2764"},
		{name: "multiple trailing FE0F", raw: "\u2764\uFE0F\uFE0F", want: "\u2764"},
		{name: "star", raw: "⭐", want: "⭐"},
		{name: "fire", raw: "🔥", want: "🔥"},
		{name: "beach with FE0F", raw: "🏖️", want: "🏖"},
		{name: "joy", raw: "😂", want: "😂"},
		{name: "party popper", raw: "🎉", want: "🎉"},
		{name: "wastebasket with FE0F", raw: "🗑️", want: "🗑"},
		{name: "round pushpin", raw: "📍", want: "📍"},
		{name: "thumbs up skin tone", raw: "👍🏽", want: "👍🏽"},
		{name: "family ZWJ", raw: "👨‍👩‍👧", want: "👨‍👩‍👧"},
		{name: "heart on fire ZWJ with inner FE0F", raw: "❤️‍🔥", want: "❤️‍🔥"},
		{name: "rainbow flag ZWJ", raw: "🏳️‍🌈", want: "🏳️‍🌈"},
		{name: "US flag regional indicator", raw: "🇺🇸", want: "🇺🇸"},
		{name: "Scotland subdivision flag", raw: "🏴󠁧󠁢󠁳󠁣󠁴󠁿", want: "🏴󠁧󠁢󠁳󠁣󠁴󠁿"},
		{name: "standalone black flag", raw: "🏴", want: "🏴"},
		{name: "keycap 1 with FE0F", raw: "1️⃣", want: "1️⃣"},
		{name: "keycap 1 without FE0F", raw: "1\u20E3", want: "1\u20E3"},
		{name: "keycap hash", raw: "#️⃣", want: "#️⃣"},
		{name: "keycap asterisk", raw: "*️⃣", want: "*️⃣"},
		{name: "copyright", raw: "©️", want: "©"},
		{name: "trademark", raw: "™️", want: "™"},
		{name: "watch misc technical", raw: "⌚", want: "⌚"},
		{name: "circled M", raw: "Ⓜ️", want: "Ⓜ"},
		{name: "play button", raw: "▶️", want: "▶"},
		{name: "curved arrow", raw: "⤴️", want: "⤴"},
		{name: "wavy dash", raw: "〰️", want: "〰"},
		{name: "left right arrow", raw: "↔️", want: "↔"},
		{name: "extended-A light blue heart", raw: "🩵", want: "🩵"},
		{name: "two emojis concatenated inner FE0F kept", raw: "❤️🔥", want: "❤️🔥"},
		{name: "two emojis trailing FE0F stripped", raw: "🔥❤️", want: "🔥\u2764"},

		// Invalid inputs
		{name: "empty", raw: "", wantErr: true},
		{name: "whitespace only", raw: "   \t\n ", wantErr: true},
		{name: "FE0F only", raw: "\uFE0F", wantErr: true},
		{name: "double FE0F only", raw: "\uFE0F\uFE0F", wantErr: true},
		{name: "plain ASCII word", raw: "hello", wantErr: true},
		{name: "single ASCII letter", raw: "a", wantErr: true},
		{name: "NFD letter text", raw: "Cafe\u0301", wantErr: true},
		{name: "mixed ASCII and emoji", raw: "love❤️", wantErr: true},
		{name: "emoji then ASCII", raw: "❤️love", wantErr: true},
		{name: "standalone digit", raw: "1", wantErr: true},
		{name: "standalone hash", raw: "#", wantErr: true},
		{name: "standalone asterisk", raw: "*", wantErr: true},
		{name: "digits", raw: "123", wantErr: true},
		{name: "space between emojis", raw: "❤️ 🔥", wantErr: true},
		{name: "standalone skin tone modifier", raw: "🏽", wantErr: true},
		{name: "standalone ZWJ", raw: "\u200D", wantErr: true},
		{name: "leading ZWJ", raw: "\u200D🔥", wantErr: true},
		{name: "trailing ZWJ", raw: "🔥\u200D", wantErr: true},
		{name: "double ZWJ", raw: "👨\u200D\u200D👩", wantErr: true},
		{name: "standalone combining mark", raw: "\u0301", wantErr: true},
		{name: "standalone keycap mark", raw: "\u20E3", wantErr: true},
		{name: "emoji followed by combining accent", raw: "❤️\u0301", wantErr: true},
		{name: "single regional indicator", raw: "🇺", wantErr: true},
		{name: "three regional indicators", raw: "🇺🇸🇺", wantErr: true},
		{name: "standalone tag char", raw: "\U000E0067", wantErr: true},
		{name: "empty flag tag sequence", raw: "🏴\U000E007F", wantErr: true},
		{name: "unterminated flag tag sequence", raw: "🏴\U000E0067\U000E0062", wantErr: true},
		{name: "exceeds max 16 runes", raw: strings.Repeat("🔥", 17), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NormalizeEmojiTag(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeEmojiTag(%q) succeeded with %q, want error", tc.raw, got)
				}
				if !errors.Is(err, domain.ErrInvalidEmojiTag) {
					t.Fatalf("NormalizeEmojiTag(%q) error = %v, want ErrInvalidEmojiTag", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeEmojiTag(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeEmojiTag(%q) = %q (% X), want %q (% X)", tc.raw, got, got, tc.want, tc.want)
			}
		})
	}
}
