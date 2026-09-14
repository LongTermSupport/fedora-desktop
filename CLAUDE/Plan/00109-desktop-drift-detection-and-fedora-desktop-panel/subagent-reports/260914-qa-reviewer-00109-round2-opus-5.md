# QA Review — Plan 00109, `fd74e09a..HEAD` (round 2)

**Reviewer**: `qa-reviewer` (Opus 5) · **Date**: 2026-09-14 · **Verdict**: **BLOCK**

**Tree QA state at time of writing**: `qa-all.bash` **PASS, 858 files** (my own run,
`untracked/scratch/qa-all-round2.log`), working tree **clean**. Every blocking finding
below passes that green run.

Scope: `fd74e09a..HEAD` excluding `640a11ba`, `d34d259e`, `7df3a846`, `d3df41e9`
(Plan 00119 `run.bash`), as instructed.

---

## Blocking

### B1. The yq assert breaks `playbook-main.yml --check` repo-wide, and tells the operator to delete a working binary

`playbooks/imports/play-basic-configs.yml:335-376`

**CONFIRMED**, three proofs.

1. `ansible/modules/command.py:339-346` — under check mode with no `creates`/`removes`,
   the module sets `skipped=True`, `rc=0`, and `stdout` keeps its line-281 initial value
   `''` (then `:355-357` re-strips it). The probe at `:337` has no `creates`/`removes`
   and no `check_mode: false`, so under `--check` it never runs and registers `stdout=''`.
2. Templated with that exact registered result:

   ```
   yq_major under --check = ''
   int(yq_major) >= 4     = False
   fail_msg renders       = 'yq reports: '
   ```

3. `playbooks/playbook-main.yml:7` imports `play-basic-configs.yml`.

So `./playbooks/playbook-main.yml --check` — documented at `docs/development.md:274`,
`docs/README.md:216`, `docs/configuration.md:301`, and `CLAUDE/InfrastructureAsCode.md:79`
("Verify with `--check`") — now fails at this assert on **every** host, including one
running yq v4.53.3. Every plan `deploy.bash --check` that reaches this play does too
(`CLAUDE/Plan/_planlib.inc.bash:161` threads `--check` into every ansible invocation).

The `fail_msg` it prints is the harm: `yq reports:` with nothing after it (the
`| default('nothing')` at `:361` is dead — `stdout` is defined-and-empty, and `default`
without `true` only substitutes for *undefined*), followed by `sudo rm /usr/bin/yq`. An
operator who follows it deletes a working yq off the back of a probe that never ran. That
is the repo's own written defect — *"'could not run: ' with nothing after it tells the
user precisely nothing"* — wearing destructive advice.

**The repo already holds both fixes, in files edited this same session.**
`play-suspend-and-lid-policy.yml:188-191` — *"`gsettings get` reads and changes nothing,
so it must run under `--check` too … Without this the probe is skipped in check mode"* —
`check_mode: false`. And `play-gnome-shell-extensions.yml:171-173` — *"the command module
has no check mode, so it never runs … Without this guard a `--check` preview dies."*

**Fix**: `check_mode: false` on the probe (it is read-only), and
`| default('nothing', true)` so an empty answer still reads as one.

### B2. `... is defined` cannot fail once `fail_key: false` is set, and the `fail_msg` is unreachable — two sites

`playbooks/imports/optional/common/play-host-health-login-report.yml:99-114` ·
`playbooks/imports/optional/common/play-container-watch.yml:30-40`

**CONFIRMED** from the module source and the templating engine.

`ansible/modules/getent.py:186-188`:

```python
elif rc == 2:
    msg = "One or more supplied key could not be found in the database."
    if not fail_key:
        results[dbtree][key] = None
        module.exit_json(ansible_facts=results, msg=msg)
```

With `fail_key: false` and a missing user, the key is present with the value `None`.
Measured:

```
"getent_passwd['joe'] is defined"     -> True
"getent_passwd['joe'] is none"        -> True
getent_passwd['joe'][1] | int > 0     -> RAISES UndefinedError None has no element 1
```

So clause 1 is True in the exact case it was added to catch, and clause 2 raises.
`ansible/plugins/action/assert.py:81` calls
`self._templar.evaluate_conditional(conditional=that)` with no `try`, so the task dies
with a conditional-check error and `fail_msg` (`:87`) is never reached.

