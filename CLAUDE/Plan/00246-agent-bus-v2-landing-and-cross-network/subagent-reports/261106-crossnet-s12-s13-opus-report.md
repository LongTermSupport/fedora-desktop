# Task 8.8, S12 and S13: the builder's report

Branch `feat/crossnet-s12-s13`, from `spike/v2-cross-network`. S12 (X31) is built, tested and
committed. S13 (X32) is not built: the context budget ran out after S12 and its rule (see "Not
done").

## S12, X31: unattended leaf renewal

### What is built

- **One leaf step, apply's and the tick's** (`host/leaf.go`, `leafStep.ensure`). Apply's
  `ensureLeaf`/`leafVerdict` in `host/tls.go` are gone: apply now calls `leafStep.ensure` with the
  CA it loaded or made, and the tick calls the same function through `host.LeafRenewer.Renew`. The
  judgement is X14's own (`pki.Leaf.NeedsRenewal`, plus apply's existing "missing, torn or signed by
  another CA is made again"), the files are written where apply writes them, with apply's modes,
  the key before the certificate. The url-and-listen-set to names rule is one function too
  (`leafNames`), which apply and `host.AppliedLeafNames` share.
- **The CA key is read only when a leaf must be made.** The verdict needs only the CA certificate
  (0644), so a tick with nothing to make never touches the key, and an unreadable key is a
  problem only when it matters (at 29 days, not at 31).
- **The restart rule.** `host.Terminator.servesOldLeaf`: an active terminator that started before
  the leaf in force was made serves the leaf it read at its start, so it is restarted. This one
  rule covers "a leaf was just made" and "a restart failed after the leaf was written" (the next
  tick sees the terminator older than the leaf and restarts it), so a failed restart is tried
  again at every tick without any state kept between ticks. The make time comes from the leaf
  itself: `pki.Leaf.MadeAt` (NotBefore plus the back-dating skew, to the second). An inactive
  terminator is neither started nor restarted (it reads the new leaf at its start; X18c reports
  it).
- **The tick** (`health/leaf.go`): after the terminator's state is read, under the team lock taken
  without waiting (a busy lock: nothing this tick), with the stamp and the restore record read
  again under the lock; never for a caller that holds the lock (`Refresh`), never while a restore
  is recorded (HB3). A made leaf and a restart are events on the record. A failure is one problem:
  - leaf still valid: `the TLS leaf could not be made again; it expires on <YYYY-MM-DD>: <cause>`;
  - leaf past its end: `the TLS leaf expired on <date>: no member can reach the homeserver; it could not be made again: <cause>`;
  - no leaf could be read: `the TLS leaf cannot be judged: <cause>`;
    with the fix `teamchat-admin health <team>, then apply the team again (<play>)`.
    The cause is a fixed class (`host.LeafError.Cause`: the CA certificate or key file that cannot
    be read, the leaf directory that cannot be written, the unit whose restart failed, the names
    that cannot be read), never the error's own text (see the rule below).
- `health.Config` has a new required part, `Leaves`; `admin/wire` wires `host.LeafRenewer` with
  the host's root, the wall clock, `crypto/rand`, the D-Bus units manager and
  `host.AppliedLeafNames(root)`. The restart in apply's own `convergeTLS` is unchanged (it is
  coalesced with the drop-in's restart, after the drop-in is written); see readings.
- `health/probe.go`: the homeserver restart's "lock, re-check the stamp, re-check the restore,
  act" prelude is now `underLock`, shared with the renewal rather than copied.

### Tests (X31's list, and what each proves)

`host/leaf_test.go` (the step itself, over an applied world): applied names; 31 days, nothing;
29 days, made, one restart of the terminator unit, modes 0600/0644, the next renewal nothing; a
changed name set at any age; the CA key gone: an error naming the key file, the old end, no
restart, nothing changed, twice, and at 31 days no error at all; a failed restart tried again at
the next renewal; an inactive terminator neither started nor restarted; an expired leaf made
again. `health/leaf_test.go` (the tick, with host's real `LeafRenewer` over a scratch tree of a
real CA and leaf, the fake units restarting the terminator at the fake clock's time): 31 days;
29 days, the leaf made, the terminator restarted once, the record clean, the event, the next tick
nothing; a changed name set; the CA key unreadable at 29 days, one problem naming the date and the
key file, no restart, the same problem at the next tick; an expired leaf, its own problem; a
restore recorded, nothing judged; the CA certificate gone, "cannot be judged"; the renewal asked
once per tick and never by `Refresh`. `pki/leaf_test.go`: `MadeAt` of a made and a parsed leaf.

### Planted defects, each seen red and restored

| Planted                                                                  | Red                                                                                     |
| ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| `MadeAt` without the skew                                                | `TestALeafSaysWhenItWasMade`                                                            |
| restart whenever the terminator is active (no "started before the leaf") | `TestARenewalWithThirtyOneDaysLeftDoesNothing`, `...TwentyNineDays...RestartsOnce`      |
| restart whether or not the terminator is active                          | `TestARenewalRestartsNoInactiveTerminator`                                              |
| restart only when this renewal made the leaf                             | `TestARestartThatFailedIsTriedAgain`                                                    |
| the CA key loaded before the verdict                                     | `TestARenewalThatCannotReadTheCAKeyChangesNothing` (the 31-day half)                    |
| `Leaves` missing from the production wiring                              | `admin/wire` `TestEverySeamIsSetAndNothingIsRead`, `TestStatusIsTheTicks`               |
| the problem built from `err.Error()` (the first draft)                   | `TestAnUnreadableCAKeyIs...`, `TestAnExpiredLeaf...`: the record was not written at all |
| the tick renews for a caller holding the lock (`held` guard removed)     | `TestTheRenewalIsAskedOncePerTickAndNotByRefresh`                                       |
| the expired and the still-valid problem swapped                          | `TestAnUnreadableCAKeyIs...`, `TestAnExpiredLeafIsAProblemOfItsOwn`                     |
| both restore guards removed (before the lock, and under it)              | `TestNoLeafIsJudgedWhileARestoreIsRecorded` (and the homeserver's own restore test)     |
| the rule's `problemTexts` answering nothing                              | `TestProblemErrorText`: every flagged fixture line missed                               |

### Defence Before Fix: `project_problem_error_text`

The first draft put `err.Error()` in the problem. With the CA key gone, `loadCA`'s refusal and its
advice made the problem longer than `status.OneLine`'s 512 bytes in the test's scratch tree, and
`status.Write` refused the record: the team's whole record was not written, by the very failure
it should report. The class: an error's own text in a status problem. Rule
`project_problem_error_text` (`qa/rules/problemtext.go`, catalogue in `CLAUDE/GoProjectRules.md`):
red on its fixture, red on the real tree (exactly the three problems of `health/leaf.go`), the
sweep over `./internal/teamchat/...` found no other instance, and green after the fix
(`host.LeafError`). It runs in the analyzers stage with the other project rules, so it blocks.
The sweep after the fix, run with the driver by hand: `go vet -vettool=<driver>` over
`./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...` untagged, and over
`./internal/teamchat/...` with `busfailpoints` and with `tuwunel`: no finding. Only the restore
guard plant needed both guards gone to go red: the under-lock re-read of the restore record alone
already keeps the renewal from running.

## Readings where the spec was silent

- **Where the tick runs, and as whom.** `teamchat-admin health --all`, run by
  `teamchat-health.service` (DESIGN-v2-C-host 3.11.1): root, `ProtectSystem=strict`,
  `CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID CAP_SYS_PTRACE`,
  `ReadWritePaths=/var/lib/teamchat-status /run/teamchat-admin`. The CA key is root's, mode 0600,
  so the tick (uid 0, the owner) reads it by its owner bits; nothing about the key's mode changes.
  **But the leaf cannot be written under that sandbox**: `/var/lib/teamchat-admin/<team>/tls/` is
  not in `ReadWritePaths`, so under `ProtectSystem=strict` every renewal would fail with "the leaf
  cannot be written to ...", shown on the record while the leaf is valid. The unit text is not
  written yet (S7b's report: "not written, as told"), so nothing is wrong today; see findings.
- **Which names the tick wants.** The listen set units.json records and the url host of
  team.json, both written by the last apply (the same source `serve` uses for the terminator). A
  failed apply that wrote a new leaf but not units.json makes the tick put the old names back
  until the next apply; that is drift an apply ends, and the tick never invents names.
- **An expired leaf with a readable CA** is simply made again (no problem); "an expired leaf is a
  problem of its own" is read as the expired leaf that cannot be made again.
- **No restart from apply's path was changed.** Apply restarts the terminator once for a changed
  drop-in and a made leaf together, after the drop-in is written; moving the restart into the
  shared step would restart twice or before the drop-in. Apply's restart is remembered in memory
  only (`leafMade`), so an apply whose restart failed and an apply run again would not restart;
  the tick's "terminator older than its leaf" rule now heals that within one tick. Reported, not
  changed in apply.
- **Refresh (a caller holding the lock) never renews**, as it never restarts; the next tick does.
- **A busy lock** (an apply running) is no problem and no renewal this tick.

## Findings for the coordinator

1. **The health unit's sandbox must allow the leaf's write** when `teamchat-health.service` is
   written: `ReadWritePaths` must include `/var/lib/teamchat-admin` (or each team's `tls/`
   directory), or X31 cannot work on a host. The key's mode stays 0600 root.
2. **The health unit's `IPAddressAllow=localhost`** (3.11.1) will also refuse X32's probe of the
   non-loopback listen addresses, when S13 is built: the unit's filter needs the listen addresses
   (or the effective networks), as the terminator's drop-in has them.
3. Apply's restart memory (above) is healed by the tick, not fixed in apply.

## Gate results

- `./scripts/qa-changed.bash --base spike/v2-cross-network`, run on the staged S12 tree: exit
  1, `✗ qa-changed FAILED: go`. go-qa's scope was the whole project (widened because
  `qa/rules/problemtext.go` changed). fmt, record, suppression, summary, docs, golangci,
  analyzers, deadcode, teamchat-failpoints-lint and -vet, project-rules-tagged, vuln, mod and
  build passed. `test` FAILED, and its one failing package is `qa/cmd/qa-js`. Its four tests
  (`TestRealNestedConfigAndInlineDisableCannotWeakenTheRules`,
  `TestRealToolsPassACleanTreeAndWriteNothingIntoIt`, `TestRealToolsFailASyntaxErrorAndALintFinding`,
  `TestRealMissingEslintToolingIsExitTwo`) failed with "the checkout's extensions/node_modules
  ... is needed for this test". That is the known environment failure (issue #135) and not part
  of this change. `qa/rules` passed. The log names `error[test]: the tests failed in qa` only.
  Also green: docs, teamchat-keeps-off-v1, `test-ccy-container-version-hook.bash` (121) and
  `test-qa-changed.bash` (179).
- Run by hand: `go test -race -shuffle=on ./internal/teamchat/...` passed, and so did
  `./qa/rules/` after the rule count went to 29 in `TestAllIsValid` (it was red at 28). golangci-lint
  reported 0 issues on health, host, pki, admin/wire and qa/rules. The driver sweep above found
  no issues.

## Not done

- **S13 (X32), the probe through the terminator, is not built.** Design read, for the next
  builder: a `matrix` constructor (the one home of transport configuration, X19) that reuses
  `newPin` with the listen address as the URL host (so the server name verified is that address,
  X16), `newHTTPClient`, `pin.detail` for the handshake class with the pinned fingerprint, and a
  bound by `matrix.Timers.AfterFunc` cancelling the request (as `client.go` does), so a test drives
  it with `clock.Fake` (`AwaitDue`, `Advance`); each address in its own goroutine, started in
  `terminator()` and collected at the end of `gather`, so a slow address delays nothing else; one
  problem per failing address with a fixed cause class (refused, the handshake's class with the
  fingerprint, no answer within the bound), only while the unit is active and never while a
  restore is recorded. Finding 2 applies.
- S14 and S15 not started, as instructed.
