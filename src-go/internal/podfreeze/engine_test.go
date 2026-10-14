package podfreeze

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// ---- load_inventory: states, name pattern, label pass (TP 34) ----

func TestLoadKeepsOnlyRunningAndPausedInPodmansOrder(t *testing.T) {
	ps := `zulu_yolo|paused|podman
mike|running|none
alpha_yolo_2|paused|podman, appnet
gone_thing|exited|podman
never_started|created|podman
kilo|running|appnet
nonet|running|
`
	_, inv := load(t, ps, "", "")
	// gone_thing and never_started sit BETWEEN real entries, so a filter that stopped at the
	// first rejection would truncate the inventory.
	eqStrings(t, "names", inv.Names(), []string{"zulu_yolo", "mike", "alpha_yolo_2", "kilo", "nonet"})
	states := make([]string, 0, inv.Len())
	for _, e := range inv.Entries() {
		states = append(states, e.State)
	}
	eqStrings(t, "states", states, []string{"paused", "running", "paused", "running", "running"})
	if got := recordFor(t, inv, "alpha_yolo_2").nets; got != "podman, appnet" {
		t.Fatalf("the networks field is stored verbatim, got %q", got)
	}
	if got := recordFor(t, inv, "nonet").nets; got != "none" {
		t.Fatalf("an empty networks field becomes 'none', got %q", got)
	}
}

func TestLoadMarksCCYByNamePattern(t *testing.T) {
	cases := map[string]bool{
		"proj_yolo": true, "proj_yolo_2": true, "proj_browser": true, "proj_browser_10": true,
		// The pattern needs a project prefix, so the bare word and a leading underscore are not
		// sessions; the CCY column is what a person reads before freezing something.
		"yolo": false, "_yolo": false, "proj_yolo_x": false, "proj_yoloo": false,
		"proj_browser_": false, "plain": false,
	}
	var ps strings.Builder
	for name := range cases {
		ps.WriteString(name + "|running|none\n")
	}
	_, inv := load(t, ps.String(), "", "")
	for name, want := range cases {
		if got := recordFor(t, inv, name).ccy; got != want {
			t.Errorf("%s: ccy %v, want %v", name, got, want)
		}
	}
}

func TestLoadReadsTheLabelPassAndWhatNoneMeans(t *testing.T) {
	ps := `zulu_yolo|paused|podman
mike|running|none
alpha_yolo_2|paused|podman, appnet
echo_yolo_3|paused|none
plain_labelled|running|none
`
	labels := `alpha_yolo_2|octo|work|key_alpha key_shared
echo_yolo_3|none|none|none
plain_labelled|hubot|none|none
exited_yolo|octo|work|key_shared
`
	_, inv := load(t, ps, labels, "")
	alpha := recordFor(t, inv, "alpha_yolo_2")
	if alpha.github != "octo" || alpha.token != "work" || alpha.sshKeys != "key_alpha key_shared" {
		t.Fatalf("identity read off the labels, a multi-key label kept whole: %+v", alpha)
	}
	// "none" is CCY saying the axis does not apply; it is dropped, not offered as a group.
	echo := recordFor(t, inv, "echo_yolo_3")
	if echo.github != "" || echo.token != "" || echo.sshKeys != "" {
		t.Fatalf("'none' must not be stored as a value: %+v", echo)
	}
	// But the SESSION is still recorded as labelled, which is what the blind-spot note turns on.
	if !echo.labelled {
		t.Fatal("a session with no identity is still labelled")
	}
	if recordFor(t, inv, "zulu_yolo").labelled || recordFor(t, inv, "mike").labelled {
		t.Fatal("a session predating the labels, and an ordinary container, are not labelled")
	}
	// The run-time label is authoritative over the name pattern.
	if !recordFor(t, inv, "plain_labelled").ccy {
		t.Fatal("the label marks a session the name would not")
	}
	// The label query is --all, so it can name a container the inventory does not hold
	// (adapted: the Go record lives on inventory entries, so it is simply not there).
	if _, ok := inv.Entry("exited_yolo"); ok {
		t.Fatal("a labelled exited container must not enter the inventory")
	}
	eqStrings(t, "not in any selection", ccyNames(inv), []string{"zulu_yolo", "alpha_yolo_2", "echo_yolo_3", "plain_labelled"})
}

func TestLoadRefusesALabelValueHoldingTheFieldSeparator(t *testing.T) {
	ps := "alpha_yolo_2|running|none\n"
	e := newEngine(scripted(ps, "alpha_yolo_2|octo|work|key"+fieldSep+"odd\n", ""))
	_, err := e.Load(context.Background())
	msg := dieText(t, err)
	for _, want := range []string{"alpha_yolo_2", fieldSep, "relaunch"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the message should mention %q: %s", want, msg)
		}
	}
	// A record with exactly the four expected fields is NOT flagged, or the guard above would
	// pass against a loader that refused everything.
	e = newEngine(scripted(ps, "alpha_yolo_2|octo|work|key_shared\n", ""))
	if _, err := e.Load(context.Background()); err != nil {
		t.Fatalf("a well-formed record is accepted: %v", err)
	}
}

