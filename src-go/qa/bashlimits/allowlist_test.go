package bashlimits

import (
	"slices"
	"strings"
	"testing"
)

func kinds(found []Finding) []string {
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.Path+":"+f.Kind)
	}
	slices.Sort(out)
	return out
}

func wantKinds(t *testing.T, name string, got []Finding, want ...string) {
	t.Helper()
	slices.Sort(want)
	if g := kinds(got); !slices.Equal(g, want) {
		t.Errorf("%s: findings %v, want %v", name, g, want)
	}
}

// scanOf is a Scan whose measured files are the given ones.
func scanOf(measured map[string]Measurement) Scan {
	scan := Scan{Measured: measured, Exempt: map[string]bool{}, Present: map[string]bool{}}
	for p := range measured {
		scan.Present[p] = true
	}
	return scan
}

func TestParseAllowlist(t *testing.T) {
	text := "# header\n# measurer: " + itoa(MeasurerVersion) + "\n\n" +
		"b/two\t80\t6\t90\tassoc,ui,data\n" +
		"a/one.bash\t61\t0\t61\t-\n"
	file, err := ParseAllowlist(text)
	if err != nil {
		t.Fatal(err)
	}
	want := Allowlist{
		"b/two":      {Code: 80, Funcs: 6, Stmts: 90, Assoc: true, UI: true, Data: true},
		"a/one.bash": {Code: 61, Stmts: 61},
	}
	if file.Version != MeasurerVersion || len(file.Records) != 2 || file.Records["b/two"] != want["b/two"] || file.Records["a/one.bash"] != want["a/one.bash"] {
		t.Fatalf("got %+v", file)
	}
}

func TestAllowlistWithoutAVersionIsTheFirstMeasurer(t *testing.T) {
	file, err := ParseAllowlist("# header\na/one.bash\t61\t0\t-\n")
	if err != nil {
		t.Fatal(err)
	}
	// Another measurer's numbers mean nothing here, so only the path is kept.
	if file.Version != 1 || file.Records["a/one.bash"] != (Measurement{}) || len(file.Records) != 1 {
		t.Fatalf("got %+v", file)
	}
}

func TestParseAllowlistRejectsMalformedLines(t *testing.T) {
	bad := map[string]string{
		"too few fields":     "a\t1\t2\t3\n",
		"too many fields":    "a\t1\t2\t3\t-\textra\n",
		"non-numeric code":   "a\tx\t2\t3\t-\n",
		"non-numeric funcs":  "a\t1\ty\t3\t-\n",
		"non-numeric stmts":  "a\t1\t2\tz\t-\n",
		"negative":           "a\t-1\t2\t3\t-\n",
		"unknown flag":       "a\t1\t2\t3\tbogus\n",
		"empty flags":        "a\t1\t2\t3\t\n",
		"duplicate":          "a\t1\t2\t3\t-\na\t3\t4\t5\t-\n",
		"empty path":         "\t1\t2\t3\t-\n",
		"garbled version":    "# measurer: two\n",
		"empty-path old row": "# measurer: 1\n\t1\t2\t-\n",
	}
	for name, body := range bad {
		text := "# measurer: " + itoa(MeasurerVersion) + "\n" + body
		if strings.HasPrefix(body, "# measurer") || strings.HasPrefix(name, "empty-path old") {
			text = body
		}
		if _, err := ParseAllowlist(text); err == nil {
			t.Errorf("%s: accepted %q", name, text)
		}
	}
}

func TestFormatRoundTripsSorted(t *testing.T) {
	list := Allowlist{
		"z": {Code: 5, Funcs: 1, Stmts: 5, UI: true},
		"a": {Code: 99, Funcs: 7, Stmts: 120, Assoc: true, Data: true},
		"m": {Code: 70, Stmts: 70},
	}
	text := list.Format()
	if !strings.HasPrefix(text, "#") || !strings.Contains(text, "\n# measurer: "+itoa(MeasurerVersion)+"\n") {
		t.Fatalf("no header or version: %q", text)
	}
	back, err := ParseAllowlist(text)
	if err != nil || len(back.Records) != 3 || back.Records["z"] != list["z"] || back.Records["a"] != list["a"] {
		t.Fatalf("round trip: %v, %v", back, err)
	}
	if strings.Index(text, "\na\t") > strings.Index(text, "\nm\t") || strings.Index(text, "\nm\t") > strings.Index(text, "\nz\t") {
		t.Fatalf("not sorted:\n%s", text)
	}
}

