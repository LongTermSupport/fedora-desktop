# Plan 00209 Task 2.2 stage 1: src-go/internal/bus/pycompat, builder notes

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Uncommitted in the worktree (branch feat/bus-v2-s1-declare); the coordinator commits.
Files: `src-go/internal/bus/pycompat/{value,text,loads,dumps,repr,ipaddr}.go`, each with its
`_test.go`, plus `oracle_test.go` (the python3 harness, test only).

## Exported API

```go
// Values (json.loads / line-reader values). Value is sealed: None, Bool, Int, Float, Str, List, *Dict.
type Value interface{ TypeName() string; isValue() }
type None struct{}
type Bool bool
type Int struct{ V *big.Int }
func IntOf(i int64) Int
type Float float64
type Str string            // WTF-8: a lone surrogate is its 3-byte ED Ax/Bx xx form
type List []Value
type Item struct{ Key string; Value Value }
type Dict struct{ ... }    // str keys, insertion order; Set of an existing key keeps its position
func NewDict() *Dict
func (d *Dict) Set(key string, v Value)
func (d *Dict) Get(key string) (Value, bool)
func (d *Dict) Len() int
func (d *Dict) Keys() []string
func (d *Dict) Items() []Item

func Truthy(v Value) bool
func Equal(a, b Value) bool                         // python ==
func Contains(container, item Value) (bool, error)  // python `in`; TypeError as *Error
func Iterate(v Value) ([]Value, error)              // iter(); TypeError as *Error
func PyStr(v Value) string                          // str()  (named Py* because Str is the type)
func PyRepr(v Value) string                         // repr()

// Python exceptions
type ErrorClass string // ClassJSONDecodeError, ClassValueError, ClassRecursionError,
                       // ClassUnicodeDecodeError, ClassUnicodeEncodeError, ClassTypeError
type Error struct{ Class ErrorClass; Msg string; Pos int; Line, Col int }
func (e *Error) Error() string // str(exc); JSONDecodeError adds ": line L column C (char P)"

// JSON
type LoadOptions struct{ NoDuplicateKeys bool } // member.json's object_pairs_hook
func Loads(text string, opts LoadOptions) (Value, error)
func RawDecodeString(text string) (value string, endByte int, err error)
func Dumps(v Value, ensureASCII bool) (string, error)

// Text
func CodePoints(s string) []rune
func Len(s string) int
func IsSpace(r rune) bool
func Strip(s string) string
func LStrip(s string) string
func RStrip(s string) string
func SplitLines(s string, keepends bool) []string
func Lower(s string) string
func IsDigitNd(r rune) bool
func DigitValue(r rune) (int, bool)
func ParseDigits(s string) (*big.Int, bool) // int(s) for an all-Nd string, <= 4300 digits

// ipaddress
func IPAddress(s string) (string, bool) // str(ipaddress.ip_address(s)), 3.13+ mapped form
```

Caller notes:

- A caller tells the classes apart with `errors.As(err, &pe)` and `pe.Class`; v1's
  `type(exc).__name__` is `string(pe.Class)`.
- `Loads` takes the text after decoding; handed bytes that are not UTF-8 it returns
  `UnicodeDecodeError` (config.py decodes before json.loads, so that class comes first).
- With `NoDuplicateKeys`, a repeated key is `ValueError("duplicate key")` raised when its
  object CLOSES (so `{"a":1,"a":2} x` is ValueError, not Extra data; `[{"a":1,"a":2}, ]`
  is ValueError, not the trailing-comma error), as the hook does.
- An int over 4300 digits is `ValueError` (CPython's int_max_str_digits), from Loads as from
  json.loads; declare.py catches only JSONDecodeError, so v1 crashes there (B-S3 class).
- `RawDecodeString`'s end is a BYTE offset into text (python's is a code point index;
  `text[end:]` is the same tail). Text not starting with '"' is refused (plain error).
- `Dumps(v, false)` refuses an unescaped lone surrogate with python's UnicodeEncodeError
  message (what printing it raises in v1); `Dumps(v, true)` escapes it as `\ud800`, which v1
  prints fine.
- NaN: every NaN is treated as json.loads' single NaN object: `Equal(NaN, NaN)` is false at
  the top, but inside lists/dicts and for `Contains` two NaNs are equal (identity first).

## Red / green evidence

1. RED: all `_test.go` files written first; `go test ./internal/bus/pycompat/` failed to
   build: `undefined: Value`, `undefined: Dumps`, `undefined: List`, `undefined: Str`,
   `undefined: Error`, `undefined: ClassUnicodeEncodeError`, ... `[build failed]`.
2. GREEN, first run with sources: three failures, all test bugs found by the oracle:
   the 500-deep array overflowed the oracle's own `enc()` recursion in python 3.11 (the
   message lacked "while decoding", so it was not json's RecursionError: the case is now 300
   deep); a position I had miscounted (char 9, not 10); unescaped quotes inside JSON string
   cases. Fixed in the tests; then `ok ... coverage: 98.9%`.
