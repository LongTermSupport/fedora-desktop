# Research: Whisper Model Manager

**Date**: 2026-02-18
**Plan**: 014-whisper-model-manager

---

## Current Extension Behaviour (Analysed)

### Model Detection (`extension.js:1612-1629`)

Models are detected by checking for the HuggingFace cache directory:

```
~/.cache/huggingface/hub/models--Systran--faster-whisper-{modelName}/
```

The extension uses a simple `Gio.File.query_exists()` check. If the directory is present,
the model is treated as installed.

### Current Menu Build (`extension.js:480-529`)

The `_buildMenu()` method renders ALL 10 models into the dropdown every time:
- Installed models: shown with `✓` prefix, selectable
- Not installed: shown with `⬇` prefix in blue, clicking opens gnome-terminal to download

**Problem**: Every model — whether installed or not — occupies a menu row. The menu is long
even if only 1-2 models are installed.

### Current Install Flow (`extension.js:1324-1354`)

Clicking an uninstalled model calls `_installModel(modelName)`, which:

1. Builds a bash script string:
   ```bash
   echo 'Installing Whisper model: Small (466MB)'
   echo 'This may take several minutes — do not close this window.'
   python3 -c "from faster_whisper import WhisperModel; WhisperModel('small', device='cpu')"
   echo 'Done! ...'
   read -p 'Press Enter to close...' _
   ```
2. Launches `gnome-terminal --title="Install Whisper Model" -- bash -c "..."`

**Problems**:
- `WhisperModel()` loads the model into RAM (~2-8GB) just to trigger download — wasteful
- No download progress shown (just a hanging terminal)
- No ability to cancel mid-download
- Only shows the 10 hardcoded models — no access to `large-v3-turbo` or others
- Requires log-out/log-in note after install (extension.js:1337) — but this isn't even shown

### Known Model List in Extension (`extension.js:69-82`)

```javascript
this._whisperModels = [
    ['auto', 'Auto (optimized per mode)', 'varies', 'Base for streaming, small for batch', false],
    ['tiny', 'Tiny', '~75MB', 'Fastest, basic accuracy', false],
    ['base', 'Base', '~142MB', 'Fast, good accuracy', false],
    ['small', 'Small', '~466MB', 'Balanced speed/accuracy', false],
    ['medium', 'Medium', '~1.5GB', 'Slow, great accuracy', false],
    ['large-v3', 'Large v3', '~3GB', 'Slowest, best accuracy', false],
    ['tiny.en', 'Tiny English', '~41MB', 'Fastest, English only', true],
    ['base.en', 'Base English', '~77MB', 'Fast, good accuracy, English only', true],
    ['small.en', 'Small English', '~252MB', 'Balanced, English only', true],
    ['medium.en', 'Medium English', '~789MB', 'Great accuracy, English only', true],
];
```

Missing from this list: `large-v2`, `large-v3-turbo`, `distil-whisper-*` variants.

---

## HuggingFace Cache Structure

Models downloaded via `faster-whisper` or `huggingface_hub` go to:

```
~/.cache/huggingface/hub/
├── models--Systran--faster-whisper-tiny/
│   ├── blobs/           ← content-addressed file data
│   ├── refs/            ← branch → commit hash mapping
│   └── snapshots/
│       └── <hash>/      ← actual model files symlinked from blobs
│           ├── config.json
│           ├── model.bin
│           ├── tokenizer.json
│           └── vocabulary.json
├── models--Systran--faster-whisper-small/
│   └── ...
└── ...
```

**Key insight**: The extension's `_checkModelInstalled()` checks for the top-level directory.
This is sufficient — if the directory exists, the download was attempted. A fully downloaded
model will have the `snapshots/<hash>/` subdirectory populated.

A more robust check would verify `snapshots/` contains at least one commit directory with
`model.bin` present. Worth considering for `wsi-model-manager`.

---

## Available Tools (Evaluated)

### Existing TUI/GUI Tools for faster-whisper Model Management

| Tool | Type | Verdict |
|------|------|---------|
| `whisper-ui` (PyPI) | Desktop GUI (tkinter) | Desktop app, not terminal |
| `faster-whisper-gui` (GitHub/CheshireCC) | PySide6 GUI | Heavy dependency, desktop only |
| `Faster-Whisper-XXL-GUI` (GitHub/cbro33) | Desktop GUI | Includes YouTube download, too complex |
| `Whisper-WebUI` (GitHub/jhj0517) | Web UI | Requires web server, overkill |
| `hf cache ls` CLI | HF Hub CLI | Lists all cached repos, not Whisper-specific |
| `HuggingFaceModelDownloader` (Go) | CLI | Downloads to HF cache; no TUI, no filtering |
| `whisper.cpp` `make tiny.en` | Makefile | Different format (GGML); not compatible |

