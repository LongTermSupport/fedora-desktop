package firewalld

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// memRecord is a Record held in memory.
type memRecord struct {
	data    []byte
	exists  bool
	writes  int
	deleted bool
	failOn  string
}

func (m *memRecord) Read() ([]byte, bool, error) {
	if m.failOn == "read" {
		return nil, false, errors.New("cannot read")
	}
	return m.data, m.exists, nil
}

func (m *memRecord) Write(data []byte) error {
	if m.failOn == "write" {
		return errors.New("cannot write")
	}
	m.data, m.exists = append([]byte(nil), data...), true
	m.writes++
	return nil
}

func (m *memRecord) Delete() error {
	if m.failOn == "delete" {
		return errors.New("cannot delete")
	}
	m.data, m.exists, m.deleted = nil, false, true
	return nil
}

// stubFirewall is a firewall-cmd that keeps two rule sets, as the real one does.
type stubFirewall struct {
	permanent map[string]bool // "zone\trule"
	runtime   map[string]bool
	failAdd   string
}

func newStub(f *execx.Fake) *stubFirewall {
	s := &stubFirewall{permanent: map[string]bool{}, runtime: map[string]bool{}}
	f.OnFunc([]string{"firewall-cmd"}, s.answer)
	return s
}

func (s *stubFirewall) answer(c execx.Cmd) (execx.Result, error) {
	set := s.runtime
	var zone, op, rule string
	for _, a := range c.Args {
		switch {
		case a == "--permanent":
			set = s.permanent
		case strings.HasPrefix(a, "--zone="):
			zone = strings.TrimPrefix(a, "--zone=")
		case strings.HasPrefix(a, "--query-rich-rule="):
			op, rule = "query", strings.TrimPrefix(a, "--query-rich-rule=")
		case strings.HasPrefix(a, "--add-rich-rule="):
			op, rule = "add", strings.TrimPrefix(a, "--add-rich-rule=")
		case strings.HasPrefix(a, "--remove-rich-rule="):
			op, rule = "remove", strings.TrimPrefix(a, "--remove-rich-rule=")
		}
	}
	key := zone + "\t" + rule
	switch op {
	case "query":
		if set[key] {
			return execx.Result{Stdout: []byte("yes\n")}, nil
		}
		return execx.Result{Stdout: []byte("no\n"), ExitCode: 1}, nil
	case "add":
		if s.failAdd != "" && strings.Contains(rule, s.failAdd) {
			return execx.Result{Stderr: []byte("Error: COMMAND_FAILED"), ExitCode: 1}, nil
		}
		set[key] = true
	case "remove":
		delete(set, key)
	}
	return execx.Result{Stdout: []byte("success\n")}, nil
}

type fixture struct {
	fw  *Firewall
	out *strings.Builder
}

func newFixture(f *execx.Fake) fixture {
	out := &strings.Builder{}
	return fixture{fw: &Firewall{Runner: f, Chatter: &strings.Builder{}, Out: out}, out: out}
}

const (
	ruleA = `rule family="ipv4" source address="198.51.100.0/24" port port="8448" protocol="tcp" accept`
	ruleB = `rule family="ipv4" source address="203.0.113.0/24" port port="8448" protocol="tcp" accept`
)

func TestRichRuleBuildsTheFamilyFromTheCIDR(t *testing.T) {
	cases := []struct {
		cidr string
		port int
		want string
	}{
		{"198.51.100.0/24", 8448, ruleA},
		{"2001:db8::/32", 8448, `rule family="ipv6" source address="2001:db8::/32" port port="8448" protocol="tcp" accept`},
		{"192.0.2.9/32", 1, `rule family="ipv4" source address="192.0.2.9/32" port port="1" protocol="tcp" accept`},
	}
	for _, tc := range cases {
		got, err := RichRule(tc.cidr, tc.port)
		if err != nil || got != tc.want {
			t.Errorf("RichRule(%q, %d) = %q, %v; want %q", tc.cidr, tc.port, got, err, tc.want)
		}
	}
}

func TestRichRuleRefusesWhatIsNotACIDROrAPort(t *testing.T) {
	bad := []string{
		"", "198.51.100.0", `198.51.100.0/24" accept rule source address="0.0.0.0/0`, "198.51.100.0/24 ", "a/24",
		"fe80::1%eth0/64", "198.51.100.0/33", "198.51.100.0/-1", "198.51.100.0/24\n", "::ffff:192.0.2.1/120",
	}
	for _, cidr := range bad {
		if got, err := RichRule(cidr, 8448); err == nil {
			t.Errorf("RichRule(%q) = %q, want a refusal", cidr, got)
		}
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := RichRule("198.51.100.0/24", port); err == nil {
			t.Errorf("RichRule with port %d accepted", port)
		}
	}
}