The comment at `play-host-health-login-report.yml:99-103` states the inverse —
*"`fail_key` defaults to true, which … makes the `is defined` clause of the assert below
unreachable — a check that cannot fail … Handing the verdict to the assert instead is what
makes its `fail_msg` the report the operator actually gets."* The change moved the defect
rather than removing it: the clause still cannot fail, and the operator now gets
`None has no element 1` instead of the three-sentence message written for them.

Same shape as B1 (a guard that raises instead of asserting), and the same shape the plan
already fixed once in the yq assert's second draft.

**Fix**: `- ansible_facts['getent_passwd'][user_login] is not none` as the first clause,
both files.

---

## Should fix

### S1. The panel contract gate covers 6 of at least 17 names that must agree, and the three whose rename is *documented* as silent are outside it

`helpers/gnome/check_panel_contract.py:44-51`

**CONFIRMED.** The gate *can* fail — I mutated it four ways in memory: a changed JS value,
a deleted JS constant, and a changed Python value all produce findings naming both sides;
only an emptied expected set passes, and `test_check_panel_contract.py:95-98` guards that.
Good.

The population is the problem. `expected()` is a hand-written list of six. Measured against
what the two halves actually share:

```
Gate's population (6) : FILE_NAME, FINDINGS, OK, SCHEMA_VERSION, SELF_SECTION, UNAVAILABLE
NOT in it (11)        : schema, generated_at, kernel, sections   (all four top-level JSON keys)
                        state, findings, unchecked               (all three per-section keys)
                        post-boot-health, play-freshness, installed-vs-pinned
                        'fedora-desktop' (the state sub-directory)
```

Proof that the section ids are live interface and unguarded:

```
Python section ids : ['post-boot-health', 'play-freshness', 'installed-vs-pinned']   (login_report.py:57-59)
JS section ids     : ['post-boot-health', 'play-freshness', 'installed-vs-pinned']   (sections/health.js:24-28)
gate findings after renaming the Python side: []  -> exit 0, "PANEL-CONTRACT-OK 6 constant(s) agree"
```

`sections/health.js:21-23` says of exactly these: *"These are the document's keys, so they
are interface: rename one here and the section silently reports unavailable for ever."*
That is the gate's own stated threat model, and it is the part the gate does not cover.
Renaming `unchecked` is worse: `health.js:56` would see `length === 0`, skip the caveat
block, and render a section in the `unavailable` state with no findings and no explanation
— the "neutral icon over an empty menu" failure the whole file exists to prevent.

This is AgentNotes row 9b: *"Replacing a stale enumeration with a fresher enumeration is
not the fix; deriving the set is."*

**Credit where due**: the pass line names its six constants rather than printing a bare
total, so the gap is visible in ordinary passing output.

**Fix**: pull the section ids and the state sub-directory into `expected()`; for the JSON
keys, build a real document with `status_document.build()` and assert every key the JS
reads (`document\.\w+` / `section\.\w+`) appears in it.

### S2. The panel is handed the reason the document could not be read and never shows it

`extensions/fedora-desktop@fedora-desktop/statusDocument.js:53-63` vs
`sections/health.js:24-28,99`

**CONFIRMED.** On a host where the producer play has not been run — which
`play-fedora-desktop-panel.yml:16-21` explicitly says is "not an error here" — the document
is `_cannot_read`, which puts the reason under `sections['status']` (`SELF_SECTION`).
Nothing in the extension reads `SELF_SECTION`: it is declared at `statusDocument.js:38`,
written at `:60`, and never looked up. `health.js:99` registers only the three check ids.

Measured, same document, both consumers:

```
SERVER route (login_message):
  Not checked — these are NOT clean results, nothing is known about them:
  - no host status has been recorded yet, so nothing is known about this host

PANEL route: three copies of "the host status document has no <id> section"
  (the real reason is in sections['status'] and is never read)
```

`statusDocument.js:53-55` justifies the shape as *"the same shape, so every consumer
renders it through the path it already has rather than needing an absence branch"* — the
only consumer's path does not reach it. The server route gets this right; the panel, which
is the primary surface, does not.

