# Batch run diagnosis — four failures

Read-only diagnosis from the newest run logs under `untracked/plan-runs/`.
Host identifiers are replaced with `<user>` / `<repo-root>` throughout.

Sources read (newest timestamp dir per unit):

- `untracked/plan-runs/00098-…/acceptance/<ts2>/acceptance.log` (and `<ts1>` for contrast)
- `untracked/plan-runs/00109-…/acceptance/<ts2>/acceptance.log`, `…/deploy/<ts2>/deploy.log`
- `untracked/plan-runs/00112-…/acceptance/<ts2>/acceptance.log` + `plan-00112-acceptance-report.md`
- `untracked/plan-runs/032-…/deploy/<ts2>/deploy.log` (and `<ts1>` for contrast)

---

## 1. 032 deploy — exit 1. NOT a defect in the plan. Operator typing error.

Failing point, quoted from the log:

```
>>> type "change-this-machine" and press Enter to proceed:
==> not confirmed (got "its change-this-machine", expected "change-this-machine")
[FATAL] change gate not confirmed — aborting before the first mutating leg
```

The gate is `plan_gate_change` at `CLAUDE/Plan/032-compression-helpers/deploy.bash:81`,
implemented by `plan_confirm` in `CLAUDE/Plan/_planlib.inc.bash:583-602`. The reply read at
`_planlib.inc.bash:594` was `its change-this-machine` — the expected token with a two-word
prefix in front of it. Exact-match compare at line 597 rejected it, `plan_confirm` returned 1,
and the script aborted before leg 1. **Nothing ran, nothing changed.** The earlier run of the
same day's deploy completed all legs with `changed=0`, so the play itself is fine.

Verdict: **not a product defect and not a gate defect in the correctness sense.** One thing IS
worth fixing, though:

> `plan_confirm` hard-aborts on the first mistyped token. `CLAUDE.md` → "Interactive Scripts:
> Human-Friendly, Standardised" requires *strict validation, friendly recovery* — show the
> error and **re-prompt in a bounded loop**, reserving hard aborts for unrecoverable states.
> A mistyped consent token is the textbook recoverable input mistake. This failure is exactly
> the cost of that: a whole deploy leg discarded over a typo, in the middle of a batch.
> `_planlib.inc.bash:594-601` is the single place to fix it for every plan.

This also explains the wrapper oddity in the batch (`032/deploy` FAIL, `032/acceptance` PASS):
the acceptance judged the state a *previous* deploy left behind. You noted that wrapper bug is
already fixed; the deploy failure above is independent of it.

---

## 2. 00098 acceptance — exit 1. REAL host finding. NOT a conversion defect.

Failing assertion, quoted:

```
### 5. desktop store contained
  [FAIL] 1 director(ies) under /<user-home>/.claude are group/other-accessible
         Containment is broken — files below them are reachable by other local users.
         Deploy it: ansible-playbook <repo-root>/playbooks/imports/play-claude-code.yml
```

Emitted by `report_store()` at
`CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/acceptance.bash:161-164`, called for
the desktop store at line 263.

### The conversion is exonerated

You asked specifically whether the `plan_start_log auto` conversion (`ba2c7d65`) caused this.
It did not, and the two runs prove it:

| run                        | check 5                                                                                                   | verdict                    |
| -------------------------- | --------------------------------------------------------------------------------------------------------- | -------------------------- |
| earlier (in CCY container) | `[SKIP] desktop store — not present here (inside CCY, <home>/.claude is a symlink into the project store)` | PASS, 6 checks, 0 failures |
| newest (on the HOST)       | `[FAIL] 1 director(ies) …`                                                                                | FAIL                       |

The guard at `acceptance.bash:262` is `[ -d "$HOME/.claude" ] && [ ! -L "$HOME/.claude" ]`.
In the container that is false (symlink), so check 5 skipped. On the host it is true, so the
check **ran for the first time**. The conversion changed only how the log is captured; the
banner, the check bodies and the verdict logic are untouched and every other check still
reports normally. This is a check that had never been exercised, not a check that broke.

### Is it real?