// Bug 14: a trailing separator left the fifth field empty, so the bash read it as four and
// silently mis-split. The record is split into exactly four fields and any other count refused.
func TestLoadRefusesATrailingSeparatorAndAShortRecord(t *testing.T) {
	ps := "alpha_yolo_2|running|none\n"
	for name, record := range map[string]string{
		"a trailing separator": "alpha_yolo_2|octo|work|key|\n",
		"an empty fifth field": "alpha_yolo_2|octo|work|a||b\n",
	} {
		e := newEngine(scripted(ps, record, ""))
		_, err := e.Load(context.Background())
		if msg := dieText(t, err); !strings.Contains(msg, "alpha_yolo_2") || !strings.Contains(msg, "relaunch") {
			t.Errorf("%s: %s", name, msg)
		}
	}
	e := newEngine(scripted(ps, "alpha_yolo_2|octo|work\n", ""))
	_, err := e.Load(context.Background())
	msg := dieText(t, err)
	if !strings.Contains(msg, "alpha_yolo_2") || !strings.Contains(msg, "3 of 4") {
		t.Fatalf("a short record names the session and the count: %s", msg)
	}
}

func TestLoadFailedQueriesAreFatalAndReportWhatPodmanSaid(t *testing.T) {
	f := scripted("", "", "")
	f.On(psPrefix, execx.Result{Stderr: []byte("Cannot connect to Podman socket\n"), ExitCode: 1})
	e := newEngine(f)
	_, err := e.Load(context.Background())
	msg := dieText(t, err)
	if !strings.HasPrefix(msg, "podman ps failed: ") || !strings.Contains(msg, "Cannot connect to Podman socket") {
		t.Fatalf("got %q", msg)
	}

	f = scripted("alpha_yolo_2|running|none\n", "", "")
	f.On(labelPrefix, execx.Result{Stderr: []byte("permission denied reading labels\n"), ExitCode: 1})
	e = newEngine(f)
	_, err = e.Load(context.Background())
	// Falling through here would leave every session looking unlabelled, which is a different
	// and wrong story: "relaunch them all to get labels".
	msg = dieText(t, err)
	if !strings.HasPrefix(msg, "podman ps --filter label=ccy=true failed: ") || !strings.Contains(msg, "permission denied reading labels") {
		t.Fatalf("got %q", msg)
	}
}

func TestNewLoadFailureFallsBackToTheStdoutAndTheErrorText(t *testing.T) {
	f := scripted("", "", "")
	f.On(psPrefix, execx.Result{Stdout: []byte("only stdout\n"), ExitCode: 125})
	e := newEngine(f)
	if _, err := e.Load(context.Background()); !strings.Contains(dieText(t, err), "only stdout") {
		t.Fatal("a failure that spoke on stdout still reports it")
	}
	f.On(psPrefix, execx.Result{ExitCode: 125})
	if _, err := e.Load(context.Background()); !strings.Contains(dieText(t, err), "podman exited 125") {
		t.Fatal("a failure that said nothing reports the exit")
	}
	f.OnError(psPrefix, &execx.NotInstalledError{Name: "podman"})
	if _, err := e.Load(context.Background()); !strings.Contains(dieText(t, err), "podman is not installed") {
		t.Fatal("a podman that cannot start is named")
	}
}

// ---- bug 1: stderr is never parsed as records ----

func TestNewStderrNoiseOnSuccessIsNotARecord(t *testing.T) {
	f := scripted(fixturePS, fixtureLabels, "")
	f.On(psPrefix, execx.Result{
		Stdout: []byte(fixturePS),
		Stderr: []byte("WARN[0000] cgroup v1 is deprecated\n"),
	})
	f.On(labelPrefix, execx.Result{
		Stdout: []byte(fixtureLabels),
		Stderr: []byte("WARN[0000] another|warning|with|pipes\n"),
	})
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatalf("a warning on stderr must not become a record or a failure: %v", err)
	}
	if inv.Len() != 6 {
		t.Fatalf("got %v", inv.Names())
	}
	for _, c := range f.Calls() {
		if c.Stdout != nil || c.Stderr != nil {
			t.Fatalf("the streams must be captured separately, not given a shared writer: %+v", c)
		}
	}
}