func TestEvaluateUnlisted(t *testing.T) {
	scan := scanOf(map[string]Measurement{
		"ok":      {Code: 60, Funcs: 4, Stmts: 60},
		"long":    {Code: 61, Stmts: 61},
		"funcs":   {Code: 5, Funcs: 5, Stmts: 5},
		"stmts":   {Code: 5, Stmts: MaxStmts + 1},
		"assoc":   {Code: 5, Assoc: true},
		"ui":      {Code: 5, UI: true},
		"data":    {Code: 5, Data: true},
		"several": {Code: 70, Funcs: 9, Assoc: true},
	})
	got := Evaluate(Allowlist{}, scan)
	wantKinds(t, "unlisted", got,
		"long:"+KindBreach, "funcs:"+KindBreach, "stmts:"+KindBreach, "assoc:"+KindBreach,
		"ui:"+KindBreach, "data:"+KindBreach, "several:"+KindBreach)
	for _, f := range got {
		if !strings.Contains(f.Msg, "src-go/CLAUDE.md") {
			t.Errorf("%s: message does not point to src-go/CLAUDE.md: %s", f.Path, f.Msg)
		}
		if f.Path == "several" && (!strings.Contains(f.Msg, "70 code lines") || !strings.Contains(f.Msg, "9 functions") || !strings.Contains(f.Msg, "associative")) {
			t.Errorf("one finding should name every breach: %s", f.Msg)
		}
	}
}

func TestEvaluateListed(t *testing.T) {
	rec := Measurement{Code: 100, Funcs: 6, Stmts: 100}
	list := Allowlist{
		"same": rec, "codeup": rec, "funcsup": rec, "stmtsup": rec, "flagnew": rec,
		"codedown": rec, "stmtsdown": rec, "within": rec,
		"flagdrop": {Code: 100, Funcs: 6, Stmts: 100, UI: true},
		"flagkept": {Code: 100, Funcs: 6, Stmts: 100, Assoc: true},
	}
	scan := scanOf(map[string]Measurement{
		"same":      rec,
		"codeup":    {Code: 101, Funcs: 6, Stmts: 100},
		"funcsup":   {Code: 100, Funcs: 7, Stmts: 100},
		"stmtsup":   {Code: 100, Funcs: 6, Stmts: 101},
		"flagnew":   {Code: 100, Funcs: 6, Stmts: 100, Data: true},
		"codedown":  {Code: 99, Funcs: 6, Stmts: 100},
		"stmtsdown": {Code: 100, Funcs: 6, Stmts: 99},
		"flagdrop":  rec,
		"within":    {Code: 40, Funcs: 2, Stmts: 40},
		"flagkept":  {Code: 100, Funcs: 6, Stmts: 100, Assoc: true},
	})
	wantKinds(t, "listed", Evaluate(list, scan),
		"codeup:"+KindGrew, "funcsup:"+KindGrew, "stmtsup:"+KindGrew, "flagnew:"+KindGrew,
		"codedown:"+KindTighten, "stmtsdown:"+KindTighten, "flagdrop:"+KindTighten, "within:"+KindWithin)
}

func TestEvaluateEntriesWithoutAMeasuredFile(t *testing.T) {
	scan := scanOf(map[string]Measurement{})
	scan.Exempt["exempt.bash"] = true
	scan.Present["exempt.bash"] = true
	scan.Present["notbash"] = true
	scan.Exempt["scripts/test-moved.bash"] = true
	scan.Present["scripts/test-moved.bash"] = true
	list := Allowlist{
		"gone": {Code: 100}, "exempt.bash": {Code: 100}, "notbash": {Code: 100}, "scripts/moved.bash": {Code: 100},
	}
	wantKinds(t, "unmeasured", Evaluate(list, scan),
		"gone:"+KindStale, "exempt.bash:"+KindOrphan, "notbash:"+KindOrphan, "scripts/moved.bash:"+KindOrphan)
}

func TestEvaluateGrewAndShrankReportsGrowth(t *testing.T) {
	list := Allowlist{"x": {Code: 100, Funcs: 6, Stmts: 100}}
	got := Evaluate(list, scanOf(map[string]Measurement{"x": {Code: 90, Funcs: 7, Stmts: 90}}))
	wantKinds(t, "mixed", got, "x:"+KindGrew)
}

