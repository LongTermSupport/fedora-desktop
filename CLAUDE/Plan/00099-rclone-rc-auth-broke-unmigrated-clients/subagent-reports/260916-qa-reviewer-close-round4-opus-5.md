# QA Review — Plan 00099, closing round 4 (`68c8bbfa`)

**Verdict: FIX-BEFORE-MERGE.**

**Has the pattern stopped? No.** Round 4 finds the same thing rounds 2 and 3 found: check [6]'s
stand-in input still does not carry the property that broke the client, and COVERAGE's
self-contradiction was closed route-by-route again while the comment claims the class is closed.
Four of the six items are genuinely and verifiably fixed; two were fixed in the place the reviewer
quoted rather than in the place the defect lives.

**More than Task 5.9's host re-deploy stands between this plan and Complete.** Findings 1–3 below
are container-doable and need no host.

## Blocking

None. Nothing here breaks a user, loses data, leaks private information, or violates a hard rule.
Every new error path fails closed.

## Should fix

### 1. Check [6] is still blind to a normalisation mutant — the client's input is deeper than the gate's

`CLAUDE/Plan/00099-…/acceptance.bash:361`

The new input is `find "$rc_mount" -mindepth 1 -maxdepth 1 -type d -print -quit` — **exactly one
component** below the root. The client's input is not. `ftp-camera:1002-1004` documents its own shape:

```
# e.g. RCLONE_REMOTE=lts-photo:/PHOTO/LIBRARY/photos with mount lts-photo:/ at ~/mnt/lts-photo
# → returns ~/mnt/lts-photo/PHOTO/LIBRARY/photos
```

and `ftp-camera:1042-1046` builds `${mount_target}/${rel}` where `rel` is the whole multi-component
offset. So a **fixed-depth** normalisation — `dirname`, or `${p%/*}` — satisfies check [6] and leaves
`ftp-camera --copy` broken exactly as B1 left it. Driven end-to-end through the shipped library, with
the mutant emulated by making the stubbed `findmnt` answer `dirname` of its target and everything else
unmodified:

```
mutant = dirname:
  input /mnt/photos/PHOTO                  resolves -> localhost:5573   <- check [6] PASSES
  input /mnt/photos/PHOTO/LIBRARY/photos   FAILS                        <- CLIENT BROKEN
shipped (true findmnt --target):
  input /mnt/photos/PHOTO                  resolves -> localhost:5573
  input /mnt/photos/PHOTO/LIBRARY/photos   resolves -> localhost:5573
```

`dirname "$mountpoint"` is at least as plausible a "simplification of `findmnt --target`" as the
`realpath -m` this round killed — it is the canonical bash spelling of "the parent".

Two claims are therefore overstated and should not stand:

- `acceptance.bash:355-356` — *"Only a path with a real component below the root can distinguish a
  resolution from a tidy-up."* A one-component tidy-up is not distinguished.
- `JOURNAL/00099-Journal-26-09-16.md` (18:23 entry) — *"which is the shape `find_mount_path` returns:
  a genuine component below the root. Only a real resolution satisfies it; a tidy-up cannot."*
  `find_mount_path` returns the offset, which is `N` components, not one.

This is round 3's own diagnosis recurring: three mutants chosen, three killed, class declared dead.
The population of "normalisations that are not `findmnt --target`" was never enumerated, and
`falsify-check6-input2.bash` cannot see the gap by construction — its `findmnt` stub prints
`MOUNT_ROOT` regardless of its argument, so **depth is invisible to the harness**, and its
`REAL_CHILD` is `${MOUNT_ROOT}/PHOTO` (depth 1) while it creates `${REAL_CHILD}/LIBRARY` and never
uses it.

**Cheap fix**: descend one more level — keep the depth-1 find, then
`find "$child" -mindepth 1 -maxdepth 1 -type d -print -quit` and prefer the grandchild when one
exists. **Faithful fix**: build the client's actual input — `/etc/ftp-camera/config` (`ftp-camera:91`)
holds `RCLONE_REMOTE`, and check [0] already has the mount SOURCE in `$REFRESH_FS`, so the gate can
compute the exact `find_mount_path` output and assert the directory exists. The faithful route also
fixes the latent mismatch that check [0] picks the *first* `fuse.rclone` mount, which need not be
ftp-camera's.

### 2. The `--rc-addr` claim was softened in the comment and left intact in the message the operator actually reads

`files/home/.local/bin/rclone-rc-auth.bash:212`

