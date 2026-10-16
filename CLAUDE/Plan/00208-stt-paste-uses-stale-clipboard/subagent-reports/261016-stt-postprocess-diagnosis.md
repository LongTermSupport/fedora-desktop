# Ctrl+Insert / Alt+Insert: how it works, how it fails unseen, what to run on the host

Worker report for Plan 00208 Phase 3 (branch `feat/stt-postprocess-button`). The cause of the
owner's failed Ctrl+Insert is NOT established here: that needs the host commands below.

## How it is meant to work

1. GNOME Shell calls the extension's handler for `toggle-recording-claude` (default
   `<Primary>Insert`, corporate) or `toggle-recording-claude-natural` (`<Alt>Insert`, natural).
   Both are registered in `enable()` with `Main.wm.addKeybinding`, in the NORMAL and OVERVIEW
   action modes only.
2. `panelSession.js` decides what the press means. With nothing running it starts a recorder
   with `--claude-process --claude-model <claude-model> --claude-style <style>`: `wsi` (Go) in
   batch mode, `wsi-stream` (Python) in streaming mode. While a recording runs, ANY of the three
   keys stops it; the style was fixed by the key that started it.
3. After transcription the recorder runs `wsi-claude-process --model M --style S <text>`
   (bash). That reads `~/.config/speech-to-text/claude-prompt-<style>.txt`, picks the account
   from the `claude-token` setting (a named token in `~/.claude-tokens/ccy/tokens/`, or the
   desktop login when empty), and runs `claude -p --model M` with a 170 s limit. The recorder
   waits up to 180 s, with the icon on TRANSCRIBING meanwhile.
4. Claude's text replaces the transcript, is saved to `last-transcription.txt`, and is pasted
   with no Enter after it (auto-paste on) or put on the PRIMARY selection (auto-paste off: a
   middle-click, not Ctrl+V).
5. If Claude fails: nothing is pasted, the raw text goes on the CLIPBOARD, the state is ERROR,
   and a notification that stays says why.

## Every way it can fail unseen (before this branch)

| #   | Failure                                                                                                                                    | What the owner saw                                                                                                                                                                                                                      | Fixed here?                                                     |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| F1  | Claude failed (not logged in, token expired or missing, `claude` not on the session PATH, prompt missing, timeout, empty answer)           | **Nothing**: Show Notifications is off by default, so the panel passes `--no-notify`, and both recorders dropped the "stays" notification. Only a 2 s ERROR flash, the menu status line (first 30 characters) and the debug log (if on) | Yes (Task 3.1): defect, test-first                              |
| F2  | The shell refused the key (`addKeybinding` answered NONE: the name is already registered, for example) or the key setting is empty         | Nothing at all on the press. The registration result went only to the debug log, which is off by default                                                                                                                                | Yes (Task 3.2): notification at enable                          |
| F3  | Another owner of `<Primary>Insert` or `<Alt>Insert` (a custom shortcut, another extension), or a keyboard layout that sends something else | Nothing, or the other action                                                                                                                                                                                                            | No: host check H3                                               |
| F4  | The recording was started with Insert and **stopped** with Ctrl+Insert                                                                     | Raw text, no post-processing (the stop key does not choose the style)                                                                                                                                                                   | Documented (docs, "The style is chosen by the key that starts") |
| F5  | A recorder was still running or finishing (or one the panel did not start, after a screen lock)                                            | The press stopped that one, or flashed the no-entry sign, instead of starting a Claude recording                                                                                                                                        | Behaves as designed; host check H6                              |
| F6  | Auto-paste off                                                                                                                             | Claude's text is on PRIMARY (middle-click); Ctrl+V pasted the old clipboard                                                                                                                                                             | Documented                                                      |
| F7  | Claude is slow (up to 180 s)                                                                                                               | Orange "…" transcribing for minutes; easy to take for "nothing happened"                                                                                                                                                                | No; the menu items now say "started" at once                    |
| F8  | The extension was not reloaded after a deploy (Wayland: only a log-out reloads it), or failed to load                                      | Old code, or no keys at all                                                                                                                                                                                                             | No: host check H1                                               |
| F9  | Settings' "Enable Claude processing" switch (`claude-enabled`) is read but gates nothing                                                   | Misleading only; turning it off does not stop Ctrl+Insert                                                                                                                                                                               | No: Task 3.7                                                    |

## Host commands (read-only, as the login user, in the desktop session)

H9 calls Claude once with a test sentence; everything else only reads.