3. RED again (caught by the oracle): refactoring scanArray/scanObject for gocognit lost the
   trailing-comma refusal: `json.loads([1,])`: python `JSONDecodeError: Expecting value: line 1 column 4 (char 3)`, Go accepted `[1]` (also `{"a":1,}`, `[1, 2, 3, ]`). GREEN after
   `afterItem` returned whether the bracket closed rather than the loop re-testing it.
4. Final: `go test -race -cover -count=1 ./internal/bus/pycompat/` ok, coverage 98.9%;
   `golangci-lint run ./internal/bus/pycompat/...` 0 issues; `go vet` clean. No //nolint, no
   discarded errors, no wall clock (floats come from a fixed splitmix64 sequence).

## Cases compared with the real python3 (3.11 in the container)

Every test that compares runs python3 through `execx.OS{}` (`python3 -I -c <oracle>`), one
process per test, strings crossing as code point lists so lone surrogates survive.

| Test                    | Requests | What                                                                                      |
| ----------------------- | -------- | ----------------------------------------------------------------------------------------- |
| TestPythonLoads         | 198      | 99 documents x (plain, object_pairs_hook): value tree or class + full str(exc)            |
| TestPythonRawDecode     | 10       | raw_decode: value and end                                                                 |
| TestPythonDumps         | 42       | 21 values x ensure_ascii True/False, incl. the UnicodeEncodeError message                 |
| TestPythonValueOps      | 3280     | 40 values: bool(), iter(), and every pair for == and `in` (incl. TypeError messages)      |
| TestPythonStrRepr       | 168      | str()/repr() of values and tricky strings, repr of every text case                        |
| TestPythonFloatRepr     | 3184     | float.__repr__: specials, 3000 pseudo-random bit patterns, powers of ten                  |
| TestPythonText          | 208      | strip/lstrip/rstrip/len, splitlines both ways, lower, int() of digit strings              |
| TestPythonIPAddress     | 84       | ip_address + str(); 7 IPv4-mapped answers set aside on 3.11 (B-S5)                        |
| TestPythonUnicodeTables | 1        | EVERY code point (1,114,112): isspace, isprintable, lower, isdecimal + value, final sigma |

7,175 compared requests, plus the exhaustive per-code-point table. In the table, 4,489 code
points differ, every one unassigned in python 3.11's Unicode 14.0 or in Go's 15.0 (the one
allowed difference); no assigned code point differs.

## What could not be matched exactly, and why

- RecursionError threshold: CPython's json limit is its recursion limit less the frames
  already on the stack (3.11 refuses at ~996 nesting from the top level, fewer from inside
  declare.py/config.py); 3.13 counts C recursion separately. Go refuses nesting > 990 with
  the same class and message. Only the class at "very deep" is proven, not the exact depth.
- IPv4-mapped IPv6: python 3.11 writes `::ffff:102:304`; the host (3.13+) writes dotted.
  Go writes dotted (B-S5), asserted directly in TestIPv4MappedIsDotted; the 3.11 answers are
  set aside, and the test fails if none were (so the cases cannot silently lose them).
- Messages that changed after 3.11 and could not be checked here: 3.13 reports a trailing
  comma as "Illegal trailing comma before end of array/object" (Go keeps 3.11's "Expecting
  value" / "Expecting property name..."; the CLASS is the same, and v1 prints only the class);
  3.14 rewords the `in` TypeError. Messages follow 3.11.
- UnicodeDecodeError message: Go names the first bad byte and its position but not python's
  reason suffix (": invalid start byte"); the class matches. Not oracle-compared (the
  oracle hands python a str).
- Unicode edition: Go's tables are 15.0, python 3.11's 14.0, the host's 3.13 15.1. Code points
  new in one edition may answer differently (Cn vs assigned); none that both assign do.
- No python `str.isdigit` (it is wider than Nd: superscripts, circled digits) because Go has
  no Numeric_Type table: `IsDigitNd` + `ParseDigits` answer "not a number" where v1's
  isdigit()-then-int() crashed (the B-S3 class the brief names).

## QA

- `./scripts/qa-changed.bash` from the worktree: the main module (`src-go`, this package
  included) passed; the run failed only in the nested `src-go/qa` module, in
  `qa/cmd/qa-patterns` `TestRealSemgrepScansTestsDirectoriesAndLargeScripts`: real semgrep
  timed out on its own generated `scripts/big.bash` fixture. Re-run alone it failed the same
  way with the host's load average at 36-48. The package builds its own temporary git tree and
  touches nothing in pycompat; it is the host's load, not this change. Log:
  `untracked/scratch/261021-pycompat-qa-changed.log`.
