# Independent search: dropdown option text that carries explanation

Judged against HEAD of `extensions/speech-to-text@fedora-desktop/prefs.js` (the working tree has an uncommitted change to this file, 16 insertions / 10 deletions; not judged).

## Forms expected before searching

1. Literal label arrays passed to a combo-row helper (Adw.ComboRow + Gtk.StringList).
2. Label constants (catalogues) whose display field is later pushed into a list.
3. Labels pushed at runtime (`labels.push(...)`, template strings adding suffixes).
4. Gtk.DropDown / ComboBoxText / ListStore in Python or JS.
5. PopupMenu sub-menus / radio-style item lists in panel extensions.
6. zenity / yad / gum / fzf choice lists in shell scripts.
7. argparse `choices=` (not a GUI; checked only to rule out).

## Instances (all in one file, one helper)

All reach the screen through `_addComboRow` (HEAD lines 257-283): `new Adw.ComboRow` + `Gtk.StringList.append(label)`, so every label is the displayed selected value in the row's fixed-width control. Technique: text search found the helper and call sites; reading found the runtime-built entries and the constant.

| #   | HEAD line | Row                     | Label                                                                                         | Chars                                                               | Form                                                                               | Technique                                                                                               | Confidence                                                                         |
| --- | --------- | ----------------------- | --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| 1   | 68        | Startup mode            | `Standard — load then start (~3-6s)`                                                          | 34                                                                  | em-dash clause + timing                                                            | text + read                                                                                             | confident (originating instance)                                                   |
| 2   | 69        | Startup mode            | `Pre-buffer — record while loading (~2-4s)`                                                   | 41                                                                  | em-dash clause + timing                                                            | text + read                                                                                             | confident                                                                          |
| 3   | 70        | Startup mode            | `Server mode — persistent server (<0.5s, uses more memory)`                                   | 57                                                                  | em-dash clause + timing + caveat                                                   | text + read                                                                                             | confident                                                                          |
| 4   | 183       | Whisper Model           | `Auto (GPU: Distil Large v3.5 for English, else Large v3 Turbo)`                              | 62                                                                  | parenthetical rule, pushed at runtime in `_buildInstalledModelList`                | read (text search for `ComboRow` alone does not show it; it is not in a literal array at the call site) | confident, longest label                                                           |
| 5   | 121       | Paste shortcut          | `Ctrl+Shift+V (default — correct for terminals)`                                              | 46                                                                  | parenthetical + em-dash                                                            | text + read                                                                                             | confident                                                                          |
| 6   | 122       | Paste shortcut          | `Ctrl+V (correct for browsers and most GUI apps)`                                             | 47                                                                  | parenthetical explanation                                                          | text + read                                                                                             | confident                                                                          |
| 7   | 148       | Claude model            | `Sonnet — balanced speed and quality`                                                         | 35                                                                  | em-dash clause                                                                     | text + read                                                                                             | confident                                                                          |
| 8   | 149       | Claude model            | `Opus — best quality, slower`                                                                 | 27                                                                  | em-dash clause                                                                     | text + read                                                                                             | likely truncates / still carries explanation; unsure on pixel width                |
| 9   | 150       | Claude model            | `Haiku — fastest`                                                                             | 15                                                                  | em-dash clause (short)                                                             | text + read                                                                                             | carries explanation but short; unlikely to truncate. Unsure                        |
| 10  | 194       | Whisper Model (runtime) | `<label> — not installed` appended, e.g. `Distil Large v3.5 English (~1.5GB) — not installed` | 50 for that case (39 for `Large v3 Turbo (~1.6GB) — not installed`) | status clause appended at runtime to a label that already has a size parenthetical | read only (template string)                                                                             | confident it is the same class; only appears when the saved model is not installed |

### WHISPER_MODELS display labels (HEAD lines 17-28), shown as the model option text

These carry a size in a parenthetical. Technique: read (the constant is far from the ComboRow; text search for `ComboRow` does not hit it).

| Line | Label                                | Chars |
| ---- | ------------------------------------ | ----- |
| 17   | `Tiny (~75MB)`                       | 12    |
| 18   | `Base (~142MB)`                      | 13    |
| 19   | `Small (~466MB)`                     | 14    |
| 20   | `Medium (~1.5GB)`                    | 15    |
| 21   | `Large v2 (~3GB)`                    | 15    |
| 22   | `Large v3 (~3GB)`                    | 15    |
| 23   | `Large v3 Turbo (~1.6GB)`            | 23    |
| 24   | `Tiny English (~41MB)`               | 20    |
| 25   | `Base English (~77MB)`               | 20    |
| 26   | `Small English (~252MB)`             | 22    |
| 27   | `Medium English (~789MB)`            | 23    |
| 28   | `Distil Large v3.5 English (~1.5GB)` | 34    |

