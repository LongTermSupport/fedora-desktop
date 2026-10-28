# Plan 00192 criterion 3: the defect-class audit of the P6 ports

Branch `plan/00192-defect-class-rules` (from F44 `77d68ab96`). Commits: `0b5f9081e` (the rule,
red), `641356a12` (its findings fixed, green), and the commit that carries this report (the
catalogue's recorded non-rules, PLAN.md's criteria). Not pushed.

Ports audited: reclaim, open, nord, nvidia-status, the ssh-with-key family, and vmtest's
prune-runs, retention move and sweep fix. Sources: each `SPEC-*.md` departure list (the bash's
defects), the Fable reviews (`261019-open-port-review-fable.md`, `261028-fable-review-{nord, nvidia-status,ssh-with-key,vmtest-retention,sweep}.md`), the fix reports
(`261027-reclaim-review-fixes.md`, `261028-nvidia-status-follow-ups.md`,
`261028-vmtest-follow-ups.md`, `261028-sweep-keeps-evidence.md`), and the host findings in
PLAN.md Task 3.3 and 3.10.

## Classes and dispositions

| #   | Class                                                        | Instances                                                                         | Disposition                                                                               |
| --- | ------------------------------------------------------------ | --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| 1   | Duration built from an unbounded number (overflow)           | vmtest F1 (`--older-than`), and the sweep's 5 more (below)                        | (b) new rule `project_duration_overflow`                                                  |
| 2   | Error or status thrown away                                  | nord B3 (`\|\| true`), B11 (`2>/dev/null`); reclaim B3, B6, B7; open B5, B10, B11 | (a) `errcheck`, `nilerr`, hook R-ERROR-HIDING                                             |
| 3   | Path joined from a user's name                               | nord B2 (`connect ../x`)                                                          | (a) gosec G304                                                                            |
| 4   | Shell template                                               | open B8 (`bash -c` handlers)                                                      | (a) `project_seam_commands` (`sh -c`)                                                     |
| 5   | Probe that cannot tell no match from error; status misread   | nvidia B2, B6, B8, F2; reclaim B3; open B11; ssh-with-key B11                     | (c) extends the existing readiness-probe entry                                            |
| 6   | Secret in argv, output or log                                | nord B3, B11, nmcli echo                                                          | (c) `execx.Secret`/`Feed` run-time refusal, `creds.hide`, `wantNoPassword`                |
| 7   | Output on the wrong stream, escapes into a pipe              | open B12; nvidia B3, B10                                                          | (c) stream-apart tests, `IsTerminal` guard                                                |
| 8   | Removal wider than the action means                          | sweep B19 (whole run dirs), live build's disk; reclaim B13, B16; nord B18         | (c) tests naming what survives, specs, default-no questions; RemoveAll table rule weighed |
| 9   | Ordering by a value the code itself changes                  | sweep's directory mtime                                                           | (c) order by run id; `TestE51Sweep` inverts mtimes                                        |
| 10  | Live thing taken as a leftover                               | sweep's running build; prune-runs' live run                                       | (c) libvirt listing, `run.lock`, tests                                                    |
| 11  | Record says done before it is                                | retention.log "evicted" before a failed removal; partial prune unnamed; nord U5   | (c) `TestE62PruneRunsRemovalFails`, `TestE51SweepRemovalFails`                            |
| 12  | Interrupt kills a change half made, or is misread            | nord B16, F2; reclaim B15, M-1; open B20 (finding 1)                              | (c) `WithoutCancel`, `NewSession`, cancel tests, real-SIGINT test                         |
| 13  | Value another program re-parses, read or written differently | nord B17 (`,` in a password); open B16 (`-` path); ssh B3, B4, B12, U1, U2        | (c) refusal before the program, option-string tests, host checks                          |
| 14  | Structured value matched as loose text                       | nvidia B1, B5; nord B8; open B3, B15                                              | (c) field parsers, near-miss tests                                                        |
| 15  | Fake answers what the real program does not                  | reclaim M-2; nvidia F2; nord F2; ssh-copy-id B11                                  | (c) `execx.ErrUnexpected` fakes, real error shapes, host checks                           |
| 16  | Verdict names the wrong next step                            | nvidia B14, B15, B19; open B17; nord B7, nvidia B12                               | (c) verdict tests, review                                                                 |
| 17  | Command unbounded, or resource never given back              | nvidia B11; vmtest's overlays (108G); ssh B7 agent                                | (c) fixed bounds, `prune-runs` + disk guard, ssh-agent command mode                       |
| 18  | Keys typed between screens lost                              | ssh-with-key U6                                                                   | (c) the existing terminal-echo entry, extended                                            |
| 19  | Mechanical but one site                                      | vmtest F7 (`Bsize` for `Frsize`)                                                  | (c) one function (`freeOf`), `TestFreeOfCountsFragments`                                  |

