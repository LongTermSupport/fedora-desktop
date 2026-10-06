# `wsi-model-manager --download-auto` (Task 3.4)

## What changed

- `files/home/.local/bin/wsi-model-manager` gains `main(argv)` and `download_auto()`.
  `--download-auto` asks `auto_suggestions(session_language())` (the resolver's
  `--suggest`) for each mode in `AUTO_MODES`, dedupes by model id, downloads each repo
  from the same `MODELS` mapping the TUI uses with
  `snapshot_download(repo_id=..., local_files_only=False, token=False)`, then runs
  `wsi-resolve-model --mode <mode> --language <lang> auto` WITHOUT `--suggest` for each
  mode and fails unless every one exits 0 (it exits 3 when `model.bin` is missing).
- Progress goes to stderr (huggingface_hub's own progress bars also go to stderr). Only
  after every pick is verified does stdout get one line per model:
  `WSI-MODEL-DOWNLOADED <model_id> <repo>`.
- Exit codes: 0 all verified; 1 the resolver cannot choose, a pick has no download
  entry, a download raises, or the post-check fails; 2 an unknown argument. `-h/--help`
  prints the docstring. With no argument the TUI runs as before. The flag path never
  constructs `ModelManagerApp` and needs no TTY.
- The module still imports textual at load (the TUI classes subclass `App`). That needs
  the package installed, not a terminal, so the flag works unattended on the host.
- `docs/features/speech-to-text.md`: one sentence on the flag in "Model Size Selection".
- No version or changelog convention applies to `wsi-model-manager`. No `wsi-*` script
  carries a version, and the play deploys it with a plain copy.

## Tests

`DownloadAutoTest`, 9 tests in `tests/speech_to_text/test_model_manager_installed.py`.
They cover:

- each pick is downloaded exactly once (and both are downloaded when the modes differ);
- the download is anonymous;
- the TUI is never constructed;
- each mode is checked on disk without `--suggest`;
- a download error fails and names the repo;
- missing weights after the download fail;
- a resolver failure stops the run before any download;
- an unknown argument is a usage error.

Red against the old manager (9 errors, no `main`), green after. The gate's full unittest
list for `tests/speech_to_text` passes.

## Not done (scope change)

Following the owner's direction, no plan script runs the download. The owner runs
`wsi-model-manager --download-auto` after the next speech-to-text deploy. The xet stall in
Task 2.3 is still not understood. The flag has no time limit of its own, so a stalled
download waits until it is interrupted, as the TUI does.
