package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PromptWidth bounds the first prompt as a chooser row shows it, in runes, the ellipsis included.
const PromptWidth = 60

// commandMarkers open a user entry Claude Code writes for a slash command or its output: not a
// prompt the person typed.
var commandMarkers = []string{"<command-name>", "<command-message>", "<local-command-", "<bash-input>", "<bash-stdout>"}

// promptEntry is the part of a user entry the first prompt is read from.
type promptEntry struct {
	Type        string `json:"type"`
	IsSidechain flag   `json:"isSidechain"`
	IsMeta      flag   `json:"isMeta"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// FirstPrompt is the conversation's first prompt as a person typed it (Plan 00265 R90, Q5): the
// first main-thread user entry that is not Claude's own note, a slash command or a tool's result,
// its first non-empty line with its spaces collapsed, cut to PromptWidth. "" is a conversation
// with no prompt yet; a line that is not JSON is passed over, as the other readings do. A file
// that cannot be read is an error.
func FirstPrompt(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("the transcript %s: %w", path, err)
	}
	prompt, readErr := firstPromptIn(bufio.NewReader(f))
	if closeErr := f.Close(); readErr == nil && closeErr != nil {
		readErr = closeErr
	}
	if readErr != nil {
		return "", fmt.Errorf("the transcript %s: %w", path, readErr)
	}
	return prompt, nil
}

func firstPromptIn(r *bufio.Reader) (string, error) {
	for {
		line, err := r.ReadBytes('\n')
		if text := promptOf(line); text != "" {
			return text, nil
		}
		switch {
		case errors.Is(err, io.EOF):
			return "", nil
		case err != nil:
			return "", err
		}
	}
}

// promptOf is the prompt one transcript line holds, "" for a line that holds none.
func promptOf(line []byte) string {
	var e promptEntry
	if json.Unmarshal(line, &e) != nil || e.Type != typeUser || bool(e.IsSidechain) || bool(e.IsMeta) {
		return ""
	}
	text := contentText(e.Message.Content)
	for _, marker := range commandMarkers {
		if strings.HasPrefix(strings.TrimSpace(text), marker) {
			return ""
		}
	}
	return oneLine(text)
}

// contentText is a message's typed text: the string itself, or its text blocks (a tool result
// is no text).
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// oneLine is text's first non-empty line, its spaces collapsed, cut to PromptWidth.
func oneLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		words := strings.Join(strings.Fields(line), " ")
		if words == "" {
			continue
		}
		if runes := []rune(words); len(runes) > PromptWidth {
			return strings.TrimSpace(string(runes[:PromptWidth-1])) + "…"
		}
		return words
	}
	return ""
}
