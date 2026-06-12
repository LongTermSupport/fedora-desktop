# Extensions Audit

## Scope & Method

This audit covers the GNOME Shell extension code and its supporting tooling in the public `fedora-desktop` repository:

- `/workspace/extensions/speech-to-text@fedora-desktop/` — `extension.js` (1,346 lines), `prefs.js`, `metadata.json`, `schemas/`
- `/workspace/extensions/remote-desktop-toggle@fedora-desktop/` — `extension.js`, `metadata.json`
- `/workspace/extensions/workspace-names-overview@fedora-desktop/` — `extension.js`, `metadata.json`, `stylesheet.css`, `README.md`
- `/workspace/extensions/scripts/gnome-shell-extract-js.bash`, `/workspace/extensions/tests/`, `.eslintrc.json`, `package.json`, `extensions/CLAUDE.md`
- Deployment playbooks: `/workspace/playbooks/imports/optional/common/play-speech-to-text.yml`, `/workspace/playbooks/imports/optional/common/play-remote-desktop-toggle.yml`, `/workspace/playbooks/imports/play-gnome-shell-extensions.yml`
- QA wiring: `/workspace/scripts/qa-all.bash`, `/workspace/scripts/git-hooks/pre-commit`

Method: enumerated every tracked file under `extensions/` (`git ls-files`), read all three `extension.js` files and `prefs.js` in full, read all `metadata.json`, the gschema XML, the ESLint config, the extract script, and the deployment tasks in the three playbooks. Ran `node_modules/.bin/eslint .` from `extensions/` (passes, exit 0). Cross-checked the extension's process-control assumptions against `files/home/.local/bin/wsi`. Hunted specifically for: missing `disable()` cleanup, pre-GNOME-45 import style, subprocess bugs, key-press races, memory leaks, shell-version drift versus Fedora 43 (GNOME 49), ESLint coverage gaps, and missing tests.

## Summary

The extensions are generally well built: all three use the modern GNOME 45+ ESM import style, follow the project's "thin wrapper" architecture (business logic lives in `~/.local/bin` scripts), and the remote-desktop-toggle extension is a model of correct cleanup. No pre-45 import style, no blocking `communicate()`/`wait()` calls, and no zombie-process patterns were found; `metadata.json` for all three extensions includes shell-version `49` (Fedora 43's GNOME).

The notable issues are: ESLint is documented as mandatory but is not enforced by `qa-all.bash` or the pre-commit hook (EXT-01); an inconsistently applied shell-argument sanitisation that leaves the `language` setting uninterpolated-unsafe inside a `bash -c "..."` string (EXT-02); a real start/stop race between the Insert keybinding and the asynchronous DBus state machine, compounded by `wsi` unconditionally overwriting its PID file (EXT-03); and several deliberate-but-undocumented silent `catch {}` blocks that contravene the repository's number-one fail-fast rule (EXT-04). The remainder are lower-severity polish items: synchronous file I/O on the shell thread, incomplete actor cleanup if disabled mid-recording, a stale committed `gschemas.compiled` binary, documentation contradictions, and a complete absence of tests.

## EXT-01: ESLint is mandated by docs but not wired into any QA gate

**Severity: medium — area: scripts/extensions**

`extensions/CLAUDE.md` declares ESLint "MANDATORY... after making ANY changes to extension JavaScript files" because "blocking operations freeze GNOME Shell completely... users may need to hard reboot". `CLAUDE/QA.md` repeats: "Run ESLint before EVERY commit that touches extension JavaScript". Yet:

- `/workspace/scripts/qa-all.bash` runs only `qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`, and `qa-ansible.bash` — no ESLint step (verified by reading the full script).
- `/workspace/scripts/git-hooks/pre-commit` contains zero references to `eslint` (`grep -c eslint` → 0).

So the only QA check capable of catching a shell-freezing `proc.wait(null)` is on the honour system. The custom `no-restricted-syntax` rules in `extensions/.eslintrc.json` (blocking `communicate`, `communicate_utf8`, `wait`, `wait_check`) are exactly the kind of high-value, cheap check that should be enforced automatically. ESLint and its config are already vendored in `extensions/node_modules/` and `eslint .` currently passes in under a second.

**Impact**: a future commit touching extension JS can silently introduce a blocking call that freezes GNOME Shell on Wayland, where recovery requires a hard reboot — the precise failure mode the lint rules were written to prevent.

