package pycompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// oracleScript is the real python3 answering one batch of requests. Every string crosses as a
// list of code points, so a lone surrogate travels intact in both directions.
const oracleScript = `
import ipaddress, json, sys, unicodedata

def S(cps):
    return ''.join(map(chr, cps))

def C(s):
    return [ord(c) for c in s]

def enc(v):
    if v is None:
        return ['N']
    if isinstance(v, bool):
        return ['B', v]
    if isinstance(v, int):
        return ['I', str(v)]
    if isinstance(v, float):
        return ['F', repr(v)]
    if isinstance(v, str):
        return ['S', C(v)]
    if isinstance(v, list):
        return ['L', [enc(x) for x in v]]
    return ['D', [[C(k), enc(x)] for k, x in v.items()]]

def nodup(pairs):
    out = {}
    for k, v in pairs:
        if k in out:
            raise ValueError('duplicate key')
        out[k] = v
    return out

def ranges(pred):
    out, start = [], None
    for cp in range(0x110000):
        if pred(cp):
            if start is None:
                start = cp
        elif start is not None:
            out.append([start, cp - 1])
            start = None
    if start is not None:
        out.append([start, 0x10FFFF])
    return out

def tables():
    space, lower, dec, cased, ign_or_cased = [], [], [], [], []
    for cp in range(0x110000):
        c = chr(cp)
        if c.isspace():
            space.append(cp)
        low = c.lower()
        if low != c:
            lower.append({'cp': cp, 'to': C(low)})
        if c.isdecimal():
            dec.append([cp, unicodedata.decimal(c)])
        if ('AΣ' + c).lower()[1] == 'σ':
            cased.append(cp)
        if ('A' + c + 'Σ').lower()[-1] == 'ς':
            ign_or_cased.append(cp)
    return {'space': space, 'lower': lower, 'decimal': dec, 'cased': cased,
            'ignOrCased': ign_or_cased,
            'unprintable': ranges(lambda cp: not chr(cp).isprintable()),
            'unassigned': ranges(lambda cp: unicodedata.category(chr(cp)) == 'Cn')}

def run(op, a):
    if op == 'version':
        return list(sys.version_info[:3])
    if op == 'loads':
        return enc(json.loads(S(a[0])))
    if op == 'loads_nodup':
        return enc(json.loads(S(a[0]), object_pairs_hook=nodup))
    if op == 'raw':
        v, end = json.JSONDecoder().raw_decode(S(a[0]))
        return [enc(v), end]
    if op == 'dumps':
        out = json.dumps(json.loads(S(a[0])), ensure_ascii=a[1])
        out.encode('utf-8')
        return C(out)
    if op == 'truthy':
        return bool(json.loads(S(a[0])))
    if op == 'equal':
        return json.loads(S(a[0])) == json.loads(S(a[1]))
    if op == 'contains':
        return json.loads(S(a[1])) in json.loads(S(a[0]))
    if op == 'iterate':
        return [enc(x) for x in json.loads(S(a[0]))]
    if op == 'str':
        return C(str(json.loads(S(a[0]))))
    if op == 'repr':
        return C(repr(json.loads(S(a[0]))))
    if op == 'reprstr':
        return C(repr(S(a[0])))
    if op == 'floatrepr':
        return repr(float(a[0]))
    if op == 'strip':
        s = S(a[0])
        return [C(s.strip()), C(s.lstrip()), C(s.rstrip()), len(s)]
    if op == 'splitlines':
        s = S(a[0])
        return [[C(x) for x in s.splitlines()], [C(x) for x in s.splitlines(keepends=True)]]
    if op == 'lower':
        return C(S(a[0]).lower())
    if op == 'digits':
        s = S(a[0])
        return str(int(s)) if s and all(c.isdecimal() for c in s) else None
    if op == 'ip':
        return str(ipaddress.ip_address(S(a[0])))
    if op == 'tables':
        return tables()
    raise SystemExit('unknown op ' + op)

out = []
for op, args in json.load(sys.stdin):
    try:
        out.append({'ok': run(op, args)})
    except Exception as e:
        out.append({'err': type(e).__name__, 'msg': C(str(e))})
json.dump(out, sys.stdout)
`

