# Adversarial review: the vmtest port (branch `go-port/vmtest`, 07b70a7b..b0977f35)

Read-only review of `git diff F44...go-port/vmtest` against SPEC-vmtest.md, the deleted bash
CLI at F44, the lab play, the systemd units, the bridge (`helpers/vmtest/bridge_run.py`), the
session runner, and the qa-all golden replay. `go test -race -cover ./internal/vmtest ./cmd/vmtest`
re-run here: green, 87.0% and 87.5%.

## 1. Credentials and secrets

What crosses into the guest or the session, and how:

- The **token and passphrase** cross by `scp` from the host paths the caller gave
  (`hostonly.go:98-109`) into guest tmpfs `/dev/shm/vmtest-secrets/{gh-token,ssh-passphrase}`,
  directory 0700 and files 0600, created as the `fedora` user over ssh. Not in argv, not in env.
- The **needles** (both values, newline-trimmed) are read into Go memory and written on ssh's
  **stdin** (`hostonly.go:119-132`); the checker gets only the path `VMTEST_NEEDLES_FILE`.
- The **account name** crosses in argv of the remote command (`RUN_BASH_GITHUB_ACCOUNTS='<u>'`,
  `VMTEST_GITHUB_USER='<u>'`) and, new in this port (B16), as a line of the desktop session's
  `run.env` (`steps.go:231-240, 299-313`). It is a username, not a secret, and it has passed
  `githubUserRE` before it reaches a shell word, so it cannot break quoting.
- The **host paths** of the secret files appear in argv of the scrubber front
  (`hostonly.go:151-155`) and of `scp`. Paths, not values; the front reads the files itself.
- The **LUKS passphrase and user password crypt** of a desktop base reach `render_kickstart` on
  stdin (`build.go:229-236`); `openssl passwd -6 -stdin` takes the password on stdin
  (`build.go:265-268`); the passphrase is written 0600 (`build.go:258`). The `serial_console`
  unlock takes the passphrase **file** path (`guest.go:739`).
- Logging: the only place a value is read on the host is `pushNeedles`, and it goes to a
  stdin reader; no `logf`/`note` formats a secret. `TestE44HostOnlyRun` (`run_test.go:288-291`)
  asserts no fake call's argv carries either value; `hostonly_test.go:133` asserts no refusal
  echoes one.
- The scrubber runs on every host-only path before `judge_run` (`run.go:275-281`), and a missing
  front, library, or transcript, or a failed redact/verify, fails the run before it is judged
  (`hostonly.go:136-162`; tested at `run_test.go:323-336`).
- https is enforced before any fetch: the probe's artefact URL must start with `https://` or
  the line is malformed (`probe.go:283`), the tree URL likewise (`probe.go:319`), `lab.env`'s
  `VMTEST_REPO_URL` likewise (`lab.go:574`), and `rawURL` builds on that plus a 40-hex commit
  (`run.go:115-118`, `run.go:102`).

Nothing here violates SecurityRules.md. The desktop host-only identity (B16) is a change the
bash did not have; it delivers no additional secret, only the account and the two guest paths.

## 2. Destructive operations

- `os.RemoveAll` targets: `bases/<name>.build` (`build.go:123`, `545`) where `name` is
  validated non-empty by `parseBaseMarker` (`facts.go:397`); `members/<name>` (`member.go:860`)
  after `memberNameRE` (`member.go:598-603`); the private gpg home from `os.MkdirTemp`
  (`fetch.go:162`). No glob feeds a removal; `builtBases` globs only to **read** `base.json`
  (`freshness.go:218`). An empty lab home is impossible: `VMTEST_HOME=""` means the default
  (`lab.go:484-486`).
- The build checks `domainKnown` before `RemoveAll(st.dir)` (`build.go:120-124`), the same order
  as the bash (759 then 761).
- `virsh destroy`/`undefine` only ever name `vmtest-build-<name>`, `vmtest-run-<run-id>` or
  `vmtest-member-<name>`; `dropDomain` returns on an empty domain (`build.go:56`), and
  `removeListedDomain` treats a failed `virsh list` as an error, never "absent"
  (`member.go:608-619`). `member-down` matches the domain as a whole line.
- Ctrl-C: `WithSignals` cancels with the signal as cause (`app.go:773-791`); every long flow
  defers its teardown on `context.WithoutCancel(ctx)` (`run.go:179`, `build.go:129`,
  `member.go:753`), so the teardown's virsh calls are not themselves cancelled. Tested:
  `TestE27BuildSignal`, `TestE47RunSignal`, `TestE57MemberUpSignal`, exit 130/143/129.