```bash
# H1 the extension is loaded and active (State: ACTIVE, not ERROR or OUT OF DATE)
gnome-extensions info speech-to-text@fedora-desktop | cat

# H2 its settings: the three record keys, claude-model, claude-token, show-notifications,
#    auto-paste, streaming-mode (wsi-stream if true, else wsi), debug-mode
gsettings --schemadir ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas \
    list-recursively org.gnome.shell.extensions.speech-to-text | cat

# H3 any other binding on Ctrl+Insert or Alt+Insert (a second owner of the key)
gsettings list-recursively | grep -i -E "<(primary|control|ctrl|alt)>insert" | cat
dconf dump / | grep -n -i -E "<(primary|control|ctrl|alt)>insert" | cat

# H4 what GNOME Shell logged this boot about the extension and key bindings
journalctl --user -b --no-pager -o short-iso | grep -i -E "speech-to-text|STT|keybind" | cat
journalctl -b --no-pager -o short-iso _COMM=gnome-shell | grep -i -E "speech-to-text|STT|keybind" | cat

# H5 the debug log (only written while Debug Logging is on): did the press arrive, was the
#    key registered, what was launched, what Claude said
grep -n -E "INSERT PRESSED|keybinding registration result|Launching (claude|dictation)|claude-process|Claude|Insert does nothing|Insert while" \
    ~/.local/share/speech-to-text/debug.log ~/.local/share/speech-to-text/debug.log.old 2>&1 | cat

# H6 a recorder still running or stopping, which a press would stop instead of starting one
pgrep -a -u "$USER" -f '/\.local/bin/[w]si' | cat
ls -l --time-style=full-iso /dev/shm/stt-recording-"$USER".pid /dev/shm/stt-stopping-"$USER".pid 2>&1 | cat

# H7 the last transcription and when it was written (did that dictation finish at all)
ls -l --time-style=full-iso ~/.cache/speech-to-text/ | cat

# H8 the post-processor's pieces, as the desktop session sees them
SHELL_PID=$(pgrep -o -u "$USER" -x gnome-shell); echo "gnome-shell pid: $SHELL_PID"
tr '\0' '\n' < /proc/"$SHELL_PID"/environ | grep '^PATH=' | cat
SESSION_PATH=$(tr '\0' '\n' < /proc/"$SHELL_PID"/environ | grep '^PATH=' | cut -d= -f2-)
env PATH="$SESSION_PATH" bash -c 'command -v claude wsi-claude-process' | cat
ls -l ~/.local/bin/wsi-claude-process ~/.config/speech-to-text/ | cat
~/.local/bin/wsi-setting claude-token

# H9 the post-processor end to end, once, with the session's PATH (calls Claude once)
env PATH="$SESSION_PATH" ~/.local/bin/wsi-claude-process --model haiku --style corporate \
    "this is a test of the post processing step"; echo "exit=$?"
```

## Reading the results

- H1 not ACTIVE, or H4 shows an extension error: F8.
- H5 has no `CTRL+INSERT PRESSED` for the attempt while Debug Logging was on: the press never
  reached the extension (F2, F3; H3 names another owner). A registration result of `0` is F2.
- H5 has the press and `Insert while RECORDING` or `Insert does nothing`: F4 or F5.
- H5 has the launch with `--claude-process` and then a `Claude` error, or H9 exits 1: F1, and
  H9's message is the cause (not logged in, no unexpired token, `claude CLI not found in PATH`,
  no prompt template, timeout).
- H9 prints rewritten text and exit 0, and H5 shows a successful Claude run: the pipeline
  worked; check auto-paste (H2) for F6.

## Part 2 choices

- **Where the result goes**: the CLIPBOARD (Ctrl+V), through `St.Clipboard`, with a notification
  showing the start of it. Not a paste: a click in the panel menu leaves no window the panel can
  be sure the owner meant, and the recorders' own Claude-failure path already uses the clipboard.
- **Raw kept apart**: the result is saved to `~/.cache/speech-to-text/last-postprocessed.txt`;
  `last-transcription.txt` is not touched, so Copy Last Transcription keeps giving the
  transcription. The input is that file, less a `speech-to-text:"…"` marker. One limit of the
  existing design remains: after a Ctrl+Insert dictation that file holds Claude's text, since the
  recorders save the final text, so a menu run then works on that.
- **Where the logic lives**: Go (`wsi-postprocess`); the extension only spawns it asynchronously,
  sets the clipboard and notifies (`postProcess.js`, tested under Node).