func TestTightenOnlyLowers(t *testing.T) {
	rec := Measurement{Code: 100, Funcs: 6, Stmts: 100}
	list := Allowlist{
		"same": rec, "up": rec, "within": rec, "gone": rec, "mixed": rec, "flagnew": rec, "codedown": rec,
		"down": {Code: 100, Funcs: 6, Stmts: 100, Assoc: true, UI: true, Data: true},
	}
	scan := scanOf(map[string]Measurement{
		"same":     rec,
		"down":     {Code: 90, Funcs: 5, Stmts: 80, UI: true},
		"up":       {Code: 120, Funcs: 6, Stmts: 130},
		"within":   {Code: 10, Funcs: 1, Stmts: 10},
		"mixed":    {Code: 90, Funcs: 8, Stmts: 100},
		"flagnew":  {Code: 100, Funcs: 6, Stmts: 100, UI: true},
		"codedown": {Code: 99, Funcs: 6, Stmts: 100},
		"newbad":   {Code: 500, Funcs: 50, Stmts: 500},
	})
	got, refused := Tighten(list, scan)
	if len(refused) != 0 {
		t.Fatalf("refusals: %v", refused)
	}
	want := Allowlist{
		"same":     rec,
		"down":     {Code: 90, Funcs: 5, Stmts: 80, UI: true},
		"up":       rec,
		"mixed":    {Code: 90, Funcs: 6, Stmts: 100},
		"flagnew":  rec,
		"codedown": {Code: 99, Funcs: 6, Stmts: 100},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, m := range want {
		if got[p] != m {
			t.Errorf("%s: got %+v, want %+v", p, got[p], m)
		}
	}
}

func TestTightenRefusesWhatLivesOnOutsideTheGate(t *testing.T) {
	scan := scanOf(map[string]Measurement{})
	scan.Exempt["scripts/test-x.bash"] = true
	scan.Present["scripts/test-x.bash"] = true
	scan.Present["renamed.cmd"] = true
	_, refused := Tighten(Allowlist{"scripts/test-x.bash": {Code: 99}, "renamed.cmd": {Code: 99}, "gone": {Code: 99}}, scan)
	wantKinds(t, "refused", refused, "scripts/test-x.bash:"+KindOrphan, "renamed.cmd:"+KindOrphan)
}

func TestSeedRecordsOnlyOverLimitFiles(t *testing.T) {
	got := Seed(map[string]Measurement{
		"ok":   {Code: 60, Funcs: 4},
		"long": {Code: 61, Funcs: 2},
		"ui":   {Code: 5, UI: true},
	})
	if len(got) != 2 || got["long"] != (Measurement{Code: 61, Funcs: 2}) || got["ui"] != (Measurement{Code: 5, UI: true}) {
		t.Fatalf("got %v", got)
	}
}

func TestRemeasureIsTheSeedUnlessItWouldHideAFile(t *testing.T) {
	scan := scanOf(map[string]Measurement{"long": {Code: 99, Stmts: 99}, "ok": {Code: 3}})
	got, refused := Remeasure(Allowlist{"long": {Code: 80}, "ok": {Code: 80}, "gone": {Code: 80}}, scan)
	if len(refused) != 0 || len(got) != 1 || got["long"].Code != 99 {
		t.Fatalf("got %v, %v", got, refused)
	}
	scan.Exempt["scripts/test-gone.bash"] = true
	_, refused = Remeasure(Allowlist{"scripts/gone.bash": {Code: 80}}, scan)
	wantKinds(t, "twin", refused, "scripts/gone.bash:"+KindOrphan)
}

func fileOf(version int, list Allowlist) File { return File{Version: version, Records: list} }

func TestCompareBase(t *testing.T) {
	rec := Measurement{Code: 100, Funcs: 6, Stmts: 100}
	base := fileOf(MeasurerVersion, Allowlist{"keep": rec, "lower": rec, "raise": rec, "funcs": rec, "flag": rec, "remove": rec, "moved": rec})
	cur := fileOf(MeasurerVersion, Allowlist{
		"keep":  rec,
		"lower": {Code: 90, Funcs: 5, Stmts: 90},
		"raise": {Code: 101, Funcs: 6, Stmts: 100},
		"funcs": {Code: 100, Funcs: 7, Stmts: 100},
		"flag":  {Code: 100, Funcs: 6, Stmts: 100, Assoc: true},
		"added": rec,
	})
	scan := scanOf(map[string]Measurement{"remove": {Code: 5}})
	scan.Present["moved"] = true
	wantKinds(t, "base", CompareBase(base, cur, scan),
		"raise:"+KindRaised, "funcs:"+KindRaised, "flag:"+KindRaised, "added:"+KindAdded, "moved:"+KindDropped)
}

func TestCompareBaseAllowsRaisesOnlyAcrossANewerMeasurer(t *testing.T) {
	old := fileOf(MeasurerVersion-1, Allowlist{"a": {}, "b": {}})
	cur := fileOf(MeasurerVersion, Allowlist{"a": {Code: 900, Stmts: 900}, "new": {Code: 70}})
	scan := scanOf(map[string]Measurement{"b": {Code: 1}})
	wantKinds(t, "newer", CompareBase(old, cur, scan))
	wantKinds(t, "older", CompareBase(cur, old, scan), ":"+KindMeasure)
}
