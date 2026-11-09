# CI green: the GitHub QA workflow against the runner's environment (Task 4.12)

Branch `fix/ci-green`, from F44 at 977a206f1. The failing run was a `qa-all` on an
`ubuntu-26.04` runner, umask 022, uid 1001, Python 3.12.

## Failing steps in the run (the whole log, not only the three reported)

| Step / gate           | Failure                                                     | Cause                                                                                                              |
| --------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| gitleaks job          | 2 `generic-api-key` findings                                | the identifiers `password` and `esckeyPkg` hold a keyword and are assigned a string of 10+ chars                   |
| `go`: pycompat        | `TestPythonLoads`, trailing-comma messages                  | CI's Python 3.12; 3.13 changed the message; Fedora 44 ships 3.14                                                   |
| `go`: difftest        | `TestFrontParity`, `TestFrontAnswersItself`, every scenario | goldens recorded under umask 077 (a CCY container); the runner has 022                                             |
| `go`: containerimages | `TestScanPrintsTheStoreTheOwnersAndTheDirectories`          | its assertion runs only under a non-root uid outside the fixture's ranges; the expectation was wrong               |
| `helper-tests`        | `agent_bus_lxc.test_derive` SessionsScriptTest              | Ubuntu 26.04's coreutils is multi-call (uutils): `sleep` symlinked as `claude` picks its tool by argv[0] and exits |
| `go`: difftest (time) | `panic: test timed out after 10m0s`, in both runs           | CPU-bound: 658s measured locally on 4 cores under `-race`; go-qa passes no `-timeout`                              |

The colour lines in the difftest failures are not a separate cause: the front's allowed colour
difference (`colourAllowance`) applies only when the rest of the golden matches, and the modes
did not.

## Fixes