**Recommendation**: add a `qa-js.bash` (or an ESLint stage in `qa-all.bash`) that runs `node_modules/.bin/eslint .` in `extensions/` when extension `.js` files exist/changed, and invoke it from `scripts/git-hooks/pre-commit` for staged `extensions/**/*.js` files. Fail fast if `node_modules/.bin/eslint` is absent (per the Missing Dependencies rule, install via the relevant playbook, not ad hoc).

## EXT-02: `language` setting bypasses the extension's own shell-argument sanitisation inside `bash -c`

**Severity: medium — area: extensions**

`extensions/speech-to-text@fedora-desktop/extension.js` defines `_validateShellArg()` (lines 756–764) and conscientiously applies it to `whisper-model` (lines 810–814, 884–888) and `claude-model` (lines 870–873, 922–925) "to prevent shell injection". But the `language` value is interpolated with no validation at all:

```js
// line 791 (and 856 in _launchWSIClaude):
const langFlag = ` --language ${this._getWhisperLanguage()}`;
...
// line 816 (and 890): when whisper-model !== 'auto':
command = `/bin/bash -c "WHISPER_MODEL=${safeModel} ${scriptPath}${scriptArgs}"`;
```

`_getWhisperLanguage()` (lines 1214–1221) returns the raw `language` GSettings string (type `s`, free-form per the gschema at `schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml`, key `language`). In the default branch the command goes through `GLib.spawn_command_line_async` (g_shell_parse_argv — no expansion, limited risk), but in the non-`auto` model branch the entire argument string is embedded inside a **double-quoted `bash -c` string**, where a `language` value containing `$(...)`, backticks or `"` achieves arbitrary command execution in the user session.

**Impact**: limited in practice — writing the dconf key already requires code execution as the user — but it is an inconsistent application of the extension's own defence-in-depth, and the `--paste-with-shift`/`--claude-style` values show the authors intended every interpolated value to be constrained. The same gap exists in both `_launchWSI` and `_launchWSIClaude` (duplicated ~60-line blocks).

**Recommendation**: pass `this._getWhisperLanguage()` through `_validateShellArg()` with an allow-list (or at minimum the metacharacter strip), and prefer `Gio.Subprocess.new([scriptPath, ...argv], ...)` with an `env` override instead of string-built `bash -c` commands, which eliminates the entire class. Factor the duplicated flag-building out of the two launch methods while doing so.

## EXT-03: Start/stop race — second Insert press during start-up spawns a second recorder that clobbers the PID file

**Severity: medium — area: extensions**

The toggle logic in `_launchWSI` (extension.js lines 768–774) decides start-vs-stop from `this._currentState`, which is only updated when the spawned `wsi`/`wsi-stream` script emits a `StateChanged` DBus signal (lines 419–439). Between `GLib.spawn_command_line_async(...)` (line 823) and the arrival of `PREPARING`/`RECORDING` — hundreds of milliseconds to several seconds for the Python streaming path — `_currentState` is still `IDLE`, so a second Insert press launches a **second** instance instead of stopping the first.

The companion script makes this worse: `files/home/.local/bin/wsi` writes its PID file unconditionally with no liveness check on an existing one:

```bash
# files/home/.local/bin/wsi:612
echo $$ > "$PID_FILE"
```

So the second instance overwrites `/dev/shm/stt-recording-$USER.pid`, and the extension's `_stopRecording` (extension.js lines 993–1013) then kills only the second PID. The first recorder keeps running (microphone open, eventually transcribing and possibly auto-pasting) until the broad `pkill -f` fallbacks happen to fire. The `pkill` fallbacks (lines 1010–1012, 1018–1020, 1037–1039) are themselves pattern-based kills that take out *all* matching user processes.

**Impact**: double-tap of Insert (easy under latency) produces overlapping recordings, a stuck red icon, an orphaned recorder, and surprise pasted text. No data loss, but a real, reproducible defect in the primary interaction path.

**Recommendation**: guard launch with a local debounce flag (set on spawn, cleared on first DBus signal or a timeout), and make `wsi` fail fast if `$PID_FILE` exists and `kill -0 $(cat $PID_FILE)` succeeds (the fail-fast rule applies to the script anyway). Either fix alone substantially closes the race; both together close it fully.

## EXT-04: Silent empty `catch` blocks contravene the project fail-fast rule

**Severity: medium — area: extensions**

The repository's number-one rule is "No silent failures — every error must stop execution with clear message", and the hooks daemon explicitly blocks "empty catch blocks that swallow exceptions" in new JS. The tracked extension code contains several:

