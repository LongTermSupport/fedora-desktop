# Plan 00150: image paste cli

**Status**: In Progress
**Created**: 2026-10-03
**Owner**: joseph
**Priority**: Medium

## Overview

Pasting a screenshot into Claude Code in a Linux terminal does not work today, and an
agent on a remote server over SSH can never reach the local clipboard at all. This plan
adds a small CLI, deployed by Ansible, that turns an image file into a self-decoding
text block: a short bash heredoc carrying a compact base64-encoded image and its
sha256. Paste the block into any Claude chat; the receiving agent runs it with bash,
the checksum proves the paste survived intact, and the agent views the image with its
Read tool. The receiving host needs only coreutils (`base64`, `sha256sum`).

A side task checks the local root cause: Claude Code's own image paste (Ctrl+V) reads
the clipboard through `wl-paste` (Wayland) or `xclip` (X11). This is not yet verified
on this host.

Prototype: [`imgpaste-proto.bash`](imgpaste-proto.bash). Measurements and the format
choice: [RESEARCH-encoding.md](RESEARCH-encoding.md). Example input:
[`assets/example-terminal-screenshot.png`](assets/example-terminal-screenshot.png).

## Goals

- One command, `imgpaste <image>`, that prints a pasteable block on stdout.
- Text in the decoded image stays legible when viewed with the Read tool.
- Decoding needs only `bash`, `base64` and `sha256sum` on the receiving host.
- Bad input fails fast: non-images and oversized images are rejected with a clear stderr message.
- The command and its packages are deployed by an existing playbook.

## Non-Goals

- Capturing screenshots or reading the clipboard (the input is a file path).
- Output formats the Read tool cannot display (AVIF, JPEG XL, HEIC).
- Any upload service or server-side component.

## Tasks

### Phase 1: Prototype and dogfood

- [x] ✅ **Task 1.1**: Measure candidate encodings on the example screenshot ([RESEARCH-encoding.md](RESEARCH-encoding.md)).
- [x] ✅ **Task 1.2**: Pick the default: WebP q50, longest edge capped at 2000 px, no extra compression. (The first step of the Task 1.5 ladder.)
- [x] ✅ **Task 1.3**: Write the prototype encoder with input validation and the self-decoding block format.
- [ ] 🔄 **Task 1.4**: Dogfood the decode. An agent runs the printed block and views the result.
  - [x] ✅ Fix the decode target first. The prototype writes to `/tmp/imgpaste-<id>.webp`, and this repo's hooks block an agent from writing outside the project (R-WRITE-OUTSIDE-PROJECT-ROOT). Write to a path relative to the current directory instead (e.g. `./imgpaste-<id>.webp`), so it works both here and on bare servers.
  - [x] ✅ Re-encode, have the agent transcribe and run the block, and confirm the sha256 check passes and the image reads as legible. This tests whether an LLM can copy a ~15 KB base64 block accurately; the checksum is the guard.
  - [ ] ⬜ Paste a block into a fresh session with no context; confirm the agent follows the block's one-line instruction unaided.
- [x] ✅ **Task 1.5**: Test a photo or busy-UI image and confirm q50 is still acceptable, or add a size-driven quality step-down. q50 alone gave a busy UI a ~175K-char block, so the prototype now steps quality, then size, down to a 40K-char budget and fails if nothing fits ([RESEARCH-encoding.md](RESEARCH-encoding.md)).

### Phase 2: Productionise

- [ ] ⬜ **Task 2.1**: Move the prototype to `files/home/.local/bin/imgpaste`, following `CLAUDE/StderrHygiene.md`.
- [ ] ⬜ **Task 2.2**: Deploy it from an existing play (no new playbook). Make sure ImageMagick with WebP support and `file` are installed by that play. **Owner decision:** no single play deploys the `~/.local/bin` tools; each has its own optional play. The closest are `play-clean-paste.yml` (installs `wl-clipboard`, but is built around its Ctrl+Alt+V keybinding) and `play-image-watermarking.yml` (installs ImageMagick 7).
- [ ] ⬜ **Task 2.3**: Check whether `wl-clipboard`/`xclip` are installed by IaC. If not, add them to the relevant system playbook and confirm Ctrl+V image paste works locally. Found: `wl-clipboard` is installed only by the optional `play-clean-paste.yml`, and `xclip` by nothing. The Ctrl+V check needs the host.
- [ ] ⬜ **Task 2.4**: Write `deploy.bash` and `acceptance.bash` on `_planlib.inc.bash`. Acceptance round-trips a fixture (encode, run the block, sha256 match), tests the rejection paths, and prints a COVERAGE line.
- [ ] ⬜ **Task 2.5**: Document the command under `docs/`.
- [ ] ⬜ **Task 2.6**: Run `./scripts/qa-all.bash`, then the `qa-reviewer` agent; resolve all findings.

## Success Criteria

- [x] The example screenshot round-trips: block run by an agent, sha256 OK, text legible in Read.
- [ ] A fresh agent decodes a pasted block using only the block's own instruction line.
- [ ] Non-image and oversized inputs fail fast with a clear stderr message.
- [ ] Deployed via Ansible; `acceptance.bash` passes on the host.
- [ ] `./scripts/qa-all.bash` and the `qa-reviewer` agent are clean.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00150-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan, prototype and encoding research committed (Phase 1 partly done).
