package diff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"dview/internal/meta"
)

// show renders segments as text with [-removed-] and {+added+} markers.
func show(segs []Segment) string {
	var sb strings.Builder
	for _, s := range segs {
		switch s.Op {
		case Equal:
			sb.WriteString(s.Text)
		case Insert:
			sb.WriteString("{+" + s.Text + "+}")
		case Delete:
			sb.WriteString("[-" + s.Text + "-]")
		}
	}
	return sb.String()
}

func TestWords(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"a red fox", "a red fox", "a red fox"},
		{"a red fox", "a grey fox", "a[- red-]{+ grey+} fox"},
		{"a fox", "a quick fox", "a{+ quick+} fox"},
		{"a quick fox", "a fox", "a[- quick-] fox"},
		// Punctuation is separate from words.
		{"girl, smiling", "girl smiling", "girl[-,-] smiling"},
		// A weight change marks only the number.
		{"(red hair:1.2)", "(red hair:1.4)", "(red hair:[-1.2-]{+1.4+})"},
		// Whitespace-only differences are not changes; image 2's spacing is shown.
		{"a  red\nfox", "a red fox", "a red fox"},
		{"", "new prompt", "{+new prompt+}"},
		{"old prompt", "", "[-old prompt-]"},
		{"", "", ""},
		// Lists of attributes.
		{"masterpiece, best quality, 1girl, solo", "best quality, 1girl, smile, solo",
			"[-masterpiece,-] best quality, 1girl,{+ smile,+} solo"},
	}
	for _, tt := range tests {
		t.Run(tt.a+"|"+tt.b, func(t *testing.T) {
			segs := Words(tt.a, tt.b)
			assert.Equal(t, tt.want, show(segs))
			assert.NotNil(t, segs)
			// Apart from spacing, the non-deleted text is image 2's text.
			assert.Equal(t, strings.Fields(tt.b), strings.Fields(Join(segs, Delete)))
		})
	}
}

func TestWordsMergesSegments(t *testing.T) {
	segs := Words("a b c", "x y z")
	assert.Equal(t, []Segment{{Delete, "a b c"}, {Insert, "x y z"}}, segs)
}

func TestWordsLargeInput(t *testing.T) {
	a := strings.Repeat("word ", 3000)
	b := strings.Repeat("other ", 3000)
	segs := Words(a, b)
	assert.Len(t, segs, 2, "falls back to remove-all, add-all")
}

func TestTokenize(t *testing.T) {
	var got []string
	for _, tk := range tokenize("  a1.5 1.x (b_c:0.9), d'e") {
		got = append(got, tk.text)
	}
	assert.Equal(t, []string{"a1.5", "1", ".", "x", "(", "b_c", ":", "0.9", ")", ",", "d", "'", "e"}, got)
}

func TestSettings(t *testing.T) {
	a := []meta.Setting{st("Steps", "30"), st("Sampler", "Euler"), st("CFG scale", "7"), st("Lora", "x")}
	b := []meta.Setting{st("CFG scale", "5"), st("Steps", "30"), st("Sampler", "Euler"), st("Denoise", "0.4")}
	assert.Equal(t, []Line{
		{Equal, "Steps", "30"},
		{Equal, "Sampler", "Euler"},
		{Delete, "CFG scale", "7"},
		{Insert, "CFG scale", "5"},
		{Delete, "Lora", "x"},
		{Insert, "Denoise", "0.4"},
	}, Settings(a, b))
}

func TestSettingsOrderIgnored(t *testing.T) {
	a := []meta.Setting{st("A", "1"), st("B", "2")}
	b := []meta.Setting{st("B", "2"), st("A", "1")}
	for _, l := range Settings(a, b) {
		assert.Equal(t, Equal, l.Op)
	}
	assert.Equal(t, []Line{}, Settings(nil, nil))
}

func st(key, value string) meta.Setting { return meta.Setting{Key: key, Value: value} }
