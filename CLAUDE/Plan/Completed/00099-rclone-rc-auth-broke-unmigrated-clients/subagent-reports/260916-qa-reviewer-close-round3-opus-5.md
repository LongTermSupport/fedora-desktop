# QA Review — Plan 00099, closing round 3

Scope: `510dbbf4` (the round-2 response), read against the whole repo. Round 2's
"Checked and clean" items were not re-verified except where this commit touched them.

**Verdict: FIX-BEFORE-MERGE.** B1 and B2 are genuinely fixed and I reproduced both fixes
in both directions. Nothing here breaks a user, loses data or violates a hard rule. What
stops a PASS is one gate-strength defect that is this plan's own named subject for the
third round running, plus a broken in-plan reference introduced by this commit.

## Verified fixed (each reproduced, with the failing direction shown)

- **B1** — `files/home/.local/bin/rclone-rc-auth.bash:140-152`. Ran
  `untracked/scratch/falsify-b1.bash` against the real library and a real process carrying
  the unit template's argv: mount root resolves, offset path resolves, a path in no mount
  refuses. `findmnt -n -o TARGET --target` on a non-existent path exits 1 here (measured),
  so a stale offset path fails closed rather than resolving upward. `ftp-camera:1084-1088`
  gates on `[ ! -d "$dest_path" ]` before the call, so the caller cannot reach the library
  with a non-existent path at all. I looked for an input shape that now yields a WRONG
  address and did not find one — every mis-shaped input I could construct fails closed.
- **B2** — `triage.bash:341`. Ran the host-guard-free copy with no discoverable address:
  `==> FAILED legs: RC address discovery`, `==> run log: …`, exit 1. No unbound variable.
  Control (`falsify-triage-leg.bash`, stubbed mount): `RC address: localhost:5573`,
  `==> all legs OK`, exit 0. Both directions real.
- **S-a** — `triage.bash:78`. Refuses in this container, naming `/run/.containerenv`;
  `--help` still exits 0 ahead of the guard. Order matches the R2 skeleton in
  `CLAUDE/PlanScriptStandards.md:279-283` exactly (`plan_mode`, `plan_require_host`,
  `plan_start_log`).
- **S-b / S-c** — the address is stated in both branches (`:166-172`) and the discovery is
  the one `plan_gather_leg` (`:179`). `plan_gather_leg` records by name and `plan_finish`
  exits non-zero on it (`_planlib.inc.bash:723-735`, `:761-775`) — exercised above.
- **S-d** — `acceptance.bash:148-156`. Confirmed `findmnt -n -t fuse.rclone` exits **0**
  with zero rows on this system, so the old `if !` really could not fire; the live run now
  ABORTs at `[0]` with the intended message, exit 1.
- **m-b** — `scripts/qa-deployed-drift.bash:264-272`. `falsify-drift-summary.bash` drives
  the real failing path against a fake HOME:
  `✗ deployed-drift: 1 of 1 … differ from the repo; 76 not installed on this host; 1 template(s) not byte-comparable`,
  with the passing line as control. Clause-for-clause identical to `:285-287`. The new line
  contains no `skipped`, so `acceptance.bash` check [7]'s matcher is unaffected.
- **m-c** — `triage.bash:194` now uses `substr($0, index($0, "=") + 1)`, matching its
  sibling at `:221-222`.
- **m-d** — `--help` is now rendered from `CHECK_CATALOGUE`. Diffed the old and new help
  output: byte-identical apart from the two intended wording changes (check 6's label, and
  the added undeclared-run sentence). Padding and `6b.` alignment preserved.
- **m-e** — `falsify-coverage-direction.bash`: control `COVERAGE: 9 of 9` with no extra
  line; mutant (one catalogue entry deleted) `COVERAGE: 9 of 8` +
  `RAN BUT NOT DECLARED: 6b`. `${#undeclared[@]}` and the `${RAN_CHECKS[@]+…}` guard are
  safe under `set -u` on bash 5.2.15 (tested directly).
- **m-f** — `acceptance.bash:168-170` guarded, matching the library.
- **m-g** — `rc helper library missing` added to checks 4 and 5 (`:288`, `:309`).
- **Round-2 nits**: pgrep bracketed (`rclone-rc-auth.bash:162`); `PLAN.md` stray space
  gone; Task 5.7's never-to-be-ticked box moved to **Known, Out of Scope**; 5.7/5.9 in
  numeric order; `logs/` directory gone from the plan folder; success criteria 1/3/5
  unticked; index row no longer says "Deployed".