**Fix**: have `health.js` (or `extension.js`) render
`sectionOf(document, StatusDocument.SELF_SECTION)` when it is not `OK`.

### S3. `login_message.main()` raises out to the login shell it promises never to raise in

`helpers/host_health/login_message.py:63` (the `except ValueError`)

**CONFIRMED** with a concrete input — a schema-1 document whose `generated_at` carries no
timezone designator:

```
$ login_message.main(["--state-dir", d], stdout=StringIO())
RAISED OUT OF main(): TypeError - can't subtract offset-naive and offset-aware datetimes
```

`_age_days` catches `ValueError` around `fromisoformat`, but the naive/aware subtraction at
`:66` raises `TypeError`, which escapes `_age_days`, `render`, `read_and_render` and
`main`. The module docstring at `:25-27` is absolute about the contract: *"**Nothing here
raises.** It is called from a login shell, so a traceback costs the user their prompt …
Every branch ends in a string."* The delivery is not wired yet (PLAN.md Task 3.2 unticked),
so this is not live — but the code and the claim are both committed.

**Fix**: `except (TypeError, ValueError)`, or normalise both stamps to aware before
subtracting.

Related, same function: a **future** stamp reads as fresh. `(now - then).days` floors, so
a document stamped six days ahead gives `-6`, `-6 >= 14` is False, and nothing is said —
while `fetch_clock.py:92` treats precisely that condition on the sibling clock as a finding
(*"last reached the remote in the future"*). `render`'s own rule is that an unknown age is
not a fresh one; an *impossible* age is not either.

### S4. Pin coverage: the zero case is guarded, the partial case is silent on the surface a human reads

`helpers/version_pins/check_pins.py:404-422`

**CONFIRMED** against the live manifest:

```
declared pins : 9
tracked pins  : 1  ['evdi_version']
findings      : []
status document section: {'state': 'ok', 'findings': [], 'unchecked': []}
```

So the panel renders "Installed versus pinned — nothing to report" for a check that
compared one pin of nine, and the server route prints nothing. The new zero-coverage guard
is real and correct; the docstring beside it reads *"Partial coverage is a decision; zero
coverage is a check that cannot fail, and it says so with the number"* — and `PLAN.md:134`
repeats it. That is the AgentNotes tell, verbatim: *"Whenever you write 'refuse to report
an empty result, it would read as nothing found', immediately ask what a partial result
would read as. Usually: the same thing."* Round 1's M8 raised both halves; only the zero
half landed.

`qa-version-pins.bash` does now print `COVERAGE: 9 of 9 resolve to a live playbook var` —
but that is a different population (rows resolving to a live playbook var), not pins whose
installed version was compared.

**Fix**: emit `COVERAGE: n of m pins compared` as an `unchecked` finding whenever
`tracked < len(pins)`, so it reaches both consumers.

### S5. `play-fedora-desktop-panel.yml`'s stated justification for being a new play is false today

`playbooks/imports/optional/common/play-fedora-desktop-panel.yml:10-14` ·
`DESIGN-panel.md:128-132`

**CONFIRMED.** Both say: *"the login report needs only `notify-send` and is meaningful on a
server profile, while the panel needs GNOME Shell."* Measured, the two plays are identical
on every lifecycle axis the rubric names:

```
play-host-health-login-report.yml   hosts=desktop become=true scope=gnome  imported-by-main=0
play-fedora-desktop-panel.yml       hosts=desktop become=true scope=gnome  imported-by-main=0
```