func TestNewLoadRunsExactlyTheTwoQueriesEveryTime(t *testing.T) {
	f := scripted(fixturePS, fixtureLabels, "")
	e := newEngine(f)
	for range 2 {
		if _, err := e.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	lines := callLines(f)
	one := []string{
		"podman ps --all --format " + psFormat,
		"podman ps --all --filter label=ccy=true --format " + labelFormat,
	}
	eqStrings(t, "argv", lines, append(append([]string{}, one...), one...))
}

func TestNewAWarningLineOnStdoutIsDropped(t *testing.T) {
	_, inv := load(t, "a warning with no separator\n\nmike|running|none\n|running|none\n", "", "")
	eqStrings(t, "names", inv.Names(), []string{"mike"})
}

// ---- table columns and the CCY marker (TP ccy_marker 4) ----

func TestColumnsAndRowsArePaddedToTheLastColumn(t *testing.T) {
	e, inv := fixture(t)
	if got := e.Columns(); got != "CCY  NETWORKS              " {
		t.Fatalf("header %q", got)
	}
	if got := e.Row(entryOf(t, inv, "alpha_yolo_2")); got != "CCY  podman, appnet        " {
		t.Fatalf("row %q", got)
	}
	if got := e.Row(entryOf(t, inv, "mike")); got != "-    none                  " {
		t.Fatalf("row %q", got)
	}
}

func TestCCYMarker(t *testing.T) {
	e, inv := fixture(t)
	for name, want := range map[string]string{
		"alpha_yolo_2": "CCY", // a labelled session
		"zulu_yolo":    "CCY", // a legacy named session
		"mike":         "-",   // an ordinary container
	} {
		if got := strings.Fields(e.Row(entryOf(t, inv, name)))[0]; got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	// A name with no record at all defaults to not-CCY rather than failing: the marker is drawn
	// beside a row, so a missing record must not take the whole table down.
	if got := strings.Fields(e.Row(freeze.Entry{Name: "nosuch"}))[0]; got != "-" {
		t.Fatalf("a name the engine never saw: %q", got)
	}
}

// ---- preflight and act ----

func TestPreflightNamesWhatToDoWhenPodmanIsMissing(t *testing.T) {
	e := newEngine(&execx.Fake{})
	if err := e.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	want := "podman is not installed.\n" +
		"  It is declared in playbooks/imports/play-podman.yml. Deploy it with:\n" +
		"    ansible-playbook playbooks/imports/play-podman.yml\n" +
		"  Do NOT install it by hand."
	if got := dieText(t, e.Preflight(context.Background())); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestActPausesAndUnpausesOneContainer(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"podman", "pause", "mike"}, execx.Result{Stdout: []byte("mike\n")})
	f.On([]string{"podman", "unpause", "mike"}, execx.Result{Stdout: []byte("mike\n")})
	e := newEngine(f)
	for _, verb := range []freeze.Verb{freeze.VerbFreeze, freeze.VerbThaw} {
		if _, err := e.Act(context.Background(), verb, "mike"); err != nil {
			t.Fatal(err)
		}
	}
	lines := callLines(f)
	eqStrings(t, "argv", lines, []string{"podman pause mike", "podman unpause mike"})
}

func TestActReturnsPodmansOwnWordsOnFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"podman", "pause", "mike"}, execx.Result{Stderr: []byte("Error: cgroup freezer unavailable\n"), ExitCode: 125})
	e := newEngine(f)
	out, err := e.Act(context.Background(), freeze.VerbFreeze, "mike")
	if err == nil || !strings.Contains(out, "cgroup freezer unavailable") {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestActRefusesAnUnresolvedVerb(t *testing.T) {
	e := newEngine(&execx.Fake{})
	if _, err := e.Act(context.Background(), freeze.VerbDerived, "mike"); err == nil {
		t.Fatal("the library resolves the derived verb before acting; the engine must not guess")
	}
}

func TestConfigIsValidAndUsesThePodmanWords(t *testing.T) {
	cfg := Config()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Tool != "podfreeze" || cfg.StateRunning != "running" || cfg.StateFrozen != "paused" {
		t.Fatalf("%+v", cfg)
	}
}

// ---- constants (TP 4) ----

func TestConstantsTheRowsAndTheParserDependOn(t *testing.T) {
	// The all-row sentinel shares a column with container names; podman requires a name to
	// begin with an alphanumeric, so one that does not cannot collide with a real container.
	if strings.ContainsAny(freeze.AllRowKey[:1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Fatalf("%q could be a container name", freeze.AllRowKey)
	}
	if len(fieldSep) != 1 {
		t.Fatalf("the field separator is a single character: %q", fieldSep)
	}
	if strings.ContainsAny(fieldSep, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") {
		t.Fatalf("%q is legal in a container or network name", fieldSep)
	}
	if freeze.MaxTries < 2 {
		t.Fatalf("the menu retry budget must allow a recoverable mistake: %d", freeze.MaxTries)
	}
}
