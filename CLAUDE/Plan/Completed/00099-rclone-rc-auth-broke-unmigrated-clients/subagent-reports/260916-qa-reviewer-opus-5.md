# QA Review — Plan 00099, full diff (delivery commit `942fb724`)

**Verdict**: FIX-BEFORE-MERGE — 0 blocking, 3 should-fix, 8 minor, 3 nits.

Reviewed from the diff, not from the conversation. The delivery commit is
`942fb724` ("Plan 00072: authenticate every rclone RC client, not just one") —
the plan was renumbered from 00072 to 00099 after a collision, so the commit
message and the code comments it shipped cite the pre-renumber numbers.
`scripts/qa-deployed-drift.bash` has since been edited by Plans 00081, 00110 and
00122; those changes are noted where they bear on 00099's claims but are not
themselves reviewed.

The core engineering is sound. All five of the plan's load-bearing claims verify,
including the fail-fast one by execution. What is wrong is concentrated in the
plan's own verification instrument and its documentation, plus one live defect in
the shipped clients that reproduces the plan's own theme.

## Blocking

None. Nothing here breaks another user, loses data, leaks private information, or
violates a HARD RULE.

## Should fix

### M1 — A missing helper library is reported as `rc unreachable`, the exact misdiagnosis this plan exists to kill

`files/home/.local/bin/rclone-cache-status:199-203`, `files/home/.local/bin/rclone-tail:144-148`

Each client defines a stub `rclone_rc` for the case where `rclone-rc-auth.bash`
is not deployed. The stub writes to stderr (`rclone-cache-status:44-46`,
`rclone-tail:36-38`):

```
ERROR: rclone RC helper library not found: $_RC_LIB
  It is deployed by play-rclone.yml. Deploy it with:
    ansible-playbook playbooks/imports/optional/common/play-rclone.yml
```

That stderr is captured into `$err_file` and classified:

```bash
case "$reason" in
    *401* | *Unauthorized*) echo "ERR|rc rejected credentials (401)" ;;
    *credential*)           echo "ERR|rc credential missing" ;;
    *)                      echo "ERR|rc unreachable" ;;
esac
```

The stub message contains neither `401` nor the lowercase word `credential`, so
it lands in the `*)` bucket. **On a host where `play-rclone.yml` has not been run
— which is precisely the deploy-gap scenario that produced this plan — the user
is told `error: rc unreachable`**, the mount reads as dead, and the stub's
accurate "run this play" text is discarded on the floor.

This is the same collapse-of-distinguishable-causes that the journal (09:20,
09:29) identifies as why the original outage looked like a dead mount for a week.
The library's own absent-credential message does match (`"rclone RC credential
file not found"` contains `credential`), so only the library-absent path is
affected — which is why it was not caught.

**Fix**: add an arm ahead of `*)` in both files, e.g.
`*"helper library not found"*) echo "ERR|rc helper library missing — run play-rclone.yml" ;;`.
Both are deployed scripts, so this needs a deploy.

### M2 — `acceptance.bash` renders a verdict with no denominator

`CLAUDE/Plan/00099-.../acceptance.bash:255-258` prints
`ACCEPTED — $PASS check(s) passed.`

There is no expected total anywhere in the script, and the number of `ok` calls
is **data-dependent**:

- check `[2]` emits **two** passes when `$RC_LIB` is readable and **one fail**
  when it is not (lines 110-121);
- check `[3]` emits **one** pass, or **N** fails — one per bypassing file (137-153);
- `[2]`'s second half, `[6]` and `[6b]` are each conditional on `$RC_LIB`.

So "ACCEPTED — 10 check(s) passed" reads identically whether 10 of 10 ran or 10
of 12. That is the repo's named recurring class — coverage implied by a count
rather than stated (`CLAUDE/AgentNotes.md`, *A partial result read as a complete
one*), which demands a `COVERAGE: n of m` line.

This is not theoretical: it is the direct cause of the three irreconcilable
figures in the record. `PLAN.md:110-114` says **9 failed / 1 passed** then
**8/8**; the commit message says **10/10**. The journal (10:12, line 279) does
explain it — checks `[0]` and `[6b]` were added between the two runs — but
nothing in the gate's own output would have let a reader work that out.

**Fix**: declare the expected check list, and print `COVERAGE: n of m checks
executed` alongside the verdict so a check that stops running is visible.

### M3 — Every shipped client still cites a wrong, real plan number

