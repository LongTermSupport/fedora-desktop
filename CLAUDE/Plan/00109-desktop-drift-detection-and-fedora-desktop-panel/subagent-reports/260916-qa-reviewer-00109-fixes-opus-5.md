# QA Review — commit `50849715`, Plan 00109's response to the full-plan-diff review

**Verdict**: FIX-BEFORE-MERGE — 0 blocking, 4 should-fix, 4 minor, 3 nits.

Reviewer: `qa-reviewer` (Opus 5), 2026-09-16. Read-only: no file in the repo was changed
by this review except this report. Every mutation below was applied **in memory** (a Node
ESM `load` hook, and the pre-fix `check_pins.py` exec'd from `git show`), never on disk.

The blocking finding of
[`260916-qa-reviewer-full-plan-diff-opus-5.md`](260916-qa-reviewer-full-plan-diff-opus-5.md)
is genuinely fixed and reproduced in both directions. Two of the other findings are only
partly resolved, and this commit's own new gate comment claims a protection it does not
deliver.

## Blocking

None. No secret, username, hostname, container name or absolute checkout path in the
diff — every hit from a scan of added lines (`/home/`, private IPs, `.local`, email
domains) is the tracked repo directory `files/home/…`.

## Should fix

### 1. `scripts/test-panel-sections.bash:58-69` — the "discovery is broken" guard can never fire, and this commit's new comment asserts that it can

`scripts/test-panel-sections.bash:28-30` (added here) says each suite is checked "rather
than trusted to the runner: `node --test` exits 0 when a named file declares no tests, so
a suite that stopped being found would be a gate reporting a pass for a run that judged
less." The readability loop proves the file *exists*; the count check then reads one
aggregate total. Measured:

```
$ node --test tests/extensions/gjs-loader.mjs          # declares no tests
ℹ tests 1   ℹ pass 1   ℹ fail 0     exit=0
$ node --test tests/extensions/gjs-loader.mjs tests/extensions/gi-stubs.mjs
ℹ tests 2   ℹ pass 2   ℹ fail 0     exit=0
```

A file declaring zero tests scores **one pass**, so `count -eq 0` is unreachable while any
file is named — even if *both* suites emptied out, the gate would print `passed: 2` and
exit 0. If the indicator suite silently stopped declaring tests the gate would print
`passed: 28` instead of `33` and stay green.

**Fix**: assert a per-suite count (run each file separately, or parse the per-file lines)
and print it, e.g. `panel-sections: sections 27, indicator 6`. This is the AgentNotes
shape "coverage implied by the length of a list rather than stated as a number", one level
up.

### 2. Finding 5 is half-resolved — the `document === null` render path still has no test, and `menu.removeAll()` is untested despite the stub being extended for it

Eight mutants of the shipped `extension.js`, run against
`tests/extensions/test-panel-indicator.mjs`:

| mutant | result |
| --- | --- |
| `unavailable` icon → neutral | killed (3 tests) |
| `findings` icon → neutral | killed |
| initial `St.Icon` icon → OK | killed |
| `unavailable` style → `''` | killed |
| drop `GLib.source_remove` | killed |
| drop `indicator.destroy()` | killed |
| **`if (document === null) {` → `if (false) {`** | **SURVIVES** |
| **drop `menu.removeAll()`** | **SURVIVES** |

Also surviving: deleting `this._render(null);` from `enable()`, and rewriting the
`'reading host status…'` string to anything at all.

The test named *"before the first read lands, nothing is known"* reads
`icon.iconNames[0]`, which is set by the `St.Icon` **constructor** — so it proves the
constructor's initial value, not the `_render(null)` wiring that
`test-panel-indicator.mjs:14-16` says it is proving ("a test that sets private fields and
calls a private method proves the mapping and not the wiring"). The prior review's finding
5 named "the `document === null` 'reading host status…' path" as one of the two untested
panel decisions; the icon half landed, this half did not. `gi-stubs.mjs:216-219` is candid
that the Gio callback fires synchronously — but that choice is precisely what makes this
branch unobservable, so closing it needs a stub mode that defers the first read (or a
direct `_render(null)`-then-assert-the-menu test).

`RecordingMenu.removeAll` (`gi-stubs.mjs:34-36`) is correct and inert for the existing
suite — every test in `test-panel-sections.mjs` builds a fresh `new RecordingMenu()` and
calls `build` once, so nothing there depended on items accumulating. But nothing asserts
the rebuild either: drop `menu.removeAll()` from `extension.js:152` and the suite stays
green, while a live shell would stack a second copy of every line on each 300-second poll.
One test that renders twice and asserts the line count is stable would kill it.

### 3. The "three checks" correction was not generalised to the test twin of the docstring it fixed

`helpers/host_health/login_report.py:3` was corrected to four. Its mirror was not:

- `tests/helpers/host_health/test_login_report.py:3` — *"It runs the three checks this
  plan built — post-boot health, play freshness, installed-vs-pinned"*, enumerating three
  of the four and omitting ledger presence: the pre-fix sentence verbatim.
- `tests/helpers/host_health/test_login_report.py:360` — *"The notification flattens three
  checks into one list"*.

This is the AgentNotes "a lesson written down beside the thing it fixed, never
generalised" pattern (`qa-python.bash` / `qa-bash.bash`, Plan 00076). Every other location
the review listed is correct at HEAD — a repo-wide grep leaves only hits that are right
(`statusDocument.js:44`, `status_document.py:65`: *the other three* of four) or historical
measurements in journals and reports.

### 4. The DECISIONS.md extraction left two stale cross-references

The extraction itself is faithful (Decisions 1 and 2 are byte-identical to the deleted
PLAN.md block) and PLAN.md links to it. But two files still send the reader to PLAN.md for
content that is no longer there:

- `CLAUDE/Plan/00109-…/DESIGN-panel.md:319` — "Technical Decision 1 in `PLAN.md`"
- `CLAUDE/Plan/00109-…/DESIGN-play-ledger.md:4` — "`PLAN.md` Decision 2; this file owns
  the detail."

`plan-qa`'s path-existence check cannot see this: PLAN.md exists. Repoint both at
`DECISIONS.md`.

## Minor

### 5. Zero coverage is now counted on the host; *partial* coverage is still silent

The guard fires on `compared == 0`. Measured with a two-pin manifest (one tracked DKMS,
one tracked rpm) against a host with `DkmsRegistry(present=False)`:

```
mixed, rpm clean (1 of 2 compared): state: ok   n=0   ← no coverage line
```

The host compared 1 of 2 and reports as a clean host. Not reachable with today's manifest
(`version-pins: 9 pins, 1 tracked`), and credit where due:
`test_the_real_manifest_on_a_server_reports_its_zero_coverage` asserts every tracked pin is
DKMS-resolved with the message *"a non-DKMS tracked pin would be compared here… this test
asserts the wrong thing"*, so adding one **fails the suite loudly** rather than arriving
silently. That mitigation is why this is minor and not a repeat of the blocking finding —
but `check_pins.check`'s docstring claim that "partial coverage is a decision" is true only
of the `untracked:` split, not of a host-side DKMS skip, and a `COVERAGE: n of m` line
would settle it.

### 6. `store.clear_broken(base, *, at: str = "")` keeps the defect it just fixed representable

`helpers/play_ledger/store.py:101`, `:113`. The sole production caller now passes
`repo.utc_now()`, and `tests/helpers/play_ledger/test_check_freshness.py:387-395` asserts
the marker carries an ISO timestamp — good. But the `at=""` default and its
`f"{at}\n" if at else "\n"` branch have no production caller and are exactly the
undated-marker path that was the finding. Making `at` required makes an undated CLEARED
marker unrepresentable instead of merely unchosen.

### 7. Finding 9 (journal ordering) is not resolved, and now conflicts with the tool's own remediation

`plan-qa --sweep` still reports 00109's `-11.md` (six), `-14.md` (two) and `-15.md` (one),
unchanged. The commit appended `23:59 · correction` entries stating *"append-only means the
fix for a misplaced entry is a note at the bottom, not a move"*. The sweep's remediation
says the opposite for a misplaced entry: *"move the out-of-order entry back to its
chronological slot, keeping its text unchanged"*, reserving a new bottom entry for an entry
whose **timestamp** is wrong. `CLAUDE/PlanJournalling.md:72-78` does support the commit's
reading ("earlier entries are never edited"), so this is a genuine contradiction between a
repo rule and a daemon rule — worth raising where the rule lives rather than settled in a
journal note, because as it stands three files carry a permanent advisory, and a
permanently-firing advisory is the thing this plan exists to stop.

### 8. `extensions/…/statusDocument.js:269` still reasons "two of three sections are fine"

Harmless as an illustration of the icon rule; inconsistent with the same file's `:44`,
which the commit updated around.

## Nits

9. `PLAN.md:327` ticks *"`./scripts/qa-all.bash` passes (929 files, **every gate
   green**)"*. The run prints three `⚠` lines — `shellcheck: 172 issues`, `patterns: 15
   file(s) semgrep parsed only in part`, `deployed-drift: skipped`. Exit 0, all three
   long-standing, and the very next bullet correctly calls the skip "an advisory rather
   than a pass" — but "every gate green" is the overclaim this plan is about. "929 files,
   exit 0, three standing advisories" is the true sentence.
10. `scripts/test-panel-sections.bash` now runs the indicator suite too, and
    `CLAUDE/QA.md:62` still describes the gate as only "the panel's own decisions on
    boot-stale, malformed and `state`-disagreeing documents". The gate label
    `panel-sections:` no longer names what it runs.
11. `extensions/…/sections/health.js:7-11` — the reflow left an orphan line (`them. A
    check` alone on line 9). Cosmetic.

## Checked and clean

- **The blocking fix, both directions, measured.** Real manifest + `registry.present=False`
  + a `dkms_status` rigged to raise: one `unchecked` finding, *"compared 0 of 1 tracked
  pins on this host — every tracked pin is DKMS-resolved and this host has no DKMS
  subsystem"*, section state `unavailable` (was `ok` with zero findings). A host that
  compared its pins still returns `[]` / `ok`. All-pins-untracked still yields the
  declared-count message. Every-probe-raises yields the per-pin lines and no duplicate
  coverage sentence, state `unavailable` — so "compared nothing" is never indistinguishable
  from clean. `pins=[]` is unreachable: `manifest.parse` raises on an empty list. No state
  found where the guard fires wrongly, and `unanswerable_dkms == tracked` cannot attach the
  DKMS clause to a non-DKMS skip (any such pin produces a finding, which suppresses the
  guard).
- **The new tests genuinely falsify.** Loading the pre-fix `check_pins.py` from
  `50849715^` in memory and running the current suite: 51 tests, 2 failures —
  `test_a_host_with_no_dkms_subsystem_does_not_resolve_a_dkms_pin` and
  `test_the_real_manifest_on_a_server_reports_its_zero_coverage`. The control
  (`test_a_host_that_DOES_compare_its_pins_gets_no_coverage_finding`) passes against both,
  which is what a control should do.
- **Every number the commit restates is the number a gate prints.** `grep -c '^test('` →
  27 / 6; contract gate → `9 constant(s)`, `8 document key(s)`, `4 section id(s)`;
  `find playbooks/imports/optional -name '*.yml' -not -path '*/archived/*'` → 46, 47 with
  archived; two optional plays added by 00109 (`play-host-health-login-report.yml`,
  `play-fedora-desktop-panel.yml` — `play-host-health-server-report.yml` was created and
  deleted inside the plan, `play-lxcfreeze.yml` is 00122).
- **"Four" is right everywhere it now says four.** `health.js` `CHECKS` holds exactly the
  four ids, `documentSections` derives from it, and `:7`, `:23`, `:133`, `:196` agree;
  `login_report.py:3`, `docs/playbooks.md:762`, the play header and the bashrc template all
  say four and enumerate four. `DESIGN-server-route.md:9` and the `.j2` dropped the number
  instead, which is better.
- **`check-pinned-versions.bash`'s EXIT trap is the only one in the file** (`grep -n trap`
  → line 94 alone), and it is installed *after* the three early exits (`:56`, `:62`,
  `:69`), so no path leaves an orphan temp file and nothing else relied on EXIT. The split
  matches the reasoning `qa-version-pins.bash:66-78` records.
- **`store.clear_broken`'s new caller is wired** — `repo` is in `check_freshness`'s import
  list, and `helper-tests` went 1558 → 1560.
- **`freshness.py:10`** now says "dozens" rather than a number that goes stale — the right
  shape of fix.
- **Decision 3 is recorded OPEN, not answered** (`DECISIONS.md:32-47`), with options A/B/C
  and an explicit NOT TAKEN — which is what finding 8 asked for.

## Mechanical gates

- `./scripts/qa-all.bash`: **PASS**, exit 0, 929 files. `helper-tests: 1560 tests in 66
  modules (66 tracked), 1 skipped`; `panel-sections: passed: 33`; `panel-contract` green;
  `version-pins: 9 pin(s), 1 tracked, COVERAGE: 9 of 9`. Three standing `⚠`: shellcheck
  (172, repo-wide), semgrep partial parse (15 files, regex rules unaffected),
  `deployed-drift` skipped in the container.
- `hooks-daemon plan-qa --sweep`: **exit 1** — 1 block (`CLAUDE/Plan/README.md` retention
  window, repo-wide, not 00109) and 7 advisories, three of which are 00109's journal
  ordering (finding 7 above, unchanged by this commit).
- `ansible-playbook --syntax-check playbooks/imports/optional/common/play-host-health-login-report.yml`:
  **PASS** (the only playbook in the diff; comment-only change).
- Conditional gates triggered by this diff, all run: `qa-helper-tests.bash` (inside
  qa-all) PASS; `cd extensions && node_modules/.bin/eslint .` exit 0;
  `python3 -m helpers.gnome.check_extension_compat` PASS (5/5, inside qa-all). No
  `files/var/local/claude-yolo/**` path in the diff, so no `CCY_VERSION` / Dockerfile LABEL
  / `REQUIRED_CONTAINER_VERSION` obligation arises; no `extensions/**/metadata.json`
  change.
