# QA Review — Plan 00109, full-plan diff (branch F44)

**Verdict**: BLOCK — 1 blocking, 3 should-fix, 6 minor, 2 nits/observations.

Reviewer: `qa-reviewer` (Opus 5), 2026-09-16. Read-only: nothing in the repo was changed
by this review except this file.

## How the commit set was derived (and what could not be attributed)

- `git log --oneline --grep=00109` on F44 → **85 matching commits** (13 of them
  `merge origin/F44` merges, which contribute no files via `--name-only`). First plan
  commit `2fdce2cd` (2026-09-11), most recent `cce482e2`.
- `git log --grep=00109 --name-only` gave a 130-path file set. Every ambiguous path was
  then attributed with `git log --grep=00109 -- <path>`, and files that matched only
  because a *different* plan's commit mentions 00109 in its body were **excluded**:
  `files/home/.local/bin/lxcfreeze`, `playbooks/imports/optional/common/play-lxcfreeze.yml`,
  `CLAUDE/ContainerEngines.md` (all `9b6c8d55`, Plan 00122); `.ruff-version` (`d45da38b`);
  `requirements.yml`, `run.bash` (`757c79d4` / `7b8df3ef`); the 00112 / 00117 / 00122 /
  00124 / 00125 plan files.
- Commits that are 00109 work but carry a **different subject prefix**, and so are easy to
  miss from a subject-only list: `9a79dd77` and `72c347b3` (`DisplayLink: …`, Task 5.4),
  `bf4ad487` (`Plan 00112: the same defect in the sibling play`), `cedc9426`
  (`Plans 00109 and 00125`). The first two were included as 00109 — Task 5.4 is theirs per
  `PLAN.md:277-292` — and `helpers/displaylink_recovery/` was reviewed accordingly.
- **Could not cleanly attribute by diff**: `scripts/qa-all.bash`, `CLAUDE/QA.md`,
  `docs/playbooks.md`, `vars/vm-test-scenarios.yml` and `ruff.toml` are touched by 00109
  *and* by 00110 / 00121 / 00122 / 00125 inside the same range. For those, the 00109 hunks
  (`git log --grep=00109 -p -- <path>`) and the file's state at HEAD were reviewed rather
  than a range diff. `2fdce2cd^..HEAD` restricted to plan paths is 223 files / +28,155
  lines, but that includes other plans' merged work and was **not** treated as this plan's
  diff.
- Plan-owned deliverables reviewed at HEAD: `callback_plugins/play_ledger.py`,
  `helpers/play_ledger/*` (10 modules), `helpers/host_health/*` (6),
  `helpers/version_pins/*` (3), `helpers/gnome/check_panel_contract.py`,
  `extensions/fedora-desktop@fedora-desktop/*` (5), both new plays, 3 systemd unit
  templates, the `bashrc-includes` template, `vars/version-pins.yml`, the new VM scenario
  plus its 2 guest scripts, `scripts/qa-version-pins.bash`,
  `scripts/test-panel-sections.bash`, `scripts/test-host-health-login-snippet.bash`,
  `tests/extensions/*`, and the four new test suites.

## Blocking

### 1. The installed-vs-pinned axis reports a clean host having compared nothing, on every host with no DKMS state directory

`helpers/version_pins/check_pins.py:213-220` (the zero-coverage guard) and
`helpers/version_pins/check_pins.py:249-251` (the DKMS skip).

The guard counts pins declared `tracked` in the manifest. The skip then `continue`s a
DKMS-resolved pin whenever `registry.present is False`, **after** the count.
`vars/version-pins.yml` declares 9 pins of which exactly **one** is tracked, and it is
`kind: dkms` (`vars/version-pins.yml:106-111`). So on any host without `/var/lib/dkms` —
every server, which is precisely the route Task 3.2 just built, and any desktop that never
ran `play-displaylink.yml` — the check compares **0 of 9** pins, returns no findings, and
`status_document.section([])` yields `state: ok`. The panel renders "nothing to report"
(`extensions/fedora-desktop@fedora-desktop/sections/health.js:100-102`) and
`login_message` says nothing at all.

Measured, not inferred — the real manifest, `registry` present=False, and `dkms_status`
rigged to raise if it were ever called:

```
declared: 9 tracked: 1
tracked kinds: [('evdi_version', 'dkms')]
server-like findings: []
```

Nothing else covers it. `probe_results.build_report` deliberately stays silent for "no
dkms command and no modules" (`helpers/host_health/probe_results.py:213-223`), which is
correct for *its* question, and `scripts/qa-version-pins.bash:169-179` enforces the
coverage floor **in the repo** only — its own comment describes this exact end state:
"the login-time check compares nothing, finds nothing, and this gate still exits 0 — a
drift axis that has quietly stopped existing, which is the whole subject of Plan 00109".
This is also the shape `DESIGN-server-route.md` §5.1 already fixed once for the ledger
scoping ("left the population empty, which skipped the zero-coverage guard entirely"),
recurring through the second selector.

