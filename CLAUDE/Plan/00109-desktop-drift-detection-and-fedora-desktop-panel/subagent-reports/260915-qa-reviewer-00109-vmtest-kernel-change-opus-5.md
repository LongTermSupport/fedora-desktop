# qa-reviewer — `server-host-health-kernel-change`, range `216dd9c5..a32739ed`

**Transcribed by the coordinator.** The reviewer runs read-only: `Write`/`Edit` are
withheld for that role and its operating rules forbid Bash writes, so it returned the
report in a message and asked for it to be filed here. The wording below is the
reviewer's; only this note and the closing status are not.

The final section of its report was truncated in transit at the nits; the remainder was
requested and is appended when it arrives.

## Verdict: **BLOCK**

The scenario cannot pass. Reproduced, not inferred.

## Blocking

**1. The fixture's record is unparseable bash.**
`guest-prepare-…kernel-change.bash:59,136` — `record()` writes `printf '%s=%s\n'`
unquoted; line 136 emits
`PREPARED_FIXTURE_FINDING=vmtest-health-fixture.service: failed (system scope)`. `(` is a
metacharacter → `syntax error near unexpected token '('`, `source` returns 2, **every key
after line 9 is unset**. The file's own header (`:31`) promises "free-form values base64"
and this is the one free-form value not base64'd. Fix in `record()` (`%q` or base64), not
at 136 — `PREPARED_TIMER_ENABLED` (`:101`) and `PREPARED_TIMER_AFTER` (`:187`) capture
`systemctl is-enabled` with `2>&1` and are the same exposure.

**2. The checker guards the record's absence, not its brokenness.**
`guest-acceptance-…kernel-change.bash:125-131` — `[[ -r ]]` then a bare `source`, status
discarded (`set -e` off). Then `:213` reads `${PREPARED_DOCUMENT_SHA:0:12}` with no `:-`
default and `set -u` kills the run. Ran it against the real record: 7 checks emitted, no
`VMTEST-CHECKS-DONE`, `transcript.py` verdict `error` — pointing the reader at the lab,
not the fixture.

**3. `clean-login-is-silent` fails on stderr noise, and passes on nothing.**
Fixture `:78`, checker `:100,165-169` — both capture `bash -lic true </dev/null 2>&1`.
Measured here (incl. under `setsid`, all pipes): 116 bytes of `bash: cannot set terminal process group … / no job control in this shell` on **stderr**. `ssh` without `-t` ⇒ no
pty ⇒ this always fires ⇒ check 4 fails. `scripts/test-host-health-login-snippet.bash:130-146`
already settled this — *"the snippet's stdout … is the only stream that matters"* — and
drops stderr with `2>/dev/null`. Separately the check is **fail-open**:
`${CLEAN_LOGIN_B64:-}` → `""` → `pass`. It did exactly that in my reproduction. Every
sibling defaults to a failing sentinel; this one defaults to the passing answer.

## Should fix

04. `FINDING_COLLECT_RC` recorded, never judged (fixture `:139`); the `[[ -r DOCUMENT ]]`
    guard (`:70`) is satisfied by the *clean* document. Check 2 is named
    `collect-service-ran` but judges only the CLEAN run.
05. `remote-fetches-without-an-agent` (`:266-276`) is true by construction — `vmtest:108`
    requires an `https` `VMTEST_REPO_URL`. Rename; print `git remote get-url origin` in the
    detail.
06. `play-vm-test-lab.yml:178` — `with_fileglob` is *silent* on an empty match (task skips,
    nothing deployed). The sibling at `:146-148` uses `loop:` +
    `lookup(fileglob, …, wantlist=true)`; this is the only `with_*` left in `playbooks/`.
    The comment's claim that `qa-deployed-drift.bash` makes the sets agree "by
    construction" is wrong: drift globs `*` (`qa-deployed-drift.bash:205`) vs the play's
    `*.bash`, and drift *skips* never-deployed files on the passing line (`:219-222`).
07. `guest_prepare` prints nothing when no fixture is found (`vmtest:1014-1016`) — a
    missing deployment and "no fixture" look identical in the transcript.
08. `server_reboot` (`vmtest:974-980`) has no boot-identity gate; checker emits `boot_id`
    evidence (`:292`) with nothing to compare it to.
09. The 6 mutants all targeted the dispatcher/gates; **nothing ever ran the fixture→checker
    contract**. `test-vmtest-reboot-dispatch.bash` also never covers the `if` at
    `vmtest:1232` that calls both functions — the actual seam.
10. `PLAN.md`: `[x] ✅ The VM lab runs this route end to end` sits above the unticked
    `⬜ VM` run. Never executed, and per 1–3 cannot be.

## Nits

6 recorded-but-unread keys (`:103,132,145,188` + finding 4); inert
`VMTEST-GUEST-PREPARE-DONE` (`:227`); *(remainder truncated in transit — requested)*.

---

## Disposition

Every blocking finding reproduced independently before being acted on. Findings 1 and 3
were verified by running the exact shapes: `source` of the unquoted record exits 2 and
leaves every later key unset, and `bash -lic true` emits 116 bytes on stderr with 0 on
stdout.

| #   | Action                                                                                                                                                                                                                                                                                               |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `record()` now uses `%q`, so no value can opt out of quoting. Header corrected — the record is shell-quoted, and base64 is only for multi-line captures                                                                                                                                              |
| 2   | `source`'s status is judged; a `REQUIRED_KEYS` list asserts completeness. Both failures exit 70 before any check is emitted                                                                                                                                                                          |
| 3   | Fixture and checker capture stdout and stderr separately; the check judges **stdout**, matching `test-host-health-login-snippet.bash`'s settled reasoning. stderr is recorded as evidence. The fail-open default is gone — no read carries a `:-`                                                    |
| 4   | Check 2 judges both collections; new check 2a requires the second collection to have rewritten the document, which `[[ -r ]]` could not tell                                                                                                                                                         |
| 5   | Renamed `this-guests-remote-fetches-without-an-agent`, prints the remote URL, and says in the file that it cannot fail for §6's reason                                                                                                                                                               |
| 6   | `loop:` + `lookup(fileglob, …, wantlist=true)` matching the sibling, plus an explicit assert on an empty match. The wrong claim about `qa-deployed-drift.bash` is replaced by an accurate one                                                                                                        |
| 7   | `guest_prepare` says so in the transcript when no fixture resolves                                                                                                                                                                                                                                   |
| 8   | `server_reboot` reads the boot id before and after and polls until it changes, dying at 180s                                                                                                                                                                                                         |
| 9   | `scripts/test-vmtest-prepare-record.bash` — 7 cases driving the real `record()` into the real checker. Three mutants; the third (`source` status discarded) initially **survived**, because the completeness assertion subsumed it, so a case was added where the record is complete AND unparseable |
| 10  | The tick is gone. The task now says the scenario has never been executed and that nothing under it is established until it has                                                                                                                                                                       |

`PLANNED` 14 → 15 and the manifest with it.

**On finding 9, the generalisation.** `qa-00109-t32` observed in parallel that
`max_skipped: 0` guards a check that *skips*, not one that runs on an empty population.
All fifteen were walked: fourteen fail on an empty or absent value because each compares
against an asserted key; check 12 iterates a list and now asserts that list is non-empty;
check 4 passes on nothing **by definition** and is therefore paired with check 5, which
requires a live fault through the same login. That audit is recorded in the checker's own
header rather than here, so the next person to add a check meets the question.