Yes — it is a genuine group/other-accessible directory under the desktop store. But it is also
an **expected consequence of remediation never having been applied on this host**:
`CLAUDE/Plan/00098-…/` contains `PLAN.md`, `acceptance.bash`, `triage.bash`, `research/`,
`JOURNAL/` — **there is no `deploy.bash`**. The batch therefore ran the gate with no deploy leg
ahead of it, and the remedy the gate itself names
(`ansible-playbook playbooks/imports/play-claude-code.yml`) was never run.

Two follow-ups for the plan owner:

1. **The plan has no `deploy.bash`.** Its gate names a playbook in prose instead. Under the
   batch harness that guarantees a FAIL forever: the harness runs `deploy.bash` then
   `acceptance.bash`, and 00098 supplies only the second half.
2. **The gate reports a count, not a path.** `report_store()` prints
   `"$open_dirs director(ies)"` and never names *which*. With one offending directory that is
   a needless round trip — the operator cannot act on the finding without re-deriving it by
   hand. `count_open_dirs` already walks them; printing them costs nothing.

---

## 3. 00112 acceptance — exit 2. NOT an abort. A designed third verdict, structurally unreachable under this batch.

**Exit 2 is not an abort and not a failure.** `acceptance.bash:31` and `:76` document it:

```
#   2  COULD NOT ESTABLISH — nothing failed, but a check had no evidence to judge …
Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH, 64 = usage error.
```

The gate ran to completion and judged everything it could:

```
COVERAGE: 10 of 10 checks executed (9 assertion(s) passed, 0 failed, 1 could not be established)
```

So the verdict **is** a verdict — it is simply a third one, and the batch wrapper collapsed it
into "FAIL". Nine assertions passed; zero failed.

The one unestablished check:

```
[5] the deploy removed nothing from the enabled list (before/after triage evidence)
  UNKNOWN  no before/after triage pair bracketing a deploy run
           run, in order: ./triage.bash, ./deploy.bash, ./deploy.bash, ./triage.bash — then this gate.
```

### The real finding here is a harness/gate mismatch

Check [5] requires the operator to bracket the deploy with `triage.bash` runs. The batch
harness runs exactly `deploy.bash` then `acceptance.bash` and nothing else. **Under this
harness, plan 00112 can never exit 0**, no matter how healthy the host is. That is not a
product defect and not, strictly, a bug in the check — it is a contract the gate states and the
harness cannot satisfy. Either the harness learns the plan's required run order, or check [5]
learns to demote itself to a NOTE when invoked with no triage pair available.

Nothing about the extension state is wrong: all 9 declared extensions are deployed, enabled,
honoured, ACTIVE, idempotent on re-run, and the deployed VM-lab checker is byte-identical to
the repo copy.

---

## 4. 00109 acceptance — exit 1. ONE root cause explains 7 of the 8 failures. One is separate and may be real.

`COVERAGE: 19 of 19 checks executed (13 assertion(s) passed, 8 failed)`.

### Root cause: no logout since the deploy

Six of the eight are one fact, stated by the gate itself at check \[12\]:

```
[12] host-health.service has run at a login and did not fail
  FAIL  host-health.service has never started
        log out and log back in — that is what fires it, and it is also the only way the shell loads the panel
```

The unit is a `systemd --user` unit wanted by `graphical-session.target`; it fires at login and
produces the status document every downstream check reads. It has never fired, so:

| check | failure                                       | follows from                                                                                       |
| ----- | --------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| [12]  | `host-health.service has never started`       | **ROOT**                                                                                           |
| [1]   | `no status document at …/host-status.json`    | [12] — the unit is the producer                                                                    |
| [2]   | `the reader could not use the document`       | [1] — nothing to read                                                                              |
| [3]   | `the document is missing 4 section(s)`        | [1]                                                                                                |
| [4]   | `carries no installed-vs-pinned state`        | [3]                                                                                                |
| [5]   | `carries no play-freshness state`             | [3]                                                                                                |
| [15]  | `the extension is not loaded in this session` | same root fact — on Wayland only a new login loads new extension code; the gate's own text says so |

**One finding, six consequences.** Exactly as you anticipated: the plan cannot fully pass until
the operator logs out and back in. Do not treat these as seven defects.

Everything the gate *could* establish without a login passed: the ledger is complete with 14
plays folded and all 4 deploy plays recorded, a non-applying ansible run adds no ledger row
(byte-identical ledger across a `--list-tasks` run), the login report honours its exit-3
contract, the panel is deployed complete with its uuid in `enabled-extensions`, the deployed
reader and producer agree on the document path, the VM-lab scenario is allowlisted, the remote
resolves non-interactively, and the DisplayLink recovery is deployed and armed.

