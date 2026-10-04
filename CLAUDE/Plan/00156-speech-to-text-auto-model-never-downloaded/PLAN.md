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

- `auto`'s model is downloaded by the playbook, for each recorder mode, and a partial
  download is resumed.
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
- [ ] 🔄 **Task 1.2**: Class "model judged present without its weights"
  (`model-present-without-weights`): rule, page and fixture, committed red; sweep; fix
  every instance.
- [ ] ⬜ **Task 1.3**: Class "a ready-wait that ignores its child's exit": rule, page and
  fixture, committed red; sweep; fix every instance. 16 instances found across five
  languages, so this class runs after 1.2 and 1.4 have been fixed and deployed.
- [ ] 🔄 **Task 1.4**: Class "a dropdown option label carries explanation"
  (`dropdown-label-carries-explanation`): rule, page and fixture, committed red; sweep;
  fix every instance.
- [ ] ⬜ **Task 1.5**: The playbook not downloading `auto`'s model. No static rule is
  practical here: knowing which strings name a downloadable artefact needs the resolver's
  runtime answer. Fixed conventionally (the playbook downloads each mode's `auto` model),
  with the reason recorded in the report.

### Phase 2: Original defects

- [ ] ⬜ **Task 2.1**: `wsi-stream` raises the server's own error when the server it started
  exits (`tests/speech_to_text/test_server_start.py`).
- [ ] ⬜ **Task 2.2**: The playbook downloads `auto`'s model; prefs labels and descriptions.

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

## Success Criteria

- [ ] `./CLAUDE/Plan/meta-deploy.bash` runs the play green and `auto`'s model has `model.bin`.
- [ ] Insert in server mode starts a recording after a fresh login.
- [ ] `./scripts/qa-all.bash` is green with each new rule loaded, and each rule is proved
  red on its fixture.

## Delivery & Milestones

- <!-- delivery commit hashes -->