The report play is `scope: gnome` (`:39`) and `meta: end_play`s on a server (`:54-56`), so
it cannot run on a server at all — which is the gap `PLAN.md:153-155` records as open (*"A
server profile gets no drift detection … Needs a second route, not a scope change"*). It
also needs more than `notify-send`: it installs `python3-pyyaml` (`:67-70`) and its unit is
`WantedBy=graphical-session.target`. The justification describes a future state in the
present tense.

The repo's own precedent runs the other way: `play-container-watch.yml` deploys a reporting
backend, its GNOME extension AND its timer in one play — the established shape for
exactly this pairing.

The play is nonetheless defensible, on an argument neither document makes: the panel is a
**generic multi-section surface** (Task 4.3's play runner, Task 4.4's registry), not the
health report's UI, so it has a lifecycle of its own that does not depend on the server
route ever existing. Say that instead.

### S6. `play-fedora-desktop-panel.yml` is the only one of 46 optional plays absent from `docs/playbooks.md`

**CONFIRMED**:

```
optional plays: 46 ;  plays named in docs/playbooks.md: 75
optional plays NOT named: play-fedora-desktop-panel.yml
```

Its sibling from the same plan got an entry (`docs/playbooks.md:756`,
`play-host-health-login-report.yml`). 45 of 46 is a 100% convention everywhere else.
`hooks-daemon docs-qa --sweep` reports 37 findings, zero blocking, none in this range — it
has no "every optional play is catalogued" check, so this gap is unmechanised.

### S7. `PLAN.md:27-28` says 45 where it is 46

**CONFIRMED**: `find playbooks/imports/optional -name 'play-*.yml' | wc -l` gives 46 at
HEAD, 45 at `fd74e09a`, 45 at `55b8ef4d` (plan start), and `git log --diff-filter=A` shows
this plan added exactly one (`play-fedora-desktop-panel.yml`). So "(45 today, and this plan
added one of them)" is off by one — and would read as 44-before if taken literally.

### S8. The new runtime-dir QA rule misses the literal form of the defect it was written for

`scripts/qa-ansible.bash:469`

Both new rules can fail — I drove the exact patterns:

```
FLAGGED   XDG_RUNTIME_DIR: "/run/user/{{ user_login_uid | default(1000) }}"
exempt    XDG_RUNTIME_DIR: "/run/user/{{ ansible_facts['getent_passwd'][user_login][1] }}"
exempt    DBUS_SESSION_BUS_ADDRESS: "unix:path=/run/user/{{ ansible_facts.getent_passwd[...] }}/bus"
exempt    XDG_RUNTIME_DIR: "/run/user/1000"          <-- the gap
```

`RUNTIMEDIR_PATTERN` requires `{{` immediately after `/run/user/`, so a bare hardcoded uid
— the worst version of "a guessed uid points at the wrong runtime directory without
failing" — sails through. No such literal exists in `playbooks/` today, so this is a
coverage gap rather than a live defect.

The un-prefixed-fact rule is correct: all three `ansible_facts[...]` spellings are exempt
(including the double-quoted one), only the bare form is flagged. And the `default(1000)`
instance is genuinely gone repo-wide — the only surviving hits are three comments *about* it.

---

## Nits

- **Eight journal entries have no `HH:MM · CATEGORY · REF` heading.**
  `JOURNAL/00109-Journal-26-09-14.md:808,857,914,955,968,1011,1046,1081` — every entry
  before `:808` has one. `CLAUDE/PlanJournalling.md:57-67` makes the grammar convention
  (advise-only), but the loss is real: times no longer run monotonically down the file, so a
  resumer cannot place the last eight entries against the first twenty-one.
- **`--no-handoff` suppresses two writes and names one.** `login_report.py:302-305`
  documents both honestly; `triage.bash:15-17` mentions only the handoff file, not the
  status document it also skips. A flag named for one of its two effects.
- **`GIT_ASKPASS=""` does not do what the surrounding comment implies.**
  `git_history.py:326` — git's prompt path tests `askpass && *askpass`, so an empty value
  falls through to `core.askpass`/`SSH_ASKPASS`. `GIT_TERMINAL_PROMPT=0` plus the 20s
  timeout carry the behaviour; the docstring only claims the former, so this is an
  undocumented no-op rather than a defect.
- **JS `ageDays` reads a naive stamp as local time.** `statusDocument.js:160-170` —
  `Date.parse("2026-09-14T00:00:00")` (no `Z`) is local per ES2016, so the panel would show
  an age up to a day wrong rather than null. The Python half raises on the same input (S3).
  Different failure, same missing case.
- **`UNEXPLAINED` is classified as `broken`.** `freshness.py:29-30` calls it *"the bytes
  differ and no commit explains it. Reported, never guessed at"*, and
  `check_freshness._emit:156` writes it to `stdout`, which
  `login_report.freshness_findings:283` wraps in `broken()`. Defensible — the difference
  *is* measured, only the reason is unknown — but it is the one verdict where `checked` is a
  judgement call, and nothing says so.
