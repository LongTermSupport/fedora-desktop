# Plan 014: Whisper Model Manager

**Status**: 🔄 In Progress (Research & Planning)
**Created**: 2026-02-18
**Owner**: Claude Sonnet 4.6
**Priority**: Medium
**Type**: Feature Implementation

---

## Overview

The speech-to-text GNOME extension currently embeds Whisper model selection directly in the
panel dropdown menu. This creates two problems:

1. **Cluttered dropdown**: All known models are listed — both installed and not-installed —
   making the menu long and confusing. Users must scroll through ~10+ items to find what
   they need.

2. **Poor download UX**: Clicking an uninstalled model opens a raw `gnome-terminal` window
   running a Python one-liner. There is no progress feedback, no ability to cancel, no way to
   browse the full catalogue of available Whisper variants (e.g. `large-v3-turbo`,
   `distil-whisper-*`), and no way to filter by language capability or model size.

This plan replaces the current approach with:

- **Dropdown shows only installed models** (plus Auto), keeping it clean and short.
- A **"Manage Models..."** menu entry launches a dedicated `wsi-model-manager` terminal UI.
- The model manager is a standalone Python script using the **Textual** TUI framework that
  provides a browseable, filterable, downloadable model catalogue with live progress.

---

## Goals

- Keep the extension dropdown focused: only installed models + Auto are selectable.
- Build `wsi-model-manager` — a Python Textual TUI for discovering and downloading Whisper
  models from Hugging Face Hub.
- Support the full set of `Systran/faster-whisper-*` models (including English-only variants
  and newer models like `large-v3-turbo`).
- Show download progress within the TUI (no opaque terminal one-liners).
- Deployed via the existing Ansible speech-to-text playbook.
- Extension launches the manager via `gnome-terminal -- wsi-model-manager`.

---

## Non-Goals

- This plan does not change recording, transcription, or paste behaviour.
- This plan does not rewrite the extension architecture.
- We are not building a general-purpose HuggingFace model browser — only Whisper models.
- We are not replacing the GNOME Settings UI for any settings.
- No GPU/CUDA download variants (CPU-ready models only for this first version).

---

## Context & Background

### Current Model Detection (extension.js:1612-1629)

The extension detects installed models by checking whether the HuggingFace cache directory
exists:

```javascript
_checkModelInstalled(modelName) {
    const cacheDir = GLib.get_home_dir() + '/.cache/huggingface/hub';
    const modelDir = `models--Systran--faster-whisper-${modelName}`;
    const fullPath = `${cacheDir}/${modelDir}`;
    const file = Gio.File.new_for_path(fullPath);
    return file.query_exists(null);
}
```

Models live at: `~/.cache/huggingface/hub/models--Systran--faster-whisper-{name}/`

### Current Install UX (extension.js:1324-1354)

Clicking an uninstalled model runs:
```javascript
GLib.spawn_command_line_async(
    `gnome-terminal --title="Install Whisper Model" -- bash -c "${bashCmd}"`
);
```
Where `bashCmd` is:
```bash
python3 -c "from faster_whisper import WhisperModel; WhisperModel('tiny.en', device='cpu')"
```
This gives zero progress feedback and no ability to cancel.

### Research Findings

1. **No existing dedicated tool** for listing/downloading `faster-whisper` models with a TUI
   was found. All GUI tools are heavy (PySide6, Electron) and desktop-focused.

2. **`huggingface_hub`** (already installed as a dependency of faster-whisper) provides:
   - `scan_cache_dir()` — enumerate locally cached repos with sizes
   - `snapshot_download()` — download a full model repo with progress callbacks
   - `hf_hub_download()` — download individual files

3. **Python Textual** is the best TUI framework for this purpose:
   - Already used in the Python ecosystem on Fedora
   - Supports progress bars, tables, filtering, keyboard navigation
   - Runs inside gnome-terminal without issues

4. **Model catalogue** for faster-whisper on HuggingFace (Systran org):
   - `tiny`, `tiny.en`, `base`, `base.en`, `small`, `small.en`
   - `medium`, `medium.en`, `large-v2`, `large-v3`, `large-v3-turbo`
   - (distil variants are separate repos — out of scope for v1)

5. **HuggingFace Hub CLI** (`hf cache ls`) can list cached repos but is not user-friendly
   for this purpose.

---

## Architecture

### Component Overview

```
Extension dropdown (extension.js)
  └─ "Manage Models..." menu item
       └─ gnome-terminal -- wsi-model-manager
            └─ Python Textual TUI
                 ├─ Model catalogue (hardcoded + HF Hub list)
                 ├─ Installed model detection (scan_cache_dir or glob)
                 ├─ Download with progress (snapshot_download + callbacks)
                 └─ On exit: signals extension to refresh menu
```

### Extension Changes (extension.js)

