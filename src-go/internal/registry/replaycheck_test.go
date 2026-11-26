package registry

import (
	"errors"
	"strings"
	"testing"
)

// Plan 00266 Task A.5: a restore replays a record's words only when they are words a launch of
// its prefix records. cc forwards every word to claude on the host, so a cc record may hold only
// cc's own --bus and --teams; a ccy record may not hold a flag that acts on the host and that
// no launch records (the drop classes), nor a word no launch records in its place.
func TestReplayRefusalAllowsWhatALaunchRecords(t *testing.T) {
	for _, rec := range []Record{
		{Prefix: "cc", Args: []string{"--bus"}},
		{Prefix: "cc", Args: []string{"--teams", "1@beta"}},
		{Prefix: "cc", Args: []string{"--teams=1@beta,2@alpha", "--resume", "0a1b"}},
		{Prefix: "cc", Args: []string{"-c"}},
		{Prefix: "cc", Args: []string{"--model", "opus", "--verbose", "--model=sonnet", "--bus"}},
		{Prefix: "cc"},
		{Prefix: "ccy", Args: []string{"--token", "work", "--teams", "2@lts", "--no-ssh", "--supervise"}},
		{Prefix: "ccy", Args: []string{"--model", "opus", "--verbose"}},
		{Prefix: "ccy", Args: []string{"--teams", "1@alpha", "--", "--model", "opus", "anything"}},
		{Prefix: "ccy", Args: []string{"--token", "-c", "--resume", "0a1b-old"}},
		{Prefix: "ccy", Args: []string{"--network", "proj_default", "--github-443", "--teams=1@a"}},
	} {
		if err := ReplayRefusal(rec); err != nil {
			t.Errorf("%s %q: refused: %v", rec.Prefix, rec.Args, err)
		}
	}
}

func TestReplayRefusalNamesTheWord(t *testing.T) {
	for _, c := range []struct {
		rec  Record
		word string
	}{
		{Record{Prefix: "cc", Args: []string{"--bus", "--mcp-config", "/tmp/x.json"}}, "--mcp-config"},
		{Record{Prefix: "cc", Args: []string{"--settings", "/tmp/s.json"}}, "--settings"},
		{Record{Prefix: "cc", Args: []string{"--", "--model", "opus"}}, "--"},
		{Record{Prefix: "cc", Args: []string{"do this"}}, "do this"},
		{Record{Prefix: "cc", Args: []string{"--teams"}}, "--teams"},
		{Record{Prefix: "cc", Args: []string{"--model"}}, "--model"},
		{Record{Prefix: "cc", Args: []string{"--token", "-c"}}, "--token"},
		{Record{Prefix: "cc", Args: []string{"--permission-mode", "bypassPermissions"}}, "--permission-mode"},
		{Record{Prefix: "cc", Args: []string{"--append-system-prompt", "x"}}, "--append-system-prompt"},
		{Record{Prefix: "ccy", Args: []string{"--export-token", "work"}}, "--export-token"},
		{Record{Prefix: "ccy", Args: []string{"--custom-docker"}}, "--custom-docker"},
		{Record{Prefix: "ccy", Args: []string{"--remove-network"}}, "--remove-network"},
		{Record{Prefix: "ccy", Args: []string{"--compose", "start"}}, "--compose"},
		{Record{Prefix: "ccy", Args: []string{"--token", "work", "hello"}}, "hello"},
		{Record{Prefix: "ccy", Args: []string{"--ssh-key"}}, "--ssh-key"},
	} {
		err := ReplayRefusal(c.rec)
		var refused *ReplayWordError
		if !errors.As(err, &refused) {
			t.Fatalf("%s %q: err = %v; want a ReplayWordError", c.rec.Prefix, c.rec.Args, err)
		}
		if refused.Word != c.word || !strings.Contains(err.Error(), "'"+c.word+"'") {
			t.Errorf("%s %q: refused %q (%v); want %q named", c.rec.Prefix, c.rec.Args, refused.Word, err, c.word)
		}
	}
}

// The builders of a restore's arguments refuse a refused word themselves, so no caller can
// replay one unchecked (ResumeArgs and FreshArgs are the only way to build them).
func TestStartArgumentsRefuseARefusedWord(t *testing.T) {
	args := []string{"--bus", "--settings", "/tmp/s.json"}
	var refused *ReplayWordError
	if _, err := ResumeArgs("", "cc", args, "11111111-2222-4333-8444-555555555555"); !errors.As(err, &refused) {
		t.Fatalf("ResumeArgs err = %v; want a ReplayWordError", err)
	}
	if _, err := FreshArgs("", "cc", args); !errors.As(err, &refused) {
		t.Fatalf("FreshArgs err = %v; want a ReplayWordError", err)
	}
}

// The selectors a restore replaces are judged after they are removed, as startArgs removes them,
// so a recorded --resume of a cc session is not a refusal but its value is never a word either.
func TestReplayRefusalJudgesTheArgumentsWithoutSelectors(t *testing.T) {
	rec := Record{Prefix: "cc", Args: []string{"--resume", "0a1b-old", "--bus", "--session-id", "x"}}
	if err := ReplayRefusal(rec); err != nil {
		t.Fatalf("refused: %v", err)
	}
}
