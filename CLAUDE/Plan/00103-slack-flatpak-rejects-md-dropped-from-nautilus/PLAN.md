# Plan 00103: Slack Flatpak rejects a .md file dropped from Nautilus

**Status**: In Progress
**Created**: 2026-09-03
**Owner**: joseph
**Priority**: Medium

## Overview

Dragging `mapping-clarifications-business-questions.md` from Nautilus into the Slack
desktop app fails with Slack's generic rejection: *"Sorry, … is a type of file not
supported by Slack. Try uploading a .zip version of this file instead."* Slack accepts
Markdown as an upload in general, so the rejection is about what Slack *received* from
the drop, not about the file.

Slack on this desktop is the Flathub Flatpak, installed by
`playbooks/imports/play-comms.yml`. That puts three moving parts between Nautilus and
Slack's upload code: the Wayland drag-and-drop payload, the Flatpak sandbox's view of
the host filesystem, and the sandbox runtime's MIME database that Chromium uses to type
a dropped file. Any one of them can turn a readable `.md` into an empty, untyped file
that Slack refuses.

This plan establishes which part is at fault with a read-only host triage, then fixes
it in the playbook that owns Slack, never by hand.

## Goals

- A triage report that states, as facts, what the Slack sandbox can see and how it types
  the dropped file.
- A single confirmed cause, recorded against the hypotheses below.
- A fix in `play-comms.yml` (or a documented no-fix decision if the cause is outside
  the machine's configuration), deployed and confirmed by drag-and-drop working.

## Non-Goals

- Replacing the Flatpak with an RPM or other packaging of Slack.
- Changing how Nautilus or GNOME Shell handle drag-and-drop generally.
- Working around the problem manually (renaming files, `flatpak override` by hand).

## Context & Background

- Slack install: `playbooks/imports/play-comms.yml`, `community.general.flatpak`,
  `com.slack.Slack` from Flathub, system-wide (`become: true`).
- The repo sets no Flatpak overrides for Slack, so the sandbox runs on the manifest's
  own `finish-args`.
- The user-visible message is Slack's catch-all for a `File` whose type is empty or
  whose content could not be read; it is the same text an admin file-type restriction
  produces.

### Hypotheses (triage confirms or refutes; none is asserted as cause yet)

| ID  | Hypothesis                                                                                                                               | Confirmed by                                                                                                          | Refuted by                                                              |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| H1  | The dropped path lies outside the filesystems granted to the sandbox, so Chromium gets a name it cannot open and reports an untyped file | the sandbox stat probe fails on the path while the host stat succeeds                                                 | the path reads fine inside the sandbox                                  |
| H2  | The sandbox runtime cannot map `.md` to a MIME type, so `File.type` is empty and Slack rejects it                                        | sandbox `globs2` has no `*.md` entry, or in-sandbox `xdg-mime`/`file` returns nothing for it, while the host types it | sandbox and host both report `text/markdown`                            |
| H3  | The workspace admin has restricted uploadable file types; nothing on the machine is wrong                                                | uploading the same file through the paperclip picker fails with the same message                                      | the paperclip upload succeeds (recorded as a manual observation, below) |
| H4  | Slack runs under XWayland and the GTK4 to X11 drop payload is degraded                                                                   | the running Slack process has `--ozone-platform=x11` or no Wayland socket is granted                                  | process runs on Wayland with the `wayland` socket granted               |

### Manual observations the triage cannot make

Record these in the journal alongside the triage report:

1. Does `sample-drop.md` (the fixture next to `triage.bash`, the file the probes test by default) upload via the paperclip / "Upload from your computer" picker?
2. Does a `.png` or `.pdf` dropped from Nautilus the same way upload?
3. Does copying the file in Nautilus (Ctrl+C) and pasting into the message box work?

## Tasks

### Phase 1: Triage

- [x] ✅ **Task 1.1**: Write `triage.bash` + `probe-slack.bash` (planlib gather mode, host-only, read-only)
- [x] ✅ **Task 1.2**: Operator runs `triage.bash` on the host, drags `sample-drop.md` from Nautilus into Slack, and records the three manual observations
- [x] ✅ **Task 1.3**: Read the report; mark each hypothesis confirmed or refuted in the journal

### Phase 2: Fix (shape depends on Phase 1)

- [x] ✅ **Task 2.1**: H1 confirmed: add a Flatpak override task for `com.slack.Slack` to `play-comms.yml` granting read access to the directories the user drops from, idempotent and syntax-checked
- [ ] ❌ **Task 2.2**: H2 refuted, cancelled. If H2: fix the MIME mapping in the sandbox via the playbook (runtime extension or override), or record a no-fix decision if it is an upstream runtime defect
- [ ] ❌ **Task 2.3**: H3 moot, H1 explains the failure; cancelled. If H3: record a no-fix decision; the change belongs to the Slack workspace admin, not this repo
- [ ] ❌ **Task 2.4**: H4 refuted, cancelled. If H4: set the Slack launch flags via the playbook so it runs natively on Wayland
- [x] ✅ **Task 2.5**: Add `deploy.bash` and `acceptance.bash`; update `docs/playbooks.md` if the play gains a task

### Phase 3: Verify and close

- [ ] 🔄 **Task 3.1**: Operator deploys, then drags the original `.md` from Nautilus into Slack; it uploads
- [ ] ⬜ **Task 3.2**: Second deploy reports the new task `ok`, not `changed`
- [ ] ⬜ **Task 3.3**: `qa-reviewer` agent passes; plan moved to `Completed/`

## Dependencies

- Depends on: nothing
- Blocks: nothing

## Success Criteria

- [ ] The triage report names the failing component with evidence, not inference
- [ ] The original file drags from Nautilus into Slack and uploads
- [ ] No manual change on the host; everything is in `play-comms.yml`
- [ ] QA passes (`./scripts/qa-all.bash`)

## Risks & Mitigations

| Risk                                                                     | Impact | Probability | Mitigation                                                                                          |
| ------------------------------------------------------------------------ | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| Widening the sandbox's filesystem grant weakens the point of the Flatpak | M      | M           | Grant read-only and only the directories the user actually drops from; prefer the portal path if H1 |
| The cause is an upstream Chromium/Wayland drop bug with no local fix     | M      | L           | Record the no-fix decision and the working paperclip path; do not hack around it                    |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00103-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Triage scripts committed: d504a86
- Fix in play-comms.yml plus deploy/acceptance scripts