| File | Line | Says | Should say |
| --- | --- | --- | --- |
| `files/home/.local/bin/rclone-rc-auth.bash` | 12 | `Plan 00067 gave the mount units --rc-user/--rc-pass` | 00094 |
| `files/home/.local/bin/rclone-tail` | 138 | `a 401 from the authenticated RC (Plan 00067)` | 00094 |
| `files/home/.local/bin/rclone-cache-status` | 191 | `the Plan 00067 auth change` | 00094 |
| `files/home/.local/bin/ftp-camera` | 1096 | `(Plan 00067)` | 00094 |

`CLAUDE/Plan/Completed/00067-qa-gates-inert-in-nested-checkout` is a **real,
unrelated plan**, so a reader tracing why the RC is authenticated is sent to the
wrong place with no signal that they have been.

This is a *partial* sweep, which is why it is worth reporting rather than
shrugging at. Commit `d1f0529c` ("renumbered plan references in comments",
`00067->00094`, `00072->00099`) fixed **every other file in this diff** —
`play-rclone.yml`, `play-ftp-camera.yml`, `scripts/qa-all.bash`,
`scripts/qa-deployed-drift.bash`, `CLAUDE/QA.md` — and touched nothing under
`files/home/.local/bin/`. Impact is comprehension only; no behaviour changes.

**Fix**: four comment edits. Note these are deployed scripts — the drift gate
will fail on the host until `play-rclone.yml` and `play-ftp-camera.yml` are
re-run, which is the gate working as designed.

## Minor

### m1 — `acceptance.bash --help` documents 7 checks; the script runs 9

`acceptance.bash:26-34` lists checks 1-7. The script has nine numbered sections
(lines 78, 91, 109, 135, 157, 179, 203, 224, 245) — `[0]` and `[6b]` were added
after the review recorded at journal 10:05 and the help text was never updated.
A reader of `--help` under-counts the gate's own coverage. Same defect family as
M2.

### m2 — "ACCEPTED 8/8" is the number in `PLAN.md` and in the plan index

`PLAN.md:114` and `CLAUDE/Plan/README.md:151` both record `ACCEPTED 8/8`. The
final accepted run was **10/10** (journal 10:12). The README row is the figure a
reader meets first, and it is two checks short of what the plan actually proved.

### m3 — The Delivery section names no commit