- **The SC2317 rationale is true, not invented.** Reproduced with shellcheck on a minimal
  script: indirect dispatch plus a terminating statement reports every dispatched body
  unreachable; remove the terminator and it is clean. The probe restructure was forced.
- **No probe was lost in the restructure.** The 14 stanzas before and after are the same 14,
  in the same order, with the same labels; seven moved to inline capture and every one of
  them keeps `2>&1` and the `if …; then rc=0; else rc=$?; fi` status capture verbatim from
  the old `probe()`. All seven remaining `probe` call sites pass an external command, as the
  new contract at `:109-113` requires.

## Should fix

### 1. Check [6]'s stand-in still does not carry the property that broke the client

`CLAUDE/Plan/00099-…/acceptance.bash:351-357`

The comment claims `"$rc_mount/."` "carries the one property that matters — a path inside
the mount, textually different from the mount root". `/.` is not a path inside the mount;
it is the mount root spelled with a trailing dot, and it is removed by any trivial
canonicalisation. The client's input is a genuine subdirectory two levels down.

The mutant the harness kills is total removal of the normalisation. Two weaker
normalisations survive check [6] and leave `ftp-camera --copy` broken exactly as B1 left
it — measured:

```
  input /mnt/x/.               -> "${p%/.}" gives /mnt/x         (check [6] PASSES)
  input /mnt/x/PHOTO/LIBRARY   -> "${p%/.}" gives /mnt/x/PHOTO/LIBRARY  (client FAILS)
  realpath -m /mnt/x/.             -> /mnt/x                    (check [6] PASSES)
  realpath -m /mnt/x/PHOTO/LIBRARY -> /mnt/x/PHOTO/LIBRARY      (client FAILS)
```

`realpath` is the obvious thing a later author "simplifying" `findmnt --target` would reach
for, and the gate would stay green through it. This is the same shape as B1: the gate
exercises an input the client never computes — one round on, one step closer, still not the
client's shape.

**Fix**: resolve a real subdirectory of the mount, e.g.
`client_input=$(find "$rc_mount" -mindepth 1 -maxdepth 1 -type d -print -quit)`, with an
explicit arm when the mount root has no subdirectory so "could not build a client-shaped
input" is distinguishable from a pass — do not silently fall back to `$rc_mount/.`, which
is the blind-reads-like-clean case this gate exists for. (The fully faithful answer is a
preflight-only flag on `ftp-camera` that the gate can invoke; `ftp-camera` has no such mode
today and adding one is more than this plan needs.)

### 2. `PLAN.md` points at a journal file that does not exist

`CLAUDE/Plan/00099-…/PLAN.md:176` — Task 5.10 says the findings and falsification evidence
are in `JOURNAL/2026-09-16.md`. The directory holds `00099-Journal-26-08-16.md` and
`00099-Journal-26-09-16.md`; there is no `2026-09-16.md`. `PLAN.md:124`, in the same file,
spells the same file correctly. Introduced by this commit, in the one line that tells a
reader where the evidence for this round lives. (`plan-qa --sweep` does not catch it — it
reports 7 findings, 0 blocking, none for 00099.)

### 3. COVERAGE still prints "n of m" with n > m and still ACCEPTs — for the other cause

`CLAUDE/Plan/00099-…/acceptance.bash:431-467`

m-e closed one of the two ways `COVERAGE: 10 of 9` can arise. A check number emitted twice
produces the identical self-contradicting line and is not caught — `missing` is empty,
`undeclared` is empty, so the run is ACCEPTED. Driven through the exact block:

```
RAN_CHECKS=(0 1 2 3 4 4 5 6 6b 7)
COVERAGE: 10 of 9 checks executed
missing=0 undeclared=0
ACCEPTED
```

The verdict line contradicts itself and nothing acts on it — word for word the symptom the
m-e comment at `:439-443` says it fixed. **Fix**: assert
`[ "${#RAN_CHECKS[@]}" -eq "${#EXPECTED_CHECKS[@]}" ]` as a third condition, or have
`check()` refuse a duplicate id. Either makes the printed count incapable of contradicting
itself.

### 4. The new `findmnt` capture reproduces the missing `head -n1` one line above the place it was fixed

`files/home/.local/bin/rclone-rc-auth.bash:148`

`findmnt -n -o TARGET --target "$mountpoint"` can emit more than one row. Demonstrated on
this machine against a genuinely over-mounted target:

```
$ findmnt -n -o TARGET --target /sys/devices/virtual/powercap
/sys/devices/virtual/powercap
/sys/devices/virtual/powercap
count: 2
```