- The nightly timer and units are untouched; `ExecStart=%h/.local/bin/vmtest nightly` resolves to
  the installed binary. Networking (`passtNetwork`, `member.go:665`) is identical to the bash.
- Parity kept: a passing run removes only `overlay.qcow2` (`run.go:404-408`); a failed run and a
  failed build keep their directories.

## 3. Fail-fast and exit codes

- stdout carries only marker lines, usage, `verify_response` output and `member-ssh` remote
  stdout (`sayf`, `app.go:912`); everything else is `==> `/`ERROR: ` on stderr. The bridge's
  regexes (`bridge_run.py:51-56`, `:187`) match `==> freshness:`, `==> run <id>: booting`,
  `==> waiting for SSH`, `==> guest answered SSH`, `==> fetching run.bash`, `==> provisioning`,
  `==> verdict:` — all present verbatim (`run.go:165, 202, 236, 253, 355`; `guest.go:640, 656`).
- Exit codes: 64 usage, 1 failure, 128+signal, the remote status for `member-ssh`, 124 at a
  bound, the worst gate status for `freshness-status`, both halves of `nightly` always run
  (`freshness.go:338-368`). `term.Printer` write errors fail an otherwise-OK exit
  (`app.go:882`).
- Non-zero `virsh destroy` at judge time is fatal (`run.go:331`), as in the bash under `set -e`.

## 4. The cut-over

- `tasks/build-go-command.yml` is imported with name, dest `/home/<user>/.local/bin/vmtest`
  and mode 0755; it runs `become: false` inside the `become: true` play, as its header requires.
  `play-golang.yml` is imported first; both plays target `hosts: desktop`. No task in the lab
  play is tagged, so the untagged import is not skipped by a `--tags` run.
- `lib/run-log-scrub-cli.bash` is in the 0644 loop beside its sibling; the front `source`s by
  its own realpath, so the relative layout holds.
- Callers: `bridge_run.py` (`DEFAULT_VMTEST = ~/.local/bin/vmtest`), `vmtest-nightly.service`,
  Plan 00161's acceptance — all by the unchanged installed path.
- Leftovers: `qa-gates.tsv`, `bash-limits.allowlist`, `CLAUDE/QA.md` and
  `test-vmtest-prepare-record.bash` are clean. Two remain (findings 6 and 7).

## 5. The full-QA golden replay

`tableAsRecorded` (`addedgates_test.go:448-472`) re-inserts the four retired rows before their
recorded neighbours and loads the result through `gates.Load`, and `replay` answers them from
the corpus defaults as the capture did. This is the mirror of `addedAfterPort` and is sound as
a **recording**: the goldens are what the bash printed over the table it had. It hides no
routing change: qa-changed's routing lives in `qa-routing.tsv`, which never named the four
suites; the `vmtest qa-go` row in `go-binary-suites.tsv` routes a `src-go/internal/vmtest` or
`cmd/vmtest` change to `qa-go`. Rot guards: a retired row still in the real table, a row with
no reason, or a vanished anchor (`before`) fails the test (`addedgates_test.go:453-456, 475-495`). The cost is that the corpus's `fail-vmtest-*` scenarios now exercise a synthetic
row only; that is inherent to the recording method and is documented in the spec (D5).

## 6. Tests

Every flow runs through `App.Run` over `execx.Fake`, a fake clock, an injected `bound`, a
temporary lab tree and a fake random source (`kit_test.go`). Seams are right: the Python
helpers are faked at argv (`helperArgv`), virsh/virt-install/ssh/scp/curl/gpg2 at the runner,
and the real `bash` front is tested by `test-run-log-scrub.bash` (7 new cases). Uncovered
(from `go tool cover -func`): `main`, `NewEnv`, `sleepContext`, `freeLoopbackPort` (the
production wiring; proven only by the host checks H1-H6), the error branches of `pushSecrets`
(57%), `memberTeardown` (67%), `probe.remove` (60%), `desktopSecrets` (69%). None hides a
decision; they are "the fake never failed scp/remove" branches.

## Findings

