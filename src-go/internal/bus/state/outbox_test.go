package state

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func eventN(i int) string { return fmt.Sprintf("$%043d", i) }

func ping(id string, to ...string) Ping {
	return Ping{EventID: id, Verb: "review", To: to, SentMS: 10}
}

// S27: an absent outbox is empty; a transaction that changes it saves v1's bytes, with settled
// pings dropped; one that fails or changes nothing writes nothing.
func TestWithOutboxSavesOnlyAChangedOutbox(t *testing.T) {
	team := newTeam(t)
	ref := "abc"
	must(t, team.WithOutbox(func(tx *OutboxTx) error {
		expectf(t, len(tx.Outbox.Pings) == 0 && tx.Outbox.Gate == nil, "a fresh outbox %+v", tx.Outbox)
		p := ping(testEvent, "@b:s", "@a:s")
		p.Ref = &ref
		settled := ping(eventN(1), "@a:s")
		settled.Answered = []string{"@a:s"}
		tx.Outbox.Pings = append(tx.Outbox.Pings, p, settled)
		tx.Outbox.Gate = &Gate{Credit: 60000, AtMS: 5, Recent: []Recent{{Key: "k2", AtMS: 4}, {Key: "k1", AtMS: 3}}}
		return nil
	}))
	want := `{"v":1,"pings":[{"event_id":"` + testEvent + `","verb":"review","ref":"abc","to":["@b:s","@a:s"],"sent_ms":10,"answered":[],"reported":[]}],"gate":{"bucket":{"credit":60000,"at_ms":5},"recent":{"k2":4,"k1":3}}}`
	got := readBack(t, team.outboxPath())
	expectf(t, got == want, "outbox.json %s", got)
	must(t, os.Chmod(team.outboxPath(), 0o400))
	must(t, team.WithOutbox(func(tx *OutboxTx) error {
		p := tx.Outbox.Pings[0]
		expectf(t, len(tx.Outbox.Pings) == 1 && *p.Ref == ref && p.To[0] == "@b:s" && tx.Outbox.Gate.Recent[0].Key == "k2", "read back %+v", tx.Outbox)
		return nil
	}))
	info, err := os.Stat(team.outboxPath())
	expectf(t, err == nil && info.Mode().Perm() == 0o400, "an unchanged outbox was rewritten: %v", err)
	boom := errors.New("boom")
	err = team.WithOutbox(func(tx *OutboxTx) error {
		tx.Outbox.Pings = nil
		return boom
	})
	expectf(t, errors.Is(err, boom), "the transaction's error: %v", err)
	expectf(t, readBack(t, team.outboxPath()) == want, "a failed transaction saved")
}

// S27, SB4: the writer refuses what the reader refuses, and the reader what v1 does not write.
func TestOutboxRefusesWhatV1DoesNotWrite(t *testing.T) {
	team := newTeam(t)
	bad := map[string]Ping{
		"a bad event ID":     ping("x", "@a:s"),
		"no target":          ping(testEvent),
		"a target twice":     ping(testEvent, "@a:s", "@a:s"),
		"an answer not a to": {EventID: testEvent, Verb: "review", To: []string{"@a:s"}, Answered: []string{"@b:s"}},
		"a verb with no ack": {EventID: testEvent, Verb: "say", To: []string{"@a:s"}},
	}
	for name, p := range bad {
		err := team.WithOutbox(func(tx *OutboxTx) error {
			tx.Outbox.Pings = []Ping{p}
			return nil
		})
		wantClass(t, name, err, ClassGrammar)
	}
	err := team.WithOutbox(func(tx *OutboxTx) error {
		tx.Outbox.Gate = &Gate{Credit: -1}
		return nil
	})
	wantClass(t, "a negative credit", err, ClassGrammar)
	entry := `{"event_id":"` + testEvent + `","verb":"review","ref":null,"to":["@a:s"],"sent_ms":1,"answered":[],"reported":[]`
	cases := map[string]struct {
		text string
		want Class
	}{
		"a list":            {`[]`, ClassType},
		"no gate":           {`{"v":1,"pings":[]}`, ClassMissingKey},
		"pings an object":   {`{"v":1,"pings":{},"gate":null}`, ClassType},
		"a ping a list":     {`{"v":1,"pings":[[]],"gate":null}`, ClassType},
		"a ping extra key":  {`{"v":1,"pings":[` + entry + `,"x":1}],"gate":null}`, ClassUnknownKey},
		"a ref of 1":        {`{"v":1,"pings":[` + strings.Replace(entry, `"ref":null`, `"ref":1`, 1) + `}],"gate":null}`, ClassType},
		"a verb of 1":       {`{"v":1,"pings":[` + strings.Replace(entry, `"verb":"review"`, `"verb":1`, 1) + `}],"gate":null}`, ClassType},
		"a to of 1":         {`{"v":1,"pings":[` + strings.Replace(entry, `["@a:s"]`, `[1]`, 1) + `}],"gate":null}`, ClassType},
		"sent_ms true":      {`{"v":1,"pings":[` + strings.Replace(entry, `"sent_ms":1`, `"sent_ms":true`, 1) + `}],"gate":null}`, ClassType},
		"a gate a list":     {`{"v":1,"pings":[],"gate":[]}`, ClassType},
		"a gate short":      {`{"v":1,"pings":[],"gate":{"bucket":{"credit":1,"at_ms":1}}}`, ClassMissingKey},
		"a bucket short":    {`{"v":1,"pings":[],"gate":{"bucket":{"credit":1},"recent":{}}}`, ClassMissingKey},
		"a bucket a list":   {`{"v":1,"pings":[],"gate":{"bucket":[],"recent":{}}}`, ClassType},
		"at_ms a float":     {`{"v":1,"pings":[],"gate":{"bucket":{"credit":1,"at_ms":1.0},"recent":{}}}`, ClassType},
		"recent a list":     {`{"v":1,"pings":[],"gate":{"bucket":{"credit":1,"at_ms":1},"recent":[]}}`, ClassType},
		"a recent of true":  {`{"v":1,"pings":[],"gate":{"bucket":{"credit":1,"at_ms":1},"recent":{"k":true}}}`, ClassType},
		"version 2":         {`{"v":2,"pings":[],"gate":null}`, ClassVersion},
		"a duplicate key":   {`{"v":1,"v":1,"pings":[],"gate":null}`, ClassDuplicateKey},
		"a negative credit": {`{"v":1,"pings":[],"gate":{"bucket":{"credit":-1,"at_ms":1},"recent":{}}}`, ClassGrammar},
	}
	must(t, team.EnsureDirs())
	for name, c := range cases {
		writeRaw(t, team.outboxPath(), c.text)
		called := false
		err := team.WithOutbox(func(*OutboxTx) error {
			called = true
			return nil
		})
		wantClass(t, name, err, c.want)
		expectf(t, !called, "%s: the transaction ran on a refused outbox", name)
	}
}