- **`user_extensions_dir` is now spelled out in 6 plays.**
  `play-fedora-desktop-panel.yml:39` joins `play-gnome-shell-extensions.yml:10`,
  `play-container-watch.yml:21`, `play-speech-to-text.yml:18`,
  `play-remote-desktop-toggle.yml:25`. Prevailing practice, not a new defect — no shared var
  exists — but the count is now worth a `vars/` entry.
- **`XDG_STATE_HOME` divergence, PLAUSIBLE only.** `ledger.state_dir:60-68` strips the value
  and raises on a relative path; `GLib.get_user_state_dir()` (per its source,
  `if (dir && dir[0]) use it`) neither strips nor validates absoluteness. So
  `XDG_STATE_HOME=" /tmp/x "` or a relative value would put producer and panel in different
  places, or crash one and not the other. **Not reproduced** — no `gi`, no `gjs` and no glib
  headers in this container. Both agree in the two cases that matter (unset, and set to a
  clean absolute path), which I did verify on the Python side.
- **`%h/Projects/fedora-desktop`** appears at `JOURNAL/…26-09-14.md:293`, described as a
  rejected guessed path. `fedora-desktop` is the permitted self-reference and the path is
  explicitly hypothetical, so not a leak — noting it only because a reader could mistake it
  for the real checkout location.

---

## Checked and clean

- **PLAN.md's 301-line trim lost nothing load-bearing.** Task-identifier sets are identical
  before and after (22 ids including `T5.4a`); the section-heading list is identical (18
  headings); every section reference resolves (`DESIGN-play-ledger.md` 1-4 and 6-7,
  `DESIGN-host-health.md` 8 and 1-11, `DESIGN-panel.md` 1-10 and 9, `RESEARCH-…` Recovery
  all exist). No unchecked item was dropped: Task 5.4's *"still to confirm in the wild"* and
  *"HOST: deploy and verify"* both survive in the collapsed HOST line; Task 5.1's blocked
  blue-grey question is replaced by its resolution, not deleted. The stale test counts round
  1 flagged are gone, and the `SuccessExitStatus=0 1` narrative that had outlived its fix is
  gone with them — which is the AgentNotes lesson this trim was for.
- **Round 1's B2 is genuinely fixed, over the whole population.** I drove every reachable
  producer path — `probe_results.build_report` (all branches),
  `login_report.freshness_findings` through its real entry point for all four statuses plus
  all four `fetch_clock` wordings, `check_pins.check` (resolution failure / mismatch / zero
  coverage), and `status_document.collect`'s guard — and ran `handoff._split` over the
  result: nineteen findings, 5 under "What is wrong", 14 under "Not checked", every one
  correct. The six that round 1 proved misfiled are all in the right bucket now. The split
  is read from `Finding.checked`, never from prose.
- **Merged-not-chained genuinely has one implementation.** `status_document.collect:125-133`
  is the only guard loop; `login_report.collect_sections:95` calls it;
  `login_report.collect:115-116` derives from `collect_sections`. The notification and the
  document cannot disagree about which checks ran.