- `extension.js:225–227` — keybinding removal in `disable()`: `catch (e) { // Ignore if keybinding doesn't exist }`
- `extension.js:1063–1065` — `_ensureLogDirectory`: `catch (e) { // Silent fail - will be created by wsi script }`
- `extension.js:1198–1200` and `1208–1210` — `_saveDebugSetting`/`_saveAutoPasteSetting`: `catch (e) { // Setting may not exist yet }`
- `extension.js:1244–1246` — `_log`: `catch (e) { // Silent fail for logging }`
- `prefs.js:200–202` — prompt-file open: `catch (_e) { // No default text editor configured — ignore silently }`
- `workspace-names-overview@fedora-desktop/extension.js:105–107` — `catch (e) { // Ignore cleanup errors }`

Nuance: in GNOME Shell, *throwing* from these paths would be worse (an uncaught exception in `disable()` can wedge the shell), so "catch" is correct — but "catch silently" is not. Each has a one-line remediation: `logError(e, 'context')` or `log(...)`, which surfaces the failure in `journalctl /usr/bin/gnome-shell` without destabilising the shell. The `_log` self-logging case is the only one where swallowing is arguably unavoidable, and it should say so via a `// FAIL-FAST-OK:`-style annotation so audits and the error-hiding hook can distinguish deliberate from lazy.

**Recommendation**: replace every empty catch with `logError(e, '<context>')` (or `console.warn` on 45+), and annotate the genuinely-unavoidable swallow in `_log` with an explicit justification comment.

## EXT-05: Synchronous file I/O on the GNOME Shell main thread

**Severity: low — area: extensions**

`extensions/CLAUDE.md` lists "Using Synchronous File Operations" as Common Mistake #1, yet `speech-to-text` does synchronous I/O on the compositor thread:

- `_log` (extension.js lines 1229–1243): for **every log line** — `query_exists`, `query_info('standard::size')`, possible `file.move` rotation, `append_to`, `write_all`, `close`, all synchronous. With debug mode on, the countdown timer logs once per second (line 674) and flashing/status changes log more, so this runs continuously during recording.
- `_stopRecording` (line 1000) and `_copyLastTranscription` (line 1311): synchronous `file.load_contents(null)`. The PID file on `/dev/shm` is harmless; the transcription file is unbounded user content from `~/.cache`.

**Impact**: micro-stutters at worst on a healthy SSD; a frozen shell at worst on a hung filesystem. It is a guidance violation in the project's own words rather than an observed defect.

**Recommendation**: buffer log lines and flush via `GLib.idle_add`/async stream writes (or simply `log()` to the journal when debug is on, which is what `journalctl -f /usr/bin/gnome-shell` workflows expect), and use `load_contents_async` for the transcription read.

## EXT-06: `disable()` during an active recording leaks the detached `_iconBox` and skips countdown teardown

**Severity: low — area: extensions**

During recording, `_startCountdown` (extension.js lines 624–626) and `_startElapsedTimer` (lines 951–953) **remove `_iconBox` from the indicator** and add `_countdownLabel` instead. `disable()` (lines 208–262) cancels the timers and destroys `this._indicator`, which destroys its current children (`_countdownLabel`) — but the detached `_iconBox` (containing `_icon` and `_serverStatusDot`) has no parent at that point and is never explicitly destroyed; nor are `_icon`, `_iconBox`, `_countdownLabel` or `_serverStatusDot` nulled. `_stopCountdown()` (lines 725–754), which knows how to restore/clean these, is not called from `disable()`.

**Impact**: per the GJS review guidelines, actors removed from the stage should be explicitly `destroy()`ed in `disable()`; relying on GC finalisation of detached Clutter actors is the canonical extension-review memory-leak pattern. Practical impact is small (one actor tree per disable-while-recording), but lock-screen `disable()`/`enable()` cycles can hit this path repeatedly.

**Recommendation**: in `disable()`, call `this._stopCountdown()` before destroying the indicator, then explicitly `this._iconBox?.destroy()` if unparented, and null all actor references (`_icon`, `_iconBox`, `_serverStatusDot`, `_countdownLabel`, `_statusLabel`).

## EXT-07: Article-mode spinner can animate forever if the helper fails after launch; `_elapsedSeconds` is dead state

**Severity: low — area: extensions**

