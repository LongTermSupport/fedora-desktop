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