```bash
echo "  rclone uses the last; the sibling helpers use the first. Fix the unit rather than guess." >&2
```

The comment at `:203-209` now says the semantics are **not** established here. Eight lines later the
runtime message still asserts them as fact — to the operator, who never sees the comment. Round 3's
finding was precisely *"this refuses a working configuration while telling the operator something
untrue"*, and the journal's own write-up repeats that sentence as the risk. The untrue half is
`rclone uses the last`; `the sibling helpers use the first` is true and verifiable
(`rclone-cache-warm:105`, `rclone-tail:119`, `rclone-cache-status:169` all `head -n1`). Fix the
clause, or record the evidence on the host (`rclone help flags rc-addr`) and keep it.

### 3. COVERAGE has a fourth cause, and the comment claims the class is closed

`CLAUDE/Plan/00099-…/acceptance.bash:468-473, 500-501`

The `duplicates` scan fixes the RAN-side duplicate — I confirmed it, including the
`REJECTED — n check id(s) ran more than once` wording the journal says is unexercised. But a duplicate
**declaration** in `CHECK_CATALOGUE` produces the same self-contradicting line in the other direction
and still ACCEPTs, because `missing` and `undeclared` are set-membership tests, not counts. Driven
through the real verdict block (`awk 'NR>=447' acceptance.bash`, sourced with synthetic arrays):

```
=== A: catalogue declares 6 twice; all nine ids ran once ===
COVERAGE: 9 of 10 checks executed (9 assertion(s) passed, 0 failed)
ACCEPTED — every declared check ran exactly once and every assertion passed.
exit=0

=== C: duplicate in RAN (round-3 finding 3) ===
COVERAGE: 10 of 9 checks executed (9 assertion(s) passed, 0 failed)
  RAN MORE THAN ONCE: 4
REJECTED — 1 check id(s) ran more than once.
exit=1
```

The comment at `:471-473` says *"the count is now made incapable of contradicting itself rather than
having its two known causes enumerated."* The implementation did the opposite: it enumerated a third
cause, and the count remains capable of contradicting itself. Round 3's actual recommendation —
`[ "${#RAN_CHECKS[@]}" -eq "${#EXPECTED_CHECKS[@]}" ]` as a further condition — closes both directions
in one line and makes the claim true.

Related, and cheap: the journal states *"this container cannot produce a run with zero failed
assertions, so the specific `REJECTED — n check id(s) ran more than once` wording is unexercised."*
That limit is not real — driving the verdict block directly with synthetic
`RAN_CHECKS`/`EXPECTED_CHECKS`, as above, exercises the exact wording in this container, with no mount
and no host.

### 4. An unrelated Plan 00109 artefact rode in on this Plan 00099 commit

```
$ git show --diff-filter=A --name-status --format='' 68c8bbfa
A  CLAUDE/Plan/00099-…/subagent-reports/260916-qa-reviewer-close-round3-opus-5.md
A  CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/subagent-reports/260916-qa-reviewer-00109-rerun-opus-5.md
```

290 lines belonging to Plan 00109, not mentioned in the commit message, and not referenced from
`CLAUDE/Plan/00109-…/PLAN.md` (which references three other `qa-reviewer` reports, at `:112`, `:177`,
`:348`). `CLAUDE.md`'s Plan Commit Rule lists *"Bundling unrelated plan edits with unrelated code
changes"* under **Prohibited**. Plan 00109 has concurrent uncommitted work in this checkout, so the
likely route is a broad `git add`; either way 00109's review is now in history with nothing in 00109
pointing at it. Fix forward: a 00109-only commit adding the PLAN.md reference.

### 5. PLAN.md records rounds 1, "closing", and 2 as tasks — round 3 has no task

`CLAUDE/Plan/00099-…/PLAN.md:118-176`

Task 5.6 (round 1), Task 5.8 (closing), Task 5.10 (round 2) each get a task with findings and outcome.
Round 3 — this commit's entire subject — got a journal entry and a Delivery bullet but no task, and
Task 5.10 ✅ still reads as the closing round. A reader of the task list concludes that only Task 5.9's
host re-deploy remains, which is the exact question this review exists to answer and is not true. Add
the round-3 task in the plan's own established shape, and expect a round-4 one.

## Nits

- **`find` has no timeout where its siblings do.** `acceptance.bash:361` lists a live rclone VFS mount
  root with no bound; checks 4 and 5 wrap their clients in `timeout 60` (`:278`, `:304`). An rclone
  mount whose backend is unreachable blocks in `readdir`, and the gate would hang silently before
  printing anything for check [6]. `timeout 30 find …` keeps the existing "could not list" arm (exit
  124 is non-zero).