`_launchArticleMode` (extension.js lines 905–942) starts the panel spinner (`_startElapsedTimer`, lines 944–974) immediately after `GLib.spawn_command_line_async` succeeds. The spinner is only stopped by a DBus state transition (`_updateIconState` → `_stopCountdown`) or by `disable()`. If `wsi-article-window` launches but exits before emitting any DBus signal (missing Python dep, crash on startup — `spawn_command_line_async` only reports fork/exec failure, not early exit), the spinner animates indefinitely and `_isArticleMode` stays `true`, which suppresses the normal RECORDING icon for subsequent regular recordings (lines 560–569). Separately, `_elapsedSeconds` (lines 69, 607–608) is initialised and reset but never incremented or displayed — dead code.

**Recommendation**: add a watchdog timeout (e.g. 15 s without any DBus signal → reset to IDLE and notify), or spawn via `Gio.Subprocess` and reset state in the completion callback. Delete `_elapsedSeconds`.

## EXT-08: workspace-names-overview verified against GNOME 48.7 but shipped for 49, plus deprecated `schema` construct property

**Severity: low — area: extensions**

`workspace-names-overview@fedora-desktop/extension.js` header (lines 5–8) states "API paths verified against GNOME Shell 48.7 source", and the code depends entirely on private internals: `Main.overview._overview._controls`, `controls._thumbnailsBox._thumbnails`, `display._workspacesViews[i]._thumbnails._thumbnails`, `display._primaryIndex` (lines 50–73). `metadata.json` declares `"shell-version": ["45", "46", "47", "48", "49"]`, and the F43 branch targets GNOME 49 — but nothing records a re-verification against 49. The failure mode is benign (the `try/catch` at line 75 logs and labels simply do not appear), so this is verification drift rather than a crash risk. Additionally, line 19 uses the long-deprecated construct property `new Gio.Settings({schema: WM_PREFS_SCHEMA})` — should be `schema_id`. Minor behavioural nit: labels are only created on overview `shown`, so renaming a workspace or adding one while the overview is open shows stale/missing labels until reopened.

**Recommendation**: re-run `extensions/scripts/gnome-shell-extract-js.bash` on a Fedora 43 host, confirm the four private paths in the 49.x `workspacesView.js`/`workspaceThumbnail.js`, update the header comment, and change `schema:` to `schema_id:`.

## EXT-09: Stale generated binary `gschemas.compiled` tracked in git

**Severity: low — area: extensions**

`extensions/speech-to-text@fedora-desktop/schemas/gschemas.compiled` is committed (in `git ls-files`), last regenerated in commit `24c6f940`, while the source XML has been modified in later commits (`e749077d`, `81f3d711`, `1a3fa423`) — the tracked binary therefore does not match the tracked XML. It is also redundant: `play-speech-to-text.yml` copies only the XML (line 357) and runs `glib-compile-schemas` on the target (line 366), so the committed binary is never deployed. A stale committed artefact invites confusion if anyone copies the extension directory wholesale (as the workspace-names playbook does for that extension).

**Recommendation**: `git rm` the file and add `schemas/gschemas.compiled` to `extensions/.gitignore`; rely on deploy-time compilation, which is already in place.

## EXT-10: Documentation contradictions in `extensions/CLAUDE.md`

**Severity: low — area: docs**

- `extensions/CLAUDE.md` instructs (three times) `cd /workspace/extensions && npm run lint`, while `CLAUDE/QA.md` explicitly says "Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks)". An agent following the extensions doc gets blocked or warned by the `npm_command` hook.
- The "Extension Structure" section is outdated: it shows `wsi` and `wsi-transcribe` living inside `speech-to-text@fedora-desktop/` (they live in `files/home/.local/bin/`) and omits the other two extensions entirely.
- The deployment section names only `play-speech-to-text.yml`; `remote-desktop-toggle` is deployed by `play-remote-desktop-toggle.yml` (explicit file loop, lines 90–97) and `workspace-names-overview` by `play-gnome-shell-extensions.yml` (whole-directory copy) — worth indexing so the "add new file → update playbook loop" checklist is applied to the right playbook.

**Recommendation**: align the lint command with `CLAUDE/QA.md` (`node_modules/.bin/eslint .`), and refresh the structure/deployment sections to cover all three extensions.

## EXT-11: No automated tests for any extension; ESLint 8 is end-of-life

**Severity: info — area: extensions**

