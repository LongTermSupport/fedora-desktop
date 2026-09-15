# QA Review — commit `55ad09ac`, Plan 00109 Task 3.2 (server route)

**Verdict**: FIX-BEFORE-MERGE

Nothing here breaks another user, loses data, leaks anything, or violates a HARD RULE.
What it does have is a report that will never be silent on the profile it was written
for, and a play that is a copy of its sibling.

**Context for the gate results.** The working tree was **not** clean during this review:
`helpers/host_health/login_message.py` and `tests/helpers/host_health/test_login_message.py`
carry uncommitted changes from a concurrent session (a kernel-drift finding, +45 lines in
the helper). Those changes break three of this commit's twelve assertions. Against a clean
export of `55ad09ac` (`git archive 55ad09ac | tar -x -C "$(mktemp -d)"`), the suite is
**12/12 green**. So `qa-all.bash` failing right now is not this commit's defect — but see
finding 6, because it is this commit's fragility.

---

## Should fix

### 1. A healthy server is never silent — measured, not inferred

`helpers/host_health/login_report.py` wires `dkms_text` into both `probe.collect`
(post-boot health) and `check_pins` (installed-vs-pinned). `dkms` is installed by exactly
two plays — `playbooks/imports/optional/hardware-specific/play-displaylink.yml:61` and
`playbooks/imports/optional/experimental/play-virtualbox-windows.yml:21` — both optional,
both desktop hardware. A stock server provisioned by `playbook-main.yml` has no `dkms`.

Measured, running the two checks in a dkms-less environment:

```
  checked=False  the dkms probe could not run: dkms: command not found (dkms status)
  checked=False  evdi_version: could not be checked — dkms: command not found (dkms status)
```

`login_message.render` emits the header whenever `unchecked` is non-empty, so **every
interactive SSH login on a clean server prints "fedora-desktop: this machine needs
attention" plus two lines that will never become actionable.** That is precisely the
"a check that speaks on every login gets muted" failure `login_message.py`'s own docstring
says the surface exists to avoid, and `evdi_version` is a DisplayLink pin with no meaning
on a headless host.

**Fix**: decide what these checks mean on a server. Either declare `dkms` a dependency of
this play, or make "this host has no DKMS at all" a legitimate `ok` in `probe.collect` and
`check_pins` while keeping "dkms present, module missing" a finding. Either way, add
**"a clean server login is silent"** to the HOST list at `PLAN.md:167-168` — the four
claims recorded there do not include it, and it is the one that decides whether this
surface survives contact with a user.

### 2. The timer's `git fetch` has no credentials, and the consequence is permanent

`helpers/play_ledger/git_history.py:39-52` runs the fetch with `GIT_TERMINAL_PROMPT=0` and
`GIT_ASKPASS=""`. `files/home/.config/systemd/user/host-health-collect.service.j2` sets no
`Environment=` and no `SSH_AUTH_SOCK`. If the server's checkout has an SSH remote, the
fetch fails on every timer run, and `helpers/play_ledger/fetch_clock.py:75-79` (`last=None`)
then returns *"play-freshness has never successfully reached the remote on this host"* —
for ever, on every login.

The desktop route never had this problem because it runs inside a graphical session that
has an agent; the server route inherits the code and loses the environment. Same class as
finding 1, different cause.

**Fix**: state the assumption in the unit (a public HTTPS remote fetches anonymously and
this is fine), or give the unit what it needs. Add it to the HOST checks either way.

### 3. The play is a re-wrapped copy of its sibling, and the pair will drift

`diff -u play-host-health-login-report.yml play-host-health-server-report.yml` shows
identical shebang, identical `hosts: desktop`, identical `become: true`, identical opt-in
story (neither is imported by `playbook-main.yml`), a byte-identical two-task scope guard,
and four byte-identical tasks:

| Task | login-report | server-report |
| ---- | ------------ | ------------- |
| `Install The Report's Runtime Dependencies` | 67-70 | 70-73 |
| `Ensure User Systemd Unit Directory Exists` | 72-78 | 75-81 |
| `Resolve The Session User UID` (incl. its 8-line `fail_key: false` comment) | 95-108 | 142-154 |
| `Assert The Session User Exists` | 110-119 | 156-165 |

Five further comment blocks differ **only by line-wrapping**, which is the signature of a
copy: a future fix to the getent comment or the uid assert lands in one file.

The repo's own standard for splitting a play is stated at `docs/playbooks.md:812` —
*"Its own play rather than part of `play-host-health-login-report.yml` because the panel is
a generic multi-section surface **with a lifecycle of its own**"*. This play has no such
claim available: same `become`, same host group, same opt-in story. The only difference is
`scope`, which is a guard variable evaluated inside the play, and the two values are
mutually exclusive, so exactly one of the pair ever does anything on any host. That is the
`play-claude-state-hygiene.yml` shape.

**Fix**: one `play-host-health-report.yml`, `scope: general`, prelude declared once, two
delivery blocks gated on `when: provisioning_profile == 'server'` / `!=` — the pattern the
repo already uses at `playbooks/imports/play-basic-configs.yml:213`. If two files are
genuinely wanted, extract the prelude to `tasks/`, which is an existing convention
(`tasks/ensure-jq.yml`; three plays already use `import_tasks`).

### 4. `CLAUDE/AnsibleStyle.md:240` is now false

It reads: *"**`server`** — headless-only (no core/optional play is `server` today, but the
value exists for symmetry)."* This commit introduces the **only** `scope: server` play in
the repo. A tracked rule file asserting the opposite of what the diff just did.

### 5. The new gate is not in the QA inventory

`CLAUDE/QA.md:19` says `qa-all.bash` runs "**seventeen**" gates; line 34 says "Ten further
gates run"; the table at lines 38-50 lists eleven rows. The actual run printed fifteen such
gates before `host-health-login-snippet`, and aborted before the rest. So the doc was
already stale by roughly five entries and this commit adds a sixth without touching it.

