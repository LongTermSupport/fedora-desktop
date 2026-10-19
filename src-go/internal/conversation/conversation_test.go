package conversation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

const (
	id      = "0f1e2d3c-4b5a-4968-8776-655443322110"
	otherID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func record(name, prefix, dir string, args ...string) registry.Record {
	return registry.Record{Name: name, Dir: dir, Launcher: "/usr/local/bin/" + prefix, Prefix: prefix, Restore: true, Args: args}
}

func recorded(session, sessionID string) string {
	return `{"v":1,"session":"` + session + `","session_id":"` + sessionID + `","source":"clear","written_ms":1791000000000}`
}

// ccyCase is a ccy session in a checkout (its Dir) with its conversation recorded and its
// transcript there.
func ccyCase(t *testing.T, args ...string) registry.Record {
	t.Helper()
	checkout := t.TempDir()
	rec := record("ccy-proj", "ccy", checkout, args...)
	write(t, recordPath(checkout), recorded("ccy-proj", id))
	write(t, filepath.Join(checkout, ".claude/ccy/projects/-workspace", id+".jsonl"), "{}\n")
	return rec
}

func recordPath(checkout string) string {
	return filepath.Join(checkout, ".claude/ccy/pingbus/sessions/ccy-proj", File)
}

func mustFail(t *testing.T, rec registry.Record, home string, says ...string) {
	t.Helper()
	got, err := For(rec, home)
	if err == nil {
		t.Fatalf("For gave %q, want an error", got)
	}
	for _, s := range append(says, rec.Name, "resume it by hand: "+rec.Prefix+" --resume") {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not say %q", err, s)
		}
	}
}

// SPEC-team-add-and-restart C4: the recorded id, when the file is exactly C1's shape for this
// session and its transcript is there.
func TestForReadsTheRecordedConversation(t *testing.T) {
	rec := ccyCase(t, "--teams", "1@alpha")
	got, err := For(rec, "/nonexistent")
	if err != nil || got != id {
		t.Fatalf("For = %q, %v", got, err)
	}
}

// A cc session records it under its git checkout's top, which a session started in a folder
// below it is not; its transcript is under the Claude home, named for the folder.
func TestForFindsACCSessionsCheckoutAboveItsFolder(t *testing.T) {
	top, home := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(top, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(top, "sub", "dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := record("cc-proj", "cc", dir, "--teams", "1@alpha")
	write(t, filepath.Join(top, ".claude/ccy/pingbus/sessions/cc-proj", File), recorded("cc-proj", id))
	tdir, err := transcript.Dir("cc", dir, home)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(tdir, id+".jsonl"), "{}\n")
	if got, forErr := For(rec, home); forErr != nil || got != id {
		t.Fatalf("For = %q, %v", got, forErr)
	}
	if got, forErr := For(record("cc-proj", "cc", dir, "--bus"), home); forErr != nil || got != id {
		t.Fatalf("cc --bus: For = %q, %v", got, forErr)
	}
}

func TestForRefusesACCSessionOutsideAGitCheckout(t *testing.T) {
	mustFail(t, record("cc-proj", "cc", t.TempDir(), "--bus"), t.TempDir(), "git checkout")
}

// Fable's review of the spec (the M6 B1 class): a launch without the bus records nothing, so a
// file under its reused name is an earlier session's, and resuming it would resume the wrong one.
func TestForRefusesARecordWhoseLaunchCarriedNoBus(t *testing.T) {
	mustFail(t, ccyCase(t, "--no-bus"), "/nonexistent", "without the agent team bus")
	top := t.TempDir()
	if err := os.Mkdir(filepath.Join(top, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(top, ".claude/ccy/pingbus/sessions/cc-proj", File), recorded("cc-proj", id))
	mustFail(t, record("cc-proj", "cc", top, "--model", "opus"), t.TempDir(), "without the agent team bus")
}

func TestForRefusesWhatIsNotTheHooksRecord(t *testing.T) {
	cases := map[string]string{
		"an unknown key":     `{"v":1,"session":"ccy-proj","session_id":"` + id + `","source":"clear","written_ms":1,"x":1}`,
		"another version":    `{"v":2,"session":"ccy-proj","session_id":"` + id + `","source":"clear","written_ms":1}`,
		"another session":    recorded("ccy-other", id),
		"an id not a UUID":   recorded("ccy-proj", "d2dae64d"),
		"an unknown source":  `{"v":1,"session":"ccy-proj","session_id":"` + id + `","source":"fork","written_ms":1}`,
		"no time":            `{"v":1,"session":"ccy-proj","session_id":"` + id + `","source":"clear"}`,
		"not one object":     recorded("ccy-proj", id) + recorded("ccy-proj", id),
		"not JSON":           "not json",
		"a negative written": `{"v":1,"session":"ccy-proj","session_id":"` + id + `","source":"clear","written_ms":-1}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			rec := ccyCase(t)
			write(t, recordPath(rec.Dir), content)
			mustFail(t, rec, "/nonexistent", File)
		})
	}
}

func TestForNamesAMissingRecord(t *testing.T) {
	rec := ccyCase(t)
	if err := os.Remove(recordPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	mustFail(t, rec, "/nonexistent", "no conversation is recorded", File)
}

func TestForRefusesASymlinkedRecord(t *testing.T) {
	rec := ccyCase(t)
	elsewhere := filepath.Join(t.TempDir(), "x.json")
	write(t, elsewhere, recorded("ccy-proj", id))
	if err := os.Remove(recordPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, recordPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	mustFail(t, rec, "/nonexistent", "not a regular file")
}

// F4: several sessions share one transcript directory, so the transcript is the recorded
// id's, never the newest there.
func TestForNeedsTheRecordedIDsTranscriptNotTheNewest(t *testing.T) {
	rec := ccyCase(t)
	dir := filepath.Join(rec.Dir, ".claude/ccy/projects/-workspace")
	if err := os.Remove(filepath.Join(dir, id+".jsonl")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, otherID+".jsonl"), "{}\n")
	mustFail(t, rec, "/nonexistent", id)
}

func TestClaudeHome(t *testing.T) {
	env := map[string]string{"HOME": "/home/u"}
	if got := ClaudeHome(func(k string) string { return env[k] }); got != "/home/u/.claude" {
		t.Fatalf("got %q", got)
	}
	env["CLAUDE_CONFIG_DIR"] = "/cfg"
	if got := ClaudeHome(func(k string) string { return env[k] }); got != "/cfg" {
		t.Fatalf("got %q", got)
	}
}
