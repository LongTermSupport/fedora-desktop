package membership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// The files a writer writes: membership.json as writeDesired writes it, applied.json as the
// keeper's Applied.to_json writes it (helpers/pingbus/membership.py).
const (
	validDesired = `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,` +
		`"seats":[{"team":"alpha","seat":"1"}],"farewell":[{"team":"beta","seat":"2","reason":"removed"}]}`
	validReport = `{"v":1,"generation":2,"inbound":true,` +
		`"seats":[{"team":"alpha","seat":"1","handle":"proj.1+local.podman","state":"held"}],` +
		`"refused":[{"team":"beta","seat":"2","why":"held-elsewhere","holder":"seat"}],` +
		`"told":{"generation":2,"roles":[{"team":"alpha","handle":"proj.1+local.podman","role":"coordinator"}]}}`
	minimalReport = `{"v":1,"generation":0,"seats":[],"refused":[]}`
)

// fileCase is one file and whether a reader takes it. Every case is judged by the Go reader and
// by pingbus's Python reader, which must agree with it.
type fileCase struct {
	name   string
	report bool
	body   string
	ok     bool
}

// swap is base with old replaced once by new; a fixture that does not hold old is a broken
// test, not a passing one.
func swap(t *testing.T, base, old, with string) string {
	t.Helper()
	if !strings.Contains(base, old) {
		t.Fatalf("the fixture %s holds no %s", base, old)
	}
	return strings.Replace(base, old, with, 1)
}

// missing is a refused case for each key, the base without it.
func missing(t *testing.T, prefix string, report bool, base string, keys ...string) []fileCase {
	t.Helper()
	cases := make([]fileCase, 0, len(keys))
	for _, key := range keys {
		cases = append(cases, fileCase{name: prefix + "no " + key, report: report, body: without(t, base, key)})
	}
	return cases
}