A restarted rclone unit stacked on its own mountpoint gives exactly that shape. `mountpoint`
then holds two newline-separated copies and the `case` at `:175` can never match:

```
NO MATCH -> "no rclone mount process serves [/mnt/photos
/mnt/photos]"
```

It fails closed, so this is not a wrong-address bug — but it is unserviceable where the
pre-normalisation code worked, and the diagnostic names a two-line path. Round 2 raised the
missing `head -n1` against the address capture; that site was fixed by the multiplicity
refusal and the identical un-headed shape was introduced six lines above it in the same
change.

**Fix carefully**: `| head -n1` alone is wrong here. `ftp-camera` runs `set -e` **without**
`pipefail` (`ftp-camera:` its only `set` line is `set -e`), so a failing `findmnt` piped
into `head` would give exit 0 and an **empty** `mount_root` — and an empty `$mountpoint`
degenerates the matcher to `*"  "*`, which is precisely the defect S-d just removed from
`acceptance.bash`. Take the first row *and* assert non-empty.

### 5. "rclone uses the last" is asserted as fact and is not evidenced anywhere

`files/home/.local/bin/rclone-rc-auth.bash:182-189`, repeated at
`JOURNAL/00099-Journal-26-09-16.md:380-385`.

The refusal itself is safe for this repo — `play-rclone.yml:388` emits exactly one
`--rc-addr`, so no deployed unit can trip it, and the previous behaviour (a two-line `addr`
fed into `http://…`) was worse. But the *reason* given for refusing rather than choosing is
a claim about rclone's flag parsing, and there is no rclone binary in this container, no
vendored rclone doc in the repo, and no probe in the plan or journal that establishes it.
If `--rc-addr` is a repeatable list flag (rclone has moved several server flags to
`stringArray`), then a multi-listener unit is legitimate, any of the addresses would work,
and this refuses a working configuration while telling the operator something untrue.

**Fix**: verify it on the host (`rclone help flags rc-addr`, or `rclone mount --help`) and
either keep the comment with the evidence recorded, or soften it to what is actually known —
"the unit template emits one; more than one is unexpected here and is refused rather than
guessed at".

### 6. The Delivery section still describes a single-commit delivery

`CLAUDE/Plan/00099-…/PLAN.md:263-276`

The 13:40 journal entry's whole lesson was that this plan's diff was nearly unfindable
because no hash was recorded, and it says the hash was written into Delivery "so the next
reader does not repeat the search". Three review rounds have since changed **deployed**
files — `files/home/.local/bin/ftp-camera`, `files/home/.local/bin/rclone-rc-auth.bash`,
`scripts/qa-deployed-drift.bash` — across `17819cda`, `c3554f2e` and `510dbbf4`, and none
of them appears there. A reader tracing `rclone-rc-auth.bash` from this plan lands on
`942fb724` and a library that no longer looks like that. The lesson was written down beside
the thing it fixed and not applied to the commits that came after it.

## Nits