Judgement: names with a size suffix. Size is metadata, not a sentence, so these are the mildest members; the 34-char Distil label is the one likely to truncate. Unsure whether the size suffix counts as "explanation"; flag as borderline (12 entries, counted as one instance group, #11).

### Short names (not instances)

- Language row, lines 48-49: `System default` (14), `English` (7).
- Subtitles/descriptions elsewhere carry the explanation correctly (e.g. `'Only downloaded models shown — use "Manage Whisper Models" ...'` is a subtitle, not an option).

## Instance count

Confident: 8 labels across 4 rows (Startup mode 3, Whisper auto 1, Paste shortcut 2, Claude model sonnet/opus 2; haiku borderline) plus the runtime `— not installed` suffix. Counting by row: 4 of the 5 combo rows in HEAD (Startup mode, Whisper Model, Paste shortcut, Claude model) carry explanation in option text; only Language is clean. Borderline: Haiku (15), the size-suffixed model catalogue (12 entries).

## Evidence about width / truncation

- None found in the repo: no CSS, no `width_chars`, `max_width_chars`, `ellipsize` or `set_default_size` constraint on the combo, apart from `window.set_default_size(600, 700)` at HEAD line 34 for the preferences window.
- The owner's report ("all options are truncated") is the only concrete evidence that 34-62 char labels truncate in this window. I did not find libadwaita documentation in the repo, and did not guess at pixel widths. From that report, the shortest confirmed-truncating label is bounded above by 34 chars (the originating instance; the owner said all options were truncated, which includes the 34-char one).

## Other places searched (no instance)

- `extensions/speech-to-text@fedora-desktop/extension.js`: PopupMenuItem / PopupSwitchMenuItem items only (lines 327-379); no sub-menu of choices.
- `extensions/fedora-desktop@fedora-desktop/` (extension.js, sections/plays.js, health.js, containers.js): PopupMenuItem actions and status lines; no PopupSubMenuMenuItem anywhere in the repo, no choice list.
- `dock-recovery-on-unlock`, `remote-desktop-toggle`, `workspace-names-overview` extensions: no Adw/Gtk/dropdown use found.
- `files/home/.local/bin/wsi-article-window` (Python, Gtk 4): uses Button, Label, Entry, Notebook; no DropDown/ComboBox. `choices=["sonnet","opus","haiku"]` at line 495 is argparse, plain names.
- `files/home/.local/bin/wsi-model-manager` (Python TUI, Textual DataTable): a table, not a fixed-width dropdown.
- Shell pickers `ftp-camera` (lines 234-243, gum/fzf mode list with sentence-length rows such as `default      [+viewer]  Start FTP server, sort at end on Ctrl+C`), `open`, `lxcfreeze`, `freeze-common.bash`: terminal full-width lists; the explanation-in-option form is present in `ftp-camera` but the control is a TUI list, not a fixed-width selected-value control, so out of the stated class. Mentioned for completeness; unsure.
- zenity: only an install package entry in the speech-to-text playbook and docs; no zenity `--list` invocations.
- `files/var/local/claude-yolo`, `files/usr/local/bin/RapidRAW` matched on generic words only; not GUI choice lists.

## Searches run

- `ComboRow|Gtk\.DropDown|StringList|ComboBoxText|PopupSubMenu|zenity|yad |Gtk\.ComboBox|dropdown|Dropdown|PopupMenuItem|PopupBaseMenuItem|PopupSwitch|set_model|Gtk\.ListStore` over the whole repo excluding the keep-out directories and node_modules.
- `ComboRow|StringList|DropDown|ComboBox|model|—` over HEAD prefs.js.
- Listing of every file under `extensions/` containing `prefs|Adw\.|Gtk\.`.
- Listing of every file under `files/` containing `gi.repository|import gi|Gtk|tkinter|textual|curses|questionary|inquirer|fzf|whiptail|dialog|select`, then shebang and Gtk/menu grep of each hit.
- `gum choose|gum filter|fzf` over `files/home/.local/bin` and `.local/lib`.

## What text search found that reading could not have checked

The exhaustiveness claim: every `Adw.ComboRow`/`StringList`/`PopupSubMenu` site in the whole repo (only `_addComboRow` exists), and the absence of Gtk dropdowns in other scripts and extensions.

## What reading found that text search could not have

- The Whisper `Auto (...)` label (62 chars) is assigned inside `_buildInstalledModelList`, not at the call site; the model-label catalogue `WHISPER_MODELS` (display labels in tuple position 3) is only identifiable as option text by following the data into `_addComboRow`.
- The runtime `${label} — not installed` suffix.
- That `Language` is the only clean row, and which long strings are subtitles (correct) versus option text.
