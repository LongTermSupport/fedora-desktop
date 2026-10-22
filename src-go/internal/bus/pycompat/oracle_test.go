package pycompat

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"sync"
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
        return list(sys.version_info[:2])
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

// runOracle runs the real python3 once over a batch. A missing python3 fails the test: the
// parity proof cannot be skipped.
func runOracle(t *testing.T, reqs []pyReq) []pyResult {
	t.Helper()
	batch := make([][]any, 0, len(reqs))
	for _, r := range reqs {
		batch = append(batch, []any{r.op, r.args})
	}
	in, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	res, err := execx.OS{}.Run(t.Context(), execx.Cmd{
		Name: "python3", Args: []string{"-I", "-c", oracleScript}, Stdin: bytes.NewReader(in),
	})
	if err != nil {
		t.Fatalf("python3: %v", err)
	}
	var out []pyResult
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		t.Fatalf("python3 answer: %v", err)
	}
	if len(out) != len(reqs) {
		t.Fatalf("python3 answered %d of %d requests", len(out), len(reqs))
	}
	return out
}

// python is runOracle with the count logged, so `go test -v` shows how many cases each
// parity test compared.
func python(t *testing.T, reqs []pyReq) []pyResult {
	t.Helper()
	out := runOracle(t, reqs)
	v := pythonVersion(t)
	t.Logf("PYCOMPARED %d cases with python3 %d.%d", len(reqs), v[0], v[1])
	return out
}

var (
	versionOnce  sync.Once
	versionMM    [2]int
	errVersionMM error
)

// pythonVersion is the (major, minor) of the python3 the oracle runs.
func pythonVersion(t *testing.T) [2]int {
	t.Helper()
	versionOnce.Do(func() {
		out := runOracle(t, []pyReq{{op: "version", args: []any{}}})
		if out[0].failed() {
			errVersionMM = strconv.ErrSyntax
			return
		}
		errVersionMM = json.Unmarshal(out[0].OK, &versionMM)
	})
	if errVersionMM != nil {
		t.Fatalf("python3 version: %v", errVersionMM)
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
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var x, y any
	if err := json.Unmarshal(ab, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

// pyErrText is python's exception as "Class: message", for failure output.
func pyErrText(r pyResult) string { return r.Err + ": " + string(r.Msg) }
