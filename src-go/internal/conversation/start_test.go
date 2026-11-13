package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

func transcriptPath(rec string) string {
	return filepath.Join(rec, ".claude/ccy/projects/-workspace", id+".jsonl")
}

// A recorded conversation with its transcript is resumed, by its id.
func TestStartPlanResumesTheRecordedConversation(t *testing.T) {
	rec := ccyCase(t, "--teams", "1@alpha")
	got, err := StartPlan(rec, "/nonexistent")
	if err != nil || got.Fresh || got.ID != id {
		t.Fatalf("StartPlan = %+v, %v", got, err)
	}
	if !slices.Equal(got.Args[len(got.Args)-2:], []string{"--resume", id}) {
		t.Fatalf("args %q do not resume %s", got.Args, id)
	}
}

// Plan 00135 Phase 13: a record marked fresh-if-no-conversation whose hook recorded nothing is
// started fresh, with no conversation selector.
func TestStartPlanStartsAMarkedRecordWithNothingRecordedFresh(t *testing.T) {
	rec := ccyCase(t, "--teams", "1@alpha")
	rec.FreshIfNoConversation = true
	if err := os.Remove(recordPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	got, err := StartPlan(rec, "/nonexistent")
	if err != nil || !got.Fresh || got.ID != "" || slices.Contains(got.Args, "--resume") {
		t.Fatalf("StartPlan = %+v, %v", got, err)
	}
	if !strings.Contains(got.Why, "no conversation is recorded") {
		t.Fatalf("Why = %q", got.Why)
	}
}

// fedora-desktop#270: a session that never took a turn has a conversation.json from its
// SessionStart hook but no transcript, which Claude Code writes at the first turn. A record
// marked fresh-if-no-conversation starts fresh then too, as it does with nothing recorded.
func TestStartPlanStartsAMarkedRecordWhoseConversationHasNoTranscriptFresh(t *testing.T) {
	rec := ccyCase(t, "--teams", "1@alpha")
	rec.FreshIfNoConversation = true
	if err := os.Remove(transcriptPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	got, err := StartPlan(rec, "/nonexistent")
	if err != nil || !got.Fresh || got.ID != "" || slices.Contains(got.Args, "--resume") {
		t.Fatalf("StartPlan = %+v, %v", got, err)
	}
	if !strings.Contains(got.Why, "has no transcript") || !strings.Contains(got.Why, id) {
		t.Fatalf("Why = %q, want the conversation named and its missing transcript", got.Why)
	}
}

// Without the mark, neither form of "no conversation" starts anything: Decision 10, by the
// recorded id or not at all.
func TestStartPlanRefusesAnUnmarkedRecordWithNoConversation(t *testing.T) {
	noTranscript, nothing := ccyCase(t), ccyCase(t)
	if err := os.Remove(transcriptPath(noTranscript.Dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recordPath(nothing.Dir)); err != nil {
		t.Fatal(err)
	}
	for says, rec := range map[string]registry.Record{"has no transcript": noTranscript, "no conversation is recorded": nothing} {
		if _, err := StartPlan(rec, "/nonexistent"); err == nil || !strings.Contains(err.Error(), says) || isArgsError(err) {
			t.Errorf("%s: %v", says, err)
		}
	}
}

// The mark covers no conversation, nothing else: a launch without the bus, or a file that is
// not the hook's, is refused whatever the mark.
func TestStartPlanRefusesAMarkedRecordForAnythingElse(t *testing.T) {
	notTheHooks := ccyCase(t)
	write(t, recordPath(notTheHooks.Dir), "not json")
	for name, rec := range map[string]registry.Record{"no bus": ccyCase(t, "--no-bus"), "not the hook's": notTheHooks} {
		rec.FreshIfNoConversation = true
		if got, err := StartPlan(rec, "/nonexistent"); err == nil {
			t.Errorf("%s: StartPlan = %+v, want a refusal", name, got)
		}
	}
}

// Arguments that cannot be worked out are told apart from a missing conversation.
func TestStartPlanMarksUnworkableArguments(t *testing.T) {
	rec := ccyCase(t)
	rec.Compose = "maybe"
	if _, err := StartPlan(rec, "/nonexistent"); !isArgsError(err) || !strings.Contains(err.Error(), "maybe") {
		t.Fatalf("resume: %v", err)
	}
	rec.FreshIfNoConversation = true
	if err := os.Remove(recordPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartPlan(rec, "/nonexistent"); !isArgsError(err) {
		t.Fatalf("fresh: %v", err)
	}
}

func isArgsError(err error) bool {
	var argsErr *ArgsError
	return errors.As(err, &argsErr)
}