`PLAN.md:181-185` has only `- Plan opened; root cause confirmed by triage (F1-F6)`.
Because the delivery commit's subject line says "Plan 00072", `git log
--grep=00099` returns nothing and the diff is effectively unfindable from the
plan. Record `942fb724` and note the renumbering.

### m4 — `acceptance.bash` hardcodes port 5572, which the repo no longer guarantees

`acceptance.bash:51` — `RC_ADDR="localhost:5572"`, used by checks `[1]`, `[6]`
and `[6b]`. Since commit `a68c99bb` the unit template defaults the RC port to
`rclone_rc_port_base + mount_index` (`play-rclone.yml:101,388`), so only the
*first* mount is on 5572, and an explicit `rc_port` can move even that one.
Check `[6b]` compounds it: it picks `findmnt … | head -n1`'s filesystem and
issues `vfs/refresh` for it against port 5572, which may belong to a different
mount.

This degrades **loudly** (a refused connection or an unknown-fs error both hit
`bad`), so it is not a silent pass — but on a multi-mount host the gate now
REJECTS for a reason that has nothing to do with what it is testing. 00099's own
clients (`rclone-cache-status`, `rclone-tail`, and `rclone-cache-warm` since
`a68c99bb`) all read the address off the mount's own rclone process; the
acceptance gate is the last thing still assuming the default. *A later plan
changed the fact under 00099's gate.*

### m5 — `CLAUDE/QA.md` does not document the linked-worktree skip

`CLAUDE/QA.md:566-571` names two skip conditions — CCY container and clean CI
checkout. `scripts/qa-deployed-drift.bash:74-89` has a **third**: a linked git
worktree, added by Plan 00099 itself (journal 10:05, 10:25). The documented skip
set is a reader's only way to know when this gate is inert, so an undocumented
skip is the clean-vs-blind problem one level up.

Separately, and *not* 00099's drift: the same QA.md section describes the gate's
scope as `files/home/.local/bin/` only, while `qa-deployed-drift.bash:214-220`
now also compares the VM acceptance lab and the freeze library via `EXTRA_PAIRS`
(Plans 00110, 00122). The gate's own header comment (lines 16-20) is current;
QA.md is not.

### m6 — The three plan scripts do not use `_planlib.inc.bash`

`triage.bash`, `deploy.bash` and `acceptance.bash` each hand-roll the repo-root
walk, the run log and the ansible invocation. `CLAUDE/Plan/CLAUDE.md` marks this
IN STONE: *"Build these on `_planlib.inc.bash` — source it and use its primitives
rather than hand-rolling the repo-root walk, the run log, the prompts, the change
gate or the ansible invocation."*

The library and `CLAUDE/PlanScriptStandards.md` landed in `73396b34` on
**2026-07-29**, about three weeks before this plan. 39 of 92 plan-folder scripts
source it, including every plan numbered 00104 and above.

Two things to be fair about:

- The documented failure mode does **not** apply. All three use
  `git -C "$PLAN_DIR" rev-parse --show-toplevel`, and the `-C` anchors resolution
  to the script's own directory, so the Plan 00068 "resolves the wrong repo"
  incident the CORRECTION block describes cannot happen here.
- There is **no scrubbing loss**. `_planlib.inc.bash:436-437` prints that plan run
  logs are deliberately unscrubbed and that `scripts/lib/run-log-scrub.bash` is
  not wired in, so the sanctioned path is no safer than the hand-rolled one.

The live consequence is placement: `triage.bash:47-50` writes to
`<plan>/logs/rclone-rc-clients-triage.log`, not the
`untracked/plan-runs/<plan>/<script>/<timestamp>/` location `plan_start_log` now
owns — so a gitignored `logs/` directory (currently holding one 0-byte file) will
travel into `Completed/` when this plan is archived. That location rule postdates
the plan (`80530c35`, 2026-09-15); the `_planlib` rule does not.

### m7 — The drift gate marks a skip with a TICK, the same symbol as a real pass

`scripts/qa-deployed-drift.bash:69, 85, 92`. Verified in today's run:

```
✓ deployed-drift: skipped (CCY container — no deployed copies to compare); 1 template(s) verified to map to a playbook dest:
```

— one tick among thirty, visually indistinguishable from a gate that did work.
This repo already uses `⚠` as the "ran but incomplete" stage symbol
(`qa-bash.bash:163`, `qa-patterns.bash:297,321`, and `qa-docs.bash:147-149`
explicitly calls a `⚠`-then-`✓` pair "a shape `verdicts.py` parses").

**Judged, since the brief asked**: this is a readability defect, not a blindness
one. `helpers/qa_environment/verdicts.py:43` parses the line as a stage, and the
class compares the **whole verdict line** including the detail — so a
container-skipped run and a host run differ in the detail text and the
comparison does surface it. A human scanning `qa-all.bash` output is the one who
is misled. `⚠` would cost one character and fix that.

### m8 — `acceptance.bash` discards the diagnosis it exists to surface

`acceptance.bash:116` and `:211` redirect the library's stderr to `/dev/null`
and print a generic failure (`"credential could not be loaded"`, `"preflight
probe failed"`). The library carefully distinguishes *credential file absent*
from *credential file present but valueless* (`rclone-rc-auth.bash:58-78`), and
`rclone_rc_available` reports the actual probe error (`:107`). None of that
reaches the operator reading a REJECTED verdict. Small instance of the same
theme.

## Nits

### n1 — `awk -F= '{ print $2 }'` truncates a value at its first `=`

`files/home/.local/bin/rclone-rc-auth.bash:68-69` (and the same shape in
`triage.bash:131-132`). Safe **only** because `play-rclone.yml:305` generates the
password from `chars=['ascii_letters', 'digits']`; nothing links the parser to
that constraint, and the library's own comment invites overriding
`RCLONE_RC_AUTH_FILE`. A value containing `=` yields a silently truncated
password, which surfaces as a 401 and reads as a credential mismatch.
`substr($0, index($0, "=") + 1)` is unconditionally correct.

### n2 — Asymmetric `case` arms in `rclone-tail`

`rclone-tail:154-157` omits the `*credential*` arm that the identical `case` at
`:144-148` has. Unreachable today — the `vfs/stats` call at `:141` fails first on
the same cause and returns — so this is asymmetry inviting a future divergence,
not a live bug.

### n3 — Journal ordering

`JOURNAL/00099-Journal-26-08-16.md`: `09:18` precedes `09:17` at the top of the
file. The `10:06` correction entry discloses the `09:31`-after-`09:40` inversion
but not this one. Append-only discipline is otherwise correctly observed — the
correction is a new entry, not an edit.

## Checked and clean

