# Independent search: "model present" judged without the weight file

## Forms expected before searching

1. Existence/non-emptiness of `models--*` or `snapshots/` (JS Gio, Python Path, bash test, Ansible stat).
2. Existence of `refs/`, `config.json`, or any single small file as the marker.
3. Directory size (`du`, `rglob` + `st_size`, `blobs/` sum) used as "downloaded".
4. "Download returned without exception" treated as "present" (success flag set from a call that does not prove model.bin).
5. Docs telling a user to `ls` the cache or trust a list.
6. Other model artefacts (ollama, piper, gguf, onnx, torch hub).

## Searches run (whole repo, excluding the keep-out paths, .git, node_modules)

- `models--|snapshots|\.incomplete|model\.bin|HF_HOME|huggingface|isModelInstalled|local_files_only|snapshot_download|try_to_load_from_cache|scan_cache`
- `ollama|piper|\.gguf|\.onnx|silero|torch\.hub|\.cache/(torch|whisper|models)|pull_model|model.*(exists|isdir|is_dir)|blobs`
- `faster.whisper|WhisperModel|whisper` (file list), then `installed|cache|model\.bin|snapshot|download` in docs and the extension.
- Reading: HEAD prefs.js, wsi-model-manager (get_installed, download, poll), wsi-resolve-model (whole), wsi-stream-server load_models, wsi-stream resolve_model call sites, play-speech-to-text.yml model tasks, docs/features/speech-to-text.md, extension.js model handling.

## Instances

### 1. extensions/speech-to-text@fedora-desktop/prefs.js, `_isModelInstalled`, HEAD lines 206-221 (originating; confident)
Form: `snapshots/` directory exists and has any child entry => installed. Feeds `_buildInstalledModelList` (HEAD 181-195), which offers the model unlabelled. Found by: text search + reading. Working tree has an uncommitted fix (checks `<snapshot>/model.bin` query_exists); not itself an instance. Residual note on the fix only as an observation: `query_exists` follows the symlink, so it checks the blob target exists; it does not check size.

### 2. files/home/.local/bin/wsi-model-manager, `get_installed`, lines 98-108 (confident)
Form: `models--*/snapshots` exists and `any(snapshots.iterdir())` => model id is "installed". Same defect as #1, in Python. Found by: text search (`snapshots`), confirmed by reading. Consequences inside the same file, all driven by this set:
- table row shows "installed" (line ~242, `model_id in self._installed`);
- footer count "N of M models installed" (253-256);
- `action_download` refuses: "is already installed" (306-307), so a partial model cannot be re-downloaded from the manager (no way to repair it from the UI except Remove first; Remove is offered for it at 379-380 since it is in the set);
- F5 refresh re-runs it (293).
This is the more severe sibling: it blocks the repair path as well as misreporting.

### 3. files/home/.local/bin/wsi-model-manager, `_download_done`, lines 361-366 (unsure; weaker form)
Form: `self._installed.add(model_id)` on `snapshot_download` returning without exception, without checking model.bin. snapshot_download normally raises on incomplete download, so the hazard is only if it returns with a partial snapshot (e.g. repo lacking the file, ignore patterns). Found by: reading (text search for the add would not distinguish it). Not a cache-existence test, so it is a looser member of the class.

### 4. files/home/.local/bin/wsi-model-manager, `_poll_progress`, lines 333-354 (unsure)
Form: download progress/percent computed from `blobs/` directory size (`st_size` sum, `human_size` of the dir) against `EXPECTED_BYTES`. Capped at 99%, only shown while `_downloading`, so it does not claim "installed". It reports status from directory sizes (the brief asked for this); it carries the hazard only weakly (a 0-byte `.incomplete` contributes 0, so it understates rather than overstates). Also line 352 `except Exception: pass` hides errors. Found by: text search (`blobs`, `st_size`) + reading.

## Checked and NOT instances (for the record)

- playbooks/imports/optional/common/play-speech-to-text.yml lines 478-500 ("Download The Auto Model"): decides presence by `os.path.isfile(<download_model(local_files_only=True)>/model.bin)`; it checks the weight file. Conforming. (It does swallow every exception into `present = False`, which fails safe by re-downloading.)
- files/home/.local/bin/wsi-resolve-model: chooses a model name from GPU/language only; never tests presence. Not an instance, but it is the place where "auto" is decided without knowing whether the weights exist; the load failure surfaces later in the server (see below).
- files/home/.local/bin/wsi-stream-server `load_models` (952-970): `WhisperModel(...)` itself is the loader, failure logged and returns False. Surfaces at the loader, correct place; the caller (wsi-stream) waits on a socket with a start timeout, which is where the "times out somewhere else" symptom appears.
- extension.js: no presence check at all; passes `WHISPER_MODEL` through. Not an instance (its list is hard-coded; the installed filtering lives in prefs.js).
- docs/features/speech-to-text.md lines 143-144 ("only downloaded models are listed") and 738 ("download it first with Manage Whisper Models"): document the behaviour of #1/#2; they become false claims for a partial download, not code instances. Lines 200, 917 only describe the cache location.
- Plans (CLAUDE/Plan/014-whisper-model-manager/PLAN.md lines 61-70, 197; research.md 12-15, 94-97, 309-312): describe/propose the same directory-existence detection (design text, not running code). research.md 96-97 itself says a model.bin check "is more robust". Listed as documentation echoes of #1/#2, not code.
- No other model artefacts found: no ollama, piper, gguf, onnx, torch-hub caches or other HF consumers in the repo. Silero VAD is loaded from faster-whisper's bundled assets (`get_vad_model`), no cache test. `huggingface_hub` is only installed (play-python.yml:180).
- No bash or YAML (`stat`, `creates:`, `when: ... exists`) test on any HF cache path anywhere else.

## Count

Confident instances: 2 (#1, #2). Unsure/looser: 2 (#3, #4). Total 4. Documentation/plan echoes: docs lines 143-144 and 738, Plan 014 PLAN.md and research.md.

## What text search found that reading could not have checked

- The absence of any other consumer: greps for `models--`, `snapshots`, `huggingface`, `local_files_only`, `.cache/...` over every language show only prefs.js, wsi-model-manager, and the playbook. Reading cannot prove a negative over the whole tree.
- The doc and plan echoes (Plan 014 research/PLAN) were found only by the repo-wide grep.

## What reading found that text search could not have

- #3: no distinguishing string; it is "success flag from a download call", found by reading `_download_done`.
- The consequence chain in wsi-model-manager (an "installed" partial model blocks re-download via `action_download`, line 306), invisible to a grep for the cache path.
- That wsi-resolve-model decides `auto` with no presence probe at all, and that the failure then surfaces in wsi-stream-server's loader and wsi-stream's start wait (the "somewhere else" part of the hazard).
- That the playbook's check is conforming and why (weight-file test).
