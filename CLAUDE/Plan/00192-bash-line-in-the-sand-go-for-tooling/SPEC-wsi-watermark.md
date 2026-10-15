# wsi and watermark: specification of the Go ports

Programme P8 of [INVENTORY.md](INVENTORY.md), the two tools left after `ftp-camera`: `wsi`, the
batch speech-to-text recorder the panel extension starts, and `watermark`, the image
watermarking command. **Method: the legacy bash is not an oracle.** This document states the
intended behaviour of each as numbered, testable expectations (`W*` for wsi, `M*` for
watermark). The bash (`files/home/.local/bin/wsi`, 1430 lines, and
`files/usr/local/bin/watermark`, 402 lines, at `d4e82ea6`) was read only to find the
requirements; where it was wrong, the expectation states the correct behaviour and section 5
lists the departure (`B*`). The proof is the Go tests in `src-go/internal/wsi` and
`src-go/internal/watermark` (every flow over a fake runner, fake timers and temporary trees)
and `scripts/test-wsi-stop-grace.bash`, which now builds and drives the Go `wsi` end to end
with stub audio tools. What only a desktop session can prove is section 7.

## 1. Role and callers

`wsi` is started by the speech-to-text panel (`extensions/speech-to-text@fedora-desktop`) for a
batch dictation, through `setsid` with no shell: it records with `pw-record`, resamples with
`sox`, transcribes with `faster-whisper-transcribe` (or a whisper.cpp server), and puts the text
on a selection or pastes it with `ydotool`, telling the panel its state over D-Bus. The panel
stops it with SIGTERM (Insert), discards it with SIGKILL to its process group (Escape), finds it
through `/dev/shm/stt-recording-$USER.pid` and accepts a PID there only when
`/proc/<pid>/cmdline` matches `/\/wsi(-stream|-article)?(\0|$)/`. A person may also run it by
hand. Its siblings `wsi-stop-grace`, `wsi-setting` and `wsi-resolve-model` stay bash and
Python and are run from the directory the binary is in.

`watermark` is run by a person on one image at a time. No other tool calls it.

## 2. Scope decisions

- **D1. Two Go binaries at the old paths.** `src-go/cmd/wsi` is built by
  `tasks/build-go-command.yml` from `play-speech-to-text.yml` and installed at
  `~/.local/bin/wsi`, mode 0755; `src-go/cmd/watermark` from `play-image-watermarking.yml` at
  `/usr/local/bin/watermark`, owned by root, mode 0755 (the task file gains an optional
  `go_command_owner`, so a root-owned install is the same task). Both plays import
  `play-golang.yml` first. The bash files are deleted and their rows leave
  `scripts/bash-limits.allowlist`.
