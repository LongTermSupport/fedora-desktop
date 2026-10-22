package admin

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// RS8, RS9: an answer is printed when the restore ran, or beside the failure that marks it
// Printed; any other failure has none.
func TestEngineAnswered(t *testing.T) {
	team := alpha(t)
	done := restore.Answer{Team: team, Backup: 7}
	if out, err := answered(done, nil); err != nil || string(out) != string(done.Encode()) {
		t.Fatalf("done = %q, %v", out, err)
	}
	cleanUp := errors.New("restored; clean-up failed")
	printed := restore.Answer{Team: team, Backup: 7, Printed: true}
	if out, err := answered(printed, cleanUp); !errors.Is(err, cleanUp) || string(out) != string(printed.Encode()) {
		t.Fatalf("printed = %q, %v", out, err)
	}
	if out, err := answered(done, cleanUp); !errors.Is(err, cleanUp) || out != nil {
		t.Fatalf("an unprinted answer beside a failure = %q, %v", out, err)
	}
}

// RW1: the Engine is restore.Run, Resume and Discard over its Deps, under the verb's lock; with
// nothing recorded, a resume and a discard are refused (70) with no answer.
func TestEngineRunsTheRestore(t *testing.T) {
	root := t.TempDir()
	uid := selfUID(t)
	e := Engine{Deps: restore.Deps{Root: root, StateOwnerUID: uid, FS: restore.OS{Root: root, StateOwnerUID: uid}}}
	if out, err := e.Resume(t.Context(), nil, alpha(t)); out != nil || ExitCodeOf(err) != ExitRefused {
		t.Fatalf("Resume = %q, %v", out, err)
	}
	if out, err := e.Discard(t.Context(), nil, alpha(t)); out != nil || ExitCodeOf(err) != ExitRefused {
		t.Fatalf("Discard = %q, %v", out, err)
	}
	if out, err := e.Run(t.Context(), nil, RestoreRequest{Team: alpha(t), Backup: 0}); out != nil || err == nil {
		t.Fatalf("Run of backup 0 = %q, %v", out, err)
	}
}