1. **gitleaks**: the names no longer look like secrets: `password` became `loginTyped`
   (`src-go/internal/consumer/app_test.go`), `esckeyPkg` became `joinedPkg`
   (`src-go/qa/rules/escjoined.go`). No allowlist entry or inline suppression. Local
   `gitleaks dir .` (8.30.1, CI's version): 2 leaks before, none after.
2. **Python**: pycompat reproduces python 3.13, the ccy image's. The first CI iteration set up
   3.14 (Fedora 44's) and showed 3.14 differs as well: the `in` TypeError and bad-escape
   messages, the Unicode tables, and the nesting depth (`seat`'s TestParityWithV1). So CI sets
   up 3.13, and the oracle refuses any version but 3.13 with that reason (`oracleUnsupported`,
   `TestOracleVersion`, red first), instead of a page of diffs; proven with a fake python3 that
   says 3.12. Nothing else pins a Python (`ruff.toml`, `.qa-versions`, the toolchain test). The
   IP test's pre-3.13 branch could no longer run and was removed. **For the owner**: the Go v1
   readers do not match the host's python 3.14, and a go test run with 3.14 first on PATH is
   refused.
2a. **difftest timeout**: the Go gate passes `-timeout=30m` through `GOFLAGS`
   (`scripts/qa-go-qa.bash`); go build, vet and list ignore it (checked). The same limit holds
   locally and in CI.
3. **Umask, under Defence Before Fix.** Class: a QA verdict that depends on the umask of the
   machine running it (a dimension of `suite-env-isolated`, Task 4.11). Hazard: a suite that
   records or compares modes of created files passes on one machine and fails on another, with
   no code change. Defence: every `qa-all` and `qa-changed` command runs under umask 077
   (`gates.SuiteUmask`). A umask is a process attribute and Go cannot set it per child, so each
   runner sets its own once, before its first command, through a new `Env.Umask`
   (`syscall.Umask` in both mains); a run without one is refused (exit 2). Guards, red first
   (on a fake machine at 022: 120 rows and commands reported, and again with the pin call
   removed after the switch to 077), then green: `TestSuiteEnvIsolatedEveryRowRunsUnderTheSuiteUmask`,
   `TestSuiteEnvIsolatedNoUmaskControlRunsNothing` (`qa/all`),
   `TestSuiteEnvIsolatedEveryCommandRunsUnderTheSuiteUmask`,
   `TestSuiteEnvIsolatedNoUmaskControlRunsNoCheck` (`qa/changed`). They are in the `go` gate,
   which blocks.
   - **Why 077, and the goldens are unchanged.** The goldens' 0700/0600 were recorded under
     the container's 077. A first cut pinned 022 and re-recorded the goldens (a mode-only diff).
     The coordinator rejected that, and it was the wrong trade: it moves every recording to suit
     one machine. 077 is the umask every suite was written and recorded under, and every
     container `qa-all` has run green under it. With the pin at 077, re-recording reproduces the
     committed goldens byte for byte, so the branch carries no golden change.
   - **Why both the runner and the suite.** The suite also pins 077 in its `TestMain`, as it
     gives itself a HOME, so a direct `go test` agrees with QA (it passes from a 022 shell). The
     runner pin is the class defence, which no suite has to remember (Task 4.11 found suites did
     not).
   - **Sweep**: under 077 every suite is green (every container `qa-all`). Under 022, the
     machine the pin now protects against: both Go modules' whole test suites locally (only the
     difftest failed; the qa module, nothing), the hook-handler tests (127 passed; CI exempts
     them), and the CI run itself, a complete `qa-all`, whose only other failures are the
     non-umask causes above.
4. **containerimages** (not umask): under a foreign uid the scan reports `overlay` itself (one
   path, its own store directory by O2) and `overlay/L1` (three); the test expected only the
   second. Fixed to assert both, by key and count. In the container the test runs as root,
   where the assertion does not run; CI is its proof.
5. **helper test** (not umask): the fake `claude` is this test's python under that name, not
   `sleep`, which a multi-call coreutils cannot run under another name.

## For the owner and the coordinator

- Seen while sweeping: under umask 022 (a plain login terminal) the launcher creates
  `~/.claude-tokens`, `.claude/ccy` and the update-check cache world-readable (0755/0644), and
  its permission repair then reports them (`repaired 4 owner-only`) on the same run. That is
  not changed here. Whether the launcher should create them owner-only itself is the owner's
  decision.
- gitleaks runs only in CI, so a leak finding is never seen before a push. A local gate needs
  gitleaks installed on the host by a play first.
- Two classes found here are not umask: an assertion that runs only on some machines (uid), and
  a test that relies on a tool's argv[0] behaviour. Each was fixed as its single instance; no
  detector was written for them.

## Addendum: the oracle is python3.13 by name (branch `fix/ci-green-py313`)

The fix above checked the version of `python3`. On a Fedora 44 host `python3` is 3.14, so the
host's full QA would have failed pycompat on every run, however the machine was set up.

- **The oracle is `python3.13`, never `python3`** (`oracleCommand` in
  `src-go/internal/bus/pycompat/oracle_test.go`). The version check stays: a `python3.13` that
  reports another version fails with both named (`python3.13 is python 3.14, and parity is
  proven against python 3.13, the ccy image's`). A missing `python3.13` fails naming it and
  why (`python3.13 is not on PATH: the Go reproduces python 3.13, the ccy image's python ...`).
  Nothing skips. Tests first, red as a build failure, then green:
  `TestOracleIsPython313ByName` (a PATH holding only a fake `python3` that says 3.14) and
  `TestOracleNamedPython313ReportingAnotherVersion` (a fake `python3.13` that says 3.14).
- **seat's v1 parity run** (`TestParityWithV1`), the other test found to differ under 3.14
  (nesting depth), also runs `python3.13` by name.
- **CI**: `actions/setup-python` with `python-version: "3.13"` installs a CPython whose `bin/`
  holds `python3.13` beside `python3` and `python`, and puts it first on PATH. The workflow
  comment says so.
- **Host (IaC)**: `playbooks/imports/play-python.yml` installs Fedora's `python3.13` package
  beside the QA toolchain's other packages. `CLAUDE/Plan/meta-deploy.list` gains a
  `changed-plays` entry for it; the host agent runs `./CLAUDE/Plan/meta-deploy.bash`.
- Other tests that run `python3` (declare, playlock, the helpers) were not found to depend on
  the version and are unchanged.

### CI at 244a24d5b (run 41438845818): one difference left, a decision

CI found `python3.13` (setup-python's CPython 3.13.17) and accepted it as 3.13
(`PYCOMPARED 198 cases with python3.13 3.13`). Everything else in `qa-all` passed (seat's
v1 parity run among them), and so did gitleaks. One case of `TestPythonLoads` failed:

```text
json.loads("é): python JSONDecodeError: Unterminated string starting at: line 1 column 1 (char 0), Go <nil> Invalid \uXXXX escape: line 1 column 3 (char 2)
```

The Go gives the message of 3.13.5, the python3 of the ccy image (Debian 13's). 3.13.17 words
a truncated `\u` escape at the end of the input differently. So the oracle is a patch release,
not a minor version:

- CI's `ubuntu-latest` is ubuntu-26.04, and actions/python-versions builds 3.13.5 only for
  22.04 and 24.04, so `python-version: "3.13.5"` cannot be set up on it.
- Fedora's `python3.13` package is a later 3.13 patch than Debian's, so the host would fail
  the same case.

Which python the Go reproduces (the image's 3.13.5, the newest 3.13, or both, with this
message per patch release) is the coordinator's and the owner's decision. It is not changed
here.

## Addendum: the coordinator's decision, applied (branch `fix/ci-green-py313b`)

Nothing reads Python's exact error wording, so v1's readers must agree with Python on accept
against reject and on the decoded value, not on the words.

- **Always on, any python3.13**: what Python rejects the Go rejects with the same exception
  class (seat names the class in its refusal), and what Python accepts decodes to the same
  value. A difference fails.
- **The message and its position** are compared only when the oracle is the reproduced
  release, 3.13.5 (`reproducedPatch`, beside `oracleVersion` in
  `src-go/internal/bus/pycompat/oracle_test.go`). On any other 3.13 a different message is
  logged with `t.Logf` naming both releases, never failed and never silently skipped. The
  oracle now reports its patch release (`pyRelease`), and `PYCOMPARED` lines name it.
- **Where**: `compareResult` (`value_test.go`), now `compareResultOn` over a small `judge`
  interface, is the only place the pycompat oracle tests compare a message: loads, raw decode,
  dumps and the value ops all go through it. The IP, text, repr and Unicode-table tests
  compare accept against reject and values only. seat's `TestParityWithV1` compares v1's
  stderr, but v1's refusal names only the exception class (`notJSON` in `seat/member.go`),
  never Python's message, so it needs no change.
- **Tests first** (red as a build failure, `compareResultOn` undefined, then green):
  `TestMessageDifferenceOnAnotherPatchIsLogged` (a wording difference on 3.13.17 passes with
  one log line naming 3.13.17 and 3.13.5, and fails on 3.13.5) and
  `TestAcceptRejectDifferenceFailsOnEvery313` (accepting what Python rejects, rejecting what
  it accepts, another class, or another value fails on 3.13.0, 3.13.5 and 3.13.17).
- Locally against python3.13 3.13.5 every pycompat oracle test passes with messages compared.