- **Claim 1 — one source of truth.** `grep -rn 'rclone rc ' files/ playbooks/
  scripts/ helpers/ tests/ docs/ CLAUDE/` returns exactly three live hits:
  `rclone-rc-auth.bash:91` (the library's own call), `rclone-cache-warm:120` (a
  diagnostic string), and `play-rclone.yml:41` (a comment). **No caller was
  missed.** The only other hits are in plan folders (00094's archived scripts,
  00099's own triage/acceptance).
- **Claim 2 — fail-fast, no unauthenticated fallback. VERIFIED BY EXECUTION**,
  not by reading. Drove `rclone_rc_load_credentials` and `rclone_rc` with the
  credential file absent and with a readable file carrying no `RCLONE_RC_*` keys:
  both return 1 with a play-naming stderr message, stdout is empty, and
  `rclone_rc` never reaches `rclone` (no unauthenticated call is issued). The
  half-populated case takes the same branch by inspection —
  `rclone-rc-auth.bash:71` ORs both emptiness tests.
- **Claim 3 — password never in argv.** `rclone-rc-auth.bash:91` uses an
  environment prefix scoped to the child. Nothing echoes, logs, or interpolates
  the value; the error messages print the *path* only. `triage.bash:137-140`
  carries the same fix and additionally reports the credential by **length only**
  (`:109`). `triage.bash:220` filters any `pass`-bearing line out of the unit
  status before it reaches the log.
- **Claim 4 — the false premise is dead.** Swept `files/`, `playbooks/`, `docs/`,
  `CLAUDE/` and `scripts/`. Every surviving occurrence of "unauthenticated" is
  either the correction itself (`play-rclone.yml:264-272`,
  `rclone-rc-auth.bash:14-17`) or unrelated. Plan 00094's own archived files still
  contain the original claim, which is correct — the required correction was
  appended as a new journal entry
  (`Completed/00094-.../JOURNAL/00094-Journal-26-08-16.md:54-59`, "superseded by
  Plan 00099"), not a rewrite.
- **Claim 5 — the owning play is derived, not tabulated.** `owning_play()`
  (`qa-deployed-drift.bash:108-145`) greps the playbooks in two tiers: an
  anchored `src:` declaration, then templated-`src:` plays that name the basename
  in a loop list. No hardcoded table. The tier-1 regex correctly escapes dots, so
  `rclone-rc-auth.bash` resolves to `play-rclone.yml:459`.
- **Fail-fast compliance.** No `|| true`, no `ignore_errors`, no unannotated
  `failed_when: false` anywhere in the diff. The four `2>/dev/null` occurrences
  (`acceptance.bash:116,211`, `qa-deployed-drift.bash:82,83`) are all
  probe-then-check: the exit status is consumed by the enclosing `if`. Every
  `$( )` capture whose value is tested has its status checked —
  `rclone-cache-status:196`, `rclone-tail:141,151`, `ftp-camera:1131,1176,1182`
  all use `if ! var=$(…)`. `deploy.bash:83-92` explicitly guards the `grep -v`
  that empties in the normal case (the bug the journal records introducing and
  fixing).
- **Stderr hygiene.** `rclone_rc`'s stdout is exclusively rclone's response;
  `rclone_rc_load_credentials` writes only to stderr and emits nothing on stdout
  (confirmed by capture). Consumers redirect the RC call's stderr to a **file**,
  not into the capture, so a warning on a successful call cannot corrupt the JSON
  payload (`rclone-cache-status:193-196`). `ftp-camera:1131`'s `2>&1 > /dev/null`
  ordering is the correct capture-stderr-only idiom and is deliberate.
- **IaC graph placement.** The library is owned by `play-rclone.yml:457-466`
  alongside every other rclone helper, at `0644` with a comment explaining that
  it is sourced rather than executed, and deployed **before** its consumers.
  `play-ftp-camera.yml:216-225` declares the cross-play dependency in a comment
  and deliberately does **not** duplicate the copy task — with a stated reason
  (the FTP/`--sort`/`--pass` modes must keep working on a machine with no
  rclone), backed by the naming stub at `ftp-camera:17-30`. **The exact mistake
  that caused this plan — a file fixed in one play and deployed by another — is
  not repeated**, and `deploy.bash` runs both plays for the same reason. Neither
  play is in `playbook-main.yml` (both are opt-in under `imports/optional/`), so
  there is no import-ordering question.
- **No new play was created where an edit belonged.** The change is entirely
  edits to two existing plays plus one new file each in `files/` and `scripts/`.
- **Naming.** `rclone-rc-auth.bash` and `qa-deployed-drift.bash` both say what
  they do. No "hygiene", "management", "utils" or "support". Ansible task names
  are action-oriented; none contains the Ansible 2.19 `: -x` trap.
- **The drift gate's own summary states n of m** — `CHECKED`, `NOT_DEPLOYED` and
  the template count are printed **always, including zero**
  (`qa-deployed-drift.bash:261-273`), with a comment explaining why a
  conditionally-appended clause would be the same defect. That is the pattern M2
  is missing. Population today: 42 regular files under `files/home/.local/bin/`
  (41 byte-comparable + 1 `.j2`) plus the `EXTRA_PAIRS` trees; the journal's host
  run reported 32 compared.
- **Public-repo safety.** Swept the plan folder and journal for home paths,
  usernames, emails, private IPs and hostnames — **zero hits**. The earlier
  review's B1 fix held: the journal uses `<home>` and `<repo>` placeholders
  throughout. One residual: `JOURNAL:141` contains the rclone remote name
  `LTS-G-Drive`. It is install-specific under `CLAUDE.md`'s rule, but it is
  **pre-existing and repo-wide** — nine other occurrences including two in
  `play-ftp-camera.yml:39,474` as the documented example value — so 00099 is not
  its origin and this is not a finding against this plan. Whether that string
  should be an approved example value at all is a separate, repo-wide question.
- **Plan Commit Rule.** `git status` is clean; the plan folder was committed with
  its code in `942fb724`. The README index row exists (`README.md:151`). Task
  statuses match reality — 5.6 is correctly the only unticked task and the header
  correctly reads In Progress.
- **Journal discipline.** Append-only observed; corrections are new entries
  (10:06, 10:25). The 10:25 entry is a model of the standard the brief asks for:
  it records that the worktree-skip claim *"had only been reasoned about, never
  executed"* and then exercises it in **both** directions, including a control
  proving the main checkout does not over-skip.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `./scripts/qa-all.bash` | **PASS**, exit 0 — 928 files checked, 58 stage lines, no failures |
| `hooks-daemon plan-qa --sweep` | exit 1, 2 findings, **0 blocking** — both advisory and **neither concerns Plan 00099** (a stale path in 00046, and journal-freshness for twelve other plans) |
| `ansible-playbook --syntax-check` | **PASS** on both changed plays (`play-rclone.yml`, `play-ftp-camera.yml`) |
| `qa-deployed-drift.bash` (via qa-all) | `✓ deployed-drift: skipped (CCY container …); 1 template(s) verified` — see m7 |

**Conditional gates, per `CLAUDE/QA.md` — checked whether the diff triggers them:**

- `qa-helper-tests.bash` — **not triggered**: the diff touches no `helpers/` or
  `tests/helpers/` file. (It ran anyway inside `qa-all.bash` since Plan 00081
  wired it in: 1558 tests, 66 modules, 1 skipped.)
- `helpers.gnome.check_extension_compat` — **not triggered**: no
  `extensions/**/metadata.json` change. (Ran inside `qa-all.bash`: 5 extensions OK.)
- `eslint` in `extensions/` — **not triggered**: no extension JS in the diff.
  (`✓ js: 11 files OK` inside `qa-all.bash`.)

No required gate was skipped.

## What I could NOT check from the CCY container

Stated explicitly so the gap is visible rather than implied. Everything below is
host-only and was taken from the journal, not reproduced:

1. **`acceptance.bash` has not been run by this review.** It is host-only and
   aborts without a `fuse.rclone` mount. The 10/10 result is the journal's
   (10:12), not mine. M2 says why that number is not self-verifying.
2. **`qa-deployed-drift.bash` never performed a real comparison here** — it took
   the CCY-container exit at line 68. I verified its *source-tree* half (the
   template-to-`dest:` mapping, which by design runs before the host skips) and
   read the comparison loops, but I did not observe a single `cmp`. The "32
   deployed scripts in sync" figure is the journal's.
3. **`deploy.bash` was not run** (container rule: never run Ansible here), so the
   fixed `pgrep` guard, the play run counts, and the mount restart behaviour are
   unverified by me.
4. **`triage.bash` was not run** — host-only, and it writes a log.
5. **The `RCLONE_USER`/`RCLONE_PASS` env form was not exercised against a live
   rclone.** I proved the library never *reaches* `rclone` without a credential,
   and that the password is passed as an environment prefix rather than in argv
   by reading `rclone-rc-auth.bash:91` — but that rclone 1.74.3 honours the env
   form, and the consequent absence from `ps` output, rests on the journal's host
   verification (09:25).
6. **No live 401 was observed.** F1-F6 in `PLAN.md` are taken on the journal's
   evidence.

## Housekeeping

`git status` was left exactly as found, apart from this report file. Note that
`CLAUDE/Plan/**/subagent-reports/` is **not** gitignored and has no precedent
elsewhere in the plan tree, so this file will appear as untracked and needs a
decision (commit it with the plan, or add an ignore rule).

The `Write` tool is disabled for this agent role, so this file was authored via a
Bash heredoc; no other file was created or modified by this review.
