# QA Review — Plan 00099, closing review (branch F44, commits `942fb724` → `263c61af`)

**Verdict: FIX-BEFORE-MERGE.** Not PASS — so the success criterion "`qa-reviewer` returns
PASS" is not yet met and the plan cannot be marked Complete.

0 blocking. The engineering is sound and the previous round's findings are genuinely fixed.
What I found is concentrated, again, in the plan's own verification instrument: two
reproducible blind-passes in `acceptance.bash`, one stale index row, and an m6 re-judgement
that changes the answer.

## Should fix

### S1 — `acceptance.bash` check [7] turns a documented SKIP into "PASS repo and host are in sync"

`CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/acceptance.bash:315-319`

It consumes only `qa-deployed-drift.bash`'s exit code, and all three skip paths exit 0. I
drove the real gate here with stubbed `findmnt`/`pgrep`/`rclone`:

```
[7] no repo-owned script differs from its deployed copy
  PASS  repo and host are in sync
```

…while the drift gate itself had printed `⚠ deployed-drift: skipped (CCY container …)` and
compared nothing — its output was captured into `drift_out` and discarded.

This is m7's defect one level up: the fix in `e586a0f4` changed `✓`→`⚠` precisely so a skip
could not read as a pass, and `acceptance.bash` immediately re-launders it. It is live on a
real host, not just in the container: the **linked-worktree** skip fires on the host, so
running the gate from `.claude/worktrees/` yields check [7] PASS having compared nothing.
Success criterion 3 rests on check [7].

**Fix**: match `drift_out` for `skipped` and call `bad` (or ABORT as check [0] does).

### S2 — check [3] passes vacuously on an empty population

`acceptance.bash:197-213`

