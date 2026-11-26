package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFirstPromptIsTheFirstTypedUserMessage(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"summary","summary":"a title"}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"Caveat: the messages below were generated"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","isSidechain":true,"message":{"role":"user","content":"a sub-agent's prompt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"  Fix the restore\nso every seat comes back  "}]}}`,
		`{"type":"user","message":{"role":"user","content":"a later prompt"}}`,
	)
	got, err := FirstPrompt(path)
	if err != nil || got != "Fix the restore" {
		t.Fatalf("FirstPrompt = %q, %v; want the first line of the first typed prompt", got, err)
	}
}

func TestFirstPromptIsCutToOneShortLine(t *testing.T) {
	long := strings.Repeat("word ", 30)
	path := writeTranscript(t, `{"type":"user","message":{"role":"user","content":"`+long+`"}}`)
	got, err := FirstPrompt(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got)); n > PromptWidth || !strings.HasSuffix(got, "…") {
		t.Fatalf("FirstPrompt = %q (%d runes); want at most %d ending in an ellipsis", got, n, PromptWidth)
	}
}

func TestFirstPromptOfAConversationWithNoneIsEmpty(t *testing.T) {
	path := writeTranscript(t, `{"type":"summary","summary":"x"}`, `not json`)
	got, err := FirstPrompt(path)
	if err != nil || got != "" {
		t.Fatalf("FirstPrompt = %q, %v; want empty and no error", got, err)
	}
}

func TestFirstPromptOfAMissingFileIsAnError(t *testing.T) {
	if _, err := FirstPrompt(filepath.Join(t.TempDir(), "gone.jsonl")); err == nil {
		t.Fatal("FirstPrompt of a missing transcript gave no error")
	}
}