- `acceptance.bash:324-330` — the old check [6] comment block ("ftp-camera now discovers the
  address from the mount's own process too … 'same function, same library' is once again
  'same call'") now sits directly above the new block at `:345-355`, which exists to say the
  opposite was true of the input. Both are accurate as written; together they make the
  reader work out which one is current. Fold them.
- `triage.bash` has **no trailing newline** (`plan_finish` is the last byte). Its siblings
  all end with one; `git diff` shows `\ No newline at end of file`.
- `rclone-rc-auth.bash:128-130` still names two of the sibling cmdline walks as knowing
  duplication. There are now five copies in total (the library, `acceptance.bash:164-177`,
  `rclone-cache-status`, `rclone-tail`, `rclone-cache-warm`). Round-2 nit, unactioned and
  unrecorded — the journal's "two nits in the library taken too" does not mention it.
- The library's "is not inside any mount" arm now answers for a path the caller never named
  when the argument resolves upward to a parent mount, e.g. "no rclone mount process serves
  /". Diagnostic quality only; it still fails closed.
- `untracked/scratch/falsify-drift-summary.bash` writes its mutant to
  `scripts/qa-deployed-drift-harness.bash` — inside a tracked directory — and relies on an
  EXIT trap to remove it. Nothing is there now and nothing was committed, but a killed run
  leaves an untracked file in `scripts/`. The harness is untracked, so this is an
  observation about the working method, not a repo defect.

## Checked and clean

- **Placement in the IaC graph**: no new play, no new file, no playbook in the diff. The
  library change lives where the plan's own Decision 1 says discovery belongs, and it is the
  only place all three callers reach. No runtime probing stands in for declared state.
- **Fail-fast**: no `failed_when:`/`ignore_errors:` added; no skip-and-continue introduced.
  Every new error path returns non-zero and names the cause. `triage.bash`'s
  record-and-continue is `plan_mode gather`'s sanctioned contract and now drives a non-zero
  exit, which it did not before. The one pre-existing `# FAIL-FAST-OK:` in `ftp-camera` is
  untouched.
- **Stderr hygiene**: every diagnostic added to `rclone_rc_addr_for_mount` is `>&2`; the
  address remains the only thing on stdout. The drift gate's new failure line goes to stderr
  like the one it replaced. `emit_probe`'s stanzas are the report — its payload.
- **Version bumps**: none owed. `git show --name-only` confirms nothing under
  `files/var/local/claude-yolo/`, `lib/*.bash` or the Dockerfile.
- **Exec bits and shebangs**: `triage.bash`, `acceptance.bash`, `qa-deployed-drift.bash` all
  `-rwx`; `rclone-rc-auth.bash` stays non-executable, correct for a sourced library.
- **Public-repo safety**: scanned every added line for emails, home paths, usernames and
  IPs — none. The only host-shaped strings are repo-owned paths (`/etc/ftp-camera`,
  `/srv/ftp-camera`) that predate this commit. The falsification harnesses live under
  `untracked/scratch/`.
- **Plan Commit Rule**: the plan, the journal, the index row and the code all moved together
  in `510dbbf4`. The working tree was clean at the commit.
- **Downstream callers**: only `ftp-camera`, `triage.bash` and `acceptance.bash` call
  `rclone_rc_addr_for_mount`. `rclone-cache-status`, `rclone-tail` and `rclone-cache-warm`
  keep their own target-driven walks and are untouched by the normalisation.
- **`CHECK_CATALOGUE` parsing**: `${entry%%|*}` / `${entry#*|}` split on the first `|` and
  keep later ones in the label; no id contains a space, so the
  `case " ${EXPECTED_CHECKS[*]} " in *" $x "*` membership tests are sound.

## Observations (not findings against this commit)

- Capturing `triage.bash`'s stdout to a file loses roughly 190 bytes of the report header:
  `triage.bash > out.txt` produced a file whose banner and the first half of the
  `RC address: NOT DISCOVERED` line were overwritten by `plan_start_log`'s own `==>` lines.
  The plan run log itself is complete and correct, and `plan_finish` points at it. This is
  `_planlib.inc.bash`'s `plan_start_log` behaviour, unchanged by this commit — flagged only
  so the next reviewer does not read a truncated capture as a defect in the script.
- A concurrent session created `CLAUDE/Plan/00131-semgrep-or-true-rule-is-blind-to-the-enclosed-form/`
  in this container during the review. Unrelated to 00099; `git status` is otherwise clean.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `./scripts/qa-all.bash` | **PASS**, exit 0 — 943 files |
| `hooks-daemon plan-qa --sweep` | exit 1, **7 findings, 0 blocking, none for Plan 00099** |
| `shellcheck -x` | CLEAN on `triage.bash`, `acceptance.bash`, `deploy.bash`, `rclone-rc-auth.bash`, `qa-deployed-drift.bash`, `ftp-camera` |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook in the diff; `qa-ansible-syntax` inside `qa-all.bash` covered the tree green |
| `acceptance.bash`, live | ABORTs at check [0], exit 1 (no mount in this container) — the S-d path, now asserting |
| `triage.bash`, live | refuses with `plan_require_host`, exit 1 — the S-a path |

**Conditional gates**, per `CLAUDE/QA.md`: `qa-helper-tests.bash` **not triggered** (no
`helpers/` or `tests/helpers/` change); `helpers.gnome.check_extension_compat` **not
triggered** (no `metadata.json`); `eslint` in `extensions/` **not triggered** (no extension
JS). All three ran inside `qa-all.bash` regardless and passed. No required gate was skipped.

## Bottom line for the plan

**More than Task 5.9 stands between this plan and Complete.** The six should-fix items above
are all container-doable and none needs the host. Items 1 and 3 are the ones that matter:
both are this plan's own subject — a check that cannot fail for the case it vouches for —
and both are cheap.

Once they are fixed, what remains is Task 5.9's host re-deploy and a re-run of
`acceptance.bash` on the host, which is the only thing that can tick success criteria 1, 3
and 5. The last criterion, "`qa-reviewer` returns PASS", is not met by this round.
