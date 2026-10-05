# Plan 00156: speech to text auto model never downloaded

**Status**: In Progress
**Created**: 2026-10-04
**Owner**: joseph
**Priority**: High

## Overview

After a reboot, speech-to-text in streaming server mode would not start. The server log and
the model cache showed three separate faults behind it:

- **The `auto` model was never on disk.** `auto` resolves to `distil-large-v3.5` on this GPU,
  but `play-speech-to-text.yml` never downloads it. Its cache snapshot held the small config
  files and a 0-byte `.incomplete` blob instead of `model.bin`. Settings still listed it as
  installed, because `_isModelInstalled` in `prefs.js` counted any non-empty snapshot
  directory.
- **The real error was hidden.** With `large-v3` chosen instead, the server died after 6 s
  (`CUDA failed with error out of memory` on the 4 GB GPU). `wsi-stream` went on waiting
  for the full 45 s and reported "timeout", so the error never reached the panel.
- **Settings could not be read.** The Startup mode dropdown labels were whole sentences,
  truncated in the row, and nothing said what "server mode" is, although Continuous
  dictation requires it.

The owner asked for the fix to be locked in under Defence Before Fix (DBF,
<https://defence-before-fix.github.io/>). So each fault is first attributed to a class,
then a rule is written and proved red, then every instance is swept and fixed, and only
then is the original defect fixed. The DBF records live in [`dbf/`](dbf/).

## Goals

- No model is downloaded by default (owner's decision, Task 2.4). With none on disk, or
  with `auto` picking one that is not on disk, recording stops before it starts with a
  clear message: download at least one model, and from where. Nothing downloads at run
  time unasked.
- Settings lists a model as installed only when its weights (`model.bin`) are present.
- A server that dies while loading is reported at once, with its own error.
- The Settings dropdowns show short option names, and the explanation sits in the subtitle.
- Every class above that can be written as a rule has a permanent, blocking defence in
  `qa-all.bash`.

## Non-Goals

- Filtering the model list by GPU memory. `large-v3` does not fit the 4 GB GPU, and the
  list still offers it. This is recorded as a finding (Task 4.2), not fixed here.

## Tasks

### Phase 1: DBF, one class at a time

- [x] ✅ **Task 1.1**: Independent searches for each class, dispatched before any rule
  exists ([`dbf/`](dbf/) `search-*.md`).
- [x] ✅ **Task 1.2**: Class "model judged present without its weights"
  (`model-present-without-weights`): rule, page and fixture, committed red; sweep; fix
  every instance.
- [ ] ⬜ **Task 1.3**: Class "a ready-wait that ignores its child's exit": rule, page and
  fixture, committed red; sweep; fix every instance. 16 instances found across five
  languages, so this class runs after 1.2 and 1.4 have been fixed and deployed.
- [x] ✅ **Task 1.4**: Class "a dropdown option label carries explanation"
  (`dropdown-label-carries-explanation`): rule, page and fixture, committed red; sweep;
  fix every instance.
- [x] ✅ **Task 1.5**: The playbook not downloading `auto`'s model. No static rule is
  practical here: knowing which strings name a downloadable artefact needs the resolver's
  runtime answer. Fixed conventionally (the playbook downloads each mode's `auto` model),
  with the reason recorded in the report.

### Phase 2: Original defects

- [ ] ⬜ **Task 2.1**: `wsi-stream` raises the server's own error when the server it started
  exits (`tests/speech_to_text/test_server_start.py`). The fix and its test are drafted
  and held in [`held/`](held/) until Task 1.3's defence is committed red. The patch is
  against 6515dd7c's `wsi-stream`, and is re-applied by hand since that file has moved on.

- [x] ✅ **Task 2.2**: The playbook downloads `auto`'s model; prefs labels and descriptions.

- [ ] 🔄 **Task 2.3**: The play's "Download The Auto Model" hung on the first deploy:
  `model.bin` stayed a 0-byte `.incomplete`, the process sat in `futex_do_wait` for
  30 minutes holding the blob's `.lock`, and the HEAD request (302 to the xet CDN)
  answered at once. The same 0-byte state is what the reboot left behind. Find why the
  xet download stalls, give the task a time limit that fails loudly, and decide with the
  owner whether a large model download should run by default on a metered (5G) link.
  The deploy was stopped by a shutdown at this task, so the rest of the play, its
  acceptance and Plan 00151 did not run. Every file before the download was deployed.

  - [x] ✅ The task has a 30-minute time limit per model (`timeout: 1800`), so a stall
    fails the play instead of holding it.
  - [ ] ⬜ HOST: why the xet download stalls (needs a host run to observe). Moot for the
    play once Task 2.4 removes its download; still matters for the model manager.
  - [x] ✅ Owner decision: no. See Task 2.4.

- [ ] ⬜ **Task 2.4**: **Owner's decision: no model downloads by default.** One model is not
  right for every machine this repo installs, so the play suggests and never installs, and a
  metered-link check is not trusted. Supersedes Tasks 1.5 and 2.2's download half.

  - [x] ✅ The play drops "Download The Auto Model" (keeps "Verify The Auto Model
    Resolves"). Plan 00148's `acceptance.bash` stops requiring `auto`'s `model.bin`.
  - `wsi` and `wsi-stream` (and the warm server) check, before the microphone opens, that
    the model to be used has `model.bin` on disk, and otherwise fail with: no speech model
    is downloaded (or: `auto` picked X, which is not downloaded); open the model manager
    and download at least one. The panel shows that message. faster-whisper is never left
    to download on first use.
  - Settings and the model manager show which model `auto` suggests for this machine.
  - Tests first, red against the current recorder.

### Phase 3: Review and deploy

- [ ] ⬜ **Task 3.1**: The DBF conformance review, then `qa-reviewer`.
- [ ] ⬜ **Task 3.2**: `deploy.bash` runs `play-speech-to-text.yml`; it is added to
  `meta-deploy.bash`.
- [ ] ⬜ **Task 3.3**: Owner checks: log out and in, open Settings, press Insert in server mode.

### Phase 4: Findings not fixed here

- [ ] ⬜ **Task 4.1**: The DBF plugin (`defence-before-fix@defence-before-fix`) is not
  installed. Installing it was refused in-session; the owner runs the install.
- [ ] ⬜ **Task 4.2**: The model list offers models that do not fit the GPU's memory
  (`large-v3`, fp16, on a 4 GB card).
- [x] ✅ **Task 4.3**: `qa-all.bash` gate `wsi-stop-grace` was red in the CCY container:
  `test_model_manager_installed.py` loads `wsi-model-manager`, which exits on import
  without `textual`, `rich` and `huggingface_hub`, and the ccy image has none of them.
  The test now loads stand-ins for them (none is used by the code under test), so it
  runs anywhere; a control run against 6515dd7c's manager still fails on the defect.

## Success Criteria

- [ ] `./CLAUDE/Plan/meta-deploy.bash` runs the play green and `auto`'s model has `model.bin`.
- [ ] Insert in server mode starts a recording after a fresh login.
- [ ] `./scripts/qa-all.bash` is green with each new rule loaded, and each rule is proved
  red on its fixture.

## Delivery & Milestones

- <!-- delivery commit hashes -->