// pyReq is one request: an operation and its arguments (strings already as code points).
type pyReq struct {
	op   string
	args []any
}

// pyResult is python's answer: the JSON of the value, or the exception's class and str().
type pyResult struct {
	OK  json.RawMessage `json:"ok"`
	Err string          `json:"err"`
	Msg []rune          `json:"msg"`
}

func (r pyResult) failed() bool { return r.Err != "" }

// cps is a string as the code point list python rebuilds it from.
func cps(s string) []rune { return CodePoints(s) }

// fromCPs is the WTF-8 string of a code point list python returned.
func fromCPs(rs []rune) string {
	var b []byte
	for _, r := range rs {
		b = appendRune(b, r)
	}
	return string(b)
}

// oracleCommand is the oracle by name, never python3: on a Fedora 44 host python3 is 3.14.
// The host's python play installs python3.13; CI's setup-python 3.13 puts it on PATH.
const oracleCommand = "python3.13"

// askOracle runs the real python3.13 once over a batch. A missing python3.13 is an error
// naming it: the parity proof cannot be skipped.
func askOracle(ctx context.Context, reqs []pyReq) ([]pyResult, error) {
	batch := make([][]any, 0, len(reqs))
	for _, r := range reqs {
		batch = append(batch, []any{r.op, r.args})
	}
	in, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	res, err := execx.OS{}.Run(ctx, execx.Cmd{
		Name: oracleCommand, Args: []string{"-I", "-c", oracleScript}, Stdin: bytes.NewReader(in),
	})
	if execx.IsNotInstalled(err) {
		return nil, fmt.Errorf("%s is not on PATH: the Go reproduces python 3.13, the ccy image's python, and is proven against it only (the host's python play installs it)", oracleCommand)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", oracleCommand, err)
	}
	var out []pyResult
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("%s answer: %w", oracleCommand, err)
	}
	if len(out) != len(reqs) {
		return nil, fmt.Errorf("%s answered %d of %d requests", oracleCommand, len(out), len(reqs))
	}
	return out, nil
}