| #   | Where                                                           | Scenario                                                                                                                                                                                                                                                                                                                                                      | Severity | Fix                                                                                                                                                                                                    |
| --- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | `hostonly.go:49`, SPEC E30, `hostonly.go:13-16`                 | The `VMTEST_FROM_BRIDGE=1` gate is honoured by the CLI but **nothing sets it**: not `bridge_run.py`, not `vmtest-bridge@.service`, not `vmtest-bridge-watcher` (`git grep` at F44 finds it only in the bash CLI and its deleted suite). The spec and the Go comment call it one of three independent gates; it is an unarmed one. Pre-existing, now codified. | Medium   | Have `bridge_run.py` pass `env={**os.environ, "VMTEST_FROM_BRIDGE": "1"}` to its `Popen` of `vmtest run`/`refresh-base`, with a Python test; or reword E30 and the comment to say the gate is dormant. |
| 2   | `steps.go:231-240, 299-313` (B16)                               | A desktop host-only run's `run.env` now carries the account and the two guest secret paths. No test asserts it: `steps_test.go:253-258` covers only `RUN_BASH_GITHUB_ACCOUNTS=none`. A regression to `none` would again provision the session as no identity, silently.                                                                                       | Low      | Add a desktop host-only case asserting the three identity lines in the `run.env.tmp` stdin, and that no secret value is among them.                                                                    |
| 3   | `run.go:234`, `hostonly.go:156`, `run-log-scrub.bash:98-105`    | `r.transcript` stays open across the scrub, and `scrub_redact` replaces the inode (`os.replace`). Today nothing writes to the handle after the scrub (`judge` uses paths), so no byte is lost; the first future `r.note` after `scrub` would write to an unlinked inode and vanish from the judged transcript. The bash was immune (`>>` reopened per write). | Low      | Close `r.transcript` before `scrub` and set it nil, or make `note` open-append-close by path.                                                                                                          |
| 4   | `run_test.go:288-291`                                           | The host-only test proves no secret in any **argv**; it does not scan the captured stdout/stderr for the planted values. The Go never formats them, but the assertion that would catch a future `logf("%s", needles)` is absent.                                                                                                                              | Low      | Add `if strings.Contains(k.out+k.err, plantedToken) …` to `TestE44HostOnlyRun`.                                                                                                                        |
| 5   | `freshness.go:294` vs SPEC E50                                  | The spec says `freshness-status.txt.new`; `fsx.WriteAtomic` writes `freshness-status.txt.tmp.<pid>`. Harmless (same directory, renamed into place), but the spec is the oracle this plan chose.                                                                                                                                                               | Low      | Change E50's wording to "a temporary file in the same directory, renamed into place".                                                                                                                  |
| 6   | `src-go/qa/changed/classify_test.go:569`                        | The routing test still touches `files/home/.local/bin/vmtest` as a representative "must not run vmtest-manifest" input. The path no longer exists; the case is meaningless rather than wrong.                                                                                                                                                                 | Low      | Replace with `src-go/internal/vmtest/run.go` (which also proves the Go CLI does not route to the manifest gate).                                                                                       |
| 7   | `scripts/qa-routing.tsv`, `scripts/lib/run-log-scrub-cli.bash`  | No `also` row routes a change to `scripts/lib/run-log-scrub*.bash` to the `run-log-scrub` gate, so `qa-changed` does not run `test-run-log-scrub.bash` for an edit to the new front (nor, pre-existing, to the library). Only `qa-all` catches it.                                                                                                            | Low      | Add `also scripts/lib/run-log-scrub.bash,scripts/lib/run-log-scrub-cli.bash,git-hooks/lib/secret-scan.bash run-log-scrub -`.                                                                           |
| 8   | `src-go/internal/vmtest/*_test.go` headers (builder's own note) | Test comments cite the deleted bash suites as the origin of their cases (`member_test.go:15`, `hostonly_test.go:10`, `steps_test.go:14`, `build_test.go:280`). Accurate history, but a reader will look for files that are gone.                                                                                                                              | Low      | Reword to "the cases formerly in scripts/test-vmtest-member.bash (removed in Plan 00192)".                                                                                                             |

No finding is a correctness or security regression against the bash. The departures B1-B17
are each an improvement and each is tested except B16 (finding 2).

## Verdict

**MERGE** (after the full `qa-all` the builder could not run as a sub-agent). Findings 2-4 are
worth a follow-up commit on this branch before the host proof; finding 1 is a pre-existing
defence gap outside the port's files and should become its own small change. The behaviours
that matter most (boot, LUKS unlock, the session runner, the bridge's stage parsing) are
proven only by H1-H6 on the owner's host, which `meta-deploy.list` already queues.