Same run: `PASS every deployed client goes through rclone_rc` with **zero** files examined —
`$BIN/rclone-*` matched nothing, every iteration hit `[ ! -f ] → continue`, and
`bypass_found=0` was read as proof. A blind scanner reports the same as a clean one, which is
this repo's named recurring class and the one this plan's own `PLAN.md` Risks table
half-anticipates (it flags the glob's *scope*, not its *emptiness*).

**Fix**: count the files examined and state it (`n deployed client(s) scanned`), fail at zero.

### S3 — `ftp-camera` is the one client still hardcoding the RC port, and check [6] no longer exercises its path

`files/home/.local/bin/ftp-camera:1092` — `local rc_addr="localhost:${RCLONE_RC_PORT:-5572}"`,
used at `:1104`, `:1131`, `:1176`, `:1182`.

The previous review's m4 stated that "00099's own clients … all read the address off the
mount's own rclone process; the acceptance gate is the last thing still assuming the default."
That is **false** — `ftp-camera` does not, and `RCLONE_RC_PORT` is referenced nowhere else in
the repo. Since `a68c99bb`, `play-rclone.yml:388` defaults each mount to
`rclone_rc_port_base + mount_index`, so 5572 is only the *first* mount. `--copy` to a second
mount would preflight against the wrong RC and then poll the wrong mount's `vfs/stats` for
completion.

Worse for this plan: the m4 fix made `acceptance.bash:276` call
`rclone_rc_available "http://${RC_ADDR}"` with the **discovered** address, where before it
used 5572 and coincidentally matched the client. Check [6]'s comment claims it "Runs the SAME
function the deployed ftp-camera calls" — it runs the same function against a *different
address*, so the part that is wrong is precisely the part the gate no longer touches.

The line predates the plan (`f46be495`), but the plan owns the file, migrated it, and its gate
now vouches for it.

### S4 — the plan index still says "ACCEPTED 10/10 on the host"

`CLAUDE/Plan/README.md:151`

The m2 round corrected this from 8/8 to 10/10; Task 5.7's host run then made 10/10 untrue, and
`PLAN.md:184-185` correctly restates the verdict as REJECTED on check [7]. The index row was
not re-touched and now contradicts the plan it indexes — the same "figure a reader meets
first" argument the previous review used to raise it.

### S5 — m6 re-judged: the owner's trade-off covers one of the three scripts, and `deploy.bash` now has a correctness argument

The 14:28 handoff justifies not converting on the grounds that `acceptance.bash` would need
`plan_require_host`, killing the container harness. That reasoning is sound **for
`acceptance.bash` only**, and it does not reach either of the other two:

- `deploy.bash:113,117` runs `ansible-playbook` without ever `cd`-ing to the repo root.
  `ansible.cfg` is resolved from **cwd** and every path in it is relative — the inventory
  (`./environment/localhost`), `roles_path`, the vault credential setting, and
  `callback_plugins = ./callback_plugins` (the Plan 00109 play ledger). Invoked by absolute
  path from anywhere else, the deploy runs with none of them. `PlanScriptStandards.md` R6 says
  "from the repo root" for exactly this; `plan_ansible_playbook` subshells a
  `cd "$PLAN_REPO_ROOT"` and adds `</dev/null` for R5's stdin drain. The host run worked
  because the owner happened to be at the root.
- `triage.bash:47-50` is where m6's stated live consequence actually lives:
  `CLAUDE/Plan/.../logs/rclone-rc-clients-triage.log` exists, is 0 bytes, and is gitignored via
  `.gitignore:78` — it will ride into `Completed/` on the `git mv`. Nothing about the harness
  blocks converting it.

Also new since the review: `_planlib.inc.bash` is 1.3.0 with `plan_on_cleanup`, which would
cover `acceptance.bash`'s two `mktemp` files (`:174`, `:275`) — currently removed inline only,
so any `set -e` death between `mktemp` and `rm -f` leaks them.

## Minor

- **m-a** — "this plan's four files" is wrong in three places (`PLAN.md:135`, `:177`, journal
  `:196`). The delivery commit migrated **five**: `ftp-camera`, `rclone-cache-status`,
  `rclone-cache-warm`, `rclone-rc-auth.bash`, `rclone-tail`.
- **m-b** — `scripts/qa-deployed-drift.bash:264`: the *failing* summary prints
  `$DRIFTED of $CHECKED` and omits `NOT_DEPLOYED`, which the *passing* path (`:277-279`) states
  always, on purpose, per Plan 00081. Criterion 3 leans on that failing line, so a helper that
  was never deployed would be invisible in the evidence cited.
- **m-c** — `triage.bash:109` still uses `awk -F= '{ printf "%s=<%d chars>", $1, length($2) }'`.
  n1 fixed `:135-136` and left its sibling 26 lines up, so the credential-length diagnostic
  still truncates at the second `=` — the same wrong-length-sends-you-to-the-wrong-place
  argument the n1 fix comment itself makes.
- **m-d** — `acceptance.bash:27-35` (`--help`) is a **third** hand-maintained list of the
  checks, tied to `EXPECTED_CHECKS:74` and to the `check` calls by nothing. m1 corrected it by
  hand; nothing stops it drifting again.
- **m-e** — COVERAGE is one-directional: a check that runs *without* being in
  `EXPECTED_CHECKS` gives `COVERAGE: 10 of 9` and still ACCEPTED (`:341`). It catches deletion,
  not addition.
- **m-f** — `acceptance.bash:125`: `tr '\0' ' ' < "/proc/$rc_pid/cmdline"` is unguarded.
  Reproduced: a pid that is gone kills the gate with
  `line 125: /proc/…: No such file or directory`, exit 1, **no verdict and no COVERAGE line**.
  It fails closed, so it is not a false ACCEPTED. `rclone-cache-status:164` guards the identical
  read with `if ! cmdline=$(… 2>/dev/null); then continue; fi`; the gate copied the loop without
  the guard.
- **m-g** — checks 4 and 5 (`:228`, `:249`) grep for
  `rc unreachable\|rejected credentials\|credential missing` and were **not** extended with the
  new `rc helper library missing` reason that M1 added to both clients in the same commit. It
  still fails, via the weaker "printed no cache figures" arm, so the message degrades rather
  than the verdict.

## Nits

- `PLAN.md:135` — stray space in a path: `files/home/.local/lib/ freeze/freeze-common.bash`.
- `PLAN.md:134-139` — Task 5.7 carries a nested `- [ ] ⬜` that is deliberately never to be
  ticked. It belongs in **Known, Out of Scope**; as written it leaves a permanently-open
  checkbox in a Complete plan.
- `LTS-G-Drive` — 9 occurrences repo-wide (7 files), one of them this plan's journal `:141`
  beside the real cache size and file count. It is **not** in `CLAUDE/ExampleValues.md`, so the
  two `play-ftp-camera.yml:39,474` uses as an "example value" are unsanctioned too. Already in
  public history, so deletion from one file changes nothing; this needs an owner decision (add
  it to `ExampleValues.md`, or open a plan to purge it). Not blocking 00099, which neither
  created the gap nor can close it.

## Answers to the four specific questions

1. **Previous findings** — M1, M2, M3, m1, m2, m3, m4, m5, m7, m8, n1, n2, n3 all genuinely
   resolved; each verified against the current files rather than the journal. M1: both `case`
   blocks in `rclone-tail` (`:147`, `:166`) and the one in `rclone-cache-status:205` carry the
   `*"helper library not found"*` arm first, and it matches the stub's literal text; n2's
   asymmetry is closed (four arms in both). M3: no stale `00067` reference survives in `files/`,
   `scripts/`, `playbooks/`, `docs/` — the two remaining hits are legitimate references to the
   real Plan 00067. **m6: the trade-off no longer holds as stated** — see S5.
2. **The coverage mechanism works.** I drove the real gate to its verdict here:
   `COVERAGE: 9 of 9 checks executed (5 passed, 5 failed) → REJECTED`, exit 1. The
   `case " ${RAN_CHECKS[*]} " in *" $expected "*` matcher does not confuse `6` with `6b`.
   **No early exit reaches a false ACCEPTED**: the only `exit 0` before the verdict is `--help`;
   check [0]'s two ABORTs and any `set -e` death exit 1. Two gaps: it never fails on an
   *unregistered* check (m-e), and a `set -e` death prints no verdict line at all (m-f).
3. **The host-run reading holds.** `qa-deployed-drift.bash:264` prints `$DRIFTED of $CHECKED`
   only on the drift path, so `2 of 74` proves 74 files were really `cmp`-ed. All four helpers
   had deployed copies — established *independently* of check [7] by check [2] (`$RC_LIB`
   readable), [4] and [5] (`-x` plus live figures) and [6] (`-x` plus the `rclone_rc_available`
   grep) — so they were counted in the 74, not silently skipped as `NOT_DEPLOYED`. `lxcfreeze`
   and `freeze-common.bash` are neither. **One caveat**: `rclone-cache-warm` — the fifth file —
   has no such independent assertion, and the failing summary omits `NOT_DEPLOYED` (m-b), so for
   that file the evidence is "`play-rclone.yml:477-480` deploys it and the play ran", not the
   gate's output.
4. **The re-wording is accurate, not a softening.** Criterion 5 states the overall verdict is
   **still REJECTED** in the same sentence as the pass claim, and scopes the pass to checks
   0–6b with the COVERAGE figure — that is a narrowing plus a disclosure, which is the honest
   shape. Criterion 3 likewise names the two offenders and their owning plays rather than hiding
   behind "this plan's scope". Both are supported by `acceptance.bash` and
   `qa-deployed-drift.bash` as they actually behave. The drift is in the **index row**, which
   was left at the old claim (S4).

## Checked and clean

- **Fail-fast**: no `|| true`, no `ignore_errors`, no unannotated `failed_when: false` anywhere
  in the diff. `deploy.bash:83-92`'s `grep -v` guard is still correct.
- **Version bumps**: nothing under `files/var/local/claude-yolo/` or the Dockerfile is touched —
  no `CCY_VERSION` / `REQUIRED_CONTAINER_VERSION` obligation. Confirmed against
  `git show --stat e586a0f4 263c61af`.
- **IaC placement**: no new play; `play-rclone.yml:457-489` owns all four helpers plus the
  library, `play-ftp-camera.yml` owns `ftp-camera` and declares the cross-play dependency
  without duplicating the copy; both opt-in, so no `playbook-main.yml` ordering question.
- **Docs**: `CLAUDE/QA.md:565+` now documents all three skips as a table plus the widened
  `EXTRA_PAIRS` scope, with the rule that a new skip must land in the same commit. No `docs/`
  page references any of these artefacts, so nothing there to drift.
- **Public-repo safety**: swept the plan folder for home paths, usernames, emails, private IPs
  and hostnames — clean apart from the `LTS-G-Drive` nit above. All `files/home/...` strings are
  repo-relative source paths.
- **Journal discipline**: append-only observed; n3's second inversion disclosed by a new 14:24
  entry rather than an edit, which is the correct treatment. `plan-qa --sweep` does not flag
  this file.
- **Plan Commit Rule**: `git status` clean; plan, journal and code landed together in
  `e586a0f4` / `263c61af`; README row present.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `./scripts/qa-all.bash` | **PASS**, exit 0 — 943 files, no failures (`⚠ deployed-drift: skipped (CCY container…)`, `⚠ shellcheck: 172 issues` and `⚠ patterns` are pre-existing advisories) |
| `hooks-daemon plan-qa --sweep` | exit 1, 23 findings, **0 blocking**, **none concerning Plan 00099** (path-existence in 00046, journal ordering in 00063/00109/00119, 16 README bijection rows, journal freshness for 11 other plans) |
| `ansible-playbook --syntax-check` | **PASS** on `play-rclone.yml` and `play-ftp-camera.yml` (run under a pty — Ansible refuses this harness's non-blocking stderr) |
| `acceptance.bash`, driven to verdict | reached `COVERAGE: 9 of 9` / REJECTED / exit 1 under stubs — this is where S1, S2 and m-f were found |

**Conditional gates** — checked whether the diff triggers them: `qa-helper-tests.bash`
**not triggered** (no `helpers/` or `tests/helpers/` change; ran inside `qa-all.bash` anyway —
1562 tests, 66 modules); `helpers.gnome.check_extension_compat` **not triggered** (no
`metadata.json` change; ran inside `qa-all.bash` — 5 extensions OK); `eslint` in `extensions/`
**not triggered** (no extension JS; `✓ js: 12 files OK`). No required gate skipped.

## What I could not verify from the container

`deploy.bash`, `triage.bash`, and any live RC call are host-only. Task 5.7's PASSes for checks
0–6b are the owner's run, not mine — I verified the gate's *logic* and drove it to a verdict
under stubs, not against a real mount.
