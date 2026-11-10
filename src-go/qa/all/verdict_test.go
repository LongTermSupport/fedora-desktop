package all

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
)

// The verdict the pre-push gate reads (issue #187): every finished run records its status, the
// commit it judged and when it ran, apart from the printed text.
func TestAFinishedRunRecordsItsVerdictOnTheCommitItJudged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   bool
		code   ExitCode
		status string
	}{
		{"pass", false, ExitOK, qaverdict.StatusPass},
		{"fail", true, ExitFailed, qaverdict.StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, smallTable)
			passing(w)
			if tc.fail {
				w.runner.replies["test-a.bash"] = reply{code: 1}
			}
			if code := w.main(context.Background()); code != tc.code {
				t.Fatalf("exit %d: %s", code, w.errOut.String())
			}
			got, err := qaverdict.Read(w.verdictPath())
			if err != nil {
				t.Fatal(err)
			}
			want := qaverdict.Record{Status: tc.status, Commit: worldHead, Started: worldClock[0], Finished: worldClock[1]}
			if got != want {
				t.Fatalf("verdict %+v, want %+v", got, want)
			}
		})
	}
}

func TestARunThatEndsWithoutAVerdictLeavesNoneBehind(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	if err := qaverdict.Write(w.verdictPath(), qaverdict.Record{Status: qaverdict.StatusPass, Commit: worldHead, Started: worldClock[0], Finished: worldClock[0]}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.runner.replies["test-a.bash"] = reply{onRun: cancel, code: -1}
	if code := w.main(ctx); code != ExitInterrupted {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(w.verdictPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an interrupted run left the last run's verdict to be read as its own: %v", err)
	}
}

func TestARunThatCannotNameItsCommitRunsNothing(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.locateErr = errors.New("not a git repository")
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "not a git repository") {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
	if len(w.runner.log) != 0 {
		t.Fatalf("ran %v", w.runner.log)
	}
}

func TestAVerdictThatCannotBeWrittenIsExitTwo(t *testing.T) {
	w := newWorld(t, smallTable)
	passing(w)
	w.verdictAt = filepath.Join(w.root, "no", "such", "dir", qaverdict.FileName)
	if code := w.main(context.Background()); code != ExitCannotRun || !strings.Contains(w.errOut.String(), "verdict") {
		t.Fatalf("exit %d, stderr %q", code, w.errOut.String())
	}
}

// worldHead and worldClock are what a fake world's checkout and clock answer: the commit, then
// the run's start and its end.
const worldHead = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"

var worldClock = []time.Time{time.Unix(1000, 0).UTC(), time.Unix(1600, 0).UTC()}