// runOracle is askOracle failing the test on an error.
func runOracle(t *testing.T, reqs []pyReq) []pyResult {
	t.Helper()
	out, err := askOracle(t.Context(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// pyRelease is a python's (major, minor, patch).
type pyRelease [3]int

// askOracleVersion is the release python3.13 reports.
func askOracleVersion(ctx context.Context) (pyRelease, error) {
	var v pyRelease
	out, err := askOracle(ctx, []pyReq{{op: "version", args: []any{}}})
	if err != nil {
		return v, err
	}
	if out[0].failed() {
		return v, fmt.Errorf("%s version: %s", oracleCommand, pyErrText(out[0]))
	}
	err = json.Unmarshal(out[0].OK, &v)
	return v, err
}

// oracleVersion is the python whose answers this package reproduces: 3.13, the ccy image's.
// 3.12 words a trailing comma otherwise and writes an IPv4-mapped address as hex; 3.14 (Fedora
// 44) has other messages for `in` and a bad escape, newer Unicode tables, and nests deeper. A
// different oracle is not a parity failure, and is not compared case by case.
var oracleVersion = [2]int{3, 13}

// reproducedPatch is the 3.13 patch release whose error wording the Go reproduces: 3.13.5,
// the ccy image's (Debian 13). Other 3.13 releases word some errors otherwise (3.13.17 a `\u`
// escape cut off by the end of the input). Nothing reads the wording, so any 3.13 is the
// oracle for accept against reject and for the decoded value; the message is compared only on
// this release (compareResultOn).
const reproducedPatch = 5

// oracleUnsupported is the reason an oracle reporting version v cannot be the oracle, or nil.
func oracleUnsupported(v pyRelease) error {
	if [2]int{v[0], v[1]} == oracleVersion {
		return nil
	}
	return fmt.Errorf("%s is python %d.%d, and parity is proven against python %d.%d, the ccy image's",
		oracleCommand, v[0], v[1], oracleVersion[0], oracleVersion[1])
}

// python is runOracle with the count logged, so `go test -v` shows how many cases each
// parity test compared. An oracle other than oracleVersion fails the test with that reason.
func python(t *testing.T, reqs []pyReq) []pyResult {
	t.Helper()
	v := pythonVersion(t)
	if err := oracleUnsupported(v); err != nil {
		t.Fatal(err)
	}
	out := runOracle(t, reqs)
	t.Logf("PYCOMPARED %d cases with %s %d.%d.%d", len(reqs), oracleCommand, v[0], v[1], v[2])
	return out
}

var (
	versionOnce  sync.Once
	versionMM    pyRelease
	errVersionMM error
)

// pythonVersion is the release of the oracle, asked once per test binary.
func pythonVersion(t *testing.T) pyRelease {
	t.Helper()
	versionOnce.Do(func() {
		versionMM, errVersionMM = askOracleVersion(t.Context())
	})
	if errVersionMM != nil {
		t.Fatalf("%s version: %v", oracleCommand, errVersionMM)
	}
	return versionMM
}

// enc is a Value in the oracle's tagged form, as plain JSON data.
func enc(v Value) any {
	switch x := v.(type) {
	case None:
		return []any{"N"}
	case Bool:
		return []any{"B", bool(x)}
	case Int:
		return []any{"I", x.V.String()}
	case Float:
		return []any{"F", PyRepr(x)}
	case Str:
		return []any{"S", cps(string(x))}
	case List:
		items := make([]any, 0, len(x))
		for _, item := range x {
			items = append(items, enc(item))
		}
		return []any{"L", items}
	case *Dict:
		pairs := make([]any, 0, x.Len())
		for _, it := range x.Items() {
			pairs = append(pairs, []any{cps(it.Key), enc(it.Value)})
		}
		return []any{"D", pairs}
	}
	return nil
}

// sameJSON reports whether a Go-built answer and python's raw JSON are the same data.
func sameJSON(t *testing.T, a any, b json.RawMessage) bool {
	t.Helper()
	same, err := sameJSONErr(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return same
}

// sameJSONErr is sameJSON returning what stopped the comparison.
func sameJSONErr(a any, b json.RawMessage) (bool, error) {
	ab, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	var x, y any
	if err := json.Unmarshal(ab, &x); err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false, err
	}
	return reflect.DeepEqual(x, y), nil
}

// pyErrText is python's exception as "Class: message", for failure output.
func pyErrText(r pyResult) string { return r.Err + ": " + string(r.Msg) }

// recordingJudge is a judge that keeps what compareResultOn reported.
type recordingJudge struct{ errs, logs []string }

func (*recordingJudge) Helper() {}

func (r *recordingJudge) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *recordingJudge) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

// pyRejects is python's answer raising class with message msg.
func pyRejects(class ErrorClass, msg string) pyResult {
	return pyResult{Err: string(class), Msg: cps(msg)}
}

// goUnterminated is the Go's refusal of an unterminated string, a JSONDecodeError.
func goUnterminated(t *testing.T) error {
	t.Helper()
	_, err := Loads(`"abc`, LoadOptions{})
	if err == nil {
		t.Fatal(`Loads("abc) accepted`)
	}
	return err
}

// TestMessageDifferenceOnAnotherPatchIsLogged: both reject with the same class, and only the
// wording differs. On a 3.13 other than the reproduced 3.13.5 that passes and is logged naming
// both releases; on 3.13.5 it fails.
func TestMessageDifferenceOnAnotherPatchIsLogged(t *testing.T) {
	py := pyRejects(ClassJSONDecodeError, "Some other wording: line 1 column 1 (char 0)")
	var j recordingJudge
	compareResultOn(&j, pyRelease{3, 13, 17}, "case", nil, goUnterminated(t), py)
	if len(j.errs) != 0 {
		t.Errorf("a message difference on 3.13.17 failed: %q", j.errs)
	}
	if len(j.logs) != 1 || !strings.Contains(j.logs[0], "3.13.17") || !strings.Contains(j.logs[0], "3.13.5") {
		t.Errorf("a message difference on 3.13.17 logged as %q, want one line naming 3.13.17 and 3.13.5", j.logs)
	}
	j = recordingJudge{}
	compareResultOn(&j, pyRelease{3, 13, 5}, "case", nil, goUnterminated(t), py)
	if len(j.errs) != 1 {
		t.Errorf("a message difference on the reproduced 3.13.5 reported %q, want one failure", j.errs)
	}
}

// TestAcceptRejectDifferenceFailsOnEvery313: on any 3.13 patch, accepting what python rejects,
// rejecting what it accepts, another exception class, or another value is a failure.
func TestAcceptRejectDifferenceFailsOnEvery313(t *testing.T) {
	accepted := pyResult{OK: []byte(`["N"]`)}
	cases := map[string]struct {
		got any
		err error
		py  pyResult
	}{
		"python rejects, Go accepts": {[]any{"N"}, nil, pyRejects(ClassJSONDecodeError, "Expecting value: line 1 column 1 (char 0)")},
		"python accepts, Go rejects": {nil, goUnterminated(t), accepted},
		"another class":              {nil, goUnterminated(t), pyRejects(ClassValueError, "duplicate key")},
		"another value":              {[]any{"B", true}, nil, accepted},
	}
	for _, rel := range []pyRelease{{3, 13, 0}, {3, 13, 5}, {3, 13, 17}} {
		for name, c := range cases {
			var j recordingJudge
			compareResultOn(&j, rel, name, c.got, c.err, c.py)
			if len(j.errs) != 1 {
				t.Errorf("%s on %d.%d.%d reported %q, want one failure", name, rel[0], rel[1], rel[2], j.errs)
			}
		}
	}
}

// TestOracleVersion: a python3 other than 3.13 is refused with the version it is and the one
// it must be, not compared case by case; 3.13 at any patch level is the oracle.
func TestOracleVersion(t *testing.T) {
	for _, v := range []pyRelease{{3, 12, 0}, {3, 14, 0}, {2, 7, 18}, {4, 13, 5}} {
		err := oracleUnsupported(v)
		if err == nil {
			t.Fatalf("python3 %d.%d accepted as the oracle", v[0], v[1])
		}
		want := fmt.Sprintf("python3.13 is python %d.%d, and parity is proven against python 3.13", v[0], v[1])
		if !strings.Contains(err.Error(), want) {
			t.Errorf("python3 %d.%d refused as %q, want it to say %q", v[0], v[1], err, want)
		}
	}
	for _, v := range []pyRelease{{3, 13, 0}, {3, 13, reproducedPatch}, {3, 13, 17}} {
		if err := oracleUnsupported(v); err != nil {
			t.Errorf("python3 %d.%d.%d refused: %v", v[0], v[1], v[2], err)
		}
	}
}

// fakePythons is a PATH holding only the named fake pythons, each answering the version
// request as python 3.14 does.
func fakePythons(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		script := "#!/bin/sh\necho '[{\"ok\": [3, 14, 0]}]'\n"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// TestOracleIsPython313ByName: the oracle is python3.13 by name, never python3, which is
// 3.14 on a Fedora 44 host. With only that python3 on PATH, the oracle is missing, and the
// failure names python3.13 and why it is the one.
func TestOracleIsPython313ByName(t *testing.T) {
	fakePythons(t, "python3")
	_, err := askOracleVersion(t.Context())
	if err == nil {
		t.Fatal("a PATH with only python3 3.14 gave an oracle version")
	}
	for _, want := range []string{"python3.13 is not on PATH", "the ccy image's python"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing python3.13 reported as %q, want it to say %q", err, want)
		}
	}
}

// TestOracleNamedPython313ReportingAnotherVersion: a python3.13 that is not 3.13 is refused
// with both versions named.
func TestOracleNamedPython313ReportingAnotherVersion(t *testing.T) {
	fakePythons(t, "python3", "python3.13")
	v, err := askOracleVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = oracleUnsupported(v)
	if err == nil {
		t.Fatal("a python3.13 reporting 3.14 accepted as the oracle")
	}
	want := "python3.13 is python 3.14, and parity is proven against python 3.13"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("refused as %q, want it to say %q", err, want)
	}
}