Not counted as defect classes: vmtest F5 (a dead branch, no hazard) and the exit-code and
heading conventions (nord B15, reclaim B1, B11: house style, held by `term.ExitCode` and ui).

## (a): the existing defences, seen firing

A temporary package `src-go/internal/zzprobe` (removed, never committed) with
`f.Close()`, `os.ReadFile(filepath.Join(dir, name))` and `time.Duration(days) * 24 * time.Hour`;
`golangci-lint run` from `src-go` reported `errcheck` on the first and gosec `G304` on the
second, and nothing on the third. `project_seam_commands`' `sh -c` finding is proven by
`TestSeamCommands` over the `badcmd` fixture (green). So class 1 was not held: `durationcheck`
allows `time.Duration(x) * time.Second` by design; it fired only on the new fixture's
`stdtime` import alias.

## (b): `project_duration_overflow`

DBF (method specification 1.1.0, fetched). Hazard: a Duration from input wraps negative and means
the opposite of what was asked. The independent searcher (`defence-before-fix:independent-searcher`,
dispatched before the rule; report under `untracked/agent-reports/auto/`) found 5 unbounded
sites and the prune cutoff bounded in another function; it also named the call chains text
search could not show (the configured minute behind a field, a check that looked like a bound,
a number from another program). The rule found exactly those six; nothing beyond them.

- Red: fixture `qa/rules/testdata/src/fix/internal/durations` (14 wants, the ways round
  included) and the owner stub; at `0b5f9081e` `./scripts/qa-changed.bash` failed with 6
  findings (journal `074430Z-finding-c784f4`).
- Green at `641356a12` (journal `075004Z-action-507a2a`): vmtest `planPrune` uses
  `started.AddDate`; `VMTEST_MEMBER_SSH_TIMEOUT` at most 86400; ccy-sessions knobs at most
  86400 s, `verify-restore --wait` at most 86400, `--in`/`--minutes` at most 10080,
  `secondWarning` through the new `internal/duration.Times`; wsi's grace at most the schema's
  30 s. Tests first, each seen red; the prune test (`--older-than 106752` and `1<<40`) reproduces
  the originating defect. qa-changed passed (ccy-sessions reboot 342, take-over 46,
  wsi-stop-grace 121 and the rest).
- Next wider rule, named and not built: any arithmetic on an input-derived Duration (needs data
  flow across functions; a Duration that arrives as one is already in range).
- Second rule weighed and not built: "every `os.RemoveAll` carries a table row" (class 8): the
  row would carry the same wrong belief as the code.
- The conformance reviewer was not dispatched (context budget); the red and green runs are
  reproducible from the two commits through `./scripts/qa-changed.bash`.

## Criterion 2

Not ticked. It holds for the ports without a bash suite (open, rclone-cache-status, rclone-tail,
setup-rclone, reclaim, nord, nvidia-status: PORTS.md "it had no suite"), for wsi (its
`test-wsi-stop-grace.bash` drives the Go binary) and for freeze and qa-all (golden replays). It
is not shown for vmtest: its four bash suites were deleted at cut-over with their cases moved
into Go tests, and nothing records them run against the Go binary first; that is an oracle
replaced, like freeze's, but the criterion's text does not name it, so the owner or a later pass
should either record it as the replacement or say it falls short. The record was not read for
ftp-camera, watermark, gh-account-setup (PORTS.md mentions "the suite's git stub") and the
ssh-with-key family; the PLAN.md text says so.

## Host

`meta-deploy.list`'s `changed-plays` description now also expects play-vm-test-lab.yml,
play-claude-yolo.yml (ccy-sessions) and play-speech-to-text.yml (wsi) to rebuild; nothing else.
No CCY bump: `internal/sessions` and `wsi` are not in the launcher front's build.