**Conclusion**: No suitable existing tool. Must build `wsi-model-manager`.

---

## Python Libraries Available

### `huggingface_hub` (installed as dep of faster-whisper)

Key APIs for the model manager:

```python
from huggingface_hub import snapshot_download, scan_cache_dir

# List cached repos (returns HFCacheInfo)
cache = scan_cache_dir()
for repo in cache.repos:
    print(repo.repo_id, repo.size_on_disk_str)
    # e.g. "Systran/faster-whisper-small" "466.1M"

# Download a model repo (with progress)
snapshot_download(
    repo_id="Systran/faster-whisper-small",
    local_files_only=False,      # actually download
    # tqdm_class=MyProgress,     # custom progress callback
)

# Delete a model from cache
delete_strategy = cache.delete_revisions(*revision_hashes)
delete_strategy.execute()
```

**Progress callbacks**: `snapshot_download()` accepts a `tqdm_class` parameter that can be
overridden with a Textual-compatible progress emitter.

### `textual` (TUI framework, to be installed)

```python
from textual.app import App, ComposeResult
from textual.widgets import DataTable, Input, ProgressBar, Button
from textual.worker import Worker

class ModelManagerApp(App):
    def compose(self) -> ComposeResult:
        yield Input(placeholder="Filter models...")
        yield DataTable()
        yield ProgressBar()
```

Key features needed:
- `DataTable` — sortable, selectable rows
- `Input` — live filter
- `ProgressBar` — download progress
- `Worker` — background download thread (non-blocking UI)

**Installation**: `pip install textual` — not available as Fedora RPM, need pip.

