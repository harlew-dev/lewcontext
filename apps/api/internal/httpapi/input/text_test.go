package input

import "testing"

func TestValidText(t *testing.T) {
	for _, c := range []struct {
		name, text            string
		nameOK, descriptionOK bool
	}{
		{"plain", "Acme Corp", true, true},
		{"technologist emoji (ZWJ)", "Ada 👩\u200D💻", true, true},
		{"rainbow flag (VS16 and ZWJ)", "🏳\uFE0F\u200D🌈 team", true, true},
		{"family emoji", "👨\u200D👩\u200D👧", true, true},
		{"subdivision flag (tags)", "🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", true, true},
		{"Persian with ZWNJ", "می\u200Cخواهم", true, true},
		{"soft hyphen", "Donau\u00ADdampf", true, true},
		{"line break", "one\ntwo", false, true},
		{"CRLF and tab", "one\r\n\ttwo", false, true},
		{"NUL", "nul\x00here", false, false},
		{"ESC", "escape\x1b[2J", false, false},
		{"DEL", "del\x7f", false, false},
		{"C1 control", "c1\u0085", false, false},
		{"RLO override", "abc\u202Efdp.exe", false, false},
		{"LRE embedding", "\u202Aabc", false, false},
		{"RLI isolate", "\u2067abc", false, false},
		{"Arabic letter mark", "a\u061Cb", false, false},
		{"RLM", "a\u200Fb", false, false},
		{"zero-width space", "zero\u200Bwidth", false, false},
		{"word joiner", "a\u2060b", false, false},
		{"invisible times", "a\u2062b", false, false},
		{"BOM", "\uFEFFabc", false, false},
		{"line separator", "line\u2028separator", false, false},
		{"paragraph separator", "a\u2029b", false, false},
		{"invalid UTF-8", "a\xffb", false, false},
	} {
		if got := ValidText(c.text, false); got != c.nameOK {
			t.Errorf("%s as a name: %v, want %v", c.name, got, c.nameOK)
		}
		if got := ValidText(c.text, true); got != c.descriptionOK {
			t.Errorf("%s as a description: %v, want %v", c.name, got, c.descriptionOK)
		}
	}
}