- **`find`'s stderr is dropped from the failure detail.** `:362-363` prints a static
  `"find failed on the mount root"` while find's actual message (measured as an unprivileged user:
  `find: '…': Permission denied`, exit 1) goes only to the terminal. The script argues twice that
  discarding the diagnosis is the defect this plan exists to fix (`:215-218`, `:340-342`); this arm
  should capture into a second temp file the way `probe_err` does — note that `2>&1` into
  `mount_children` would break the `-z` test.
- **`client_input=""` at `:360` is dead.** It is assigned at `:371` inside the only branch that reads
  it, and read nowhere else.
- **`mount_children` names one value.** `-print -quit` returns exactly one path; `:371` assigns it
  whole to `client_input`. `mount_child` says what it holds.
- **`find` may pick a nested mountpoint.** `-print -quit` takes whatever `readdir` yields first; if
  that child is itself a mount, `findmnt --target` resolves to *its* root and check [6] fails with
  "the client resolves a different RC address" on a healthy host. Low probability given `~/mnt/<remote>`
  siblings, and it fails closed, but the selection is non-deterministic across runs.
- **The Delivery bullet still owes a hash.** `PLAN.md:277-282` names `17819cda` and `510dbbf4` and then
  *"this round's follow-up"*. The bullet exists because an unrecorded hash nearly lost this plan's diff;
  the newest commit is the one a reader will be looking for. A follow-up commit can fill in `68c8bbfa`.
- `falsify-round3-fixes.bash` establishes the first-row expansion but not the `[ -z "$mount_root" ]`
  assertion beside it — a mutant deleting that guard passes the harness. (I exercised it directly
  instead; it fails closed with `findmnt named no mount for …`.)
- `falsify-round3-fixes.bash`'s `mutant_rejected` greps bare `^REJECTED`, which the assertion-failure
  path satisfies; it does not establish rejection *because of* duplicates. Stated honestly in the
  journal, and now closed by the transcript in finding 3.

## Verified fixed — each reproduced, both directions

- **[2] `findmnt --target` multi-row.** The reasoning holds and the code does what it says.
  `ftp-camera:2` is `set -e` and there is no other `set` line, so `| head -n1` would indeed convert a
  `findmnt` failure into exit 0 with an empty `mount_root`; `${findmnt_out%%$'\n'*}` keeps the failure
  inside the `if !` capture. All three arms exercised against the shipped library with stubs:

  ```
  two identical rows -> proceeds past normalisation ("no rclone mount process is running")
  findmnt exit 0, empty output -> "findmnt named no mount for /mnt/photos/PHOTO", rc=1
  findmnt exit 1 -> "/mnt/photos/PHOTO is not inside any mount", rc=1
  ```

  Also confirmed on this machine that a stacked mount's rows carry the *identical* TARGET string
  (`findmnt -n -o TARGET --target /sys/devices/virtual/powercap` → two identical lines), so "first row"
  cannot pick the wrong target. `findmnt` is util-linux 2.38.1.
- **[3] duplicate check id.** Detected, named and rejected; a clean run is not flagged (transcript
  above, scenarios B and C).
- **[5] the journal link.** `PLAN.md:176-178` now links
  `[JOURNAL/00099-Journal-26-09-16.md](JOURNAL/00099-Journal-26-09-16.md)`; the file exists.
  `plan-qa --sweep` reports nothing for 00099.
- **[6] the Delivery section.** Now names the later deployed-file commits — and it is **more accurate
  than round 3's finding was**. Round 3 listed `c3554f2e` among the three; `git show --stat c3554f2e`
  is `CLAUDE/Plan/Completed/README.md`, 7 insertions, no deployed file. The commit correctly dropped it
  and substituted `68c8bbfa`, which does touch `files/home/.local/bin/rclone-rc-auth.bash`. Verified
  against `git show --stat <h> -- files/ scripts/` for all four.
- **[1] partially.** The `/.` stand-in is gone and the two mutants round 3 named are now killed —
  `falsify-check6-input2.bash` reproduces its table exactly (old input kills 1 of 3, new input kills
  3 of 3, shipped passes both). The residual is finding 1.
- **The no-subdirectory arm is reachable and correct.** Measured: empty dir → exit 0, empty output →
  the "no subdirectory" `bad`; files-only dir → same; non-existent path → exit 1 → the "could not list"
  `bad`; unreadable root as an unprivileged user → exit 1 → the same arm. Neither arm falls back to the
  mount root, both increment `FAIL`, and both therefore REJECT.