Alternatively, a simpler approach with just `rich` (already installed with textual's deps) and
curses-style refresh is possible but less capable. Textual is the right tool.

---

## Model Catalogue (Verified on HuggingFace Hub)

All models are in the `Systran` organisation as `Systran/faster-whisper-{name}`:

| Model ID | HF Repo | Approx Size | Notes |
|----------|---------|-------------|-------|
| `tiny` | faster-whisper-tiny | ~75MB | Multilingual |
| `base` | faster-whisper-base | ~142MB | Multilingual |
| `small` | faster-whisper-small | ~466MB | Multilingual |
| `medium` | faster-whisper-medium | ~1.5GB | Multilingual |
| `large-v2` | faster-whisper-large-v2 | ~3.0GB | Multilingual |
| `large-v3` | faster-whisper-large-v3 | ~3.0GB | Multilingual, best quality |
| `large-v3-turbo` | faster-whisper-large-v3-turbo | ~1.6GB | Fast large model ⚠️ verify name |
| `tiny.en` | faster-whisper-tiny.en | ~41MB | English only |
| `base.en` | faster-whisper-base.en | ~77MB | English only |
| `small.en` | faster-whisper-small.en | ~252MB | English only |
| `medium.en` | faster-whisper-medium.en | ~789MB | English only |

**⚠️ To verify before implementation**: Confirm `Systran/faster-whisper-large-v3-turbo`
exists on HuggingFace Hub. The `large-v3-turbo` model was released by OpenAI in late 2024
and Systran may have released a CTranslate2 version — needs checking.

---

## Extension Modification Plan (Detailed)

### `_buildMenu()` model section change

**Before** (current — shows all 10 models with install status):
```javascript
for (const [modelName, label, size,, englishOnly] of this._whisperModels) {
    const installed = this._checkModelInstalled(modelName);
    if (installed || modelName === 'auto') {
        // show selectable item with ✓
    } else {
        // show download item with ⬇
    }
}
```

**After** (only installed models + manager launcher):
```javascript
for (const [modelName, label, size,, englishOnly] of this._whisperModels) {
    if (!this._checkModelInstalled(modelName) && modelName !== 'auto') {
        continue;  // Skip uninstalled models
    }
    // Build selectable item with ✓ (same as current installed case)
    ...
}

// Add manager launcher
const manageItem = new PopupMenu.PopupMenuItem('  ⬇ Download more models...');
manageItem.connect('activate', () => { this._openModelManager(); });
menu.addMenuItem(manageItem);
```

### Dynamic refresh on menu open

The `open-state-changed` handler (extension.js:606) already refreshes log display.
We need to also refresh model items:

```javascript
menu.connect('open-state-changed', (menu, open) => {
    if (open) {
        this._refreshLogDisplay();
        this._updateStatusLabel();
        this._rebuildModelItems();  // NEW: refresh installed model list
    }
});
```

`_rebuildModelItems()` would:
1. Remove existing model menu items
2. Re-check `_checkModelInstalled()` for all models
3. Re-add only installed ones + manager launcher

### `_openModelManager()` method

```javascript
_openModelManager() {
    const script = GLib.get_home_dir() + '/.local/bin/wsi-model-manager';
    try {
        GLib.spawn_command_line_async(`gnome-terminal -- ${script}`);
        this._log('Opening model manager');
    } catch (e) {
        try {
            GLib.spawn_command_line_async(`xterm -title "Whisper Model Manager" -e ${script}`);
        } catch (e2) {
            Main.notify('Speech to Text', 'Cannot open terminal. Run: wsi-model-manager');
            this._log(`Cannot open terminal: ${e2.message}`);
        }
    }
}
```

---

## `wsi-model-manager` Script Design

### Script Header

```python
#!/usr/bin/env python3
"""Whisper Model Manager — browse and download faster-whisper models."""

import sys
import os
from pathlib import Path
```

### Model Catalogue (hardcoded)

```python
WHISPER_MODELS = [
    # (id, label, hf_repo, size_str, english_only, description)
    ('tiny',           'Tiny',           'Systran/faster-whisper-tiny',           '~75MB',  False, 'Fastest, basic accuracy'),
    ('base',           'Base',           'Systran/faster-whisper-base',           '~142MB', False, 'Fast, good accuracy'),
    ('small',          'Small',          'Systran/faster-whisper-small',          '~466MB', False, 'Balanced speed/accuracy'),
    ('medium',         'Medium',         'Systran/faster-whisper-medium',         '~1.5GB', False, 'Great accuracy'),
    ('large-v2',       'Large v2',       'Systran/faster-whisper-large-v2',       '~3.0GB', False, 'Very high accuracy'),
    ('large-v3',       'Large v3',       'Systran/faster-whisper-large-v3',       '~3.0GB', False, 'Best quality'),
    ('large-v3-turbo', 'Large v3 Turbo', 'Systran/faster-whisper-large-v3-turbo', '~1.6GB', False, 'Fast + accurate'),
    ('tiny.en',        'Tiny (EN)',       'Systran/faster-whisper-tiny.en',        '~41MB',  True,  'English only, fastest'),
    ('base.en',        'Base (EN)',       'Systran/faster-whisper-base.en',        '~77MB',  True,  'English only, fast'),
    ('small.en',       'Small (EN)',      'Systran/faster-whisper-small.en',       '~252MB', True,  'English only, balanced'),
    ('medium.en',      'Medium (EN)',     'Systran/faster-whisper-medium.en',      '~789MB', True,  'English only, great'),
]
```

### Installed Model Detection

```python
def get_installed_models() -> set[str]:
    cache_dir = Path.home() / '.cache' / 'huggingface' / 'hub'
    installed = set()
    for model_id, _, _, _, _, _ in WHISPER_MODELS:
        model_dir = cache_dir / f'models--Systran--faster-whisper-{model_id}'
        if model_dir.exists():
            installed.add(model_id)
    return installed
```

### Download Function

```python
from huggingface_hub import snapshot_download

def download_model(repo_id: str, progress_callback=None) -> None:
    snapshot_download(
        repo_id=repo_id,
        local_files_only=False,
        tqdm_class=None,  # replaced with Textual progress updates
    )
```

For Textual integration, wrap the download in a `Worker` and update a `ProgressBar` via
`app.call_from_thread()`.

---

## Open Questions

1. **`large-v3-turbo` repo name**: Is it `Systran/faster-whisper-large-v3-turbo`? Needs
   verification. If the repo doesn't exist, exclude from catalogue.

2. **Textual version**: Which version is compatible with Python 3.12 on Fedora 42?
   `pip install textual` should work — latest is ~0.80.x as of early 2026.

3. **Download progress with Textual**: `snapshot_download()` uses `tqdm` internally.
   To hook into progress, may need to use `huggingface_hub.file_download.hf_hub_download()`
   with a custom `tqdm_class`, or poll file size during download. Worth prototyping.

4. **Rebuild model menu vs. update visibility**: Dynamically rebuilding the model section
   (removing and re-adding menu items) may cause visual glitches in GNOME Shell. Hiding/
   showing items based on install status may be safer. Needs testing.