Per `CLAUDE/AgentNotes.md` ("Replacing a stale enumeration with a fresher enumeration is not
the fix; deriving the set is"), the honest fix is to derive the list from `qa-all.bash`
rather than hand-add one more row — but at minimum the new gate needs a row and the two
counts need correcting.

### 6. Three assertions compare the snippet's *entire* stdout against a probe answer

`scripts/test-host-health-login-snippet.bash:208-210` and `:215-217` capture everything the
sourced snippet writes and compare it to `[unset]` and `0`. They only work because
`STATE_CLEAN` happens to render empty. The uncommitted `login_message.py` change in this
very tree already broke both, plus `:159`, and the failure output points at PYTHONPATH and
exit status rather than at the actual cause:

```
  FAIL  sourcing leaves PYTHONPATH unset in the caller
        want: [unset]
        got:  fedora-desktop: this machine needs attention
        ...
        [unset]
```

**Fix**: have the inner shell emit a delimiter and extract the probe's own line — the
`cwd_after` check at `:198-204` already survives this by using a `case` glob rather than
equality. Secondly, `make_state` at `:88-120` hand-builds the JSON, so it does not go
through `status_document.build()` / `write_atomic()`; building the fixture through the
producer would make it track the schema instead of restating it.

---

## Nits

- `files/home/.config/systemd/user/host-health-collect.timer` carries no
  `{{ ansible_managed | comment }}` header, unlike `host-health-collect.service.j2` beside
  it. Nothing on the host tells an editor Ansible owns it. Making it a `.j2` costs one line.
- `RandomizedDelaySec=30min` (`host-health-collect.timer:20`) applies to `OnStartupSec=5min`
  (`:18`) too, so the boot trigger fires 5-35 minutes after the user manager starts. The
  comment at `:15-18` says "shortly after". Writing the derivation beside the value is the
  good habit here, so it is worth being exact.
- `scripts/qa-all.bash:359` — `login_snippet_summary=$(… grep -oE 'passed: [0-9]+') ||
  login_snippet_summary="passed"` turns "the count line disappeared" into a green line with
  no count. Cosmetic (the exit status already gates) but it is the repo's own documented
  "fallback that answers the question it was asked" shape, copied verbatim from the
  `run-log-scrub` block above it — so the fix belongs to the shared shape, not this copy.

---

## Checked and clean

- **Fail-fast**: no `failed_when: false`, no `ignore_errors`, no `|| true`, no
  skip-and-warn. `fail_key: false` on `getent` is probe-then-assert with the assert
  immediately after, and the three `that:` clauses are ordered so `is not none` guards the
  `[1]` index.
- **The interactive guard is necessary and correct.** `grep -a` confirms this Fedora `bash`
  binary contains `SSH_CLIENT`/`SSH2_CLIENT`, i.e. it is built with `SSH_SOURCE_BASHRC`, so
  sshd's non-interactive shells really do read `~/.bashrc`. `case $- in *i*` is the right
  test, and `return 0` is safe: `play-basic-configs.yml:172-176` sources each include from a
  `for` loop, so `return` exits `source` and the loop continues.
- **The bashrc assert is correct.** `bashrc_includes_marker` matches the rendered `BEGIN`
  form of `play-basic-configs.yml:170`, that task carries no `when:` so it runs on servers,
  and the loop globs `*` so `host-health-report.bash` is picked up.
- **Ordering claims verified.** `play-systemd-user-tweaks.yml` really is import #8 of
  `playbook-main.yml` and really is `scope: general` with unconditional linger;
  `play-basic-configs.yml` is #3. Both precede this opt-in play.
- **The units.** `SuccessExitStatus=3` matches `login_report.EXIT_FINDINGS` and correctly
  leaves 1 and 2 as failures; no `[Install]` on the service is right for a timer-triggered
  oneshot; `TimeoutStartSec=120` is needed because systemd disables the oneshot start
  timeout; `OnStartupSec` on a *user* timer is relative to the user manager, which is what
  the comment says; `Persistent=true` applies to the `OnCalendar` trigger.
  `enabled: true` + `state: started` is right — enable alone leaves a timer unarmed.
- **`python3 -m` with `WorkingDirectory` and no `PYTHONPATH`** is sound: `-m` prepends the
  cwd. `PYTHONPATH=… cmd` as a prefix assignment does not export, and the test proves it.
- **Public-repo safety**: no usernames, hostnames, checkout paths, container names or emails
  in any new file. `fedora-desktop` and `LongTermSupport/fedora-desktop` are the permitted
  self-references. All host paths templated through `user_login` / `root_dir`.
- **Plan Commit Rule**: `PLAN.md` and a new `JOURNAL/` day-file landed in the same commit;
  task statuses match reality (the HOST run is correctly still unticked); the journal's
  `passed: 12` claim reproduces. `docs/playbooks.md` gained its section.
- **The 12 assertions are non-vacuous**, including the interpreter check the journal records
  as having survived the first mutation round — it now selects the non-comment line and
  requires exactly one match.

---

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `qa-all.bash` (working tree) | **FAIL** at `host-health-login-snippet: passed: 9 failed: 3` — caused by the uncommitted `login_message.py` change, not by `55ad09ac` |
| `qa-all.bash` (clean export of `55ad09ac`) | login-snippet suite **12/12 PASS** |
| upstream stages | green: `bash 245 OK`, `python 143 OK`, `ansible-syntax 82 playbooks OK (79 under playbooks/imports/, 3 elsewhere)`, `docs 71 OK`, `helper-tests 1260 tests` |
| `hooks-daemon plan-qa --sweep` | 2 findings, **0 blocking**, neither concerning Plan 00109 (a stale path in 00046, a journal-freshness advisory for twelve other plans) |
| `ansible-playbook --syntax-check play-host-health-server-report.yml` | **PASS** |
| `qa-helper-tests.bash` | triggered; runs inside `qa-all.bash` (1260 tests, green) |
| `check_extension_compat`, `eslint` | **not triggered** — the diff touches no `extensions/` metadata and no extension JS |

---

# Round 2 — follow-up commit `ae0a361c` (kernel-mismatch rule)

**Verdict for the pair `55ad09ac` + `ae0a361c`**: still FIX-BEFORE-MERGE. The new rule
closes a real gap and is well tested; it also makes the report say something false in the
scenario it was written for.

**Tree state.** Reviewed as committed. Since then the working tree has moved again: a fold
of the two plays into one `scope: general` `play-host-health-login-report.yml` is in
progress (round-1 finding 3), with `play-host-health-server-report.yml` and
`host-health-collect.timer` deleted and a `.timer.j2` added.

## New findings

### R2-1 (should fix). The stale finding it now surfaces states a false fact

`helpers/host_health/probe_results.py:130` bakes the collecting kernel into the finding
text as *"the running kernel"*. Before this commit a post-reboot document was silent;
now it speaks, and the first thing it says is wrong. Produced by running `render`:

```
fedora-desktop: this machine needs attention
  - evdi: no DKMS module installed for the running kernel 7.1.9-200.fc44.x86_64
  Not checked — these are NOT clean results, nothing is known about them:
  - these results were collected under kernel 7.1.9-200.fc44.x86_64 and this host is
    now running 7.2.4-200.fc44.x86_64, so nothing here describes the running kernel
```

Two consecutive lines give the reader two different values for "the running kernel".
`tests/helpers/host_health/test_login_message.py:209-216`
(`test_it_is_reported_alongside_real_findings_not_instead_of_them`) asserts this exact
pairing and records it as correct, so nothing will find it later.

**Fix**: when the two kernels differ, the boot-scoped findings are no longer present-tense
claims. Move the `broken` group into `unchecked` for a mismatched document, or prefix them
("collected under 7.1.9:"). Then change that test to assert the qualification, not just
co-presence.

### R2-2 (should fix). "nothing here describes the running kernel" overclaims

The document carries four sections. Only `post-boot-health` (DKMS, failed units) is
boot-scoped. `play-ledger`, `play-freshness` and the non-DKMS pin rows are unaffected by a
reboot and remain valid. Placing the line under *"nothing is known about them"* tells the
reader the whole document is void, immediately after printing findings from it. Narrow the
sentence to what a reboot actually invalidates.

### R2-3 (should fix, plan drift). The HOST list was not extended

`PLAN.md` Task 3.2 still names four HOST claims (timer arms, document appears, login shows
findings, `scp` completes). None exercises a reboot, which is this commit's entire subject.
Add a fifth: *reboot into a different kernel; the first login says the results predate this
boot.*

## Nits

- `scripts/test-host-health-login-snippet.bash:100` — `"$(uname -r)"` can be empty, and the
  script runs under `set -uo pipefail` with no `-e`. An empty kernel makes `render`'s
  `and collected_under` guard false, the mismatch goes unreported, and all 12 assertions
  pass while the fixture has quietly become an "unknown kernel" document. That is the same
  class the commit message celebrates catching, one layer down. Bind it first and assert:
  `KERNEL="$(uname -r)"; [ -n "$KERNEL" ] || { echo "FAIL: uname -r gave nothing" >&2; exit 1; }`
- `helpers/host_health/login_message.py:159-183` — ordering is load-bearing and unstated.
  The age chain's `elif age is None and not broken and not unchecked:` tests `unchecked`
  *before* the kernel line is appended, so moving the kernel block above the chain would
  suppress the unknown-age line. Current order is the more informative one; say so.
- Round-1 finding 6 was fixed at the fixture, not at the coupling. Three assertions still
  compare the snippet's whole stdout to a probe answer, so the next rule that makes a
  "clean" document speak breaks them again. `CLAUDE/AgentNotes.md` → *"Generalise a fix past
  the file you were reading"*.

## The four questions asked

**Required keyword vs defaulted — required is right, and for a stronger reason than the
precedent.** Grepped repo-wide (`.py`, `.js`, `.j2`, `.bash`): `render` and
`read_and_render` have no caller outside `login_message.py:204,230` and the tests, so no
call site was missed. A `running_kernel=""` default would have silently disabled the whole
check through the `and running_kernel` guard — a silent partial result, this repo's most
recurrent defect class. A `running_kernel=probe.running_kernel()` default would have made
every test depend on the test host's kernel.

**Import cost — measured, negligible; no cycle.** `login_message` cumulative import time,
`-X importtime`, best of 5, run from each tree root (the first attempt was contaminated by
`sys.path[0]` being the live checkout):

| Tree | Cumulative |
| ---- | ---------- |
| `55ad09ac` (no `probe`) | 16176 us |
| `ae0a361c` (imports `probe`) | 16322 us |

+146 us, ~0.9%, inside run-to-run noise. `probe` pulls only `subprocess` and
`probe_results`, both already reachable from the existing chain, and imports nothing from
`login_message` — no cycle. `probe.running_kernel()` is `os.uname().release`
(`probe.py:89`), no subprocess, so the module's "nothing here raises" contract survives the
new call in `main`.

**"unchecked" rather than "broken" — correct.** Nothing is known to be broken; what is
known is that the results are not about this boot, which is precisely `_NOT_CHECKED`'s
meaning, and it matches how staleness is already classified. The two conditions are
correctly independent and both reported
(`test_a_stale_document_from_another_kernel_reports_both`).

**What it claims but cannot establish** — R2-1 and R2-2.

## Checked and clean (round 2)

- The guard `isinstance(collected_under, str) and collected_under and running_kernel and
  collected_under != running_kernel` requires both sides known before claiming a mismatch,
  which is the right call and is tested from both directions
  (`test_it_does_not_claim_a_kernel_mismatch_it_cannot_know`,
  `test_an_unknown_running_kernel_claims_no_mismatch`).
- Eight new Python cases, each non-vacuous, including a non-string `kernel` value against
  the never-raises contract.
- The fixture change is the right diagnosis: an invented value stopped being inert the
  moment something read it.
- `docs/playbooks.md` gained a matching bullet; `PLAN.md` gained a ticked line; the journal
  entry is append-only and accurate.
- No secrets, no identifiers, no fail-fast violations in the diff.

## Mechanical gates (round 2)

- `qa-all.bash` on the working tree: **FAIL**, one error, and it is **not** `ae0a361c`'s —
  `ERROR (scope): playbooks/imports/optional/common/play-host-health-login-report.yml —
  unnecessary scope guard on a general-scope play (guard never fires for general; remove
  it)`. That is the in-flight play fold: `CLAUDE/AnsibleStyle.md:238` says a `general` play
  carries **no** guard, so the two scope-guard tasks must be deleted, not carried over.
- Everything `ae0a361c` touches is green in the same run: `python: 143 files OK`,
  `helper-tests: Ran 1260 tests`, `host-health-login-snippet: passed: 12`,
  `panel-contract: 7 constants agree`, `version-pins: COVERAGE: 9 of 9`.
- Note for the fold: moving to `scope: general` also retires round-1 finding 4
  (`CLAUDE/AnsibleStyle.md:240`, "no core/optional play is `server` today"), since no
  `scope: server` play would remain.

---

# Round 3 — verification of `7fa9edef`

**Verdict**: all six fixes land. Three residual holes, all of the same family, none
blocking. `qa-all.bash` is green (876 files) with the round-2 work in the tree.

The tree has moved past `7fa9edef` again — `login_report.HEALTH` now reads
`status_document.BOOT_SCOPED_SECTION` and `DESIGN-server-route.md` §4.1 describes the
demotion fix for R2-1. Uncommitted and **not reviewed here**.

## Fix-by-fix

| # | Fix | Verified |
| - | --- | -------- |
| 1 | healthy server silent | **Yes, both halves — and the spelling proved** (below) |
| 2 | fetch credentials left reported | **Agreed**, with one qualification |
| 3 | play merge | **Yes** — one `scope: general` play, server play deleted, both deliveries `when:`-gated, recognition assert correctly named |
| 4 | `AnsibleStyle.md:240` | **Confirmed dissolved** — `grep -rn "scope: server" playbooks/` returns nothing, so the sentence is true again |
| 5 | derived QA gate inventory | **Yes** — 28 gates parsed, 0 missing rows; two guard gaps |
| 6 | fragile assertions | **Yes** — `PROBE:` marker with `NO-PROBE-LINE` for absence; fixture built through `status_document.build`/`write_atomic` |
| — | nits | timer is a `.j2` with the `ansible_managed` header; the `RandomizedDelaySec` comment states the 5–35 minute window |

### On fix 1, the verification the container could not give you

"No dkms, no ledger → both sections return nothing" passes **whether or not
`pin.playbook` and the ledger key are the same spelling**: an empty ledger empties
`applicable` either way. So it cannot distinguish a working filter from one that never
matches — which would have silenced the whole install-state axis on every host. I drove
`check_pins.check` across five ledger states:

```
[ledger unreadable (None)]                    -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
[ledger has displaylink]                      -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
[ledger has only nvm]                         -> compared 0 of 1 pins applicable to this host
[ledger non-empty, no pinned play at all]     -> 0 findings
[empty ledger]                                -> 0 findings
```

Row 2 is the one that matters: the filter fires, so `pin.playbook` and
`ledger.fold_latest`'s key agree. Fix 1 is correctly wired.

### On fix 2 — agreed, with one qualification

The distinction is right and I withdraw the finding. An unreachable remote names a real
gap in the host's own setup with an operator-side remedy; a DisplayLink pin on a box with
no DisplayLink names nothing. Documented in the unit, the docs and a HOST item is the
right disposition.

The qualification: `fetch_clock.offline_finding(last=None)` returns its finding
**immediately**, not after `STALE_AFTER_DAYS`. So on a server whose fetch never works this
is permanent noise from the first login, not a bounded grace period. The HOST item should
therefore *confirm* the remote fetches anonymously rather than assume it — if it does not,
this is finding 1 again wearing a different hat.

## Residual holes

### H1 — the desktop consequence you asked about, and it is the founding incident

Measured, row 3 above. A host whose ledger has rows but **no `play-displaylink.yml` row**
— which is every host today, because Task 1.3 chose no backfill and the callback only
started recording recently — no longer reports `evdi_version (behind): pinned 1.15.0,
installed 1.14.16`. It reports `compared 0 of 1 pins applicable to this host` instead: a
statement about the checker, not a finding an operator can act on. Before `7fa9edef` that
host reported the drift.

Worse, row 4: a ledger with rows and **no pinned play at all** gives `applicable == []`,
the `applicable and tracked == 0` guard is skipped, and the output is **empty** —
indistinguishable from "every pin matched". The zero-coverage guard covers *some
applicable, none tracked* and is blind to *none applicable*, which is the silencing value.
That is this repo's named class with the sign flipped.

**Fix**: state coverage unconditionally, e.g.
`COVERAGE: 1 of 9 declared pins applicable here (7 plays in the ledger)`, so "the ledger
predates these plays" is distinguishable from "this host runs none of them".

**And re-derive rather than transplant.** Task 1.3's rule was settled for the *freshness*
axis, where "never run here → silent" is benign: nobody wants nagging about a play they
never ran. On the *install-state* axis the same rule silences the check this plan exists to
add, on the host the incident happened on. The two axes ask different questions of the same
absence.

### H2 — the tri-state hole you asked about

The shape is sound: `FileNotFoundError → []`, `OSError → None`, `None` falls through to
the unchecked branch, and a stray file (`dkms_dbversion`) is correctly filtered out. I
exercised all of those.

The hole is `os.path.isdir()`: it swallows **any** `OSError` and answers `False`, so an
entry that cannot be statted silently leaves the list. `os.listdir` having succeeded means
the result is a short list or `[]` — never `None` — so a partial read renders as a
complete one, in the direction that buys silence. Demonstrated adjacent behaviour: a
dangling symlink is dropped with no trace (`['evdi']`, `orphan` gone). Narrow in practice
— `/var/lib/dkms` is 0755 with statable entries, and a tightened parent raises in
`listdir` and correctly answers `None` — and I could not reproduce the permission variant
because this container runs as root.

**Fix**: `os.scandir` with a per-entry `try: entry.is_dir() except OSError: return None`.
Three lines, and it closes the one direction the tri-state cannot currently express.

### H3 — the derived gate inventory is derived in one direction, by one spelling

`qa_gates` requires the literal `$SCRIPT_DIR/`. Demonstrated:

```
$SCRIPT_DIR/x.bash        -> ['x.bash']
${SCRIPT_DIR}/x.bash      -> []
$REPO_ROOT/scripts/x.bash -> []
```

A future gate invoked in either of the other two forms is **silently exempt** from the
documentation requirement, and the zero-discovery guard fires only if *every* form fails.
That is the partial case, unguarded, inside the check written to fix a partial-coverage
problem.

Second: the check is one-directional, gates ⊆ doc. A row for a gate that no longer runs
stays for ever — which is precisely the failure `CLAUDE/QA.md` narrates two paragraphs
under the table (`qa-helper-tests.bash` and `check_extension_compat` "documented here as
gates and not run by `qa-all.bash` until Plan 00081"). I checked: 0 orphans today, so this
is a guard gap rather than a live defect. Assert both directions.

Third: "twenty-eight / twenty-one" are still hand-written prose. Correct today; nothing
derives them, so they can go stale while the table stays right.

## Nit

`probe_results.build_report`'s message says *"dkms is not installed, but N DKMS module
tree(s) are still registered"*, but `missing=True` comes from `FileNotFoundError` out of
`subprocess.run`, which means "not found on **this process's** PATH". A systemd `--user`
unit has a narrower PATH than a login shell, so the sentence can send an operator to
install something already installed — the R2-1 overclaim family. Not live on F44 (`dkms`
resolves under `/usr/bin`). Reword to "dkms could not be found on this service's PATH".

## Convergence gap in the merged play

`playbooks/imports/optional/common/play-host-health-login-report.yml:141,161` gate the
desktop delivery on `not is_server`, and `:172-238` gate the server delivery on
`is_server`. Neither branch **removes** the other's artefacts. Run once with
`-e provisioning_profile=desktop` on a server (the documented override, and the recognition
assert exists precisely because a human types it), correct it, re-run — and the host keeps
an enabled `host-health.service` it can never deliver from, alongside the timer. The two
separate plays had the same gap, so this is not a regression; the merge is the natural
place to close it, with `state: absent` / `enabled: false` on the other profile's units.

## Mechanical gates (round 3)

- `qa-all.bash`: **✓ QA passed: 876 files checked** — `helper-tests` 1282 tests,
  `host-health-login-snippet: passed: 12`, `docs: 71 files OK`, `ansible` clean (the
  general-scope guard error from round 2 is resolved), `panel-contract` 7 constants agree,
  `version-pins: COVERAGE: 9 of 9`.
- The six snippet mutants are asserted by the commit message; I did not re-kill them, as
  that needs mutating tracked files.

---

# Round 4 — verification of `2790e163` (the demotion)

**Verdict**: R2-1 is fixed properly. Demotion was the right choice over rewording.
`qa-all.bash` green, 876 files.

## The fix, demonstrated

Same document, rendered both ways:

```
=== AFTER A REBOOT (kernel mismatch) ===
fedora-desktop: this machine needs attention
  - play-podman.yml has changed since it was run here
  Not checked — these are NOT clean results, nothing is known about them:
  - these results were collected under kernel 7.1.9-… and this host is now running
    7.2.4-…, so the post-boot checks describe a different boot and nothing has looked
    at the kernel you are on
  - evdi: no DKMS module installed for the running kernel 7.1.9-…
  - foo.service (system): failed
  - some pin could not be checked

=== SAME DOCUMENT, SAME BOOT ===
fedora-desktop: this machine needs attention
  - evdi: no DKMS module installed for the running kernel 7.1.9-…
  - foo.service (system): failed
  - play-podman.yml has changed since it was run here
  Not checked — …
  - some pin could not be checked
```

Everything claimed holds: only the boot-scoped section moves; `play-freshness` keeps its
fault; the explanation is `insert(0)`-ed so it precedes what it explains; the no-mismatch
path is byte-identical to before. The corrected
`test_it_is_reported_alongside_real_findings_not_instead_of_them` now uses a *surviving*
section's finding, which is the property that was actually meant.

## `BOOT_SCOPED_SECTION`'s placement — confirmed sound

Checked rather than accepted. `helpers/gnome/check_panel_contract.py:115-117` derives
`section_ids()` from `inspect.signature(login_report.collect_sections)` and the dict it
returns, and `login_report.HEALTH` now reads `status_document.BOOT_SCOPED_SECTION`. So
renaming the constant propagates into the produced section key, and the panel gate then
demands the panel mention the new id. Producer and consumer cannot drift apart silently,
which is the reason the constant moved. No contract hole created.

## Your question: fix the root cause at the producer?

**No — and the reason is not the wording.** Leave `dkms_findings`' text alone. On the
desktop route, and at collection time on both, "the running kernel 7.1.9" is exactly
right and more informative than a version stripped of why it mattered. The demotion plus
the explanation line covers the read-later case, and I confirmed the ordering makes the
qualification unmissable.

The root cause worth fixing is one level up: **"is this document about the boot I am in?"
is a property of the document, and it is implemented in one of its two declared
consumers.** `status_document.py`'s own docstring opens with "One producer, two consumers"
— the login message and the GNOME panel. The panel has no kernel awareness at all
(`statusDocument.js` carries `kernel: ''` in its fallback shape and compares it to
nothing), so it renders `post-boot-health` findings as current faults regardless of which
boot produced them.

That is reachable, not theoretical. The argument that the desktop cannot go stale rests on
`host-health.service` running at every graphical login — so it fails precisely when that
unit fails, which is one of the things this plan exists to detect. A desktop whose
collector is broken shows the panel the previous boot's DKMS faults, naming a kernel that
is not running, with nothing saying so. Exactly the defect just fixed, one consumer over,
in the surface a user looks at most.

**Suggested**: lift the predicate into `status_document` (`is_boot_stale(document, *,
running_kernel)` or similar), have `login_message` call it instead of computing `rebooted`
inline, and put the panel side in Phase 4. `CLAUDE/AgentNotes.md` → *"Generalise a fix
past the file you were reading"*.

## Your question: the recognition assert

**Agreed — no pushback. Your reading is the correct one.** `CLAUDE/AnsibleStyle.md:236-247`
scopes "carries **no** guard" to the two-task `meta: end_play` block it then prints
verbatim and calls byte-identical, and `qa-ansible.bash` enforces exactly that block.
An `assert` validating an input is a different thing, and the play passes the gate.

I also checked the load-bearing claim rather than taking it: `is_server` is
`provisioning_profile == 'server'`, so any unrecognised value — only reachable through
`-e`, which has highest precedence — evaluates False and takes the desktop branch. Without
the assert a typo silently deploys a `notify-send` unit to a box with no session bus and
no login report at all. The assert earns its place and the comment now says why.

## Still standing from earlier rounds

- **H1 — the desktop pin-applicability consequence.** Unaddressed here, and the most
  significant open item: a host with no `play-displaylink.yml` ledger row no longer
  reports the founding incident, and a ledger with no pinned play at all produces no
  output whatsoever. (The working tree shows `check_pins.check` gaining a
  `dkms_registered` parameter, so this may be in hand.)
- **H3 — the derived gate inventory** is one-directional and keyed to one spelling of
  `$SCRIPT_DIR/`.
- **The convergence gap** in the merged play: neither branch removes the other profile's
  artefacts.
- **Fix 2's qualification**: `offline_finding(last=None)` fires from the first login, so
  the HOST item must confirm the remote fetches without an agent rather than assume it.
- **H2 appears fixed in the working tree** — `dkms_registered_modules` now uses
  `os.scandir` with per-entry `except OSError: return None` and treats an unresolving
  symlink as "could not tell". Uncommitted, so **not reviewed**; on a read it is the fix
  I would have asked for.

## Note on the record

The `uname -r` fixture and `PROBE:` marker points were round-2 findings against
`ae0a361c`; `7fa9edef` fixed them and round 3 recorded fix 6 as verified. Round 3 also
recorded the general-scope guard error as resolved. There is no stale finding of mine to
chase on any of the three.

## Mechanical gates (round 4)

`✓ QA passed: 876 files checked` — `panel-contract` 7 constants and 4 section ids agree,
`version-pins: COVERAGE: 9 of 9`, `extension-compat` clean. The four demotion mutants are
asserted by the commit message; I did not re-kill them, as that needs mutating tracked
files.

---

# Round 5 — verification of `665e64de` (the re-derived pin rule)

**Verdict**: the re-derivation is right and H1 is closed. Two holes remain in the new
rule, one of them measured and reachable on a DisplayLink host. `qa-all.bash` green,
876 files.

## Verified

Drove the incident state (`evdi 1.14.16` against a pinned `1.15.0`) across all four
ledger states, plus a stock server:

```
ledger None              -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
ledger has displaylink   -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
ledger lacks displaylink -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
ledger empty             -> evdi_version (behind): pinned 1.15.0, installed 1.14.16
stock server             -> SILENT
```

Scoping to `ABSENT` rather than to the population is the correct shape: `BEHIND`, `AHEAD`
and `UNDETERMINED` all mean the software is present and was compared, so no ledger state
can make them uninteresting. Zero-coverage counts the whole manifest again.

Also verified: `qa_gates` matches all three invocation spellings and reports a
documented-but-unrun gate (injected `qa-ghost.bash` → detected); 28 derived, 28
documented, both directions clean; `dkms_registered_modules` now uses `os.scandir` with a
per-entry `except OSError: return None` and treats an unresolving symlink as "could not
tell"; the merged play removes the other profile's unit files **and** their `.wants/`
symlinks in both directions (`:162-169`, `:253-259`).

## H4 — `dkms_registered == []` IS reachable with DisplayLink installed

Yes, and it is measured. `[]` means two different things and only one of them licenses
the skip:

- `/var/lib/dkms` **absent** — no DKMS subsystem. The server case, and a real answer.
- `/var/lib/dkms` **present with no module subdirectories** — `dkms` is installed and its
  registry is empty. Not the same fact at all.

The second is reachable on a DisplayLink host: the `dkms` RPM owns `/var/lib/dkms`, and
`play-displaylink.yml:61` installs `dkms`, so every host that ran that play has the
directory. Real hosts carry `dkms_dbversion`, a file, which the `isdir` filter correctly
drops — so an emptied registry yields exactly `[]`.

Counterfactual with `dkms status` empty and `play-displaylink.yml` in the ledger:

```
registry []          -> SILENT
registry ["nvidia"]  -> evdi_version (absent): pinned 1.15.0, nothing installed
registry None        -> evdi_version (absent): pinned 1.15.0, nothing installed
```

One unrelated module restores the correct finding, so `[]` alone is what suppresses it.
"The DisplayLink play ran here and the module is now gone" is precisely what this axis
exists to say — and **Task 0.2 of this plan is about to go and create that state**, by
removing orphaned DKMS source trees.

**The two predicates are not the same evidence.** `probe_results.build_report` requires
`dkms.missing AND dkms_registered == []`; `check_pins.check` requires
`dkms_registered == []` alone. The probe cannot be fooled by an empty registry on a host
that has `dkms`; the pin check can.

**Fix**: either narrow the pin skip to the same conjunction (pass the probe's `missing`
signal through), or give `dkms_registered_modules` a fourth state distinguishing "no state
directory" from "directory present, no modules". Both preserve the server fix, because a
server has no `/var/lib/dkms` at all.

## H5 — the BROKEN sentinel is the one state where the ABSENT backstop is off

The scoping leans on "an empty or unreadable ledger is `ledger_presence`'s finding, so
suppression can never be silent". That holds — `ledger_presence.findings` returns a
`broken` finding for an empty ledger and an `unchecked` one for an unreadable one — with
one exception it does not cover.

`ledger_presence.py:49-50` returns `[]` **deliberately** while `ledger.sentinel_path(base)`
exists, because `check_freshness` already refuses to answer and prints the reason. But
`login_report.plays_run_here:220-223` catches only `OSError`/`ValueError`; it never
consults the sentinel. So in the one state where the repo has declared the ledger has a
hole, the pin check reads it anyway, gets a possibly-incomplete set, and silently
suppresses `ABSENT` for every play whose row is in the hole — with nothing reporting the
ledger's condition, by design.

**Fix**: `plays_run_here` returns `None` when the sentinel exists. That is the same rule
already applied to `OSError` — an open question must not buy silence — and the sentinel is
the repo's own declaration that the question is open.

## H6 — `_command_version` has no ABSENT branch, so the noise returns by another door

`_rpm_version` (`check_pins.py:262-265`) catches `"is not installed"` and returns `None`,
which becomes `ABSENT` and is therefore covered by the ledger scoping. `_command_version`
(`:268-269`) is a bare `_run([command, "--version"])`, and `_run` raises
`ResolutionError(f"{argv[0]}: command not found")` on `FileNotFoundError`. That is caught
by the broad `except Exception` at `:208` and becomes an **unchecked** finding, `"<var>:
could not be checked — foo: command not found"`, reported unconditionally on every host
that never ran the owning play.

So the first tracked `command`-kind pin belonging to an optional play reintroduces the
original permanent server noise, through the one resolver the `ABSENT` scoping cannot
reach — because it never gets as far as `classify`. Not live today: the manifest has nine
pins and the one tracked pin is DKMS-kind. Invisible when it stops being true.

**Fix**: give `_command_version` the ABSENT branch `_rpm_version` already has — a command
that is not installed resolves to `None`, not an exception.

## Accepted consequence worth naming

A host that ran the play and has since *deliberately* removed the software now reports
`ABSENT` for ever. That is arguably correct — the repo declares the pin, so an
uninstalled package is drift — but it is a permanent, unactionable line for anyone who
runs an optional play once and then removes what it installed. Worth being a decision
rather than a side effect.

## Mechanical gates (round 5)

`✓ QA passed: 876 files checked`. `version-pins: COVERAGE: 9 of 9`, `panel-contract` 7
constants and 4 section ids agree, `host-health-login-snippet: passed: 12`.

Two working-tree changes seen and **not reviewed** (uncommitted): `status_document` has
gained `collected_kernel()` and `is_boot_stale()`, which is the round-4 recommendation.

---

# Round 6 — `b0679155`, and a re-check of round 5's three findings

**Do not close yet.** `665e64de` was reviewed in **round 5** above, not skipped — it
carries three open findings (H4, H5, H6), two of which are the direct answers to the
first two questions asked here. All three still reproduce at `b0679155`.

## `b0679155` — verified

`login_message.render` now calls `status_document.is_boot_stale` and
`collected_kernel` instead of computing the predicate inline; the panel gap is recorded
as a Task 4.2 item naming the predicate. `is_boot_stale` behaves correctly on all three
inputs I drove: differing kernels `True`, matching `False`, and an `unavailable` document
(`kernel: ""`) `False`. Clean export of `b0679155`: **1302 helper tests, OK**.

## Q3 — is `is_boot_stale` in the right place?

**Yes, and it does create an obligation the panel gate cannot enforce — but that gate
should not be the thing enforcing it.**

Right place: it is a property of the document, both declared readers ask it of the same
file, and the alternative is what round 4 flagged. Nothing to change.

The obligation is real, and the existing gate already demonstrates why it will not catch
it. `check_panel_contract` requires the panel to *mention* each of seven document keys,
and `kernel` is one of them — the gate passes today. The panel's only two mentions of it
are a prose comment (`statusDocument.js:20`) and `kernel: ''` in a fallback shape
(`:64`), which compares nothing. So the panel already satisfies a `kernel` obligation
while being entirely boot-unaware: **a vocabulary check being read as a behaviour check.**

Do not try to extend the contract gate to cover this — it is a category error. A
cross-language literal check can prove the two sides use the same words and can never
prove one of them asks a question. The Task 4.2 item is the right mechanism. What should
prove it when the panel side lands is a test that renders a **boot-stale document**
through the panel's own section code and asserts the findings are not presented as
current — not another mention.

## Q1 and Q2 — answered in round 5, and re-verified at `b0679155`

**Q2, `dkms_registered == []` with DisplayLink installed: yes.** Re-run at this HEAD, with
`dkms status` empty and `play-displaylink.yml` in the ledger:

```
registry []          -> SILENT
registry ['nvidia']  -> evdi_version (absent): pinned 1.15.0, nothing installed
registry None        -> evdi_version (absent): pinned 1.15.0, nothing installed
```

Unchanged from round 5. `[]` conflates "no `/var/lib/dkms`" with "directory present, no
module subdirectories"; the `dkms` rpm owns that directory and `play-displaylink.yml:61`
installs it, so the second is the state of every DisplayLink host the moment its module
is removed — which Task 0.2 sets out to do. `build_report` requires
`dkms.missing AND registered == []`; `check_pins` requires `registered == []` alone, so
the two predicates are not the same evidence. Full detail in **H4**.

**Q1, the ABSENT scoping: two cases, both still live.**

- **H5, the BROKEN sentinel.** `login_report.plays_run_here` does not mention the
  sentinel — confirmed at this HEAD by reading the function's source. In the one state
  where the repo has declared the ledger has a hole, `ledger_presence` is silent *by
  design* (`ledger_presence.py:49-50`) and the pin check reads the runs file anyway,
  suppressing `ABSENT` for every play in the hole with nothing reporting the condition.
- **H6, `_command_version` has no ABSENT branch.** Confirmed unchanged:
  `return _run([command, "--version"]).strip() or None`. A missing binary raises through
  `_run`, is caught by the broad handler, and becomes an unconditional "could not be
  checked" — the original permanent noise, through the one resolver ABSENT-scoping cannot
  reach. Not live today; invisible when it stops being so.

## Mechanical gates (round 6)

- Clean export of `b0679155`: `qa-helper-tests.bash` **1302 tests, OK**.
- `qa-all.bash` on the **working tree**: FAILS, 34 errors in the helper suite. **Not
  either reviewed commit** — a concurrent, half-applied refactor introducing
  `probe_results.DkmsRegistry` (a tri-state `present` field, which is H4's fix) has
  changed `build_report`'s signature from `dkms_registered=` to `registry=` and its
  callers and tests have not all caught up. Uncommitted and not reviewed; on a read it is
  the right shape for H4.

---

# Round 7 — verification of `8392c406` (H4, H5, H6)

**All three fixed and measured.** `qa-all.bash` green, 876 files. `b0679155` was already
verified in round 6 (1302 helper tests on a clean export).

There is a fourth instance of the shape, it is in this commit, and the fix for it already
exists in this codebase.

## Verified

**H4** — the counterfactual, `dkms status` empty, `play-displaylink.yml` in the ledger:

```
present=False (no directory)       -> SILENT
present=True, modules=() EMPTY REG -> evdi_version (absent): pinned 1.15.0, nothing installed
present=True, modules=('nvidia',)  -> evdi_version (absent): pinned 1.15.0, nothing installed
present=None (could not tell)      -> evdi_version (absent): pinned 1.15.0, nothing installed
```

The decisive row is the second: the DisplayLink-host-with-an-emptied-registry state now
reports, where it was silent in rounds 5 and 6. Only "no directory at all" buys silence.
And `dkms_registry` reads the disk correctly: absent → `present=False`; a directory
holding only `dkms_dbversion` → `present=True, modules=()`; one module tree →
`present=True, modules=('evdi',)`.

**H5** — `plays_run_here` on a temp base: no ledger → `set()`; sentinel present → `None`.

**H6** — `_command_version("definitely-not-a-real-command-00109")` → `None`, so a missing
binary becomes `ABSENT` and the ledger disambiguates it, instead of a permanent unchecked
finding.

`DkmsRegistry` as a single value read once is the right answer to the frame you named —
two consumers can no longer hold inconsistent halves of it.

## The fourth instance you asked for — and it is in H6's own fix

**`check_pins._run` discriminates by string containment on an error message**, which
conflates *"the binary is absent"* with *"the binary ran, failed, and its output happened
to contain that phrase"*. Demonstrated with a script that exists, exits 127 and prints
`inner-thing: command not found` on stderr:

```
a tool that RAN and failed, printing that phrase -> None
a tool that is genuinely absent                  -> None
```

`None` means `ABSENT`, which renders as **"pinned X, nothing installed"** — a confident
claim about this host, derived from a probe that ran and broke. That is
`CLAUDE/AgentNotes.md`'s *"a fallback that answers the question it was asked"*, and the
direction of harm is the bad one: a failure becomes data rather than a report.

`_rpm_version` has had the identical shape all along (`if "is not installed" in
str(error)`), so H6 propagated the pattern rather than introducing it.

**The fix already exists eight files away.** `probe.run_probe` faced exactly this question
and answered it structurally: `FileNotFoundError` sets `ProbeOutcome.missing=True`, and
the caller branches on a field, never on the wording. `check_pins._run` knows the same
thing at the same moment — it has the `FileNotFoundError` branch and the non-zero-exit
branch in front of it — and throws the distinction away by flattening both into one
`ResolutionError` message. Raise a distinct type from the absence branches (a
`NotInstalledError(ResolutionError)`) and have both resolvers catch the type. That is the
same generalisation this diff has now made twice: `DkmsRegistry` carried two facts apart,
`ProbeOutcome.missing` carried two facts apart, and this is the third pair still riding in
one string.

## Two stale references

- `DESIGN-server-route.md:126` — "driven by the same `dkms_registered_modules()` tri-state
  as the probe, **so the two cannot disagree about whether this host has DKMS**". The
  function no longer exists under that name, and that sentence is the exact claim H4
  disproved, still stated as current in a design document. Whatever §5.x adds below it,
  this bullet needs correcting rather than supplementing —
  `CLAUDE/AgentNotes.md` → *"Completed narrative in a PLAN is where superseded reasoning
  survives"*.
- `helpers/version_pins/check_pins.py:152` — the `check()` docstring still says
  "`ran_plays` and `dkms_registered` are the two things this host knows about itself".
  The parameter is `registry`.

(The two hits in `JOURNAL/` and in this report are correctly historical — append-only
records of what was true at the time.)

## Mechanical gates (round 7)

`✓ QA passed: 876 files checked`. `panel-contract` 7 constants and 4 section ids agree,
`version-pins: COVERAGE: 9 of 9`, `host-health-login-snippet: passed: 12`.

## Where this leaves Task 3.2's container-side work

Nothing blocking remains. The fourth conflation and the two stale references are the
whole outstanding list, and none of them changes behaviour on a host today — the
`command`-kind resolver has no tracked pin, and the doc lines are prose. Fix them and the
container-side work is done; the HOST items in `PLAN.md` are the real remaining gate.

---

# Round 8 — `f48a5bed`, and the fifth instance

`8392c406` was verified in **round 7** above — H4, H5 and H6 each measured, plus the
fourth instance of the shape. This round covers `f48a5bed` and answers the standing ask.
`qa-all.bash` green, 876 files.

**There is a fifth, it is measured, and it is the plan's own founding failure mode.**

## `f48a5bed` — verified

Docs-only and accurate. `check_panel_contract`'s docstring now states what the gate
proves and what it cannot, names `kernel` as the live example, and says why extending it
would be a category error. The Task 4.2 item says the proof is a test rendering a
boot-stale document through the panel's own section code, **explicitly not** another
required mention. That is the right disposition and there is nothing to add.

## The fifth — a malformed document reads as a healthy host

`login_message.render` reads `sections` defensively and `_texts` returns `[]` for *"not a
dict"*, *"key missing"*, *"not a list"* and *"genuinely empty"* alike. On this surface an
empty result is silence, and silence means healthy. Measured — five malformed shapes, all
carrying a **current timestamp, the running kernel and `schema: 1`**, so nothing else
flags them either:

```
sections is a string      -> SILENT (reads as healthy)
sections is a list        -> SILENT
a section is a string     -> SILENT
findings is a string      -> SILENT     <- the document's own state field says "findings"
findings holds dicts      -> SILENT
(control) a real finding  -> "- evdi: no DKMS module"
```

The fourth row is the sharpest: the document **says** `state: "findings"` and the
consumer prints nothing, because `render` never reads `state` — it reads `findings` and
`unchecked` through `_texts`, which answers `[]` for a string. The document's own
self-description and the rendered output contradict each other and nothing notices.

**The asymmetry is inside one module.** `status_document.read` turns absent, unparseable
and unknown-schema into `unavailable`, on the stated rule that *"an absent document is
ignorance, not health"*. A document that parses, declares a schema this reader knows, and
then carries unreadable sections becomes **silence** instead. `SCHEMA_VERSION` exists to
catch a shape change and guards only the top-level integer.

**Why it was invisible.** The trade was made deliberately and the instinct is right — a
login shell must not lose its prompt to a traceback — but *never raise* and *never go
silent* are not in conflict here. `status_document.collect` already shows the third
option: a producer that raises becomes an `unchecked` finding **naming the section**.
`_texts` can do the same instead of returning `[]`. And the test that covers this
(`test_a_section_whose_findings_is_not_a_list_does_not_raise`) pins *does not raise* and
says nothing about *is not silent* — the name answers the question nobody then goes and
checks.

**It crosses the consumers too.** `sections/health.js:51` branches on
`section.state === StatusDocument.OK` and iterates `section.findings` at `:56`, so on the
fourth row the panel takes a different path from the login message, which is silent. I
have not run GJS and am not claiming what the panel renders — only that the two consumers
do not agree, on a document whose own `state` field is the thing one of them reads and the
other does not.

**Fix**: have `_texts` report an unreadable group rather than return `[]`, naming the
section, in the not-checked group. Then rename the test for the property it asserts, and
add one that a section carrying a `findings` string is *not* silent.

## Round 7's three items — all in the working tree, uncommitted

Seen and not reviewed as committed, but on a read each is the right fix:

- `DESIGN-server-route.md:125-127` no longer claims the two consumers "cannot disagree";
  it now says they read one value and ask it different questions.
- `check_pins.check`'s docstring names `registry`.
- The fourth conflation is properly closed: `_command_version` catches a `NotInstalled`
  **type**, and `_rpm_version` is narrowed to `error.returncode` plus
  `f"package {package} is not installed" in error.stdout` — the stream rpm actually uses
  and the package this call asked about. The residual (`rpm -q --quiet` would be fully
  structural, at one more subprocess on the login path) is **recorded rather than taken**,
  which is the right way to leave it.

## Mechanical gates (round 8)

`✓ QA passed: 876 files checked`. `panel-contract` 7 constants and 4 section ids agree,
`version-pins: COVERAGE: 9 of 9`, `host-health-login-snippet: passed: 12`.

## Closing

The fifth is the only new finding, and it is the one I would not close the container-side
work over: a status document that parses and is garbage renders as a clean host, which is
the exact failure Plan 00109 was opened to prevent, one layer inside the mechanism built
to prevent it. The fix is small and local. Everything else on my list is either fixed or
sitting in the working tree already fixed.

---

# Round 9 — `5f14b20e`, and a sixth

**`5f14b20e` is correct and verified.** It fixes the **fourth** instance. Clean export:
**1316 helper tests, OK**.

## Verified

The two cases I named, plus the rpm narrowing:

```
a tool that RAN and printed the phrase -> raises ResolutionError   ✓
a genuinely absent binary              -> None                     ✓
an rpm-shaped failure naming ANOTHER package, asked about 'evdi'
        returncode=1, narrowed match fires: False -> raises        ✓
```

`NotInstalled` as a `ResolutionError` subclass is the right call — a caller may be more
specific, none can escape the broad handler that keeps a login shell from seeing a
traceback. Recording the fully structural `rpm -q --quiet` answer rather than taking it,
with the cost named, is the right way to leave a trade-off.

Both stale references are fixed: `DESIGN-server-route.md:125-127` no longer claims the two
consumers cannot disagree, and `check_pins.check`'s docstring names `registry`.

## The fifth is a different finding, and it is landing separately

`5f14b20e` touches `check_pins.py`, its tests and three docs — nothing else. Re-measured
at that HEAD, all five shapes from round 8 are still silent:

```
sections is a string / sections is a list / a section is a string /
findings is a string / findings holds dicts   -> SILENT (reads as healthy)
(control) a real finding                      -> "- evdi: no DKMS module"
```

Worth keeping the ledger of the shape straight, since that ledger is the durable output:
the thing that *was* inside the fix for the fourth is the string discrimination, and that
is the **fourth** (round 7). Round 8's fifth is `login_message._texts` collapsing
*malformed* into *empty* in a different module.

**And it is being fixed as I write.** The working tree carries
`status_document.unreadable_reasons()` with `TestAMalformedDocumentIsNotAHealthyHost` and
`TestAShapeItCannotReadIsNotAHealthyHost`, and all four of `qa-all.bash`'s current
failures are in exactly those two classes — the fix mid-flight, not a regression.
Uncommitted and so not reviewed, but on a read it is the right shape: it names each
unreadable section rather than returning a bare boolean, holds `unchecked` to the same
standard as `findings`, and its docstring makes the never-raise-and-never-go-silent point
directly.

## The sixth — inside the type just introduced

`NotInstalled` is raised from `_run`'s `FileNotFoundError` branch, and the docstring says
*"The command itself is absent, established by the OS rather than by reading text."* What
the OS established is narrower: **not resolvable on this process's PATH**. Demonstrated:

```
/usr/bin/env exists on disk: True
with PATH=/nonexistent, _command_version('env') -> None   <- ABSENT: "nothing installed"
```

A binary that is installed resolves to `ABSENT`, which renders as the confident claim
*"pinned X, nothing installed"* — the same harm just eliminated for the wrapper-script
case, surviving for the PATH case. Same shape, one level down: a predicate whose answer
depends on a distinction it does not make.

It is not academic because of where this runs. The consumer is a **systemd `--user`
unit**, whose PATH is narrower than the login shell an operator would test in.
`probe_results.build_report` carries the parallel version: `ProbeOutcome.missing` comes
from the same `FileNotFoundError`, and the message it feeds says *"dkms is not
installed"*.

**Not live today** — the manifest has no tracked `command`-kind pin, and `dkms` resolves
under `/usr/bin` on F44 since the sbin merge. Two ways to close it, both cheap:

- resolve with `shutil.which` before exec'ing, so "absent from PATH" is established
  deliberately and can be said in those words; or
- narrow the wording to *"not found on this service's PATH"* in both places, which costs
  nothing and stops the claim exceeding its evidence.

## Mechanical gates (round 9)

- Clean export of `5f14b20e`: `qa-helper-tests.bash` **1316 tests, OK**.
- `qa-all.bash` on the working tree: FAILS, 4 failures, **all four** in
  `TestAMalformedDocumentIsNotAHealthyHost` / `TestAShapeItCannotReadIsNotAHealthyHost` —
  the fifth's fix in progress.

## Closing

Nothing of mine blocks once the fifth's fix lands and goes green. The sixth is a nit: a
message that claims more than its evidence, on a path no host here exercises today. The
HOST items in `PLAN.md` are the real remaining gate, and the reboot-into-a-different-kernel
one is the claim I would want run first.

---

# Round 10 — `20f89945`, and the two open questions

**Fixed and verified.** `qa-all.bash` green, 876 files. All five shapes now report, each
naming the section and the group it could not read:

```
sections is a string   -> the host status file's sections could not be read, so no
                          check's result has been read from it
a section is a string  -> the post-boot-health section could not be read, so nothing is
                          known about that check
findings is a string   -> the post-boot-health section's findings could not be read, so
                          what it reported is not known
findings holds dicts   -> ...holds entries this reader cannot show, so what they said is
                          not known
```

Distinct wording per shape, so the message tells the reader *where* the document stopped
being readable rather than only that it did. Putting it on the document rather than in
`render` is right for the reason it was right for `is_boot_stale`.

## Q1 — `sections: {}` is the same bug wearing "deliberate"

Yes, change it. Measured:

- `sections: {}` at HEAD → **SILENT**.
- `collect_sections`, with **every** producer raising, still returns **4 keys**
  (`installed-vs-pinned`, `play-freshness`, `play-ledger`, `post-boot-health`), because
  `status_document.collect` guarantees a key per producer. So the producer cannot emit a
  zero-section document under any failure. `_cannot_read` emits one section, not zero.

A zero-section document therefore has no legitimate origin — it is version skew,
truncation or a hand-edit, which is exactly the population `unreadable_reasons` was
written for. Same argument as the five, same answer.

The test costs nothing to change, and is itself an instance of the shape you just wrote
down. `test_an_empty_document_is_still_silent_if_fresh` calls the helper with `{}`, so it
pins *"a document with no sections is silent"* — but the property it exists to protect is
*"a clean fresh document says nothing"*, and that is already pinned twice over by
`test_a_clean_fresh_document_says_nothing_at_all` and `test_not_even_a_reassuring_line`.
The name says "empty document" and what it guards is "clean document": **a name answering
a question nobody then goes and checks**, which is what let the fifth hide.

## Q2 — not reading `state` is the right call, and for a firmer reason

Keep it out of `render`. Not because the case cannot occur, but because `state` is not
independent data. Measured: `section()` derives it from the lists —

```
section([])          -> {'state': 'ok',       'findings': [], 'unchecked': []}
section([broken(x)]) -> {'state': 'findings', 'findings': ['x'], 'unchecked': []}
```

A renderer that reads the lists has already read everything `state` encodes. Re-reading it
in `render` would be a consistency check between a value and its own derivation — and a
second mechanism for that, living in one of two consumers, is precisely how the boot
predicate went wrong in round 4.

**The residual is real and it is on the panel side.** `sections/health.js:51` branches on
`section.state` while `render` does not, so a document whose `state` and lists disagree
makes the two consumers answer differently — and that disagreement is invisible to
`unreadable_reasons`, because such a document is structurally well formed. Put it in Task
4.2's rendering-test population, alongside the boot-stale and malformed documents already
going there. If a mechanism is ever wanted, `status_document.read` is the single place
both consumers pass through, and the right home for it.

So: no second mechanism in `render`; one more row in the Task 4.2 population.

## The sixth — and a correction to my own suggestion

The working tree has already narrowed `ProbeOutcome.missing`'s docstring to *"did not
resolve on **this process's PATH**"* with the systemd `--user` note. That closes it the
right way.

And it corrects me: I offered `shutil.which` as one of two options, and **that option was
wrong** — `which` consults the same `PATH`, so it answers the identical question and
cannot tell an installed-but-unreachable binary from an absent one. Nothing cheap can.
Wording the verdict for the evidence is the whole available fix, and the docstring now
says so explicitly, which is better than a mechanism that would have looked like a
distinction while making none.

## Mechanical gates (round 10)

`✓ QA passed: 876 files checked`; `helper-tests` 1333 tests; `panel-contract` 7 constants
and 4 section ids agree; `version-pins: COVERAGE: 9 of 9`;
`host-health-login-snippet: passed: 12`.

## Closing

Nothing of mine blocks. Q1 is a one-line addition to `unreadable_reasons` plus a repointed
test; Q2 needs no code, only a row in a test population that does not exist yet. Both are
smaller than anything that has come up in this review, and neither changes what a host
reports today.

The container-side work is done as far as I can see it. The HOST items in `PLAN.md` are
the remaining gate, and the reboot-into-a-different-kernel one is the claim I would run
first — it is the only one that exercises the route end to end on the scenario the plan
was opened for.

---

# Round 11 — `ec5602da`, and both questions re-measured at this HEAD

**Verified.** `qa-all.bash` green, 876 files.

```
build_report's user-facing line -> "dkms could not be run here, but 1 DKMS module
                                    tree(s) are still registered on this host (evdi), …"
   claims "is not installed":       False
/usr/bin/env on disk: True | _command_version('env'): None | shutil.which('env'): None
```

The `which` half of my round-9 suggestion was wrong and the test now pins why, in the
same case, so the next reader cannot re-propose it. Narrowing the claim in all three
places — the type, `ProbeOutcome.missing`, and the line a human reads — is the whole
available fix, and asserting `which`'s answer alongside it is better than a comment
saying not to bother.

## Both questions were answered in round 10; here they are re-measured at `ec5602da`

**Q1 — `sections: {}` is the same bug, and I would change it.**

```
sections: {}                                   -> SILENT
collect_sections, every producer raising       -> 4 keys
```

`status_document.collect` guarantees a key per producer, so no failure path emits a
zero-section document, and `_cannot_read` emits one section rather than none. A
zero-section document therefore has no legitimate origin — version skew, truncation or a
hand-edit, the same population the other five came from.

The pinned decision is worth overturning because the test pins the wrong property.
`test_an_empty_document_is_still_silent_if_fresh` guards *"a document with no sections is
silent"*, while the property it exists to protect — *"a clean fresh document says
nothing"* — is already pinned by `test_a_clean_fresh_document_says_nothing_at_all` and
`test_not_even_a_reassuring_line`. Changing it costs no coverage, and the mismatch between
its name and what it checks is the same shape as the defect it now conceals.

**Q2 — leaving `state` unread is right; no second mechanism.**

```
section([])                          -> {'state': 'ok', 'findings': [], 'unchecked': []}
a hand-made state/list disagreement  -> SILENT
```

`state` is derived from the lists by `section()`, so a renderer that reads the lists has
already read everything it encodes. Reading it in `render` would be a consistency check
between a value and its own derivation, and putting that in one of two consumers is how
the boot predicate went wrong in round 4.

The residual is real and belongs to the panel: `health.js:51` branches on `state` while
`render` does not, so a document where they disagree makes the two consumers answer
differently — and `unreadable_reasons` cannot see it, because such a document is
structurally well formed. One more row in Task 4.2's rendering-test population, next to
the boot-stale and malformed documents already going there. If a mechanism is ever
wanted, `status_document.read` is the single place both consumers pass through.

## Closing

Nothing of mine blocks. Q1 is one clause in `unreadable_reasons` plus a repointed test;
Q2 is a row in a test population that does not exist yet. Neither changes what a host
reports today.

Container-side work looks done from here. The HOST items are the gate, and the
reboot-into-a-different-kernel one is the claim to run first.

---

# Round 12 — `216dd9c5`, confirmed. Review closed.

```
sections: {}   -> "the host status names no checks at all, so nothing has been
                   established about this host"
clean host     -> SILENT
real finding   -> "evdi: no DKMS module"
```

All three arms hold: the zero-section case reports, a genuinely clean host is still
silent, and a real finding still reads as a fault. `qa-all.bash`: **876 files, green**.

**No open findings.** Everything raised across eleven rounds is fixed, verified against a
measurement rather than an argument, or recorded as a decision with its cost named.

## What remains, and it is not container-side

The HOST items in `PLAN.md` Task 3.2. In the order I would run them:

1. **Reboot into a different kernel and log in before the timer next fires.** The only
   claim that exercises the route's whole reason for existing, and the one the other four
   do not cover.
2. Run the play on a server profile: the timer arms, a document appears, an interactive
   login shows findings, and an `scp` to the host still completes.
3. **Confirm a clean server login is silent** — the property every noise fix in rounds
   5–12 was protecting, and the one only a real host can demonstrate.
4. Confirm the checkout has a remote the timer can fetch without an agent, or
   `play-freshness` reports "never reached the remote" from the first login onwards.

## The one to keep

Of the eight instances, the repointed test is the one worth a future reader's attention
first: **a test named for one property while guarding another is the version of this
defect that hides all the others.** Every other instance was found by measuring something;
that one is found by reading a test's name beside its body and asking whether they agree.

---

# Round 13 — correction: three of my four host items are no longer host items

**Round 12's ordering is superseded.** I tagged four claims HOST without asking whether
this repo's own VM lab could make them; the owner asked and the answer was yes. Confirmed
at `5b91bc60`:

```
server-host-health-kernel-change:
  base: server-fast
  planned: 14
  max_skipped: 0
  reboot_before_checks: true
  run_env: RUN_BASH_OPTIONAL_PLAYBOOKS: play-host-health-login-report.yml
```

| Round-12 item | Now |
| ------------- | --- |
| 1. reboot into a different kernel | checks 7–10 |
| 2. run the play on a server profile | checks 1, 2, 5, 6, 11 |
| 3. a clean server login is silent | check 4 |
| 4. a remote the timer can fetch without an agent | **still HOST** — a guest cloned over https proves the mechanism, not how *this* checkout's `origin` is configured |

The distinction on item 4 is the right one and the plan now states it rather than letting
the VM imply coverage. A guest with `SSH_AUTH_SOCK` unset shows the code path works; it
says nothing about a particular host's remote.

Better than a host run for item 1, too: a host has to wait for Fedora to ship a kernel,
while a guest can simply be given a second one.

## `reboot_before_checks` — the same shape, correctly diagnosed

Gating the mid-run reboot on `BASE_PROFILE == desktop` read a **scenario's** decision off
a **profile**. A desktop rebooted because Wayland cannot reload the shell — a fact about
what its checks look at, not a law of the profile — so a server scenario that needed a
reboot could not have one. Declaring it per scenario, with the profile supplying only the
mechanics, is the fix. `scripts/qa-vmtest-manifest.bash:162` then refuses a fixture beside
a scenario that does not declare the flag, which closes the obvious way to reintroduce it:
a fixture that exists and never runs.

## The ninth member is real, and it generalises

The demotion check would have passed **because the population it judges is empty** — no
dkms on a server, no failed units on a healthy guest — so the fixture makes a unit fail on
purpose and lets that second document survive the reboot. That is §6a in the verification
layer rather than the code, and it is well found.

**The same question is owed to the other thirteen checks in that checker**, and it is
exactly the generalisation step this review kept finding missing. `max_skipped: 0` guards
a check that *skips*; it does not guard a check that runs, judges an empty population and
passes. For each of the fourteen: what set does it filter, and is that set non-empty on a
clean `server-fast` guest? Any check whose answer is "empty" needs the same deliberate
seeding the demotion check now has, or it is a green tick over nothing.

That is for whoever audits `216dd9c5..5b91bc60`; I am not reviewing that diff.

## Standing

No open findings from this review. The gate is now the VM scenario plus the one remaining
host fact about `origin`.
