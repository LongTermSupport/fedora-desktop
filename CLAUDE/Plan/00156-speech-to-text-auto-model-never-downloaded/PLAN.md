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
- [x] ✅ **Task 1.3**: Class "a ready-wait that ignores its child's exit"
  (`ready-wait-ignores-child-exit`). The gate `qa-ready-wait-rules.bash` runs a Semgrep
  rule for Python and `helpers/ready_wait/bash_ready_waits.py` for bash, because Semgrep
  cannot parse 88 of the 371 tracked shell scripts. It was committed red with 8
  findings. GNOME JS, Ansible, daemonizing starts and step scripts get no rule; the
  reasons are on the QA.md page. 14 of the search's 16 instances are fixed. #8 and #15
  are recorded as not fixed, with the reason. The sweep is in
  [`subagent-reports/261005-task-1-3-opus.md`](subagent-reports/261005-task-1-3-opus.md).
- [x] ✅ **Task 1.4**: Class "a dropdown option label carries explanation"
  (`dropdown-label-carries-explanation`): rule, page and fixture, committed red; sweep;
  fix every instance.
- [x] ✅ **Task 1.5**: The playbook not downloading `auto`'s model. No static rule is
  practical here: knowing which strings name a downloadable artefact needs the resolver's
  runtime answer. Fixed conventionally (the playbook downloads each mode's `auto` model),
  with the reason recorded in the report.

### Phase 2: Original defects

- [x] ✅ **Task 2.1**: `wsi-stream`'s `start_server()` asks on every try whether the
  server is still there: the server it started by its handle, and one already loading
  by its lock. A server that exited raises at once with the first ERROR in its log, for
  example "Model load failed: CUDA failed with error out of memory". A server that lost
  a start race waits for the one that won. `tests/speech_to_text/test_server_start.py`
  fails 4 of its 6 cases against the unfixed `wsi-stream` and passes against the fix.
  The held draft was re-applied by hand and `held/` is removed.

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

- [x] ✅ **Task 2.4**: **Owner's decision: no model downloads by default.** One model is not
  right for every machine this repo installs, so the play suggests and never installs, and a
  metered-link check is not trusted. Supersedes Tasks 1.5 and 2.2's download half.

  - [x] ✅ The play drops "Download The Auto Model" (keeps "Verify The Auto Model
    Resolves"). Plan 00148's `acceptance.bash` stops requiring `auto`'s `model.bin`.
  - [x] ✅ `wsi-resolve-model`, which every recorder and the warm server's start go
    through, names a model only when its `model.bin` is on disk (the Hugging Face cache,
    found as huggingface_hub finds it, or a model directory). Otherwise it exits 3 with
    one line: no speech model is downloaded, or `auto` picked X (or the setting is X),
    which is not downloaded; download it in **Manage Whisper Models...**, or choose one
    you have. `wsi` and `wsi-stream` show that line as the whole notification, and
    faster-whisper never sees a model it would download. `--suggest` skips the check,
    for the play's verify task and the acceptance. Tests first, each red against the
    old code: `test_resolve_model.py` `ModelOnDiskTest`, `test_server_client.py`
    `ModelNotDownloadedTest`, the "no model downloaded" case in
    `test-wsi-stop-grace.bash`, and `CataloguesAgreeTest` (the resolver, the manager
    and Settings name the same repo per model).
  - The pre-buffered streaming mode opens the microphone before the model is resolved
    (its point is an instant start), so there the message comes a moment after the
    start, and what was captured is discarded. Every other mode checks first.
  - [x] ✅ Settings and the model manager show which model `auto` picks on this machine
    (`wsi-resolve-model --suggest`): the Whisper Model subtitle starts "On this machine
    Auto picks X", and the manager marks it "★ auto", with the modes, in its status line.
    `AutoSuggestionTest` was red first; the Settings tests in
    `test-stt-prefs-installed-model.mjs` were written after the code (the stub harness
    could not return a process's stdout until now), so they were never seen red.

### Phase 3: Review and deploy

- [ ] ⬜ **Task 3.1**: The DBF conformance review, then `qa-reviewer`.
- [x] ✅ **Task 3.2**: Plan 00148's `deploy.bash` runs `play-speech-to-text.yml`, which
  carries this plan, and is in `meta-deploy.bash`; no second deploy script is needed.
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

- [ ] `./CLAUDE/Plan/meta-deploy.bash` runs the play green, downloading no model.
- [ ] With no model downloaded, Insert says which model to download and records nothing;
  after downloading it in the model manager, Insert in server mode starts a recording.
- [ ] `./scripts/qa-all.bash` is green with each new rule loaded, and each rule is proved
  red on its fixture.

## Delivery & Milestones

- <!-- delivery commit hashes -->
