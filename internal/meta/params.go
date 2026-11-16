package meta

import (
	"regexp"
	"strings"
)

// Setting is one "Key: value" pair from the settings line.
type Setting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Params is parsed A1111/Forge generation metadata.
type Params struct {
	Prompt   string    `json:"prompt"`
	Negative string    `json:"negative"`
	Settings []Setting `json:"settings"`
}

// reParam matches one "Key: value" pair. A quoted value may contain commas.
// This is the same pattern A1111 uses.
var reParam = regexp.MustCompile(`\s*(\w[\w \-/]+):\s*("(?:\\.|[^\\"])+"|[^,]*)(?:,|$)`)

// Parse splits A1111/Forge "parameters" text into the prompt, the negative
// prompt and the settings, following A1111's own parser: the last line holds
// the settings if it has at least three "Key: value" pairs, and the negative
// prompt starts at a line beginning with "Negative prompt:".
func Parse(text string) Params {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	last := lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	if len(reParam.FindAllStringSubmatch(last, -1)) < 3 {
		lines = append(lines, last)
		last = ""
	}

	var prompt, negative []string
	inNegative := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Negative prompt:"); ok {
			inNegative = true
			line = strings.TrimSpace(rest)
		}
		if inNegative {
			negative = append(negative, line)
		} else {
			prompt = append(prompt, line)
		}
	}

	p := Params{
		Prompt:   strings.Join(prompt, "\n"),
		Negative: strings.Join(negative, "\n"),
		Settings: []Setting{},
	}
	for _, m := range reParam.FindAllStringSubmatch(last, -1) {
		p.Settings = append(p.Settings, Setting{
			Key:   strings.TrimSpace(m[1]),
			Value: strings.TrimSpace(m[2]),
		})
	}
	return p
}