### The eighth failure is NOT part of that cascade — treat it separately

```
[11] host-health.service is enabled and wanted by graphical-session.target
  FAIL  host-health.service is 'enabled' but graphical-session.target does not name it
        it will never fire at login; the .wants symlink under the user unit directory is missing or stale
```

Source: `acceptance.bash:683-699`. `systemctl --user is-enabled` returned `enabled` (a
filesystem read — so the `graphical-session.target.wants/` symlink **does** exist on disk),
while `systemctl --user list-dependencies graphical-session.target` (a live-manager read) did
not name the unit.

This is precisely the symptom `playbooks/imports/optional/common/play-host-health-login-report.yml`
already carries a fix for, in the comment above *Reload The User Manager So It Sees The New
Wants-Symlink*:

> `daemon_reload` on the task above cannot do the job … the module runs the reload FIRST,
> before it touches the enabled state … Observed: `is-enabled` reported `enabled` while
> `list-dependencies graphical-session.target` did not name it, three minutes after a
> successful deploy. **Lingering keeps `user@example.com` alive across logout, so the stale
> graph does not clear itself by logging out either.**

The deploy log for this run shows the fix ran and reported success:

```
TASK [Enable The Login-Time Health Report]                      ok: [localhost]
TASK [Reload The User Manager So It Sees The New Wants-Symlink] ok: [localhost]
PLAY RECAP  localhost : ok=9  changed=0  failed=0  skipped=10
```

So the standalone reload task executed **after** the enable, and the manager still does not
name the unit. Two consequences worth stating plainly:

1. **[11] is a candidate REAL defect — the play's fix may not actually work, or may not cover
   this state.** It is the only failure in this run not explained by the missing logout, and by
   the play's own documentation the logout will **not** clear it (lingering).
2. **If [11] is real, it also blocks the cascade fix.** A unit the live manager does not pull
   into `graphical-session.target` will not fire at the next login either — so the logout that
   is supposed to discharge [12] and its five dependants would come back green on nothing.
   **Resolve [11] before asking the operator to log out**, otherwise the logout is a wasted
   round trip and the re-run reports the identical eight failures.

Discriminating evidence to collect on the host (read-only, non-paging, for the operator):

```bash
systemctl --user list-dependencies graphical-session.target --no-pager | cat
ls -l ~/.config/systemd/user/graphical-session.target.wants/ | cat
systemctl --user show host-health.service -p LoadState -p UnitFileState -p WantedBy --no-pager | cat
loginctl show-user "$USER" -p Linger --no-pager | cat
```

If the symlink is present and `LoadState=loaded` yet the target still omits it, the play's
reload is genuinely insufficient and the fix needs revisiting — `systemctl --user daemon-reload` does not always re-evaluate an already-active target's wants, and a
`systemctl --user restart graphical-session.target`-class step (or an explicit re-enable after
the reload) may be required.

---

## Summary table

| unit             | exit | root cause                                                                                                                                                                                                       | class                                                                                                                                      |
| ---------------- | ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| 032 deploy       | 1    | consent token mistyped as `its <token>`; aborted before any mutating leg                                                                                                                                         | operator input; `plan_confirm` should re-prompt per the project's interactive-script rule                                                  |
| 00098 acceptance | 1    | one group/other-accessible directory under the desktop store; check 5 ran for the first time (it SKIPs in the container)                                                                                         | real host finding; expected — the remedy play was never run, and the plan ships no `deploy.bash`. **Not** caused by the logging conversion |
| 00112 acceptance | 2    | check [5] needs a triage/deploy/deploy/triage bracket the batch harness never provides                                                                                                                           | not a failure at all — 9 passed, 0 failed; harness/gate contract mismatch                                                                  |
| 00109 acceptance | 1    | `host-health.service` has never fired at a login → [1][2][3][4][5][15] all follow. **Plus** a separate \[11\]: the unit is enabled on disk but the live manager does not pull it into `graphical-session.target` | 7 of 8 are one expected consequence of no logout; [11] is a candidate real defect and gates the others                                     |
