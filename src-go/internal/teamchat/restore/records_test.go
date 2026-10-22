package restore

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// Records is the health tick's Restores (HB2).
var _ health.Restores = Records{}

func recordAt(t *testing.T, root string, r Record) {
	t.Helper()
	writeFile(t, root+paths.RestoreRecord(r.Team), string(EncodeRecord(r)))
}

func minimalRecord(t *testing.T, step Step) Record {
	t.Helper()
	return Record{Team: opsTeam(t), Backup: 7, Step: step, Roster: apply.NewRosterFile(opsTeam(t))}
}

// HB2: with no restore.json there is no restore and no problem.
func TestRecordsWithNoRecord(t *testing.T) {
	root := teamDir(t, opsTeam(t))
	got, err := Records{Root: root, OwnerUID: myUID(t)}.State(context.Background(), opsTeam(t))
	if err != nil || got.Restore != nil || got.Problem != (status.Problem{}) {
		t.Fatalf("State = %+v, %v", got, err)
	}
}

// HB2: the step, the backup and pre (0 for null), with staged's problem and every other step's.
func TestRecordsGiveTheStepAndItsProblem(t *testing.T) {
	resume := "teamchat-admin restore --team ops --resume"
	cases := map[Step]status.Problem{
		StepStaged: {What: "restore staged", Fix: resume + ", or teamchat-admin restore --team ops --discard"},
	}
	for _, s := range []Step{StepStaging, StepSwap, StepStart, StepClean, StepRevert} {
		cases[s] = status.Problem{What: "restore interrupted", Fix: resume}
	}
	for step, want := range cases {
		root := teamDir(t, opsTeam(t))
		r := minimalRecord(t, step)
		if step == StepStart {
			r.Pre = 9
		}
		recordAt(t, root, r)
		got, err := Records{Root: root, OwnerUID: myUID(t)}.State(context.Background(), opsTeam(t))
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if got.Restore == nil || *got.Restore != (status.Restore{Step: string(step), Backup: 7, Pre: r.Pre}) || got.Problem != want {
			t.Errorf("%s: State = %+v %+v, want %+v", step, got.Restore, got.Problem, want)
		}
	}
}

// HB2: an offline record adds "(offline)" to the problem.
func TestRecordsNameAnOfflineRestore(t *testing.T) {
	for step, want := range map[Step]string{StepStaged: "restore staged (offline)", StepSwap: "restore interrupted (offline)"} {
		root := teamDir(t, opsTeam(t))
		r := minimalRecord(t, step)
		r.Offline = true
		recordAt(t, root, r)
		got, err := Records{Root: root, OwnerUID: myUID(t)}.State(context.Background(), opsTeam(t))
		if err != nil || got.Problem.What != want {
			t.Errorf("%s: State = %+v, %v", step, got, err)
		}
	}
}

// HB2: a record that cannot be read by the trust rule, or that does not decode, is an error.
func TestRecordsRefuseAnUnreadableRecord(t *testing.T) {
	ops := opsTeam(t)
	for name, plant := range map[string]func(root string){
		"not json": func(root string) { writeFile(t, root+paths.RestoreRecord(ops), "{") },
		"a link": func(root string) {
			writeFile(t, root+"/elsewhere.json", string(EncodeRecord(minimalRecord(t, StepStaged))))
			if err := os.Symlink(root+"/elsewhere.json", root+paths.RestoreRecord(ops)); err != nil {
				t.Fatal(err)
			}
		},
		"a directory": func(root string) { mkdirs(t, root+paths.RestoreRecord(ops)) },
	} {
		root := teamDir(t, ops)
		plant(root)
		if got, err := (Records{Root: root, OwnerUID: myUID(t)}).State(context.Background(), ops); err == nil {
			t.Errorf("%s: State = %+v, want an error", name, got)
		}
	}
	root := teamDir(t, ops)
	recordAt(t, root, minimalRecord(t, StepStaged))
	_, err := Records{Root: root, OwnerUID: myUID(t) + 1}.State(context.Background(), ops)
	if err == nil || !strings.Contains(err.Error(), "restore.json") {
		t.Errorf("another owner's record = %v", err)
	}
}
