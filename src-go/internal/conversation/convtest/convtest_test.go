package convtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

const sid = "0f1e2d3c-4b5a-4968-8776-655443322110"

// A teamchat session planted by TeamchatSession is read back by conversation.For: its pool seat
// makes the team teamchat-live in the checkout, and the record is where Locate says.
func TestTeamchatSessionIsReadBack(t *testing.T) {
	home, claude, checkout := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	rec := registry.Record{Name: "cc-v2", Dir: checkout, Prefix: "cc", Args: []string{"--teams", "1@beta"}}
	path := TeamchatSession(t, rec, getenv, sid)
	if filepath.Base(path) != conversation.File {
		t.Fatalf("planted at %s", path)
	}
	dir, err := transcript.Dir("cc", checkout, claude)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := conversation.For(rec, conversation.Where{ClaudeHome: claude, Getenv: getenv})
	if err != nil || got != sid {
		t.Fatalf("For = %q, %v", got, err)
	}
}
