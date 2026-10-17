package stt

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// paragraphPause is the gap between segments that starts a new paragraph (S2).
	paragraphPause = 3.0
	// paragraphLongest is how long a paragraph runs before the next sentence end closes it:
	// steady dictation seldom pauses 3 s, and a paragraph of many minutes hides its times.
	paragraphLongest = 60.0
)

// render is the sidecar, S2 and S3: YAML front matter, then the timed paragraphs.
func render(r recording, done reply, segments []segment, now time.Time) string {
	var b strings.Builder
	b.WriteString(frontMatter)
	for _, field := range []struct {
		key   string
		value any
	}{
		{"source", filepath.Base(r.path)},
		{"parts", len(r.parts)},
		{"duration", clock(done.Duration)},
		{languageKey, done.Language},
		{"model", done.Model},
		{"transcribed", now.UTC().Format(time.RFC3339)},
		// JSON flow, which YAML reads as the same list; stt reads it back with encoding/json.
		{audioKey, r.audio},
	} {
		fmt.Fprintf(&b, "%s: %s\n", field.key, yamlScalar(field.value))
	}
	b.WriteString("---\n\n")
	if len(segments) == 0 {
		b.WriteString("_No speech found._\n")
		return b.String()
	}
	b.WriteString(paragraphs(segments))
	return b.String()
}

// yamlScalar is a JSON value, which YAML reads as the same scalar: a string is always quoted,
// so a name with a colon or a hash is not misread.
func yamlScalar(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `""` // only strings and ints reach here, and both always marshal
	}
	return string(data)
}

func paragraphs(segments []segment) string {
	var b strings.Builder
	var prev *segment
	opened := 0.0 // when the current paragraph began
	for _, s := range segments {
		switch {
		case prev == nil:
			fmt.Fprintf(&b, "**[%s]** %s", clock(s.Start), s.Text)
			opened = s.Start
		case s.Start-prev.End >= paragraphPause,
			s.Start-opened >= paragraphLongest && endsSentence(prev.Text):
			fmt.Fprintf(&b, "\n\n**[%s]** %s", clock(s.Start), s.Text)
			opened = s.Start
		default:
			b.WriteString(" " + s.Text)
		}
		prev = &s
	}
	b.WriteString("\n")
	return b.String()
}

func endsSentence(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "!")
}

// clock is seconds as HH:MM:SS, rounded down.
func clock(seconds float64) string {
	total := int(seconds)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total%3600/60, total%60)
}