`extensions/tests/` contains only `.gitkeep` (the `assets/` dir is gitignored), so there are zero tests for ~1,700 lines of extension JavaScript — including pure, eminently testable logic such as `_validateShellArg`, `_getWhisperLanguage`, `_getPasteWithShift` list parsing, and `prefs.js` `_buildInstalledModelList`. The only automated check is ESLint, pinned to `eslint ^8.57.1` (`extensions/package.json`), which reached end-of-life in October 2024 and uses the legacy `.eslintrc.json` format removed in ESLint 9.

**Recommendation**: extract the pure helpers into an importable module and cover them with a lightweight Node test runner (`node --test`), and plan a migration to ESLint 9 flat config (porting the valuable `no-restricted-syntax` blocking-call rules verbatim).

## EXT-12: remote-desktop-toggle async callback can touch the toggle after `destroy()`

**Severity: low — area: extensions**

`remote-desktop-toggle@fedora-desktop/extension.js`: `runRdt('status', ...)` (lines 74–79) and the click handler (lines 86–95) complete via `communicate_utf8_async`. If the extension is disabled (toggle `destroy()`ed, lines 98–104) while a subprocess is in flight — easy with a 10 s poll plus lock-screen disable — the callback then sets `this.checked` / calls `Main.notify` on a disposed GObject, producing "object disposed" warnings in the journal. The subprocess itself is not cancelled either.

**Recommendation**: create a `Gio.Cancellable` in `_init`, pass it to `communicate_utf8_async`, cancel it in `destroy()`, and guard the callbacks with an `if (this._destroyed) return;` flag. This is the standard GJS pattern and the only cleanup gap in an otherwise exemplary extension.

## EXT-13: workspace-names deployment copies all files with mode 0755 and no owner/group

**Severity: low — area: playbooks**

`playbooks/imports/play-gnome-shell-extensions.yml` ("Deploy Custom Extension - Workspace Names in Overview", lines ~51–57) copies the whole extension directory with a single `mode: '0755'` and no `owner:`/`group:`. Result: `extension.js`, `metadata.json`, `stylesheet.css` and `README.md` are deployed executable, deviating from the AnsibleStyle rule "Always set `owner:`, `group:`, `mode:` on every file task" and from the sibling playbooks (`play-speech-to-text.yml` lines 343–353 and `play-remote-desktop-toggle.yml` lines 90–97 both use explicit file loops with `mode: '0644'` and owner/group). The whole-directory copy also deploys `README.md` unnecessarily and would deploy any stray file added to the source dir.

**Recommendation**: switch to the explicit file-loop pattern used by the other two extension playbooks (`extension.js`, `metadata.json`, `stylesheet.css`; `mode: '0644'`, `owner`/`group: "{{ user_login }}"`), with a `file` task for the directory at `0755`.

## Positive Observations

- **Modern import style everywhere**: all three extensions and `prefs.js` use GNOME 45+ ESM imports (`import ... from 'resource:///...'`, `gi://`); no legacy `imports.ui.*` anywhere.
- **Thin-wrapper architecture honoured**: extensions delegate all business logic to `~/.local/bin` scripts and read state via DBus/files, exactly as `extensions/CLAUDE.md` prescribes — minimising logout-requiring changes.
- **remote-desktop-toggle is near-exemplary**: fully async subprocess handling with error paths and exit-code checks (`runRdt`, lines 22–47), timer removed in `destroy()`, indicator and quick-settings items destroyed in `disable()`, and a `_busy` guard against poll/click races.
- **speech-to-text `disable()` is thorough** for the common path: DBus subscriptions, three keybindings, the dynamic abort keybinding, icon-reset/recording/flash timers, server polling, the settings `changed` handler and the indicator are all torn down (lines 208–262).
- **The custom ESLint blocking-call rules** (`communicate`/`communicate_utf8`/`wait`/`wait_check` banned via `no-restricted-syntax`) are a genuinely valuable, project-specific guard against Wayland shell freezes — and the codebase currently passes `eslint .` cleanly.
- **Shell-injection awareness exists**: `_validateShellArg` allow-listing for model values, and clipboard copy via `Gio.Subprocess` argv (line 1325) explicitly to avoid shell interpolation.
- **Shell-version metadata is current**: all three `metadata.json` files include `"49"`, matching Fedora 43's GNOME, and the speech-to-text schema is compiled at deploy time rather than relying on a shipped binary.
- **`gnome-shell-extract-js.bash`** is a clean, fail-fast (`set -euo pipefail`) tool that pins API research to the exact installed shell version — strong support for the "never guess APIs" rule.