- **Round 1's other findings, verified fixed**: B1 (`SuccessExitStatus=3` at
  `host-health.service.j2:33`, with `TimeoutStartSec=120` and a corrected oneshot comment;
  `docs/playbooks.md:768-775` updated and now accurate). B3 (all three `getent_passwd` uses
  now `ansible_facts[...]`, plus a QA rule that can catch the next one — S8).
  M4/`declared_pins` (one subprocess, through `_run`, with stdout folded into the message for
  `rpm -q`'s stdout-on-failure case). M5 (`python3-pyyaml` declared, `:67-70`). M6
  (`libnotify` moved to `play-gnome-shell.yml:81`, which owns the desktop). M7
  (`triage.bash:15-20` now states the fetch and the clock stamp, and passes `--no-handoff`).
  M9 (`extra` gone from all three documents and both docstrings). M10
  (`git_history.py:302-309` corrected, cites `src/core/service.c`, and no longer rests on
  the default).
- **Public-repo safety**: swept all 5,469 added lines (excluding the out-of-scope `run.bash`
  work). No `/home/<name>` path, no non-`example.com` email, no private IP, no hostname, no
  container/project/network name, no account ID. The one email-shaped hit,
  `Vitals@CoreCoding.com` (`DESIGN-panel.md:144`), is a real public extension UUID and is
  allowlisted at `vars/gnome-shell-extensions.yml:35`, so section 7's claim about the
  scanner is accurate. The `~/.local/state/...` paths are XDG-relative, not install-specific.
- **British English**: clean. Every hit on an American-spelling sweep is a CSS property
  (`color`), a GNOME icon name (`dialog-*-symbolic`), or a correct British form
  (`catalogue`, `judgement`, `characterisations`, `practice` as a noun).
- **CCY version bump**: not required — no commit in range touches
  `files/var/local/claude-yolo/`.
- **Playbook mechanics**: the new play's shebang is byte-identical to
  `scripts/make-playbooks-executable.bash:18`'s `SHEBANG`; mode `100755` in the tree; scope
  guard plus `provisioning_profile` assert present.
- **`apply_enabled_extensions` is not a fail-fast hole.** `_fail` returns 1 (`:66-69`), so
  the `command:` task fails on every failure path — the panel play's extra assert
  (`:104-117`) is belt-and-braces, and container-watch's lack of one is not a gap.
- **Container-watch's removed suppressions leave no hole.** The `is-system-running` guard
  was answering the wrong question (`streq(state,"running")`, so `degraded` exits 1) and
  skipped the enable on the unhealthy host; the `debug` deferral asserted a `WantedBy=`
  pickup that cannot happen without `enable`. Both correctly deleted, and
  `play-systemd-user-tweaks.yml` (import 8, `scope: general`) does start `user@UID.service`
  blocking on READY=1, so on any playbook-main host the manager is reachable. The extension
  dance it replaced could not work on Wayland at all. The only residue is B2.
- **`qa-version-pins.bash`'s four controls are real**, `check_row` fixes the
  `$?`-inside-`if !` trap the right way (status captured, message asserted, not just the exit
  code), and the `rows != declared` comparison closes the partial case its own comment names.
  The `yaml_to_json` conversion is now a separate step with stderr to a file, not merged into
  the payload.
- **`status_document.write_atomic`'s atomicity test is a real test.**
  `test_a_write_that_fails_partway_leaves_the_last_good_one_intact`
  (`test_status_document.py:157`) forces a mid-write raise and asserts the previous document
  still reads — which a plain truncating write fails.

---

## Mechanical gates

| Gate                                   | Result                                                                                                           |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `scripts/qa-all.bash`                  | PASS — 858 files, 1212 helper tests, `panel-contract: 6 constant(s) agree`, `version-pins: COVERAGE: 9 of 9`     |
| `hooks-daemon plan-qa --sweep`         | 2 findings, zero blocking — both pre-existing and unrelated (Plan 00046 path, journal-quiet plans)               |
| `hooks-daemon docs-qa --sweep`         | 37 findings, zero blocking — all pre-existing, none in this range                                                |
| `ansible-playbook --syntax-check`      | PASS on all five changed playbooks and on `playbook-main.yml`                                                    |
| `qa-helper-tests.bash`                 | Triggered (helpers + tests changed) — green inside `qa-all`, 1212 tests                                          |
| `helpers.gnome.check_extension_compat` | Triggered (new `metadata.json`) — PASS, all five extensions cover GNOME Shell 50 on Fedora 44                    |
| `extensions` ESLint                    | Triggered (five new JS files) — PASS, clean                                                                      |

`--syntax-check` passing is the point about B1: it parses structure and never evaluates
`--check`-mode task results, so neither it nor `qa-all.bash` can see that assert fail.

## Disclosure

Read-only throughout. No repository file under review was modified. All mutation tests (the
panel contract, the two new `qa-ansible` rules, the yq regex, the producer/split sweep) were
run in memory against strings, never by editing a tracked file. Four scratch files were
written under `untracked/scratch/` (diffs and the QA log) for `awk`-ing, per the container
constraints. No playbook was executed, including under `--check`.

This report file was written with a Bash heredoc because this role has no `Write`/`Edit`
tool and the `subagent_report_size_blocker` hook requires the report on disk.