- **D2. External programs stay where they are the tool.** wsi: `pw-record`, `sox`, `soxi`,
  `faster-whisper-transcribe`, `curl` (the whisper.cpp server), `gdbus` (signals,
  notifications, the panel's `PasteKey`), `wl-copy`, `xsel`, `ydotool`, `wsi-claude-process`
  and the three sibling helpers. watermark: `magick` and `exiftool`. Done natively in Go: the
  free-space check (`statfs`, was `df | awk`), the image type check (content sniffing, was
  `file`), the 2 s answer limit on `PasteKey` (a context deadline, was `timeout`), temporary
  files (none: captured in memory), shell quoting for `--show-config` and `--dry-run`.
- **D3. One seam for processes.** Everything runs through `internal/execx`. The recorder is a
  background command stopped by SIGINT, so `execx.Cmd` gains `Interrupt`, the signal a
  cancelled context sends (SIGTERM when unset).
- **D4. Shell quoting is shared.** `internal/shellword` (`Quote`, `Join`) is the one POSIX
  quoter; `internal/registry`'s private copy moves there.
- **D5. The batch recording limit keeps one home.** `MaxRecordingSeconds` in
  `src-go/internal/wsi/limit.go` (30), the only line there with the number;
  `scripts/qa-stt-limits.bash` checks that file where it checked the bash.

## 3. wsi expectations

### Command line

- W1. Options: `-c/--clipboard`, `-a/--auto-paste`, `--no-auto-enter`,
  `--paste-with-shift N`, `-w/--wrap-marker`, `-l/--language L`, `-n/--netapi`, `--no-notify`,
  `--claude-process`, `--claude-model M`, `--claude-style S`, `--session-token T`,
  `-d/--debug` (debug and verbose), `-v/--verbose`, `-h/--help`. Defaults: PRIMARY, no paste,
  Enter after a paste, no marker, no language, notifications on, no Claude, model `sonnet`,
  style `corporate`. Any other argument not starting with `-` is a whisper.cpp server address
  (`host:port`); `-n` is `127.0.0.1:58080`; the last one given wins.
- W2. `-h`/`--help` prints the usage text to stderr and exits 0 (arguments are read left to
  right; an error before it wins).
- W3. Argument errors: an option that takes a value given none
  (`<option> requires a value`), `--paste-with-shift` other than `0` or `1`
  (`--paste-with-shift requires 0 (Ctrl+V) or 1 (Ctrl+Shift+V), got: <v>`), an unknown
  argument starting with `-` (`unknown option: <arg> (try --help)`). The whole command line is
  read first, so the report carries the session token and the debug setting given anywhere in
  it; the error is reported as in W9 and wsi exits 2.
- W4. A server address is probed before anything else runs:
  `curl -s -o /dev/null -w %{http_code} <addr>`; a code that does not start with `2`, or none,
  is `Can't connect to whisper.cpp server at <addr> (HTTP: <code or "connection failed">)`
  (W9), exit 1.

### One recorder at a time

- W5. After the command line is read and before anything is signalled: if the PID file
  `/dev/shm/stt-recording-$USER.pid` names a live process, wsi prints
  `wsi: already recording (PID <n>); not starting a second recorder` on stderr and exits 1,
  touching nothing (no signal, no PID file change). A file naming no live process is stale and
  is overwritten later. `$USER` empty: the account's name.
- W6. From then on every exit runs the cleanup: the PID file is removed only while it names
  this process (another client's PID is left in place, and an unreadable file is left with a
  stderr line); a recorder still running is stopped; once recording has started, the audio files
  `/dev/shm/wfile-$USER.wav` and its `.resampled.wav` and `.padded` siblings are removed (a run
  that failed before recording leaves them alone); then
  `IDLE` is signalled (dropped when the session already ended, W8).

### Log and panel

- W7. Log lines go to stderr (`[WSI] <text>`, `[DEBUG] <text>` only with `-d`/`-v`,
  `[ERROR] <text>`) and to `~/.local/share/speech-to-text/debug.log` as
  `[YYYY-MM-DDTHH:MM:SS] [WSI] <text>` (local time). With `-d` or `-v` each file line is
  written at once; otherwise it is held in memory and written only when an error ends the
  dictation (W9). Before a write the directory is created and a log over 1 MiB is renamed to
  `debug.log.old`. A log that cannot be written is said once on stderr and the dictation goes
  on. Directories wsi creates are mode 0750; the log, the PID file and a saved transcript are
  created 0600 (they hold dictated text and are the user's alone).
- W8. A signal to the panel is
  `gdbus emit --session --object-path /org/fedoradesktop/SpeechToText --signal org.fedoradesktop.SpeechToText.<Name> <value> <token>`, in the foreground, so signals
  arrive in the order sent. The token is the `--session-token` as a GVariant string literal.
  `StateChanged` carries a bare state word; `Error` carries the text as a GVariant string
  literal. After `SUCCESS`, `ERROR` or `IDLE` nothing more is signalled (debug line
  `Not sent, the session ended with <state>: ...`). A failed emit is a stderr line and is not
  fatal.
- W9. An error that ends the dictation: `[ERROR] <text>` (stderr and log), an `Error` signal
  with the text, then the `ERROR` state with a notification of the text; when log lines were
  held (W7) they are written first and the notification adds
  `\n\n📝 Debug logs saved (<n> entries):\n<log file>`, and the log then records
  `Flushed <n> buffered log entries to disk on error`. A problem that does not end it (W29,
  W33, W35): the `[ERROR]` line and the `Error` signal only.
- W10. A notification is
  `gdbus call --session --dest org.freedesktop.Notifications --object-path /org/freedesktop/Notifications --method org.freedesktop.Notifications.Notify 'speech-to-text' <id> '<icon>' 'Speech to Text' '<text>' [] {} <ms>`, every string a GVariant literal; the id of
  the reply `(uint32 N,)` is reused so each notification replaces the last. `--no-notify`
  sends none. A refused notification is a debug line. A state change notifies for 3000 ms with
  the icon `audio-input-microphone` unless stated.

### Preparing

- W11. With `-d`: a banner and the configuration (temp directory, audio file, the
  transcriber and its path or `NOT FOUND`, the paste mode, the session type, the log file) on
  stderr, then the dependency check (pw-record, sox, soxi, curl; wl-copy and ydotool on
  Wayland, xsel and ydotool on X11; the transcriber; a writable temp directory), each missing
  one an `[ERROR]` line with its purpose and package; any missing is
  `Dependency check failed: <n> errors found` (W9), exit 1.
- W12. Preflight, each failure W9 and exit 1: `pw-record`, `sox` and `soxi` on PATH
  (`Required command not found: <cmd>`); without a server, `faster-whisper-transcribe` on PATH
  (`Whisper not found: faster-whisper-transcribe`); with `-a`, `ydotool` on PATH, the ydotool
  socket (`$WSI_YDOTOOL_SOCKET`, default `/run/ydotool.socket`) a socket and writable; without
  `-a`, `wl-copy` on Wayland and `xsel` on X11; `/dev/shm` writable; at least 10240 KiB free
  there (`Insufficient space in /dev/shm (<n>KB available, need 10MB)`, then the ERROR state
  `Disk space low - cannot record`).
- W13. The stop grace is what `<bin dir>/wsi-stop-grace` prints (a whole number of seconds);
  it failing, or printing anything else, is
  `Cannot read the stop grace (<path> failed; re-run play-speech-to-text.yml)`, exit 1, before
  the microphone opens.
- W14. Without a server: the language is `--language`, else `$WHISPER_LANGUAGE` if set (even
  empty), else `<bin dir>/wsi-setting language` (`system` means `$LANG`, default
  `en_GB.UTF-8`, up to its first `_`); a failing setting reader is
  `Cannot read the language setting (<path> failed; reason above)`, exit 1. The model is what
  `<bin dir>/wsi-resolve-model --mode batch --language <lang> <$WHISPER_MODEL or auto>` prints;
  its stderr is a debug line; its exit 3 is the error text after the last
  `wsi-resolve-model: ` in its stderr (exit 1); another failure is
  `Cannot choose the Whisper model (<path> failed: <stderr>)`, exit 1. The transcriber gets
  `WHISPER_LANGUAGE` and `WHISPER_MODEL` set to these.

### Recording

- W15. The `PREPARING` state (`Preparing to record...`); `pw-record --rate 44100 --channels 2 --latency 50ms /dev/shm/wfile-$USER.wav` starts (its output shown with `-d`, discarded
  otherwise); this process's PID is written to the PID file (a failure is W9, the recorder
  stopped, exit 1). After 0.3 s the `RECORDING` state (`Recording... (Insert to stop)`), unless
  a stop is already pending or the recorder has already ended (a debug line says which).
- W16. The recorder is stopped with SIGINT (it flushes its buffers), which ends it; one that
  has not ended 3 s later is killed. It is stopped by: the recording limit,
  `MaxRecordingSeconds` (30) after `RECORDING`; INT; TERM when a stop is already pending or the
  grace is 0; the end of the grace.
- W17. The first TERM with a grace above 0 starts the grace: the `STOPPING` state and a
  notification `Stopping in <g>s - finish your sentence.\nEscape: discard. Insert waits until the text is pasted.` shown for `<g>` seconds; recording continues until the grace ends or a
  second TERM.
- W18. A TERM or INT that arrives before the recorder starts is acted on as soon as it has
  started (a TERM then starts the grace). One that arrives after the recorder has ended is
  ignored: the dictation goes on to its result.
- W19. After the recorder ends: an audio file that is missing or empty is
  `Recording file empty or missing: <path>` (W9), then the state `ERROR` with
  `Recording failed`, exit 1.

### Transcribing

- W20. `sox <wav> <wav>.resampled.wav rate 16k` (stderr shown with `-d`), then the result
  replaces the recording; either failing is W9, exit 1. `soxi -D <wav>`: a duration under 1 s
  is padded (`sox <wav> <wav>.padded pad 0 1`, then it replaces the recording; failing is W9,
  exit 1); soxi failing or printing no number is a problem (W9 problem) and the pad is skipped.
- W21. The `TRANSCRIBING` state. With a server:
  `curl -S -s <addr>/inference -H Content-Type: multipart/form-data -F file=@<wav> -F temperature=0.0 -F temperature_inc=0.2 -F response_format=text`; a failing curl is
  `The whisper.cpp server request failed (curl exit <n>)` (W9), exit 1. Otherwise
  `faster-whisper-transcribe <wav>` (stderr discarded); a failure is
  `faster-whisper failed with exit code <n>` (W9) and the state `ERROR` with
  `Transcription failed (code <n>)`, exit 1.
- W22. The text: trailing newlines removed; on each line every `(...)` and then every `[...]`
  removed (Whisper's non-speech marks); up to two leading newlines removed; leading white space
  removed; the first letter upper-cased. Empty: `No transcription result (empty string)` (W9)
  then `ERROR` with `No speech detected` and the icon `dialog-warning`, exit 1.
- W23. `--claude-process`: `wsi-claude-process` on PATH (else
  `wsi-claude-process not found (re-run play-speech-to-text.yml)`, exit 1) is run as
  `wsi-claude-process --model <m> --style <s> [--debug] <text>`; exit 0 with output replaces
  the text. Otherwise the reason is its stderr, newlines as spaces, less a leading
  `wsi-claude-process: ` (empty: `exit <n>, no output`): `Claude: <reason>` (W9), the raw text
  copied to the CLIPBOARD (W27), a notification that stays (`dialog-error`, 0 ms)
  `Claude post-processing failed: <reason>. Nothing was pasted. The raw transcript is on the clipboard: Ctrl+V.`, exit 1.
- W24. `--wrap-marker` without `--claude-process`: the text becomes `speech-to-text:"<text>"`.
- W25. The text plus a newline is saved to `~/.cache/speech-to-text/last-transcription.txt`
  (directory created); a failure is a problem (W9) and the dictation goes on.

### Delivering

- W26. No graphical session (`DISPLAY`, `WAYLAND_DISPLAY` and `DESKTOP_SESSION` all empty):
  the text and a newline on stdout, exit 0 (no `SUCCESS`; the cleanup sends `IDLE`).
- W27. Clipboard tools by session type (`XDG_SESSION_TYPE`): CLIPBOARD is `wl-copy` /
  `xsel -ib`, PRIMARY is `wl-copy -p` / `xsel -ip`, the text on stdin. A failing copy is W9,
  exit 1.
- W28. Without `-a`: the text and a newline to PRIMARY, or CLIPBOARD with `-c`; any other
  session type is a problem (W9) and the text goes to stdout. Then `SUCCESS` with the first 60
  characters (`...` added when longer) and `\n\nCtrl+V to paste` (CLIPBOARD) or
  `\n\nMiddle-click to paste` (PRIMARY).
- W29. With `-a` and `--paste-with-shift`: the panel is asked (W30), then the text without a
  newline goes to CLIPBOARD; without `--paste-with-shift`, to PRIMARY. An unknown session type
  is `Unknown window manager for clipboard: <type>` (W9), exit 1. The ydotool socket missing
  now is `ydotool socket disappeared before paste` (W9), exit 1.
- W30. The panel's answer: `gdbus call --session --dest org.gnome.Shell --object-path /org/fedoradesktop/SpeechToText/Panel --method org.fedoradesktop.SpeechToText.Panel.PasteKey`,
  answered within 2 s, stdout `('<wm_class>', <with_shift>, <save_after>[, <focused>, <gone>])`.
  No answer, a failure or another shape: the key chosen at Insert, no save, and the log line
  `WARN: The panel did not say how to paste into the focused window (<why>); using the key chosen at Insert` (`<why>` is `no answer within 2 s` or the failure). `gone`: the target is
  closed. Focused, or a three-field answer: the key is Ctrl+Shift+V when `with_shift`, the
  save follows when `save_after`, and the log says
  `Pasting into '<wm_class>': <keys>[, then Ctrl+S]`. Not focused: the panel is giving focus
  back, so it is asked again every 0.1 s, up to 20 more times (then the target is
  `unfocused`), and once it is focused again it must stay focused for 5 more answers before the
  paste (any loss starts the settle again); the paste is then marked refocused.
- W31. A target that is closed or would not take focus back: nothing is pressed;
  `Nothing was pasted: <why>` (W9) and a notification that stays
  `Nothing was pasted: <why>. The text is on the clipboard: Ctrl+V.`, exit 1; `<why>` is
  `the window this dictation started in was closed` or
  `the window this dictation started in would not take focus back`.
- W32. After 0.05 s the paste keys: `ydotool key 29:1 47:1 47:0 29:0` (Ctrl+V),
  `29:1 42:1 47:1 47:0 42:0 29:0` (Ctrl+Shift+V), or without `--paste-with-shift`
  `42:1 110:1 110:0 42:0` (Shift+Insert), with `YDOTOOL_SOCKET` set to the socket. A failure
  is `ydotool <keys> simulation failed (exit code: <n>)` (W9), exit 1.
- W33. Before the Enter: a refocused paste with an Enter to send waits 1.0 s (log
  `Waiting 1.0 s before the Enter: focus was given back for this paste`) and asks the panel
  again as in W30 (only the target is judged; the keys stay; a panel that cannot answer sends
  the Enter); a target that is now closed or unfocused is
  `The text was pasted but not sent: <why>` (W9) and a notification that stays
  `The text was pasted but not sent: <why>. No Enter was pressed.`, exit 1. Otherwise the wait
  is 0.3 s. Then, unless `--no-auto-enter`, `ydotool key 28:1 28:0`; a failure is a problem
  (W9) and the paste stands.
- W34. Then, when the panel asked for a save, `ydotool key 29:1 31:1 31:0 29:0` (log
  `Save simulated via Ctrl+S`); a failure is the problem
  `The text was pasted but the save (Ctrl+S) failed (exit code: <n>)`.
- W35. Then `SUCCESS` with the 60-character preview. Exit 0.

## 4. watermark expectations

- M1. `watermark [OPTIONS] IMAGE`: `--text`, `--artist`, `--copyright`, `--licence-url`,
  `--licence-summary`, `--profile`, `--config`, `-o/--output` take a value (missing:
  `watermark: <option> requires a value`, exit 2); `--force`, `--dry-run`, `-v/--verbose`,
  `--no-tile`, `--no-corner`, `--show-config`; `-h/--help` prints the usage to stdout and
  exits 0; `--` ends the options; another `-...` is `unknown option: <arg> (try --help)`,
  exit 2. Every error line is `watermark: <text>` on stderr.
- M2. Settings in order, each later one winning: the defaults (font `DejaVu-Sans-Bold`,
  opacity_corner `0.85`, opacity_tile `0.10`, tile_angle `-30`, size_pct_corner `0.030`,
  size_pct_tile `0.018`, quality `95`, the semantic five empty), `/etc/watermark/defaults.conf`
  and `~/.config/watermark/defaults.conf` when present, the `--profile` file
  `~/.config/watermark/profiles/<name>.conf` (must exist:
  `--profile '<name>' not found at <path>`), the `--config` file (must exist:
  `--config file not found: <path>`), then the command-line values. `text` empty is
  `copyright`. A profile name other than letters, digits, `.`, `_`, `-` (not starting with
  `.`) is refused. `HOME` unset is an error. Exit 2 for each.
- M3. A config file is read, never sourced: blank and `#` lines skipped; each other line is
  `KEY=VALUE`, KEY one of `artist copyright licence_url licence_summary text font opacity_corner opacity_tile tile_angle size_pct_corner size_pct_tile quality`; VALUE is
  `"..."` (backslash escapes `\"`, `\\`, `` \` ``, `\$`; a bare `$` or backquote is refused,
  nothing is expanded), `'...'` (literal), or a bare word without white space, quotes, `$`,
  backquotes, backslashes or shell operators; it may be followed by white space and a `#`
  comment. Anything else is `<path>:<n>: <reason>`, exit 2. `-v` logs
  `watermark: loading config: <path>`.
- M4. `--show-config` prints `# resolved config (post-precedence-chain)` and the lines
  `artist= copyright= licence_url= licence_summary= text= font= opacity_corner= opacity_tile= tile_angle= size_pct_corner= size_pct_tile= quality= output= profile= config_file= no_tile= no_corner=` (strings shell-quoted where needed, flags `0`/`1`) on stdout and
  exits 0, before any other check.
- M5. No image: the usage on stderr, exit 2. More than one: `this version processes ONE image at a time; got <n> (use 'find … -exec watermark {} \;' for batch)`, exit 2.
- M6. `magick` not on PATH: `missing tool: magick (ImageMagick 7) — see Plan 00037`, exit 4;
  `exiftool`: `missing tool: exiftool (perl-Image-ExifTool)`, exit 5; the exiftool config
  (`$WATERMARK_EXIFTOOL_CONFIG`, default `/etc/watermark/exiftool.config`) missing:
  `missing exiftool config: <path> (deploy via play-image-watermarking.yml)`, exit 5.
- M7. `--no-tile` with `--no-corner`:
  `--no-tile and --no-corner together leaves no visible watermark`, exit 2. Each of artist,
  copyright, licence-url, licence-summary and text empty:
  `missing required value for '<name>' (set via --<name> or in a config file)`, exit 2. The
  presentation values: opacities a decimal in 0..1, tile_angle a decimal, sizes a decimal
  above 0 and at most 1, quality a whole number 1..100, font not empty; otherwise
  `<key> must be <kind>, got '<value>'`, exit 2.
- M8. The image must be a regular file (`no such file: <path>`), named `.jpg`, `.jpeg` or
  `.png` in any case (`unsupported extension (JPEG and PNG only): <path>`), and its content a
  JPEG or PNG by its signature (`input content is not a JPEG or PNG image: <path>`); exit 2.
- M9. Already watermarked, unless `--force`: a name ending `.watermarked.<ext>` (any case), or
  `exiftool -config <cfg> -s3 -XMP-wm:Applied -- <image>` printing `True`: stderr
  `watermark: skip (<reason>): <absolute image path>`, the absolute path on stdout, exit 3. A
  failing exiftool here is `exiftool could not read <image>: <reason>`, exit 5.
- M10. The output is `-o`, else `<dir>/<stem>.watermarked.<ext>` (ext lower-cased as given).
  Paths are compared after resolving symlinks (missing parts as given): the output resolving
  to the image is refused, exit 2; an existing output needs `--force`
  (`output already exists: <path> (pass --force to overwrite)`, exit 2; `-v` logs the
  overwrite).
- M11. The magick command: `magick <image>`, then unless `--no-tile` the tile layer
  `( -size 400x400 xc:none -font <font> -pointsize %[fx:w*<size_pct_tile>] -fill rgba(255,255,255,<opacity_tile>) -stroke rgba(0,0,0,<opacity_tile>) -strokewidth 1 -gravity center -annotate <tile_angle> <text> -write mpr:tile +delete ) ( +clone -fill mpr:tile -draw color 0,0 reset ) -compose over -composite`, then unless `--no-corner`
  `-font <font> -pointsize %[fx:w*<size_pct_corner>] -fill rgba(255,255,255,<opacity_corner>) -stroke rgba(0,0,0,<opacity_corner>) -strokewidth 2 -gravity southeast -annotate +30+30 <text>`, then `-quality <quality> <output>`.
- M12. The exiftool command: `exiftool -config <cfg> -overwrite_original -EXIF:Artist=<a> -EXIF:Copyright=<c> -IPTC:By-line=<a> -IPTC:CopyrightNotice=<c> -XMP-dc:Creator=<a> -XMP-dc:Rights=<c> -XMP-xmpRights:Marked=True -XMP-xmpRights:UsageTerms=<summary> -XMP-xmpRights:WebStatement=<url> -XMP-wm:Applied=True -XMP-wm:AppliedAt=<UTC YYYY:MM:DD HH:MM:SSZ> -XMP-wm:AppliedBy=watermark/0.1.0 <output>`.
- M13. `--dry-run` prints `# magick:`, the magick command, `# exiftool:`, the exiftool
  command (each shell-quoted on one line) and exits 0, after every check above.
- M14. Otherwise magick runs (failure: `magick failed (visible watermark)`, exit 4), then
  exiftool (failure: `exiftool failed (metadata write)`, the unmarked output removed, exit 5);
  both tools' output goes to stderr. `-v` logs each step. Success: the absolute output path on
  stdout, exit 0.

## 5. Where the Go departs from the bash

| #   | bash behaviour                                                                                                                                                                           | Go                                                                                                   | exp.              |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ----------------- |
| B1  | wsi: an option that takes a value given as the last argument looped for ever (`shift 2` fails without shifting, and `set -e` was not yet on)                                             | `<option> requires a value`, exit 2                                                                  | W3                |
| B2  | wsi: an unknown `-x` option was taken as a server address and probed with curl                                                                                                           | `unknown option`, exit 2                                                                             | W3                |
| B3  | wsi: an argument error exited 1 and was signalled before a later `--session-token` was read, so the panel ignored it                                                                     | the whole command line is read first; exit 2                                                         | W3                |
| B4  | wsi: the one-recorder check ran before the command line, so `--help` with a live recorder said "already recording"                                                                       | after the command line                                                                               | W5                |
| B5  | wsi: a TERM before the recorder started stopped nothing, and the dictation then recorded for the full 30 s                                                                               | acted on as soon as the recorder starts                                                              | W18               |
| B6  | wsi: a recorder that ignored SIGINT was waited for for ever                                                                                                                              | killed 3 s after the SIGINT                                                                          | W16               |
| B7  | wsi: a failing `sox`, `mv`, PID-file write or clipboard copy ended the run under `set -e` silently, the panel told only `IDLE`                                                           | an `[ERROR]` line, `ERROR` to the panel, exit 1                                                      | W9, W15, W20, W27 |
| B8  | wsi: a failing whisper.cpp request was logged at debug level and the run went on to "No speech detected"                                                                                 | `The whisper.cpp server request failed`, exit 1                                                      | W21               |
| B9  | wsi: `if ! ydotool ...; then exit_code=$?` reported the negated status, always 0                                                                                                         | the tool's own exit code                                                                             | W32, W33          |
| B10 | wsi: notification texts were passed to gdbus as raw words, so a text that parsed as GVariant (`'quoted'`, a backslash) was rewritten; `\n` in them became a newline only by gdbus' retry | every string is a GVariant literal; the texts' line breaks are real newlines, escaped in the literal | W10               |
| B11 | wsi: the recorded audio was left in `/dev/shm` after every failed dictation                                                                                                              | removed at every exit after recording starts                                                         | W6                |
| B12 | wsi: a log write that failed ended the run under `set -e`                                                                                                                                | said once on stderr; the dictation goes on                                                           | W7                |
| B13 | wsi: a failing save of `last-transcription.txt` ended the run silently                                                                                                                   | a problem; the dictation goes on                                                                     | W25               |
| B14 | wsi: the Claude failure path copied with `wl-copy` whatever the session type                                                                                                             | the session's CLIPBOARD tool                                                                         | W23               |
| B15 | wsi: the per-option debug lines while reading the command line, the `ERR` trap, and one panel error per missing dependency in `-d`                                                       | none of the per-option lines; failures are explicit; one `Dependency check failed` error             | W11               |
| B16 | wsi: `PasteKey`'s stderr was folded into its answer, so a GLib warning made a good answer unreadable                                                                                     | stdout is the answer                                                                                 | W30               |
| B17 | wsi: the free space was `df -k` piped through `tail` and `awk`; also `timeout`, `mktemp` and `du`                                                                                        | `statfs`, a context deadline, memory; no size debug line                                             | D2                |
| B18 | wsi: `HOME` unset wrote the log under `/.local`; `$USER` unset named the files `-.pid`                                                                                                   | `HOME` unset is an error (exit 1); `$USER` unset is the account's name                               | W5, W7            |
| B19 | wsi: `--claude-style` was missing from the usage text                                                                                                                                    | listed                                                                                               | W1                |
| B20 | wsi: the recorder's stdout went to wsi's stdout with `-d`                                                                                                                                | stderr                                                                                               | W15               |
| B21 | watermark: config files were `source`d (any shell in them ran, any variable could be set, `output` or `force` included)                                                                  | read as data, twelve known keys, nothing expanded                                                    | M3                |
| B22 | watermark: presentation values went unchecked into magick's arguments                                                                                                                    | validated                                                                                            | M7                |
| B23 | watermark: `--profile ../x` read a file outside the profiles directory                                                                                                                   | refused                                                                                              | M2                |
| B24 | watermark: the idempotency probe's exiftool failure was swallowed (`or true`) and read as "not marked"                                                                                   | exit 5                                                                                               | M9                |
| B25 | watermark: exiftool's `1 image files updated` went to stdout beside the output path                                                                                                      | both tools' output on stderr                                                                         | M14               |
| B26 | watermark: a failed metadata write left the visibly marked file without the sentinel, so a re-run marked it twice                                                                        | the output is removed                                                                                | M14               |
| B27 | watermark: `file --mime-type` was required                                                                                                                                               | the content is sniffed in Go                                                                         | M8, D2            |
| B28 | watermark: `--show-config` and `--dry-run` quoted with `printf %q` (`John\ Smith`) and ended each command with a space                                                                   | POSIX single-quote form (`'John Smith'`), no trailing space                                          | M4, M13           |
| B29 | watermark: `HOME` unset failed as an unbound variable, exit 1                                                                                                                            | `HOME is not set`, exit 2                                                                            | M2                |
| B30 | watermark: a write failure on stdout went unnoticed                                                                                                                                      | exit 1                                                                                               | M14               |

## 6. Go design

- `src-go/cmd/wsi`, `src-go/cmd/watermark`: thin mains. The wsi main subscribes to SIGTERM and
  SIGINT before anything else, so a stop that arrives during the preflight is held (W18), and
  hands the channel to the flow.
- `internal/wsi`: `App` over an `Env` of seams (runner, streams, environment, PATH lookup,
  clock and timers, signal channel, PID liveness, free space, the recorder). The recording is
  one select loop over the signals, the preparing delay, the limit, the grace and the
  recorder's end, so state changes are signalled from one goroutine in order (W8).
  `Recorder` starts `pw-record` through `execx` with `Interrupt: SIGINT`.
- `internal/watermark`: `App` over an `Env` (runner, streams, environment, PATH lookup,
  clock); the config reader, the settings merge and validation, the two argv builders.
- `internal/shellword`: `Quote` and `Join`, shared with `internal/registry`.
- `scripts/test-wsi-stop-grace.bash` builds `src-go/cmd/wsi` into its work directory beside
  links to the three sibling helpers and drives it as it drove the bash; its two static checks
  on the bash's `kill` sites are gone (the Go stops the recorder through `execx` only).
  `scripts/go-binary-suites.tsv`: `wsi` names that suite, `watermark` is `qa-go`.

## 6a. Usage texts

`src-go/internal/wsi/usage.txt` and `src-go/internal/watermark/usage.txt`, embedded; the
bash's texts with `--claude-style` added (B19) and the config files described as read, not
sourced (B21).

## 7. What only a host run proves

- H1. After `play-speech-to-text.yml`: an Insert, speech, Insert dictation pastes the text into
  a terminal (Ctrl+Shift+V) and an editor (Ctrl+V), with the grace audible in the result.
- H2. Escape during a recording discards it: the panel's group kill ends `wsi` and its
  `pw-record` together (the Go binary is the group leader the panel's `setsid` made).
- H3. After `play-image-watermarking.yml`: `watermark --profile <name> photo.jpg` writes
  `photo.watermarked.jpg` with the visible marks, and `exiftool -XMP-wm:Applied` on it says
  `True`; a second run exits 3.