// many is n comma-joined entries, the ith from entry(i).
func many(n int, entry func(int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = entry(i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func desiredSeats(i int) string { return fmt.Sprintf(`{"team":"t%d","seat":"1"}`, i) }
func farewells(i int) string    { return fmt.Sprintf(`{"team":"t%d","seat":"1","reason":"removed"}`, i) }

func reportSeats(i int) string {
	return fmt.Sprintf(`{"team":"t%d","seat":"1","handle":"proj.1+local.podman","state":"held"}`, i)
}

func refusals(i int) string {
	return fmt.Sprintf(`{"team":"t%d","seat":"1","why":"no-bundle","holder":"-"}`, i)
}

func toldRoles(i int) string {
	return fmt.Sprintf(`{"team":"t%d","handle":"proj.1+local.podman","role":"worker"}`, i)
}

func desiredCases(t *testing.T) []fileCase {
	t.Helper()
	d := func(name, old, with string, ok bool) fileCase {
		return fileCase{name: "desired: " + name, body: swap(t, validDesired, old, with), ok: ok}
	}
	seats := `"seats":[{"team":"alpha","seat":"1"}]`
	farewell := `"farewell":[{"team":"beta","seat":"2","reason":"removed"}]`
	cases := missing(t, "desired: ", false, validDesired,
		"v", "session", "kind", "checkout", "generation", "written_ms", "seats", "farewell")
	return append(cases, []fileCase{
		{name: "desired: as written", body: validDesired, ok: true},
		d("no seats and no farewell", seats+","+farewell, `"seats":[],"farewell":[]`, true),
		d("64 seats", seats, `"seats":`+many(64, desiredSeats), true),
		d("65 seats", seats, `"seats":`+many(65, desiredSeats), false),
		d("256 farewells", farewell, `"farewell":`+many(256, farewells), true),
		d("257 farewells", farewell, `"farewell":`+many(257, farewells), false),
		d("a key in the wrong case", `"session"`, `"Session"`, false),
		d("a nested key in the wrong case", `"team":"alpha"`, `"Team":"alpha"`, false),
		d("a duplicate key", `"v":1,`, `"v":1,"v":1,`, false),
		d("a nested duplicate key", `"seat":"1"`, `"seat":"1","seat":"1"`, false),
		d("an unknown key", `"v":1,`, `"v":1,"x":1,`, false),
		d("an unknown nested key", `"seat":"1"`, `"seat":"1","x":1`, false),
		d("a null session", `"session":"s"`, `"session":null`, false),
		d("null seats", seats, `"seats":null`, false),
		d("a null farewell team", `"team":"beta"`, `"team":null`, false),
		d("v as a string", `"v":1`, `"v":"1"`, false),
		d("v as a float", `"v":1`, `"v":1.0`, false),
		d("v 2", `"v":1`, `"v":2`, false),
		d("generation 0", `"generation":1`, `"generation":0`, false),
		d("generation true", `"generation":1`, `"generation":true`, false),
		d("a negative written_ms", `"written_ms":1`, `"written_ms":-1`, false),
		d("kind lxc", `"kind":"ccy"`, `"kind":"lxc"`, false),
		d("kind cc", `"kind":"ccy"`, `"kind":"cc"`, true),
		d("a relative checkout", `"checkout":"/p"`, `"checkout":"p"`, false),
		d("a control character in the checkout", `"checkout":"/p"`, `"checkout":"/p\u0001"`, false),
		d("a session name with a leading dot", `"session":"s"`, `"session":".s"`, false),
		d("a checkout of 4095 lone surrogates", `"checkout":"/p"`, `"checkout":"/`+strings.Repeat(`\ud800`, 4095)+`"`, true),
		d("a checkout of 4096 lone surrogates", `"checkout":"/p"`, `"checkout":"/`+strings.Repeat(`\ud800`, 4096)+`"`, false),
		d("a team with a comma", `"team":"alpha"`, `"team":"a,1@b"`, false),
		d("a seat with a comma", `"team":"alpha","seat":"1"`, `"team":"b","seat":"1@a,2"`, false),
		d("seat 0", `"seat":"1"`, `"seat":"0"`, false),
		d("two seats in one team", seats, `"seats":[{"team":"alpha","seat":"1"},{"team":"alpha","seat":"2"}]`, false),
		d("a seat that is not an object", seats, `"seats":[1]`, false),
		d("a farewell reason it does not know", `"reason":"removed"`, `"reason":"why"`, false),
		{name: "desired: text after the object", body: validDesired + " {}", ok: false},
		{name: "desired: a list", body: `[]`, ok: false},
		{name: "desired: not UTF-8", body: "\xff", ok: false},
	}...)
}

func reportCases(t *testing.T) []fileCase {
	t.Helper()
	r := func(name, old, with string, ok bool) fileCase {
		return fileCase{name: "report: " + name, report: true, body: swap(t, validReport, old, with), ok: ok}
	}
	seats := `"seats":[{"team":"alpha","seat":"1","handle":"proj.1+local.podman","state":"held"}]`
	refused := `"refused":[{"team":"beta","seat":"2","why":"held-elsewhere","holder":"seat"}]`
	roles := `"roles":[{"team":"alpha","handle":"proj.1+local.podman","role":"coordinator"}]`
	cases := missing(t, "report: ", true, minimalReport, "v", "generation", "seats", "refused")
	return append(cases, []fileCase{
		{name: "report: as written", report: true, body: validReport, ok: true},
		{name: "report: with no told and no inbound", report: true, body: minimalReport, ok: true},
		r("inbound false", `"inbound":true`, `"inbound":false`, true),
		r("no holder", `"holder":"seat"`, `"holder":"-"`, true),
		r("a 32-letter holder", `"holder":"seat"`, `"holder":"`+strings.Repeat("a", 32)+`"`, true),
		r("told no roles", roles, `"roles":[]`, true),
		r("64 seats", seats, `"seats":`+many(64, reportSeats), true),
		r("65 seats", seats, `"seats":`+many(65, reportSeats), false),
		r("65 refusals", refused, `"refused":`+many(65, refusals), false),
		r("65 told roles", roles, `"roles":`+many(65, toldRoles), false),
		r("a key in the wrong case", `"generation":2,`, `"Generation":2,`, false),
		r("a seat key in the wrong case", `"handle":"proj.1+local.podman","state"`, `"HANDLE":"proj.1+local.podman","state"`, false),
		r("a told key in the wrong case", `"roles"`, `"ROLES"`, false),
		r("a told role key in the wrong case", `"role":"coordinator"`, `"Role":"coordinator"`, false),
		r("a duplicate key", `"v":1,`, `"v":1,"v":1,`, false),
		r("a nested duplicate key", `"state":"held"`, `"state":"held","state":"held"`, false),
		r("a duplicate key in told", `"generation":2,"roles"`, `"generation":2,"generation":2,"roles"`, false),
		r("an unknown key", `"v":1,`, `"v":1,"extra":1,`, false),
		r("an unknown seat key", `"state":"held"`, `"state":"held","x":1`, false),
		r("an unknown told key", `"generation":2,"roles"`, `"generation":2,"x":1,"roles"`, false),
		r("an unknown told role key", `"role":"coordinator"`, `"role":"coordinator","x":1`, false),
		r("told with no generation", `"generation":2,"roles"`, `"roles"`, false),
		r("told with no roles", `,`+roles, ``, false),
		r("a told role with no role", `,"role":"coordinator"`, ``, false),
		r("inbound null", `"inbound":true`, `"inbound":null`, false),
		r("inbound as a string", `"inbound":true`, `"inbound":"true"`, false),
		r("inbound 1", `"inbound":true`, `"inbound":1`, false),
		r("told null", `"told":{`, `"told":null,"x":{`, false),
		r("told a number", `"told":{`, `"told":7,"x":{`, false),
		r("told roles a number", roles, `"roles":7`, false),
		r("a negative generation", `"generation":2,`, `"generation":-1,`, false),
		r("generation as a float", `"generation":2,`, `"generation":2.0,`, false),
		r("v 2", `"v":1`, `"v":2`, false),
		r("a handle that is not a handle", `"handle":"proj.1+local.podman","state"`, `"handle":"h","state"`, false),
		r("a seat state it does not know", `"state":"held"`, `"state":"free"`, false),
		r("a team name in capitals", `"team":"alpha","seat"`, `"team":"Alpha","seat"`, false),
		r("a refused seat 0", `"seat":"2"`, `"seat":"0"`, false),
		r("a why it does not know", `"why":"held-elsewhere"`, `"why":"w"`, false),
		r("a holder in capitals", `"holder":"seat"`, `"holder":"X"`, false),
		r("an empty holder", `"holder":"seat"`, `"holder":""`, false),
		r("a 33-letter holder", `"holder":"seat"`, `"holder":"`+strings.Repeat("a", 33)+`"`, false),
		r("a told role it does not know", `"role":"coordinator"`, `"role":"boss"`, false),
		r("a told handle that is not a handle", `"handle":"proj.1+local.podman","role"`, `"handle":"h","role"`, false),
		r("a negative told generation", `"generation":2,"roles"`, `"generation":-1,"roles"`, false),
		r("a null handle", `"handle":"proj.1+local.podman","state"`, `"handle":null,"state"`, false),
		{name: "report: text after the object", report: true, body: validReport + "x", ok: false},
	}...)
}

// without is a top-level object with key removed, its other keys as they were.
func without(t *testing.T, body, key string) string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj[key]; !ok {
		t.Fatalf("the fixture has no %s", key)
	}
	delete(obj, key)
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// place writes the case where its reader looks, under a fresh checkout it returns.
func place(t *testing.T, c fileCase) string {
	t.Helper()
	checkout := t.TempDir()
	path := filepath.Join(checkout, File)
	if c.report {
		path = ReportPath(checkout, "s")
	}
	teamtest.MustWrite(t, path, c.body)
	return checkout
}

// readerDir is the directory a reader of the case is given: the desired file's own, or the
// report's session directory.
func readerDir(c fileCase, checkout string) string {
	if c.report {
		return SessionDir(checkout, "s")
	}
	return checkout
}

func goReads(t *testing.T, c fileCase) error {
	t.Helper()
	checkout := place(t, c)
	if c.report {
		_, err := ReadReport(checkout, "s")
		return err
	}
	_, err := Read(checkout)
	return err
}

func TestReadersTakeExactlyWhatTheWritersWrite(t *testing.T) {
	for _, c := range append(desiredCases(t), reportCases(t)...) {
		if err := goReads(t, c); (err == nil) != c.ok {
			t.Errorf("%s: want accepted %v, got %v", c.name, c.ok, err)
		}
	}
}

// pingbus's own readers (membership.read_desired and read_applied) judge every case the same
// way, so the Go and the Python read one file contract.
func TestPingbusReadsTheCasesTheSameWay(t *testing.T) {
	cases := append(desiredCases(t), reportCases(t)...)
	script, err := filepath.Abs(filepath.Join("testdata", "read_membership.py"))
	if err != nil {
		t.Fatal(err)
	}
	args := make([]string, 0, 3+len(cases))
	args = append(args, "-I", script, repoRoot(t))
	for _, c := range cases {
		kind := "desired"
		if c.report {
			kind = "applied"
		}
		args = append(args, kind+":"+readerDir(c, place(t, c)))
	}
	res, err := (execx.OS{}).Run(context.Background(), execx.Cmd{Name: "python3", Args: args})
	if err != nil {
		t.Fatalf("pingbus's readers: %v\n%s", err, res.Stderr)
	}
	verdicts := strings.Fields(string(res.Stdout))
	if len(verdicts) != len(cases) {
		t.Fatalf("pingbus judged %d files of %d: %q", len(verdicts), len(cases), res.Stdout)
	}
	for i, c := range cases {
		if got := verdicts[i] == "ok"; got != c.ok {
			t.Errorf("%s: pingbus accepted %v, want %v", c.name, got, c.ok)
		}
	}
}

func TestAnUnknownKeyAndAMiscasedKeyAreToldApart(t *testing.T) {
	cases := map[string]string{
		swap(t, validDesired, `"session"`, `"Session"`): `the key "Session" is spelt "session" here: keys are matched exactly`,
		swap(t, validDesired, `"v":1,`, `"v":1,"x":1,`): `unknown key "x"`,
	}
	for body, want := range cases {
		err := goReads(t, fileCase{body: body})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}
}

func TestTheFirstBadKeyInOrderIsTheOneReported(t *testing.T) {
	body := swap(t, validDesired, `"v":1,`, `"v":1,"zz":1,"mm":1,"aa":1,`)
	for range 20 {
		err := goReads(t, fileCase{body: body})
		if err == nil || !strings.Contains(err.Error(), `unknown key "aa"`) {
			t.Fatalf("got %v, want the key aa", err)
		}
	}
}

func TestAMissingKeyIsNamed(t *testing.T) {
	err := goReads(t, fileCase{report: true, body: without(t, minimalReport, "refused")})
	if err == nil || !strings.Contains(err.Error(), "missing refused") {
		t.Fatalf("got %v", err)
	}
}

// As membership.py's _object: a missing key is named before an unknown one.
func TestAMissingKeyIsNamedBeforeAnUnknownOne(t *testing.T) {
	body := swap(t, without(t, validDesired, "kind"), `"v":1,`, `"v":1,"x":1,`)
	if err := goReads(t, fileCase{body: body}); err == nil || !strings.Contains(err.Error(), "missing kind") {
		t.Fatalf("got %v", err)
	}
}

// An integer past int64 is refused, where Python's unbounded int would take it: no writer
// writes one, and no Go field holds one.
func TestAnIntegerPastInt64IsRefused(t *testing.T) {
	body := swap(t, minimalReport, `"generation":0`, `"generation":9223372036854775808`)
	if err := goReads(t, fileCase{report: true, body: body}); err == nil {
		t.Fatal("accepted")
	}
}

func TestReadReportDecodesEveryField(t *testing.T) {
	got, err := ReadReport(place(t, fileCase{report: true, body: validReport}), "s")
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	want := Report{
		V: 1, Generation: 2, Inbound: &yes,
		Seats:   []ReportSeat{{Team: "alpha", Seat: "1", Handle: "proj.1+local.podman", State: "held"}},
		Refused: []ReportRefused{{Team: "beta", Seat: "2", Why: "held-elsewhere", Holder: "seat"}},
		Told:    &Told{Generation: 2, Roles: []ToldEntry{{Team: "alpha", Handle: "proj.1+local.podman", Role: "coordinator"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read %+v, want %+v", got, want)
	}
	if got.ToldRole("alpha", "proj.1+local.podman") != "coordinator" {
		t.Fatalf("told role %q", got.ToldRole("alpha", "proj.1+local.podman"))
	}
}

// A Report marshalled by encoding/json, as the hot tests write one, is a file the reader takes:
// the struct tags and the reader name the same keys.
func TestAMarshalledReportReadsBack(t *testing.T) {
	no := false
	rep := Report{
		V: 1, Generation: 3, Inbound: &no,
		Seats:   []ReportSeat{{Team: "alpha", Seat: "1", Handle: "proj.1+local.podman", State: "held"}},
		Refused: []ReportRefused{{Team: "beta", Seat: "2", Why: "no-bundle", Holder: noHolder}},
		Told:    &Told{Generation: 3, Roles: []ToldEntry{{Team: "alpha", Handle: "proj.1+local.podman", Role: "worker"}}},
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadReport(place(t, fileCase{report: true, body: string(data)}), "s")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, rep) {
		t.Fatalf("read %+v, want %+v", got, rep)
	}
}

func TestWriteRefusesAMembershipPingbusWouldRefuse(t *testing.T) {
	d := sample()
	d.Checkout = "/p\x01"
	if err := Write(t.TempDir(), d); err == nil {
		t.Fatal("a checkout with a control character was written")
	}
	d = sample()
	d.Checkout = "/p\xff"
	if err := Write(t.TempDir(), d); err == nil {
		t.Fatal("a checkout that is not UTF-8, which encoding/json would write changed, was written")
	}
	d = sample()
	d.Seats = []Seat{{Team: "a,1@b", Seat: "1"}}
	if err := Write(t.TempDir(), d); err == nil {
		t.Fatal("a team with a comma was written")
	}
	d = sample()
	d.Seats = make([]Seat, maxSeats+1)
	for i := range d.Seats {
		d.Seats[i] = Seat{Team: fmt.Sprintf("t%d", i), Seat: "1"}
	}
	if err := Write(t.TempDir(), d); err == nil {
		t.Fatal("more seats than pingbus takes were written")
	}
}

func TestAReadErrorIsNotNotExist(t *testing.T) {
	err := goReads(t, fileCase{body: "{"})
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

// The file contract's bounds, grammar and closed sets are pingbus's (helpers/pingbus/membership.py);
// this package holds a copy, pinned here.
func TestTheFileContractIsPingbusOwn(t *testing.T) {
	src := readFile(t, filepath.Join(repoRoot(t), "helpers/pingbus/membership.py"))
	tuple := func(alternatives string) string { return `("` + strings.ReplaceAll(alternatives, "|", `", "`) + `")` }
	for _, line := range []string{
		fmt.Sprintf("MAX_FILE_BYTES = %d\n", maxDesiredBytes),
		fmt.Sprintf("MAX_SEATS = %d\n", maxSeats),
		fmt.Sprintf("MAX_FAREWELLS = %d\n", maxFarewells),
		`KINDS = ("` + KindCcy + `", "` + KindCC + `")`,
		`FAREWELL_REASONS = {"` + ReasonRemoved + `": `,
		`SEAT_STATES = ("` + stateHeld + `",)`,
		"WHY = " + tuple(whyPattern) + "\n",
		`NO_HOLDER = "` + noHolder + `"`,
		"ROLES = " + tuple(rolePattern) + "\n",
		`_SESSION_RE = re.compile(r"` + strings.Trim(sessionName.String(), "^$") + `")`,
		`_HOLDER_RE = re.compile(r"` + holderPattern + `")`,
		`_CHECKOUT_RE = re.compile(r"/[^\x00-\x1f\x7f]{0,4095}")`,
	} {
		if !strings.Contains(src, line) {
			t.Errorf("membership.py does not say %q", line)
		}
	}
	if !isCheckout("/"+strings.Repeat("a", 4095)) || isCheckout("/"+strings.Repeat("a", 4096)) || isCheckout("/p\x7f") {
		t.Error("isCheckout is not _CHECKOUT_RE")
	}
}
