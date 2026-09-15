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