- **Model section in `_buildMenu()`**: Only build menu items for models where
  `_checkModelInstalled(modelName)` returns `true` (plus Auto).
- **Remove** the ⬇ "click to install" items from the dropdown.
- **Add** a `"Manage Models..."` menu item that calls `_openModelManager()`.
- **`_openModelManager()`**: Launches `gnome-terminal -- wsi-model-manager`.
- **On menu open**: Refresh installed model list so newly downloaded models appear.

### `wsi-model-manager` Script

A Python script at `~/.local/bin/wsi-model-manager` implementing a Textual TUI:

**Screen layout:**
```
┌─────────────────────────────────────────────────────┐
│  Whisper Model Manager              [q]uit  [d]ownload │
├─────────────────────────────────────────────────────┤
│  Filter: [________________]  Show: [All ▾]           │
├──────────────────┬────────┬──────────┬──────────────┤
│  Model           │  Size  │  Status  │  Notes       │
├──────────────────┼────────┼──────────┼──────────────┤
│ ✓ Auto           │   —    │ Always   │ Uses base/small│
│ ✓ Base           │ 142MB  │ Installed│ Fast, good   │
│ ✓ Small          │ 466MB  │ Installed│ Balanced     │
│   Tiny           │  75MB  │ Download │ Fastest      │
│   Medium         │  1.5GB │ Download │ Slow, great  │
│   Large v3       │  3.0GB │ Download │ Best quality │
│   Large v3-turbo │  1.6GB │ Download │ Fastest large│
│ ── English-only ──────────────────────────────────  │
│ ✓ Tiny.en        │  41MB  │ Installed│ English only │
│   Base.en        │  77MB  │ Download │ English only │
│   Small.en       │  252MB │ Download │ English only │
│   Medium.en      │  789MB │ Download │ English only │
├─────────────────────────────────────────────────────┤
│  [SPACE/Enter] Download selected  [DEL] Remove      │
│  Selected: Medium (1.5GB) — Press Enter to download │
└─────────────────────────────────────────────────────┘
```

**Features:**
- Arrow keys to navigate model list
- Filter box to search by name
- Show filter: All / Installed / Not Downloaded
- Space/Enter to download selected model (with progress bar replacing status)
- Delete key to remove a model from cache (with confirmation)
- Download runs in background thread, UI remains responsive
- Model list refreshes after download completes

---

## Tasks

### Phase 1: Research & Validation

- [x] ✅ **Read current extension model handling code** (extension.js:480-529, 1324-1354, 1612-1629)
- [x] ✅ **Research available tools** — no suitable existing TUI found
- [x] ✅ **Confirm HuggingFace cache structure** — `~/.cache/huggingface/hub/models--Systran--faster-whisper-{name}/`
- [x] ✅ **Confirm `huggingface_hub` is available** — installed as dependency of faster-whisper
- [x] ✅ **Identify full model catalogue** for Systran org on HuggingFace Hub
- [x] ✅ **Confirm Textual is available** — added to pip install in playbook
- [x] ✅ **Confirm `large-v3-turbo` model name** — confirmed as `Systran/faster-whisper-large-v3-turbo`

### Phase 2: Build `wsi-model-manager`

- [x] ✅ **Create `files/home/.local/bin/wsi-model-manager`**
  - [x] ✅ Define full model catalogue (11 models incl. large-v2, large-v3-turbo, all EN variants)
  - [x] ✅ Implement `get_installed()` — checks HuggingFace cache snapshots dir
  - [x] ✅ Implement download via `snapshot_download()` in Textual Worker (background thread)
  - [x] ✅ Implement remove via `shutil.rmtree()` with `ConfirmScreen`
  - [x] ✅ Build Textual app: `DataTable` with Status/Model/Size/Lang/Description columns
  - [x] ✅ Implement filter/search via `Input` widget with live table refresh
  - [x] ✅ Download runs non-blocking; status updates via `call_from_thread()`
  - [x] ✅ Remove confirmation dialog via `ConfirmScreen` with Y/N/Esc bindings
  - [x] ✅ Script is deployed with `mode: '0755'` via Ansible

### Phase 3: Modify Extension Dropdown

- [x] ✅ **Update `_buildMenu()` model section**
  - [x] ✅ Not-installed models built with `item.visible = false` (hidden, not removed)
  - [x] ✅ Section headers (`_multilingualHeader`, `_englishOnlyHeader`) tracked for visibility
  - [x] ✅ `"⬇ Download more models..."` item added below model list
  - [x] ✅ Added `large-v2` and `large-v3-turbo` to `_whisperModels` array
- [x] ✅ **Add `_openModelManager()` method**
  - [x] ✅ Launches `gnome-terminal -- wsi-model-manager`
  - [x] ✅ Falls back to `xterm` if gnome-terminal unavailable
