# Plan 00109 — Completed Task Detail

Durable evidence, measured facts and reasoning for tasks marked ✅ in
[PLAN.md](PLAN.md), moved here per the plan-qa remedy so PLAN.md stays lean. Each
heading corresponds to one task there; PLAN.md keeps the task as a one-line title
plus a link into the matching section below. This document is completed-task
evidence, not a live task list — do not add open items here.

## Task 1.2: Write the ledger on every play run

`callback_plugins/play_ledger.py`.

- **It had never worked on ansible-core 2.19** (issue #46). 2.19 removed
  `ansible_pos` and moved the play's source position into an `Origin` tag on the play
  itself, so every play became a recorded hole and the ledger marked itself `BROKEN` on
  every run. Both shapes are read now, new first, and the choice is tested
- **`store.clear_broken` had no caller anywhere** — a sentinel, once written, left
  the ledger permanently untrustworthy with no route back. `check_freshness --clear-broken` is that route, and it says the missing rows are not recovered
- **`qa-reviewer` over the commit** — 7 should-fixes, all acted on. Report:
  [subagent-reports/260915-qa-reviewer-00109-ledger-opus-5.md](subagent-reports/260915-qa-reviewer-00109-ledger-opus-5.md).
  The one that mattered: clearing the sentinel flipped `plays_run_here` from `None` to
  a **partial** set, silently suppressing every ABSENT pin verdict whose row was in the
  hole — the precise suppression that function's own docstring calls unacceptable. A
  `CLEARED` marker now outlives the sentinel, because the missing rows never come back
- **A gate now catches the next Ansible rename** —
  `tests/helpers/play_ledger/test_source_position_against_real_ansible.py` loads a real
  playbook through the real `Play.load` under the interpreter `ansible-playbook` itself
  runs, and asserts the production helper gets the file back for **both** the parsed
  play and the `copy()` a callback is actually handed. A fake origin cannot catch a
  rename in the thing it is faking, which is why the whole suite stayed green while the
  ledger recorded nothing for its entire life. Falsified against the pre-fix behaviour
- **HOST**: verified against a real run — acceptance `20260917-143702`, checks
  [6]–\[8\]: genesis present, 18 plays folded, no hole, a row per play, repeat runs append,
  and a non-applying run leaves the ledger byte-identical (`--list-tasks`, not `--check`;
  `acceptance.bash` §8 says why). No guest checker reads the ledger
  ([DESIGN-host-health.md](DESIGN-host-health.md) §12) — that gap is unchanged

## Task 3.1: Post-boot health probe

Wired and running on the HOST.

- `probe_results.py` (verdicts) and `probe.py` (the half that touches the
  machine); `host-health.service`, deployed by `play-host-health-login-report.yml`
- **HOST**: the unit is actually *wanted*, not merely enabled — check [11],
  `graphical-session.target` names it among its dependencies. "The play succeeded" was
  always a different claim, and the two disagreed for real
- **The distinction this sub-task insisted on was a real defect,** found the first
  time it was checked: the enable task's `daemon_reload:` runs *before* the enable, so it
  re-read a directory without the symlink it existed for. Reload split into its own task
  after the enable. Evidence: `JOURNAL/00109-Journal-26-09-17.md`
- **HOST**: play re-run, logged out and back in, `acceptance.bash` re-run —
  ACCEPTED, 19 of 19 checks, 21 assertions, 0 failed (`20260917-143702`). [1]–[5], [11],
  [12] and [15] all cleared together, as predicted. Check [12] records the login itself

## Task 3.2: The server route

Sub-items of "The server route" (Task 3.2) that are done — the delivery, the
document producer/renderer, the boot-mismatch handling, the server-silence fix,
the VM scenario definition and its executor. The still-open VM run itself stays
in PLAN.md, along with the two HOST checks either side of it.

- `status_document.py` (producer) and `login_message.py` (renderer)
- The delivery — collection timer plus `~/.bashrc-includes` snippet, folded
  into `play-host-health-login-report.yml` (`scope: general`); daily (§1–2), and each
  branch removes the other's artefacts (§7)
- The snippet prints **only for an interactive shell**, or it breaks `scp` to
  the host it reports on (§3)
- A fresh document can be about the **previous boot**: the mismatch is reported
  in its own right and the one boot-scoped section is demoted (§4, §4.1)
- **A document the reader cannot interpret is reported, not read as clean**
  (§4.1a). The panel had the same gap from the other side — Task 4.2 below
- **A healthy server was never going to be silent** (qa-reviewer, 26-09-15) —
  two permanent findings, one root: no `dkms` on a server (§5, §5.1–5.3)
- A scenario exists that **can** run this route end to end:
  `server-host-health-kernel-change` in `vars/vm-test-scenarios.yml`, with a fixture
  and a fifteen-check checker (§8). **It has never been executed**, and until it has,
  nothing below it is established
- **The kernel step had no executor** — now a function driven against stubs,
  proving which version is chosen, what is downloaded, and that every way of ending up
  with one kernel refuses. This proves the **decisions**, not dnf's real output
  format (§8.2)
- **Making it a function moved it out of `set -e`** (qa-reviewer, BLOCK): bash
  disables errexit inside a command substitution, so the package transaction's status
  was discarded. No case had ever failed the install. Every guest-changing command now
  carries its own refusal (§8.2, which also corrects an initial wrong diagnosis)
- `reboot_before_checks` is a scenario's answer, not a profile's, and a profile
  the CLI has no mechanics for is a refusal rather than a silent no-reboot (§8.1)

## Task 3.3: Claude Code handoff

File and offer done.

- `handoff.py`, mode `0600`; the wrong/not-looked-at split is carried in
  `Finding.checked`, not read from the prose
- The **one-click** offer, in the panel's health section. It **copies** the
  command rather than launching it: `claude` reads the repository it starts in, and
  the panel knows no checkout path, so a launch would start it in the compositor's
  working directory where it cannot see the playbooks the diagnosis is about. Copying
  is also what `container-watch` does on this surface (§9a)
- The path reaches the panel through the status document, and `record_host_state`
  writes the handoff **before** the document that names it — a path recorded first is
  a button that fails in the user's hands. Falsified: computing the path instead of
  taking the write's result turns the ordering test red

## Task 4.2: Health section

Renders Phase 3's four checks.

- Registered and rendering; `unavailable` has its own icon, never the neutral
  one — and this is now **tested**, in `tests/extensions/test-panel-indicator.mjs`,
  driving `enable()` on the shipped `extension.js`. It was asserted here and untested:
  the loader mapped the shell's `extension.js` import from the first commit while
  `gi-stubs.mjs` exported no `Extension`, so any test importing it failed on the
  import. Falsified on three mutants — sharing the neutral icon, dropping the
  `unavailable` colour, and starting neutral before the first read lands
- Renders the document's self-section reason, so an unreadable document says why
  rather than showing four derived "no such section" lines
- **The ledger's emptiness is now its own check**, `play-ledger`, not a
  reinterpretation of `play-freshness` — and emptiness is a **fault**, not an unknown.
  `helpers/play_ledger/ledger_presence.py`, 9 tests
  ([DESIGN-play-ledger.md](DESIGN-play-ledger.md) §8)
- What a finding does when activated: **nothing, and that is the answer**. One
  handoff file describes every finding, so a clickable row per finding would offer the
  same command N times while implying each had its own. The offer is section-level
  (§9a, Task 3.3)
- **The panel is boot-aware**, and `resolvedSection` is the ONE place the demotion
  happens, so the menu and the icon read the same answer (§11)
- `state` is **derived** from the lists, as the producer derives it (§11,
  [DESIGN-server-route.md](DESIGN-server-route.md) §4.3)
- A **malformed** document is reported, not read as a clean host (§11, mirroring
  `unreadable_reasons` — [DESIGN-server-route.md](DESIGN-server-route.md) §4.1a)
- Proven by `tests/extensions/test-panel-sections.mjs` — 27 tests importing the
  **shipped** files through a `gi://` loader, falsified on six mutants. **Not** the
  contract gate, which is a vocabulary check (§11,
  [DESIGN-server-route.md](DESIGN-server-route.md) §4.2)

## Task 4.5: ESLint clean, deployed by its own play, Wayland-correct

- ESLint and compat gate green; `play-fedora-desktop-panel.yml` deploys it
- The contract gate compares a **derived** set — 9 constants, plus every key of
  a built document and every section id from the real seam — so a name added on the
  producer side cannot be one the gate forgot. Falsified on five mutants
- **HOST**: play run, logged out and back in — checks [13]–\[15\]: all 5 files
  deployed, the uuid enabled, the deployed reader and the producer agree on the document
  path, and the running shell reports `State ACTIVE`. Check [3] has the document naming
  all four checks

## Task 5.1: Establish the real cost and the real bug

A **rendering** failure, not a texture failure, so image size is irrelevant.
Converges on upstream `mutter#4767`, though **not confirmed on the axis that
would have settled it** and not fixable here either way.