Worse, the behaviour is **enshrined by a test**:
`tests/helpers/version_pins/test_check_pins.py:386-392` asserts
`check(... registry=NO_SUBSYSTEM) == []`.

**Fix**: count pins *actually compared*, not pins tracked. Move the guard after the loop,
increment a `compared` counter where `compare.classify` is reached, and when
`compared == 0 and tracked > 0` append one `probe_results.unchecked(...)` naming the
numbers and the reason ("the installed-vs-pinned check compared 0 of N tracked pins on
this host — every tracked pin is DKMS-resolved and this host has no DKMS subsystem").
That yields `unavailable` rather than `ok` in both consumers — the exact distinction this
plan exists for — at a cost of one line per login, the same trade `fetch_clock` and the
existing zero guard already accept. Update the test above to assert the coverage finding
instead of `[]`.

## Should fix

### 2. "Three checks" is stated in nine places while the producer emits four sections

`login_report` publishes `HEALTH`, `LEDGER`, `FRESHNESS`, `PINS`
(`helpers/host_health/login_report.py:60-63`) and the contract gate prints
`4 section id(s)`. Stale:

- `playbooks/imports/optional/common/play-host-health-login-report.yml:5`
- `docs/playbooks.md:762`
- `helpers/host_health/login_report.py:3`
- `files/home/bashrc-includes/host-health-report.bash.j2:6`
- `extensions/fedora-desktop@fedora-desktop/sections/health.js:7`, `:23` (names only
  HEALTH/FRESHNESS/PINS as the ids "matching" a 4-entry `CHECKS` array) and `:133`
  ("three derived") — which contradicts `:194` ("four checks") in the same file
- `CLAUDE/Plan/00109-…/PLAN.md:228`, `:263`
- `CLAUDE/Plan/00109-…/DESIGN-server-route.md:9`

No gate can catch this: `check_panel_contract` derives the ids and is a vocabulary check
by design. **Fix**: say four, and in `health.js:23` name `login_report.LEDGER` too.

### 3. `helpers/host_health/login_message.py:5-6` documents the opposite of current behaviour

It states that `play-host-health-login-report.yml` "is `scope: gnome` and ends its play
there". The play is `scope: general`
(`playbooks/imports/optional/common/play-host-health-login-report.yml:44`) and carries
**both** deliveries, with a long header at `:13-15` explaining why there is no second
playbook. This docstring is the authority a reader goes to for the server route's
rationale, and it describes the pre-fold world. **Fix**: restate as "the desktop delivery
ends at the notification; this is the server delivery of the same play".

### 4. The plan's own coverage claims are stale — the class of defect this plan is about

- `PLAN.md:248` "17 tests" → `grep -c '^test('` on `tests/extensions/test-panel-sections.mjs`
  gives **27**, and the gate prints `panel-sections: passed: 27`.
- `PLAN.md:261` "7 constants" → the gate prints `PANEL-CONTRACT-OK 9 constant(s) …`.
- `PLAN.md:19` "(46 today, one of them added by this plan)" → this plan added **two**
  optional plays (`play-host-health-login-report.yml` and `play-fedora-desktop-panel.yml`);
  46 is the non-archived count, 47 with `archived/`.
- `helpers/play_ledger/freshness.py:10` "the 43 never-run optional plays" → 46/47 now.

**Fix**: state the numbers the gates print, or drop the numbers and let the gate output be
the claim.

## Minor

### 5. The panel test harness advertises coverage of `extension.js` that it cannot deliver

`tests/extensions/gjs-loader.mjs:33` maps
`resource:///org/gnome/shell/extensions/extension.js` to
`export {Extension} from './gi-stubs.mjs'`, and `gi-stubs.mjs` exports no `Extension`
(and no `PanelMenu.Button`; the `panelMenu.js` entry re-exports nothing,
`gjs-loader.mjs:32`). Ran it:

```
IMPORT FAILED: … 'gi-stubs.mjs' does not provide an export named 'Extension'
```

No test imports `extension.js` (grep across `tests/extensions/*.mjs`), so the
icon-per-state mapping (`extension.js:48-52`, `:139-149`) and the `document === null`
"reading host status…" path are the only panel decisions with no test — while the loader
looks like it covers them. **Fix**: add the two stubs plus one test asserting the icon per
state (including `unavailable` never getting the neutral one, which `PLAN.md:229` claims
✅), or delete the two unused loader entries per YAGNI.

### 6. `store.clear_broken`'s `at` parameter has no production caller, so the CLEARED marker is dateless

`helpers/play_ledger/check_freshness.py:204` calls `store.clear_broken(base)`;
`helpers/play_ledger/store.py:96,109` then writes a bare newline. This is the same
"tested function, no caller" shape the module's own docstring narrates at
`check_freshness.py:180`. **Fix**: pass `repo.utc_now()`, or drop the parameter.

### 7. The genesis record's timestamp is written and never read

`helpers/play_ledger/store.py:27-30` justifies it as "what lets a reader tell 'no record
because never run' from 'no record because the ledger is younger than the run'".
`helpers/play_ledger/ledger.py:211` only skips genesis rows; no consumer (`freshness`,
`ledger_presence`, `check_pins`, `login_report`) reads `at`. No reader dates its silences
today. **Fix**: wire it into the reporting, or soften the comment to what it delivers.

### 8. Nothing detects that the detector was never deployed, and the plan records no decision about it

Both new plays are opt-in
(`playbooks/imports/optional/common/play-host-health-login-report.yml:36-38`),
play-freshness is silent about plays never run here
(`helpers/play_ledger/freshness.py:9-11`), and `ledger_presence` fires only on an *empty*
ledger (`helpers/play_ledger/ledger_presence.py:69-77`). A host that ran
`playbook-main.yml` but never ran this play therefore has a populated ledger, a clean
freshness verdict, and no health surface at all — the plan's own premise ("plays under
`optional/` are run by hand, once, and then forgotten", `PLAN.md:17-22`) applied to the
detector. That may be the right answer; it is not written down anywhere in `PLAN.md` or
the DESIGN files.

### 9. Journal chronology

`hooks-daemon plan-qa --sweep` reports out-of-order entries in
`JOURNAL/00109-Journal-26-09-11.md` (six), `-14.md` (two) and `-15.md` (one). Per the
sweep's remediation, corrections go in as new bottom entries, not by rewriting.

## Nits and observations

### 10. NIT — `scripts/check-pinned-versions.bash` keeps the pipeline shape its sibling gate documents fixing

The manifest is converted and validated in one pipeline;
`scripts/qa-version-pins.bash:66-78` explains why it was split ("a YAML failure went to
the terminal while the validator, handed empty stdin, reported a JSON decode error — so
the gate's diagnosis named a problem the manifest did not have"). `set -euo pipefail`
plus the YAML traceback reaching the terminal means this fails loudly rather than falsely,
so the cost is a misleading first line, not a false pass.

### 11. Observation, outside the 00109 diff (recorded so it is not lost)

`CLAUDE/QA.md:19,43` say "thirty-six" gates / "twenty-nine further";
`helpers.docs.link_check.qa_gates` parses **37** from `qa-all.bash` (7 merged + 30
separate). The row inventory is derived and green both ways; only the prose numbers are
hand-maintained. Traced to `2b84ff8f` and `ced20d08` (Plan 00125) — every 00109 gate
addition did bump the count.

## Checked and clean

- **IaC placement.** `play-fedora-desktop-panel.yml` earns its own play: `scope: gnome`
  against the report's `scope: general`, no import in `playbook-main.yml`, independent
  opt-in, and it must not deploy on the server profile the report does serve. The report
  play correctly folds both deliveries into one play with a profile branch and mirrored
  cleanup of the other delivery's unit *and* its `.wants/` symlink (`:162-172`,
  `:253-261`) — no second playbook. `play-host-health-server-report.yml` was created and
  deleted inside the plan, so no orphan remains.
- **Declared dependencies, not runtime probing.** `python3-pyyaml` for the system
  interpreter is installed by the play that needs it (`:86-89`); `libnotify` is owned by
  `playbooks/imports/play-gnome-shell.yml:29-34` with the reasoning recorded; the uid is
  resolved with `getent` and asserted before indexing rather than defaulted (`:103-127`).
- **Fail-fast.** No `failed_when: false` or `ignore_errors:` added anywhere in the plan's
  plays. Every broad `except` in `helpers/host_health` / `helpers/version_pins` converts
  to a *reported* `unchecked` finding on a login path, never to silence; `store.py` raises
  on every failure and the callback records a `BROKEN` sentinel because Ansible swallows
  callback exceptions. The three `2>/dev/null` in
  `scripts/test-host-health-login-snippet.bash:141,144,215` discard the harness's own
  interactive-bash noise while stdout is the assertion subject, and the traceback case is
  separately asserted **on stderr** at `:184-191`.
- **Verification that exercises the production path.**
  `tests/helpers/play_ledger/test_source_position_against_real_ansible.py:76-88` *refuses*
  (no skip) when `ansible-playbook` is absent and drives the real `Play.load` plus the
  `copy()` a callback is handed. The panel suite imports the shipped `statusDocument.js` /
  `sections/health.js` through a loader that **throws** on an unknown `gi://` specifier
  (`gjs-loader.mjs:57-61`). `scripts/test-panel-sections.bash:49-60` reads the pass count
  from the summary and fails when it is zero or unparseable. `qa-version-pins.bash`
  carries four falsifying controls and prints `COVERAGE: 9 of 9`.
  `qa-vmtest-manifest.bash:121-131` mutates a `planned` value to prove the comparison
  judges, and the guest checker asserts `total -ne PLANNED`
  (`guest-acceptance-server-host-health-kernel-change.bash:449-450`) against
  `planned: 15` in `vars/vm-test-scenarios.yml:153`.
- **Cross-language contract.** Gate run: 9 constants agree, 8 document keys present, 4
  section ids present. `FILE_NAME`, `STATE_DIR_NAME`, `SCHEMA_VERSION`, `SELF_SECTION`,
  `BOOT_SCOPED_SECTION`, `HANDOFF_COMMAND` and the three states are single-sourced in
  Python; both consumers **derive** `state` from the lists rather than reading the stored
  one, and `resolvedSection` is the single place the boot demotion happens.
- **Ledger ownership across processes.** `run.bash` refuses to run as root and invokes
  `ansible-playbook` as the target user, so the callback writes
  `$XDG_STATE_HOME/fedora-desktop/play-ledger` for the same user whose `systemd --user`
  units read it; `ledger.state_dir` rejects a relative `XDG_STATE_HOME` and the panel
  applies the identical XDG rule via `GLib.get_user_state_dir()`.
- **Public-repo safety.** Scanned the plan folder and every new file for `/home/<user>`,
  personal email domains, `.local`/private-IP patterns and hostnames: the only hit is the
  repo-relative path `files/home/bashrc-includes/...`. The one hardware mention
  (`subagent-reports/260911-gnome-black-background-bugs-research.md:376`) matches
  long-standing repo convention (`play-ipu6-webcam.yml`,
  `play-laptop-thermal-diagnostics.yml`, Plan 00044).
- **Version bumps.** No `files/var/local/claude-yolo/**` path appears in the 00109 commit
  set, so no `CCY_VERSION` / Dockerfile LABEL / `REQUIRED_CONTAINER_VERSION` obligation
  arises. The new `extensions/fedora-desktop@fedora-desktop/metadata.json` declares
  shell-version 45-50 and the compat gate passes for all 5 extensions.
- **Plan state.** Worktree clean; index row present at `CLAUDE/Plan/README.md:63`;
  `Status: In Progress` is consistent with the open HOST/VM items, and no host-only claim
  is marked ✅ beyond Task 0.1, whose evidence is the journal (the user's own action, not
  verifiable here).

## Not verifiable here (HOST or VM lab only)

`host-health.service` actually being *wanted* by `graphical-session.target`; a real
`notify-send` arriving and a clean login being silent; the ledger's real rows from a real
run; St rendering of the demoted lines and whether the icon reads correctly; and the
end-to-end `server-host-health-kernel-change` scenario. All five are open items in
`PLAN.md`, and nothing in the repo claims them done — checked.

## Mechanical gates

- `./scripts/qa-all.bash`: **PASS** — 928 files;
  `helper-tests: Ran 1558 tests in 66 modules (66 tracked), 1 skipped` (the skip is not in
  this plan's suites — no `skipTest` / `@unittest.skip` in
  `tests/helpers/{host_health,play_ledger,version_pins,gnome}`);
  `panel-sections: passed: 27`; `host-health-login-snippet: passed: 12`; `panel-contract`
  green.
- `hooks-daemon plan-qa --sweep`: **exit 1** — 1 block (`CLAUDE/Plan/README.md` retention
  window, repo-wide, not 00109) and 7 advisories, 3 of which are 00109's journal ordering
  (finding 9).
- `ansible-playbook --syntax-check`: **PASS** on `play-host-health-login-report.yml`,
  `play-fedora-desktop-panel.yml`, `play-vm-test-lab.yml`, `play-basic-configs.yml`,
  `play-gnome-shell.yml`, `play-displaylink.yml`, `playbook-main.yml`. (In this container
  Ansible refuses non-blocking stdio, so each run had to be piped through `cat`; that is an
  environment property, not a defect in the plays.)
- Conditional gates, all triggered by this diff and all run: `qa-helper-tests.bash` (inside
  qa-all) PASS; `python3 -m helpers.gnome.check_extension_compat` PASS (5/5);
  `cd extensions && node_modules/.bin/eslint .` PASS (exit 0).