- [x] ✅ **Add `_refreshModelSection()` method**
  - [x] ✅ Re-checks install status for all models, updates `item.visible`
  - [x] ✅ Shows/hides section headers based on installed models in each group
  - [x] ✅ Called from `open-state-changed` handler on every menu open
- [x] ✅ **Removed `_installModel()` dead code**
- [x] ✅ **ESLint passes clean**

### Phase 4: Ansible Deployment

- [x] ✅ **Update `play-speech-to-text.yml`**
  - [x] ✅ Added `textual` to pip install task (alongside faster-whisper)
  - [x] ✅ Added task: `Deploy Whisper Model Manager (wsi-model-manager)` with `mode: '0755'`
- [x] ✅ **QA passes**: `./scripts/qa-all.bash` — 293 files (199 bash + 94 python) all OK

### Phase 5: Testing & Deployment

- [ ] ⬜ **Test `wsi-model-manager` standalone**
  - [ ] ⬜ Run script directly in terminal
  - [ ] ⬜ Verify installed models show ✓
  - [ ] ⬜ Test download of a small model (tiny or tiny.en)
  - [ ] ⬜ Verify progress feedback during download
  - [ ] ⬜ Test remove function
  - [ ] ⬜ Test filter/search
- [ ] ⬜ **Deploy via Ansible**
  - [ ] ⬜ `ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml`
- [ ] ⬜ **Test extension changes (requires logout)**
  - [ ] ⬜ Verify model dropdown shows only installed models
  - [ ] ⬜ Verify "Manage Models..." launches manager in terminal
  - [ ] ⬜ After downloading new model, verify it appears in dropdown on next menu open
  - [ ] ⬜ Verify Auto is always present

---

## Technical Decisions

### Decision 1: TUI Framework

**Context**: Need a terminal UI for model browsing and downloading.

**Options Considered**:
1. **Textual** (Python) — Rich interactive TUI, tables, progress bars, keyboard nav
2. **Rich** (Python, no interaction) — Display only, no interactive selection
3. **Dialog/whiptail** (shell) — Very basic, no progress bars, limited layout
4. **Custom curses** — Too much work for this use case
5. **Raw bash with printf/tput** — Possible but fragile and limited

**Decision**: Textual (Option 1). Most capable, Python-native (consistent with other scripts),
well-documented, handles keyboard events and async operations cleanly.

**Risk**: Textual may not be installed by default. Mitigation: add to Ansible playbook as pip
install.

**Date**: 2026-02-18

---

### Decision 2: How to List Available Models

**Options Considered**:
1. **Hardcoded catalogue** — Fixed list matching current extension, easy to maintain
2. **Live HuggingFace API** — `list_models(author="Systran")` queries Hub at runtime
3. **Hybrid** — Hardcoded with "refresh from Hub" button for discovery

**Decision**: Hardcoded catalogue (Option 1) for v1.

**Rationale**:
- The set of faster-whisper models changes slowly (a few per year)
- API calls require network, add latency, and can fail
- Keeps the manager working offline (once models are downloaded)
- Matches how the extension currently works
- Can always upgrade to hybrid in a future plan

**Date**: 2026-02-18

---

### Decision 3: Download Implementation

**Options Considered**:
1. **`WhisperModel(name, device='cpu')`** — Triggers download as side effect of model load
2. **`snapshot_download(f"Systran/faster-whisper-{name}")`** — Explicit, progress-aware
3. **`hf_hub_download()`** — Per-file, too granular

**Decision**: `snapshot_download()` (Option 2).

**Rationale**:
- Download without loading the model into RAM (Option 1 allocates ~2-8GB RAM just to cache)
- `snapshot_download()` supports `tqdm` callbacks for progress reporting
- Clean separation of download vs. use
- `huggingface_hub` already installed as a dep of `faster-whisper`

**Date**: 2026-02-18

---

### Decision 4: Terminal Launcher from Extension

**Options Considered**:
1. **`gnome-terminal`** — Default terminal, already used for model install currently
2. **`xterm`** — Universal fallback but ugly
3. **`$TERM` env var** — More portable but complex to implement in GJS
4. **Custom dialog in GNOME Shell** — Much more complex, requires logout to update

**Decision**: Try `gnome-terminal`, fall back to `xterm` (Options 1+2).

**Rationale**: Matches current extension behaviour. gnome-terminal is installed by default on
GNOME. The fallback ensures it works on minimal installs.

**Date**: 2026-02-18

---

### Decision 5: Menu Refresh Strategy

**Context**: After downloading a model, the extension dropdown needs to reflect the new model.

**Options Considered**:
1. **Re-check on every menu open** — Simple, no IPC needed
2. **DBus signal from manager** — Instant update but requires manager to emit signal
3. **Inotify watch on cache dir** — Reactive but complex in GJS
4. **Manual "Refresh" menu item** — User-driven, simple

