// Package diff compares the prompts and settings of two images.
package diff

import (
	"strings"
	"unicode"

	"dview/internal/meta"
)

// Op says how a piece of text or a line differs.
type Op string

const (
	Equal  Op = "equal"
	Insert Op = "insert" // only in image 2
	Delete Op = "delete" // only in image 1
)

// Segment is a run of text with the same Op.
type Segment struct {
	Op   Op     `json:"op"`
	Text string `json:"text"`
}

// maxCells bounds the word diff's table size. Longer texts are shown as
// entirely removed and added rather than spending time and memory on them.
const maxCells = 4_000_000

// token is a word or punctuation mark with the whitespace before it.
type token struct {
	space, text string
}

// tokenize splits s into words and punctuation marks. A word is a run of
// letters, digits, marks and underscores; a dot between digits stays inside
// the word, so "1.2" is one token. Every other non-space character is a
// token of its own. Whitespace is kept with the following token, and
// trailing whitespace is dropped.
func tokenize(s string) []token {
	runes := []rune(s)
	isWord := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
	}
	var out []token
	for i := 0; i < len(runes); {
		start := i
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i == len(runes) {
			break
		}
		space := string(runes[start:i])
		begin := i
		if isWord(runes[i]) {
			for i < len(runes) && (isWord(runes[i]) ||
				runes[i] == '.' && i > begin && unicode.IsDigit(runes[i-1]) &&
					i+1 < len(runes) && unicode.IsDigit(runes[i+1])) {
				i++
			}
		} else {
			i++
		}
		out = append(out, token{space: space, text: string(runes[begin:i])})
	}
	return out
}

// Words returns image 2's text b as a word diff against image 1's text a.
// Words that differ only in the whitespace around them count as equal; the
// whitespace shown is image 2's for equal and inserted text, and image 1's
// for deleted text. Within a change, deleted text comes before inserted text.
func Words(a, b string) []Segment {
	ta, tb := tokenize(a), tokenize(b)
	var out []Segment
	add := func(op Op, t token) {
		text := t.space + t.text
		if n := len(out); n > 0 && out[n-1].Op == op {
			out[n-1].Text += text
			return
		}
		out = append(out, Segment{Op: op, Text: text})
	}

	if len(ta)*len(tb) > maxCells {
		for _, t := range ta {
			add(Delete, t)
		}
		for _, t := range tb {
			add(Insert, t)
		}
		return nonNil(out)
	}

	// lcs[i][j] is the length of the longest common subsequence of ta[i:]
	// and tb[j:].
	w := len(tb) + 1
	lcs := make([]int32, (len(ta)+1)*w)
	for i := len(ta) - 1; i >= 0; i-- {
		for j := len(tb) - 1; j >= 0; j-- {
			if ta[i].text == tb[j].text {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}

	i, j := 0, 0
	for i < len(ta) && j < len(tb) {
		switch {
		case ta[i].text == tb[j].text:
			t := tb[j]
			// After removed text, keep image 1's space so the words
			// don't run together ("[-masterpiece,-] best").
			if n := len(out); t.space == "" && n > 0 && out[n-1].Op == Delete {
				t.space = ta[i].space
			}
			add(Equal, t)
			i, j = i+1, j+1
		case lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			add(Delete, ta[i])
			i++
		default:
			add(Insert, tb[j])
			j++
		}
	}
	for ; i < len(ta); i++ {
		add(Delete, ta[i])
	}
	for ; j < len(tb); j++ {
		add(Insert, tb[j])
	}
	return nonNil(out)
}

func nonNil(s []Segment) []Segment {
	if s == nil {
		return []Segment{}
	}
	return s
}

// Line is one line of the settings diff.
type Line struct {
	Op    Op     `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Settings returns image 2's settings b as a unified diff against image 1's
// settings a. Pairs are matched by key, so key order doesn't matter. Lines
// follow image 1's key order, then the keys only in image 2 in image 2's
// order. A changed key is shown as its old value (Delete) then its new value
// (Insert).
func Settings(a, b []meta.Setting) []Line {
	bByKey := make(map[string]string, len(b))
	for _, s := range b {
		if _, dup := bByKey[s.Key]; !dup {
			bByKey[s.Key] = s.Value
		}
	}
	inA := make(map[string]bool, len(a))
	out := []Line{}
	for _, s := range a {
		if inA[s.Key] {
			continue
		}
		inA[s.Key] = true
		bv, ok := bByKey[s.Key]
		switch {
		case !ok:
			out = append(out, Line{Op: Delete, Key: s.Key, Value: s.Value})
		case bv == s.Value:
			out = append(out, Line{Op: Equal, Key: s.Key, Value: s.Value})
		default:
			out = append(out,
				Line{Op: Delete, Key: s.Key, Value: s.Value},
				Line{Op: Insert, Key: s.Key, Value: bv})
		}
	}
	seen := map[string]bool{}
	for _, s := range b {
		if inA[s.Key] || seen[s.Key] {
			continue
		}
		seen[s.Key] = true
		out = append(out, Line{Op: Insert, Key: s.Key, Value: s.Value})
	}
	return out
}

// Join returns the text of all segments except those with the given op, so
// Join(segs, Delete) is image 2's text and Join(segs, Insert) is image 1's.
func Join(segs []Segment, skip Op) string {
	var sb strings.Builder
	for _, s := range segs {
		if s.Op != skip {
			sb.WriteString(s.Text)
		}
	}
	return sb.String()
}