func TestZoneOfInterface(t *testing.T) {
	cases := []struct {
		name    string
		answer  execx.Result
		want    string
		wantErr string
	}{
		{"a zone", execx.Result{Stdout: []byte("trusted\n")}, "trusted", ""},
		{"no zone, non-zero status", execx.Result{Stdout: []byte("no zone\n"), ExitCode: 2}, "public", ""},
		{"no zone, zero status (B12)", execx.Result{Stdout: []byte("no zone\n")}, "public", ""},
		{"a failure", execx.Result{Stderr: []byte("Error: boom"), ExitCode: 252}, "", "firewall-cmd --get-zone-of-interface=wg0 failed: Error: boom"},
		{"an answer that is not a zone", execx.Result{Stdout: []byte("a b\n")}, "", "not a zone name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &execx.Fake{}
			f.On([]string{"firewall-cmd", "--get-zone-of-interface=wg0"}, tc.answer)
			f.On([]string{"firewall-cmd", "--get-default-zone"}, execx.Result{Stdout: []byte("public\n")})
			got, err := newFixture(f).fw.ZoneOfInterface(context.Background(), "wg0")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %q, %v; want error %q", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestZoneOfInterfaceFailsWhenTheDefaultZoneCannotBeRead(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"firewall-cmd", "--get-zone-of-interface=wg0"}, execx.Result{Stdout: []byte("no zone\n"), ExitCode: 2})
	f.On([]string{"firewall-cmd", "--get-default-zone"}, execx.Result{ExitCode: 1})
	fw := newFixture(f).fw
	if _, err := fw.ZoneOfInterface(context.Background(), "wg0"); err == nil {
		t.Fatal("a failed default-zone query returned no error")
	}
	f.On([]string{"firewall-cmd", "--get-default-zone"}, execx.Result{Stdout: []byte("what even\n")})
	if _, err := fw.ZoneOfInterface(context.Background(), "wg0"); err == nil {
		t.Fatal("a default zone that is not a name was accepted")
	}
	f.OnError([]string{"firewall-cmd", "--get-zone-of-interface=wg0"}, &execx.NotInstalledError{Name: "firewall-cmd"})
	if _, err := fw.ZoneOfInterface(context.Background(), "wg0"); !execx.IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}

func TestQueryTreatsAnyExitStatusAsAbsentAndAFailureToRunAsAnError(t *testing.T) {
	f := &execx.Fake{}
	stub := newStub(f)
	stub.permanent["public\t"+ruleA] = true
	fw := newFixture(f).fw
	ctx := context.Background()
	if found, err := fw.Query(ctx, "public", ruleA, true); err != nil || !found {
		t.Fatalf("present: %v, %v", found, err)
	}
	if found, err := fw.Query(ctx, "public", ruleA, false); err != nil || found {
		t.Fatalf("runtime lacks it: %v, %v", found, err)
	}
	f2 := &execx.Fake{}
	f2.OnError([]string{"firewall-cmd"}, &execx.NotInstalledError{Name: "firewall-cmd"})
	if _, err := newFixture(f2).fw.Query(ctx, "public", ruleA, true); !execx.IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}

func TestSyncAddsToPermanentThenRuntimeAndRecordsFirst(t *testing.T) {
	f := &execx.Fake{}
	stub := newStub(f)
	fx := newFixture(f)
	rec := &memRecord{}
	if err := fx.fw.Sync(context.Background(), rec, []Rule{{"trusted", ruleA}}); err != nil {
		t.Fatal(err)
	}
	if !stub.permanent["trusted\t"+ruleA] || !stub.runtime["trusted\t"+ruleA] {
		t.Fatalf("permanent %v runtime %v", stub.permanent, stub.runtime)
	}
	wantOut := "CHANGED\tfirewalld permanent trusted: " + ruleA + "\nCHANGED\tfirewalld runtime trusted: " + ruleA + "\n"
	if fx.out.String() != wantOut {
		t.Fatalf("output %q", fx.out.String())
	}
	if string(rec.data) != "trusted\t"+ruleA+"\n" || rec.writes != 1 {
		t.Fatalf("record %q after %d writes", rec.data, rec.writes)
	}
	for _, c := range f.Calls() {
		if c.Name != "firewall-cmd" || strings.Contains(c.Line(), "--reload") {
			t.Fatalf("unexpected call %s", c.Line())
		}
	}
}

func TestSyncIsQuietWhenNothingChanged(t *testing.T) {
	f := &execx.Fake{}
	stub := newStub(f)
	fx := newFixture(f)
	rec := &memRecord{}
	wanted := []Rule{{"trusted", ruleA}}
	if err := fx.fw.Sync(context.Background(), rec, wanted); err != nil {
		t.Fatal(err)
	}
	fx.out.Reset()
	before := len(f.Calls())
	writes := rec.writes
	if err := fx.fw.Sync(context.Background(), rec, wanted); err != nil {
		t.Fatal(err)
	}
	if fx.out.String() != "" || rec.writes != writes {
		t.Fatalf("a second sync changed something: %q, %d writes", fx.out.String(), rec.writes)
	}
	for _, c := range f.Calls()[before:] {
		if strings.Contains(c.Line(), "--add-rich-rule") || strings.Contains(c.Line(), "--remove-rich-rule") {
			t.Fatalf("a second sync mutated: %s", c.Line())
		}
	}
	if len(stub.permanent) != 1 {
		t.Fatalf("permanent %v", stub.permanent)
	}
}

func TestSyncReplacesTheOldRuleAndOnlyThat(t *testing.T) {
	f := &execx.Fake{}
	stub := newStub(f)
	stub.permanent["trusted\t"+ruleA] = true
	stub.runtime["trusted\t"+ruleA] = true
	stub.permanent["trusted\tsomeone else's rule"] = true
	fx := newFixture(f)
	rec := &memRecord{exists: true, data: []byte("trusted\t" + ruleA + "\n")}
	if err := fx.fw.Sync(context.Background(), rec, []Rule{{"trusted", ruleB}}); err != nil {
		t.Fatal(err)
	}
	if stub.permanent["trusted\t"+ruleA] || stub.runtime["trusted\t"+ruleA] {
		t.Fatal("the old rule is still there")
	}
	if !stub.permanent["trusted\tsomeone else's rule"] {
		t.Fatal("a rule the installer never added was touched")
	}
	if !stub.permanent["trusted\t"+ruleB] || !stub.runtime["trusted\t"+ruleB] {
		t.Fatal("the new rule is missing")
	}
	if string(rec.data) != "trusted\t"+ruleB+"\n" {
		t.Fatalf("record %q", rec.data)
	}
	for _, want := range []string{"firewalld permanent trusted: removed " + ruleA, "firewalld runtime trusted: removed " + ruleA} {
		if !strings.Contains(fx.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, fx.out.String())
		}
	}
}

func TestSyncRecordsEveryRuleBeforeTheFirstFirewallChange(t *testing.T) {
	f := &execx.Fake{}
	stub := newStub(f)
	stub.failAdd = "203.0.113.0/24"
	fw := newFixture(f).fw
	rec := &memRecord{}
	wanted := []Rule{{"public", ruleA}, {"public", ruleB}}
	err := fw.Sync(context.Background(), rec, wanted)
	if err == nil || !strings.Contains(err.Error(), "--add-rich-rule") {
		t.Fatalf("got %v", err)
	}
	if !stub.permanent["public\t"+ruleA] {
		t.Fatal("the first rule was not added")
	}
	for _, r := range wanted {
		if !strings.Contains(string(rec.data), r.Zone+"\t"+r.Text) {
			t.Fatalf("record %q lacks %q", rec.data, r.Text)
		}
	}
	// A later removal cleans up what the failed run added.
	stub.failAdd = ""
	if err := fw.RemoveAll(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(stub.permanent) != 0 || len(stub.runtime) != 0 || !rec.deleted {
		t.Fatalf("permanent %v runtime %v deleted %v", stub.permanent, stub.runtime, rec.deleted)
	}
}

func TestSyncWithNothingWantedLeavesAnEmptyRecord(t *testing.T) {
	f := &execx.Fake{}
	newStub(f)
	fw := newFixture(f).fw
	rec := &memRecord{}
	if err := fw.Sync(context.Background(), rec, nil); err != nil {
		t.Fatal(err)
	}
	if !rec.exists || len(rec.data) != 0 {
		t.Fatalf("record %q exists %v", rec.data, rec.exists)
	}
	// And with an old rule: it is removed and the record emptied.
	stub := newStub(f)
	stub.permanent["public\t"+ruleA] = true
	rec = &memRecord{exists: true, data: []byte("public\t" + ruleA + "\n")}
	if err := fw.Sync(context.Background(), rec, nil); err != nil {
		t.Fatal(err)
	}
	if len(stub.permanent) != 0 || len(rec.data) != 0 {
		t.Fatalf("permanent %v record %q", stub.permanent, rec.data)
	}
}

func TestSyncWritesTheRecordSortedAndUniqueBeforeChanging(t *testing.T) {
	f := &execx.Fake{}
	newStub(f)
	fw := newFixture(f).fw
	var seen string
	rec := &memRecord{exists: true, data: []byte("public\t" + ruleB + "\n")}
	f.OnFunc([]string{"firewall-cmd", "--permanent", "--zone=public", "--query-rich-rule=" + ruleA}, func(execx.Cmd) (execx.Result, error) {
		if seen == "" {
			seen = string(rec.data)
		}
		return execx.Result{ExitCode: 1}, nil
	})
	if err := fw.Sync(context.Background(), rec, []Rule{{"public", ruleA}, {"public", ruleB}, {"public", ruleA}}); err != nil {
		t.Fatal(err)
	}
	want := "public\t" + ruleA + "\npublic\t" + ruleB + "\n"
	if seen != want {
		t.Fatalf("pending record %q, want %q", seen, want)
	}
	if string(rec.data) != "public\t"+ruleA+"\npublic\t"+ruleB+"\npublic\t"+ruleA+"\n" {
		t.Fatalf("final record keeps the wanted lines in order: %q", rec.data)
	}
}

func TestSyncReportsRecordAndFirewallFailures(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	newStub(f)
	fw := newFixture(f).fw
	if err := fw.Sync(ctx, &memRecord{failOn: "read"}, nil); err == nil {
		t.Error("an unreadable record was ignored")
	}
	if err := fw.Sync(ctx, &memRecord{failOn: "write"}, []Rule{{"public", ruleA}}); err == nil {
		t.Error("a record that cannot be written was ignored")
	}
	if err := fw.Sync(ctx, &memRecord{exists: true, data: []byte("no tab here\n")}, nil); err == nil {
		t.Error("a malformed record was ignored")
	}
	f2 := &execx.Fake{}
	f2.OnError([]string{"firewall-cmd"}, &execx.NotInstalledError{Name: "firewall-cmd"})
	fw2 := newFixture(f2).fw
	if err := fw2.Sync(ctx, &memRecord{}, []Rule{{"public", ruleA}}); !execx.IsNotInstalled(err) {
		t.Errorf("got %v", err)
	}
	rec := &memRecord{exists: true, data: []byte("public\t" + ruleA + "\n")}
	if err := fw2.Sync(ctx, rec, nil); !execx.IsNotInstalled(err) {
		t.Errorf("removal: got %v", err)
	}
}

func TestRemoveAllWithoutARecordDoesNothing(t *testing.T) {
	f := &execx.Fake{}
	fx := newFixture(f)
	rec := &memRecord{}
	if err := fx.fw.RemoveAll(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls()) != 0 || fx.out.String() != "" || rec.deleted {
		t.Fatalf("calls %d, output %q, deleted %v", len(f.Calls()), fx.out.String(), rec.deleted)
	}
}

func TestRemoveAllReportsRecordFailures(t *testing.T) {
	f := &execx.Fake{}
	newStub(f)
	fw := newFixture(f).fw
	ctx := context.Background()
	if err := fw.RemoveAll(ctx, &memRecord{failOn: "read"}); err == nil {
		t.Error("an unreadable record was ignored")
	}
	rec := &memRecord{exists: true, data: []byte("public\t" + ruleA + "\n"), failOn: "delete"}
	if err := fw.RemoveAll(ctx, rec); err == nil {
		t.Error("a record that cannot be deleted was ignored")
	}
	if err := fw.RemoveAll(ctx, &memRecord{exists: true, data: []byte("garbage\n")}); err == nil {
		t.Error("a malformed record was ignored")
	}
}

func TestParseRecord(t *testing.T) {
	rules, err := ParseRecord([]byte("public\t" + ruleA + "\n\ntrusted\t" + ruleB + "\n"))
	want := []Rule{{"public", ruleA}, {"trusted", ruleB}}
	if err != nil || !slices.Equal(rules, want) {
		t.Fatalf("%v, %v", rules, err)
	}
	if rules, err := ParseRecord(nil); err != nil || len(rules) != 0 {
		t.Fatalf("empty: %v, %v", rules, err)
	}
	if _, err := ParseRecord([]byte("zone-only\n")); err == nil {
		t.Fatal("a line without a tab was accepted")
	}
	if _, err := ParseRecord([]byte("\trule-without-zone\n")); err == nil {
		t.Fatal("a line without a zone was accepted")
	}
}
