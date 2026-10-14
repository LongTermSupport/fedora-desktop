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

func TestParseAllowlist(t *testing.T) {
	text := "# header\n\n" +
		"b/two\t80\t6\tassoc,ui\n" +
		"a/one.bash\t61\t0\t-\n"
	list, err := ParseAllowlist(text)
	if err != nil {
		t.Fatal(err)
	}
	want := Allowlist{
		"b/two":      {Code: 80, Funcs: 6, Assoc: true, UI: true},
		"a/one.bash": {Code: 61},
	}
	if len(list) != 2 || list["b/two"] != want["b/two"] || list["a/one.bash"] != want["a/one.bash"] {
		t.Fatalf("got %v", list)
	}
}

func TestParseAllowlistRejectsMalformedLines(t *testing.T) {
	bad := map[string]string{
		"too few fields":    "a\t1\t2\n",
		"too many fields":   "a\t1\t2\t-\textra\n",
		"non-numeric code":  "a\tx\t2\t-\n",
		"non-numeric funcs": "a\t1\ty\t-\n",
		"negative":          "a\t-1\t2\t-\n",
		"unknown flag":      "a\t1\t2\tbogus\n",
		"empty flags":       "a\t1\t2\t\n",
		"duplicate":         "a\t1\t2\t-\na\t3\t4\t-\n",
		"empty path":        "\t1\t2\t-\n",
	}
	for name, text := range bad {
		if _, err := ParseAllowlist(text); err == nil {
			t.Errorf("%s: accepted %q", name, text)
		}
	}
}

func TestFormatRoundTripsSorted(t *testing.T) {
	list := Allowlist{
		"z": {Code: 5, Funcs: 1, UI: true},
		"a": {Code: 99, Funcs: 7, Assoc: true},
		"m": {Code: 70},
	}
	text := list.Format()
	if !strings.HasPrefix(text, "#") {
		t.Fatalf("no header: %q", text)
	}
	back, err := ParseAllowlist(text)
	if err != nil || len(back) != 3 || back["z"] != list["z"] || back["a"] != list["a"] {
		t.Fatalf("round trip: %v, %v", back, err)
	}
	if strings.Index(text, "\na\t") > strings.Index(text, "\nm\t") || strings.Index(text, "\nm\t") > strings.Index(text, "\nz\t") {
		t.Fatalf("not sorted:\n%s", text)
	}
}

func TestEvaluateUnlisted(t *testing.T) {
	scanned := map[string]Measurement{
		"ok":      {Code: 60, Funcs: 4},
		"long":    {Code: 61},
		"funcs":   {Code: 5, Funcs: 5},
		"assoc":   {Code: 5, Assoc: true},
		"ui":      {Code: 5, UI: true},
		"several": {Code: 70, Funcs: 9, Assoc: true},
	}
	got := Evaluate(Allowlist{}, scanned)
	wantKinds(t, "unlisted", got,
		"long:"+KindBreach, "funcs:"+KindBreach, "assoc:"+KindBreach, "ui:"+KindBreach, "several:"+KindBreach)
	for _, f := range got {
		if !strings.Contains(f.Msg, "src-go/CLAUDE.md") {
			t.Errorf("%s: message does not point to src-go/CLAUDE.md: %s", f.Path, f.Msg)
		}
	}
	for _, f := range got {
		if f.Path == "several" && (!strings.Contains(f.Msg, "70 code lines") || !strings.Contains(f.Msg, "9 functions") || !strings.Contains(f.Msg, "associative")) {
			t.Errorf("one finding should name every breach: %s", f.Msg)
		}
	}
}