## Checked and clean

- **Placement in the IaC graph**: no play, no playbook, no new file. The library change stays where
  Decision 1 puts discovery; the gate change stays plan-local. No runtime probing substituted for
  declared state.
- **Fail-fast**: no `failed_when:`/`ignore_errors:` anywhere in the diff; no skip-and-continue added.
  Every new arm returns non-zero or increments `FAIL`. No `creates:`-style existence-as-success guard.
- **Stderr hygiene**: the library's two new diagnostics are `>&2`; stdout still carries only the
  address. The gate's new `bad` lines are `>&2` via `bad()`; `ok`/COVERAGE are the report's payload.
- **Version bumps**: none owed — `git show --name-only` shows nothing under
  `files/var/local/claude-yolo/`, no `lib/*.bash`, no Dockerfile.
- **Modes and shebangs**: `acceptance.bash` `100755`, `rclone-rc-auth.bash` `100644` (correct for a
  sourced library), unchanged by this commit.
- **Public-repo safety**: scanned every added line for home paths, emails and IPv4 — none. The only
  host-shaped strings are repo-owned (`/etc/ftp-camera`) or kernel-generic
  (`/sys/devices/virtual/powercap`). Harnesses stay under `untracked/scratch/`, and
  `falsify-round3-fixes.bash` writes its mutant into `untracked/scratch/`, not `scripts/` — round 3's
  working-method nit is resolved in practice.
- **Interactive-script rules**: not triggered — `acceptance.bash` prompts for nothing; `--help` exits
  0, an unknown argument exits 1 (`:69-73`).
- **Naming**: `duplicates`, `seen_checks`, `findmnt_out`, `mount_root` all say what they hold
  (`mount_children` excepted, above).
- **Docs**: no user-facing flag, path or default changed; nothing in `docs/` describes check [6]'s
  internals or the library's normalisation.
- **Branch state**: `F44` is level with `origin/F44`; `68c8bbfa` is pushed.

## Observations, not findings against this commit

- The working tree carries uncommitted Plan 00109 and repo-wide changes from a concurrent session
  (`CLAUDE/QA.md`, `docs/playbooks.md`, `helpers/…`, `tests/…`). `git status` was clean at `68c8bbfa`;
  these arrived afterwards and are outside this review.
- `qa-all.bash` reports `files/home/.local/bin/ftp-camera — 1915 of 2499 lines outside the parse tree`
  for semgrep. Pre-existing (ftp-camera is untouched here) and every active rule is pattern-regex, but
  it is a large blind spot in the file this plan's client lives in.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `./scripts/qa-all.bash` | **PASS**, exit 0 — 943 files |
| `hooks-daemon plan-qa --sweep` | exit 1, **7 findings, 0 blocking, none for Plan 00099** |
| `shellcheck -x -S style` | CLEAN on `acceptance.bash` and `rclone-rc-auth.bash` |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook in the diff; `qa-ansible-syntax` covered 82 playbooks green inside `qa-all.bash` |
| `acceptance.bash`, live | ABORTs at check [0], exit 1 (no mount in this container); `--help` exits 0 |
| `qa-helper-tests.bash` | **not triggered** (no `helpers/`, `tests/helpers/` change) — ran inside `qa-all.bash` anyway, 1568 tests |
| `helpers.gnome.check_extension_compat` | **not triggered** (no `metadata.json`) — ran inside `qa-all.bash`, 5 extensions OK |
| `eslint` in `extensions/` | **not triggered** (no extension JS) — `qa-all.bash` js check green, 12 files |

No required gate was skipped.

## Bottom line for the plan

Findings 1, 2, 3 and 5 are container-doable now; finding 4 is a one-commit fix in Plan 00109. Only
after those does Task 5.9's host re-deploy plus a fresh `acceptance.bash` run remain, which is the only
thing that can tick success criteria 1, 3 and 5 (`PLAN.md:211-228`).

The three-rounds-running pattern has **not** stopped. Finding 1 is check [6] failing the same test for
the third consecutive round, and finding 3 is the COVERAGE count being fixed cause-by-cause for the
second round while the comment claims otherwise. In both cases the commit implemented the reviewer's
*example* rather than the property the example illustrated. The recommendation for round 5: for check
[6], stop approximating and build the client's real input from `/etc/ftp-camera/config`; for COVERAGE,
assert the two array lengths rather than enumerating a fourth cause.