**Decision**: Re-check on every menu open (Option 1).

**Rationale**:
- The `open-state-changed` signal already fires when the menu opens (extension.js:606)
- `_checkModelInstalled()` is a fast filesystem stat — no meaningful overhead
- No IPC complexity
- Newly downloaded models will appear the next time the user opens the menu

**Date**: 2026-02-18

---

## Dependencies

- **Depends on**: Plan 007 (resource leak fixes) — both touch extension.js and scripts
- **Blocks**: Nothing
- **Related**:
  - `extensions/speech-to-text@fedora-desktop/extension.js`
  - `files/home/.local/bin/wsi-model-manager` (new file)
  - `playbooks/imports/optional/common/play-speech-to-text.yml`

---

## Success Criteria

- [ ] Extension dropdown model section contains only: Auto + locally installed models
- [ ] "Manage Models..." menu item launches `wsi-model-manager` in a terminal window
- [ ] `wsi-model-manager` shows all known Whisper models with install status
- [ ] Downloading a model via the TUI works with visible progress
- [ ] After downloading, the model appears in the extension dropdown on next menu open
- [ ] Removing a model from cache via the TUI works
- [ ] Filter/search in the TUI narrows the model list
- [ ] Script runs without errors when Textual is installed
- [ ] Ansible playbook deploys the script and its dependency (textual)
- [ ] ESLint passes on modified extension.js
- [ ] QA passes on all Python/Bash files

---

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| `textual` not available as RPM/pip on Fedora 42 | High | Low | Install via pip in playbook (already using pip for other deps) |
| `snapshot_download()` API changes in `huggingface_hub` | Medium | Low | Pin compatible version or use fallback to `WhisperModel()` |
| Extension menu rebuild on open causes visual glitch | Low | Medium | Only update label text and visibility, don't rebuild whole menu |
| User has models in non-standard cache location | Low | Low | Document the default cache path; out of scope for v1 |
| `gnome-terminal` not available (e.g., KDE) | Low | Very Low | Fallback to `xterm` in `_openModelManager()` |
| `large-v3-turbo` not in Systran repo under expected name | Medium | Low | Verify repo name before adding to catalogue; skip if absent |

---

## Notes & Updates

### 2026-02-18 — Implementation Complete (Phases 1–4)

All code written and QA/ESLint passing. Awaiting host deployment and testing.

**Files changed:**
- `files/home/.local/bin/wsi-model-manager` — new Python Textual TUI script
- `extensions/speech-to-text@fedora-desktop/extension.js` — dropdown now shows only
  installed models; `_refreshModelSection()` and `_openModelManager()` added; dead
  `_installModel()` removed; `large-v2` and `large-v3-turbo` added to model catalogue
- `playbooks/imports/optional/common/play-speech-to-text.yml` — added `textual` pip dep
  and `wsi-model-manager` deploy task

**Next step (on host):**
```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```
Then log out and back in for extension JS changes to take effect, and test:
1. Dropdown shows only installed models
2. "Download more models..." launches `wsi-model-manager` in terminal
3. After downloading a model, it appears in dropdown on next menu open

### 2026-02-18 — Plan Created

**Research phase complete.** Key findings:
- No suitable existing TUI tool found — need to build `wsi-model-manager`
- Python Textual is the right framework (async-capable, runs in gnome-terminal)
- `snapshot_download()` from `huggingface_hub` is the correct download API
- Extension change is straightforward: filter model items + add manager launcher
- Full model catalogue identified (see Technical Decisions → Decision 2)

**Model Catalogue for `wsi-model-manager` v1:**

| Name | HF Repo | Size | English Only |
|------|---------|------|--------------|
| tiny | Systran/faster-whisper-tiny | ~75MB | No |
| base | Systran/faster-whisper-base | ~142MB | No |
| small | Systran/faster-whisper-small | ~466MB | No |
| medium | Systran/faster-whisper-medium | ~1.5GB | No |
| large-v2 | Systran/faster-whisper-large-v2 | ~3.0GB | No |
| large-v3 | Systran/faster-whisper-large-v3 | ~3.0GB | No |
| large-v3-turbo | Systran/faster-whisper-large-v3-turbo | ~1.6GB | No |
| tiny.en | Systran/faster-whisper-tiny.en | ~41MB | Yes |
| base.en | Systran/faster-whisper-base.en | ~77MB | Yes |
| small.en | Systran/faster-whisper-small.en | ~252MB | Yes |
| medium.en | Systran/faster-whisper-medium.en | ~789MB | Yes |

**Note**: `large-v3-turbo` repo name on HuggingFace needs verification before adding to
catalogue — confirm as `Systran/faster-whisper-large-v3-turbo` or similar.

**Next step**: Verify Textual availability on Fedora 42 and `large-v3-turbo` repo name.
Then proceed to Phase 2 (build the script).