func TestEvaluateListed(t *testing.T) {
	list := Allowlist{
		"same":     {Code: 100, Funcs: 6},
		"codeup":   {Code: 100, Funcs: 6},
		"funcsup":  {Code: 100, Funcs: 6},
		"flagnew":  {Code: 100, Funcs: 6},
		"codedown": {Code: 100, Funcs: 6},
		"flagdrop": {Code: 100, Funcs: 6, UI: true},
		"within":   {Code: 100, Funcs: 6},
		"flagkept": {Code: 100, Funcs: 6, Assoc: true},
	}
	scanned := map[string]Measurement{
		"same":     {Code: 100, Funcs: 6},
		"codeup":   {Code: 101, Funcs: 6},
		"funcsup":  {Code: 100, Funcs: 7},
		"flagnew":  {Code: 100, Funcs: 6, UI: true},
		"codedown": {Code: 99, Funcs: 6},
		"flagdrop": {Code: 100, Funcs: 6},
		"within":   {Code: 40, Funcs: 2},
		"flagkept": {Code: 100, Funcs: 6, Assoc: true},
	}
	wantKinds(t, "listed", Evaluate(list, scanned),
		"codeup:"+KindGrew, "funcsup:"+KindGrew, "flagnew:"+KindGrew,
		"codedown:"+KindTighten, "flagdrop:"+KindTighten, "within:"+KindWithin)
}

func TestEvaluateStaleEntry(t *testing.T) {
	list := Allowlist{"gone": {Code: 100}}
	wantKinds(t, "stale", Evaluate(list, map[string]Measurement{}), "gone:"+KindStale)
}

func TestEvaluateGrewAndShrankReportsGrowth(t *testing.T) {
	list := Allowlist{"x": {Code: 100, Funcs: 6}}
	got := Evaluate(list, map[string]Measurement{"x": {Code: 90, Funcs: 7}})
	wantKinds(t, "mixed", got, "x:"+KindGrew)
}

func TestTightenOnlyLowers(t *testing.T) {
	list := Allowlist{
		"same":     {Code: 100, Funcs: 6},
		"down":     {Code: 100, Funcs: 6, Assoc: true, UI: true},
		"up":       {Code: 100, Funcs: 6},
		"within":   {Code: 100, Funcs: 6},
		"gone":     {Code: 100, Funcs: 6},
		"mixed":    {Code: 100, Funcs: 6},
		"flagnew":  {Code: 100, Funcs: 6},
		"codedown": {Code: 100, Funcs: 6},
	}
	scanned := map[string]Measurement{
		"same":     {Code: 100, Funcs: 6},
		"down":     {Code: 90, Funcs: 5, UI: true},
		"up":       {Code: 120, Funcs: 6},
		"within":   {Code: 10, Funcs: 1},
		"mixed":    {Code: 90, Funcs: 8},
		"flagnew":  {Code: 100, Funcs: 6, UI: true},
		"codedown": {Code: 99, Funcs: 6},
		"newbad":   {Code: 500, Funcs: 50},
	}
	got := Tighten(list, scanned)
	want := Allowlist{
		"same":     {Code: 100, Funcs: 6},
		"down":     {Code: 90, Funcs: 5, UI: true},
		"up":       {Code: 100, Funcs: 6},
		"mixed":    {Code: 90, Funcs: 6},
		"flagnew":  {Code: 100, Funcs: 6},
		"codedown": {Code: 99, Funcs: 6},
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

func TestCompareBase(t *testing.T) {
	base := Allowlist{
		"keep":   {Code: 100, Funcs: 6},
		"lower":  {Code: 100, Funcs: 6},
		"raise":  {Code: 100, Funcs: 6},
		"funcs":  {Code: 100, Funcs: 6},
		"flag":   {Code: 100, Funcs: 6},
		"remove": {Code: 100, Funcs: 6},
	}
	cur := Allowlist{
		"keep":  {Code: 100, Funcs: 6},
		"lower": {Code: 90, Funcs: 5},
		"raise": {Code: 101, Funcs: 6},
		"funcs": {Code: 100, Funcs: 7},
		"flag":  {Code: 100, Funcs: 6, Assoc: true},
		"added": {Code: 100, Funcs: 6},
	}
	wantKinds(t, "base", CompareBase(base, cur),
		"raise:"+KindRaised, "funcs:"+KindRaised, "flag:"+KindRaised, "added:"+KindAdded)
}