// S28: the report send times are read and written inside the outbox transaction.
func TestReportTimesInsideTheOutboxTransaction(t *testing.T) {
	team := newTeam(t)
	must(t, team.WithOutbox(func(tx *OutboxTx) error {
		sent, err := tx.ReportTimes()
		expectf(t, err == nil && len(sent) == 0, "no reports %v %v", sent, err)
		return tx.SaveReportTimes([]int64{3, 7})
	}))
	expectf(t, readBack(t, team.reportsPath()) == `{"v":1,"sent":[3,7]}`, "reports.json")
	_, err := os.Lstat(team.outboxPath())
	expectf(t, os.IsNotExist(err), "an unchanged outbox was written")
	must(t, team.WithOutbox(func(tx *OutboxTx) error {
		sent, err := tx.ReportTimes()
		expectf(t, err == nil && len(sent) == 2 && sent[1] == 7, "reports %v %v", sent, err)
		return nil
	}))
	for name, text := range map[string]string{
		"sent an object": `{"v":1,"sent":{}}`,
		"a time of true": `{"v":1,"sent":[true]}`,
		"no sent":        `{"v":1}`,
	} {
		writeRaw(t, team.reportsPath(), text)
		err := team.WithOutbox(func(tx *OutboxTx) error {
			_, err := tx.ReportTimes()
			return err
		})
		expectf(t, classOf(err) != "", "%s: %v", name, err)
	}
}

// L7, L11: two transactions never interleave, even in one process.
func TestOutboxTransactionsExcludeEachOther(t *testing.T) {
	team := newTeam(t)
	const writers = 16
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Go(func() {
			errs <- team.WithOutbox(func(tx *OutboxTx) error {
				tx.Outbox.Pings = append(tx.Outbox.Pings, ping(eventN(i), "@a:s"))
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	must(t, team.WithOutbox(func(tx *OutboxTx) error {
		expectf(t, len(tx.Outbox.Pings) == writers, "a lost update: %d pings", len(tx.Outbox.Pings))
		return nil
	}))
	linked := newTeam(t)
	must(t, os.Symlink(t.TempDir(), linked.Dir))
	wantClass(t, "a linked state directory", linked.WithOutbox(func(*OutboxTx) error { return nil }), ClassFile)
}

// The send gate's duplicate key and the relay spool's TIMEOUT key, as v1 spells them.
func TestDuplicateAndTimeoutKeys(t *testing.T) {
	ref := "r" + string(rune(0xe9))
	key, err := DuplicateKey("review", &ref, nil, []string{"@b:s", "@a:s", "@b:s"})
	expectf(t, err == nil && key == `["review","r`+"\\"+`u00e9",null,["@a:s","@b:s"]]`, "%s %v", key, err)
	tk, err := TimeoutKey(testEvent, "alice.1+host.podman")
	expectf(t, err == nil && tk == testEvent+"~alice.1+host.podman", "%s %v", tk, err)
	_, err = TimeoutKey("x", "alice")
	wantClass(t, "a bad event ID", err, ClassGrammar)
	_, err = TimeoutKey(testEvent, "Alice")
	wantClass(t, "a bad target", err, ClassGrammar)
	id, target, err := SplitTimeoutKey(tk)
	expectf(t, err == nil && id == testEvent && target == "alice.1+host.podman", "%s %s %v", id, target, err)
	id, target, err = SplitTimeoutKey(testEvent)
	expectf(t, err == nil && id == testEvent && target == "", "%s %s %v", id, target, err)
	for _, name := range []string{"x", testEvent + "~", testEvent + "~A"} {
		_, _, err = SplitTimeoutKey(name)
		wantClass(t, name, err, ClassGrammar)
	}
}
