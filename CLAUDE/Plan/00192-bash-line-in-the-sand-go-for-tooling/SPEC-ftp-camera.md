# ftp-camera: specification of the Go port

Programme P8 of [INVENTORY.md](INVENTORY.md) (the `ftp-camera` part; `wsi` and `watermark` are
not in this port). **Method: the legacy bash is not an oracle.** This document states the
intended behaviour of `ftp-camera`, the camera FTP session tool that `play-ftp-camera.yml`
deploys, as numbered, testable expectations. The bash (`files/home/.local/bin/ftp-camera` at
`3474744f`, 2569 lines, 33 functions) was read only to find the requirements. Where it was
wrong, the expectation states the correct behaviour and section 4 lists the departure. The
proof is the Go tests in `src-go/internal/ftpcamera` (every flow over a fake runner, a fake
clock and a temporary upload tree) and `src-go/internal/rclonerc`. What only a host with a
camera, vsftpd and an rclone mount can prove is listed in section 7.

## 1. Role and callers

`ftp-camera` is run by a person at the laptop. It receives uploads from a Sony camera over FTP
(vsftpd under a runtime config and a runtime systemd override), sorts them by EXIF date into
`photos/YYYY/MM/DD/<TYPE>/` and `videos/YYYY/MM/DD/<TYPE>/`, ships the sorted tree to an rclone
remote, and prunes RAW files whose JPG was deleted. Other callers: none in the repository.
Completed Plan 00099's acceptance gate used `--copy-preflight`; the mode is kept.

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/ftp-camera` is built by
  `tasks/build-go-command.yml` from `play-ftp-camera.yml` (which imports `play-golang.yml` first)
  and installed at `~/.local/bin/ftp-camera`, mode 0755. The bash file is deleted and its row
  leaves `scripts/bash-limits.allowlist`.
- **D2. The rclone remote-control client is a shared Go package.** `rclone-rc-auth.bash` is a
  sourced library that the other rclone helpers (`rclone-tail`, `rclone-cache-warm`,
  `rclone-cache-status`) still use, so it stays. Its three functions that `ftp-camera` called
  (`rclone_rc`, `rclone_rc_available`, `rclone_rc_addr_for_mount`) are
  `src-go/internal/rclonerc`, the one Go place that knows the credential file and the
  mount-to-RC-address resolution, for the other helpers when they are ported. A Go binary cannot
  source bash, and `bash -c` is refused by `project_seam_commands`, so the duplication lasts
  until those helpers move; the two read the same file in the same way (E28).
- **D3. External programs stay where they are the tool:** `exiftool`, `rclone`, `findmnt`,
  `nmcli` (ftp-camera becomes its second owner in `commandOwners`), `ip`, `iw`, `firewall-cmd`,
  `vsftpd` through `systemctl` (through `internal/systemd`), `tail -F`, `inotifywait`,
  `geeqie` under `setsid -f`, `cp -r`, `getent`, `systemd-inhibit`, `rclone-tail`, and `sudo`
  through `internal/sudo`. Done natively in Go: the JSON of the RC stats (`python3` is no longer
  needed), the JPEG marker check, directory walks, sizes and ages, the IEC size format
  (`numfmt`), the per-file copy into the mount, the log-line parsing, and the session counters
  (in memory, no temporary files).
- **D4. gum and fzf through `internal/ui`.** The launcher asks with `ui.Gum.Choose`, or with
  `ui.Picker.Choose` where only fzf is installed: the shared forms, not a second way to prompt.
- **D5. Root through `internal/sudo`.** Every root command is `sudo -n`, preceded by
  `sudo.Ready`, which asks for the password on the terminal with the reason when the credential
  is not cached. The bash ran plain `sudo`, which also prompted; a session that runs for hours
  outlives the sudo timestamp, so the readiness check runs before each root command, not once.

## 3. Expectations

The tests cite these numbers.

### Command line

- E1. `-h` or `--help` prints the usage text (section 6 of the help: modes, modifiers,
  launcher, completeness, weak-link advisory, sort layout, workflows) to stdout and exits 0.
  Arguments are read left to right, so an error in an earlier argument wins over a later
  `--help`.
- E2. An unknown argument prints `ERROR: unknown argument: <arg>` and
  `Run 'ftp-camera --help' for usage` on stderr and exits 2.
- E3. `--view` and `--view-jpg` together: `ERROR: --view and --view-jpg are mutually exclusive`,
  exit 2.
- E4. More than one primary mode (`--sort --pass --push --copy --copy-preflight --prune --async --async-copy`; `--async-copy` is the async mode with copying) prints
  `ERROR: only one primary mode at a time` and the list of modes, exit 2.
- E5. `--view`/`--view-jpg`, then `--hotspot`, then `--debug-ftp`, with a mode that is not an
  FTP-server mode (default, `--async`, `--async-copy`), each print their three-line refusal,
  exit 2. `--no-tui` is accepted and does nothing else.
- E6. The tunables `LINK_SLOW_KBS` (a non-negative decimal number, default 500),
  `LINK_REPEAT_THRESHOLD` (default 3), `LINK_RATE_MIN_BYTES` (default 1000000), `LANDING_WARN_GB`
  (default 50) and `LANDING_WARN_AGE_DAYS` (default 7) (non-negative whole numbers) are read
  from the environment; an empty value is the default; any other value prints
  `ERROR: <VAR> must be <kind>, got '<value>'` and exits 2.
- E7. The upload directory is the home directory of the `camera` account
  (`getent passwd camera`, field 6). Not found: `ERROR: 'camera' user not found — run play-ftp-camera.yml first`, exit 1. Help and usage errors do not need it.
- E8. With no arguments at all, the launcher runs: gum if on PATH, else fzf, else a three-line
  note on stderr and the default FTP-server mode. Step 1 offers the eight modes (the first word
  of the chosen line is the mode; `default` adds no flag, any other adds `--<word>`). For
  `default`, `async` and `async-copy` only, step 2 offers `no viewer` / `view RAW` / `view JPG`
  (gum starts on `no viewer`; adds `--view`, `--view-jpg`) and step 3 `no hotspot` /
  `use hotspot` (gum starts on `no hotspot`; adds `--hotspot`). Leaving any step prints
  `Cancelled.` on stderr and exits 130. The run continues with the chosen flags exactly as if
  they had been typed (`--no-tui` when there are none).
- E9. Suspend inhibit: in every mode but `--pass`, `--sort`, `--prune` and `--copy-preflight`,
  when `_FTP_CAMERA_INHIBITED` is unset and `systemd-inhibit` is on PATH, the process is
  replaced by `systemd-inhibit --what=idle:sleep --who=ftp-camera --why=Camera FTP transfer in progress --mode=block -- <this program> <the flags>` with `_FTP_CAMERA_INHIBITED=1` added to
  its environment; with no flags, `--no-tui` is passed so the re-entry does not reopen the
  launcher. An exec that cannot happen is `ERROR: ...`, exit 1.
- E10. Colour codes (bold, green, yellow, cyan, red) are written to a stream only when it is a
  terminal and `NO_COLOR` is unset; otherwise the same text is written plain.

### The config file

- E11. `/etc/ftp-camera/config` (written by `play-ftp-camera.yml`) is read, never sourced:
  blank lines and `#` lines are skipped, each other line must be `KEY=VALUE` (one pair of
  matching surrounding quotes on the value is removed); any other line is
  `ERROR: <path>:<n>: not a KEY=VALUE line`, exit 1. The keys used are `REPO_PATH`,
  `RCLONE_REMOTE` and `HOTSPOT_CONNECTION`; the environment is not consulted for them.

### --pass

- E12. No config: `ERROR: /etc/ftp-camera/config not found — run play-ftp-camera.yml first`,
  exit 1. `REPO_PATH` empty: `ERROR: REPO_PATH not set in <config>`, exit 1. `<REPO_PATH>/vault.bash`
  not an executable file: two-line error naming the re-run of the play, exit 1. Otherwise the
  process is replaced by `<REPO_PATH>/vault.bash get ftp_camera_password`, which prints the
  password; nothing else is written to stdout.

### Completeness of a received file

- E13. A file of zero bytes is truncated, whatever its extension. A file whose extension
  (case-insensitive) is not jpg, jpeg, arw, mp4 or mov cannot be verified.
- E14. JPEG: the first two bytes must be `FF D8`; then the last two bytes `FF D9`, or an `FF D9`
  pair anywhere in the last 64 bytes (trailing padding), is complete; anything else is
  truncated.
- E15. ARW, MP4, MOV: `exiftool` not on PATH means cannot be verified. Otherwise
  `exiftool -s3 -a -FileSize# -StripOffsets -StripByteCounts <file>`: when it succeeds, its first
  line is the file size, its second the strip offsets and its last the strip byte counts (comma
  or space separated, paired by position up to the shorter list); a strip that ends past the
  file size is proof of truncation.
- E16. Failing that proof, `exiftool -validate -warning -error -a -s3 <file>` (both streams):
  a non-zero exit cannot be verified; output matching `truncat`, `premature`, `unexpected end`
  or `missing .*end of` (case-insensitive) is truncated; anything else is complete.

### The sort pass (`--sort`, and the end of a default session)

- E17. The upload directory must exist (`ERROR: Upload directory not found: <dir>`) and
  `exiftool` be on PATH (`ERROR: exiftool not found — run play-photography.yml first`); either
  failing ends the pass with exit 1. It prints `Sorting uploads by EXIF date...` (stdout).
- E18. It considers the regular files directly in the upload directory whose extension
  (case-insensitive) is jpg, jpeg, arw, mp4 or mov, in name order. Anything else, and anything
  in a subdirectory, is not touched.
- E19. A truncated file prints `  partial  <name>  (truncated at <bytes> bytes — left in place)` on stderr and is left where it is. A file that cannot be verified is sorted.
- E20. The capture date is the first non-blank line of
  `exiftool -s3 -d %Y/%m/%d -DateTimeOriginal -CreateDate <file>` for a photo (jpg, jpeg, arw),
  or `... -CreateDate -MediaCreateDate <file>` for a video (mp4, mov); exiftool's exit status is
  not read. No date: `  skip  <name>  (no EXIF date)` (stdout), counted as skipped.
- E21. A dated file is moved, as root, with `mkdir -p <upload>/<photos|videos>/<date>/<TYPE>`
  and `mv <file> <that dir>/`, where TYPE is the extension upper-cased and ARW is RAW; it prints
  `  move  <name>  → <photos|videos>/<date>/<TYPE>/`. A failed mkdir or mv ends the pass with
  an error.
- E22. Then, as root, `find <upload> -mindepth 1 -type d -empty -delete`; its failure ends the
  pass with an error. Then a blank line; when files were truncated, the four-line truncation
  block, a blank line and the weak-link advisory (E58) for `<n> file(s) arrived truncated during this session`; and `Sort complete: <moved> moved, <skipped> skipped`. `--sort` exits 0 when the
  pass completes and 1 when it does not.

### The rclone remote

- E23. The remote-mode prerequisites, in order: the config exists; `RCLONE_REMOTE` is set
  (two-line error naming `ftp_camera_rclone_remote`); the remote is the library root: with one
  trailing `/` removed, a remote whose lower-cased form ends in `/photos`, `/photo`, `/videos`
  or `/video` is refused with the six-line error that names the parent to use instead; `rclone`
  is on PATH (`ERROR: rclone not found — run play-rclone.yml first`).
- E24. The mount of the remote: `findmnt -n -o TARGET -t fuse.rclone` lists rclone mounts (none,
  or findmnt failing: `ERROR: No rclone mount found for remote '<name>'` and the two lines
  naming the unit); for each, in order, `findmnt -n -o SOURCE <target>` (a target whose source
  cannot be read is passed over); the first whose source starts with `<name>:` is the mount,
  where `<name>` is the remote up to its first `:`. The local path is the target, joined with
  the remote's path (after the first `:`) less the mount source's path as a prefix and less a
  leading `/`.
- E25. The RC address of a mount path (rclonerc): `findmnt -n -o TARGET --target <path>`, first
  line, is the mount root (failure or empty: error); the rclone mount processes are those whose
  `/proc/<pid>/cmdline`, joined with spaces, contains `rclone mount` (none: error); in pid order,
  the first whose argument list contains the mount root is the mount; its `--rc-addr=<addr>`
  arguments must number exactly one (none, or several: error naming which). No process serves
  the root: error.
- E26. The RC credential (rclonerc): the file `RCLONE_RC_AUTH_FILE`, default
  `$HOME/.config/rclone/rc-auth.env`, read as data; unreadable: the six-line error naming
  play-rclone.yml. `RCLONE_RC_USER` and `RCLONE_RC_PASS` are the text after the first `=` on the
  first line whose key matches; either empty: the four-line error. It is read once per run.
- E27. An RC call is `rclone rc --url=http://<addr> <endpoint>` with `RCLONE_USER` and
  `RCLONE_PASS` set in the child's environment, never in its arguments.
- E28. The RC answers when `core/stats` succeeds; otherwise `  rc probe failed: <its output>`
  is printed on stderr.

### --push

- E29. After the remote prerequisites (E23) and an existing upload directory, prints the source
  and destination, then runs `rclone copy <upload> <remote> --progress --transfers 4 --checkers 8 --update` on the terminal. Its failure is `ERROR: rclone copy failed: ...`, exit 1. Success
  prints `Push complete.`, the retained-copy lines, and, when the mount resolves (E24, its
  errors not shown), the `rclone-cache-warm <mount path>` hint. Exit 0.

### --copy-preflight

- E30. The remote prerequisites (E23), an existing upload directory, the mount path (E24) that
  must be a directory (`ERROR: Mount destination not found: <path>` and a hint), the RC address
  (E25; failure adds the three-line explanation), and an answering RC (E28; failure adds the
  six-line explanation). Success prints the RC address on stdout and exits 0; every diagnostic
  is on stderr. Nothing is written or started.

### --copy

- E31. The preflight (E30) with its stdout discarded, then the header (source, destination and
  the count of regular files under the upload directory), then `core/stats-reset` (a refusal is
  the two-line NOTE on stderr, with rclone's stderr, and the copy goes on), then
  `cp -r <upload>/. <dest>/` (failure: `ERROR: Failed to copy files to mount`, exit 1) and
  `<n> files copied to mount.`.
- E32. After three seconds, it polls `vfs/stats` and `core/stats` every five seconds with no
  time limit. A call that fails ends the run: `ERROR: rclone RC at <addr> stopped answering:`
  and rclone's stderr, exit 1. A reply that is not JSON: `ERROR: Failed to parse rclone stats`,
  exit 1. Missing numbers are 0; `eta` may be null.
- E33. The upload is complete on the second consecutive poll whose `diskCache` shows no upload
  in progress and none queued: `[<elapsed>] all uploads complete`, and, when `erroredFiles` is
  above 0, `<n> file(s) errored during upload — see rclone log` on stderr. Every other poll
  prints one progress line: elapsed time, queued, active, done/total transfers, bytes in IEC
  (E65), the rate since the previous poll (whole bytes per second over whole seconds; 0 when no
  time passed or the bytes went down), and the ETA (`—` when null or 0).
- E34. Then `rclone check <upload> <remote> --one-way` (both streams captured): success prints
  the verified lines and exits 0; failure prints `Verification failed:`, rclone's output, and
  the `--push` hint on stderr, exit 1.
- E35. An interrupt (INT or TERM) while waiting ends the run with exit 130.

### --prune

- E36. The upload directory must exist (error, exit 1). With no `photos/` under it:
  `No photos/ subtree — nothing to prune.`, exit 0.
- E37. An orphan is a regular file with extension arw (case-insensitive) directly in a directory
  named `RAW` anywhere under `photos/`, whose stem (the name less its last extension) has no
  regular file named `<stem>.jpg` or `<stem>.jpeg` (case-insensitive) in the sibling `JPG/` or
  `JPEG/` directory. None: `No orphan RAW files found.`, exit 0.
- E38. Otherwise it lists them (paths relative to the upload directory) under
  `Found <n> orphan RAW file(s) (<size> total):`, the size in IEC binary units (E65).
- E39. It asks `Delete these files? [y/N] ` on stderr when stdin is a terminal, and reads one
  line. Only `y` or `Y` deletes; anything else, or end of input (which prints a blank line on
  stderr), prints `Aborted — no files removed.` and exits 0.
- E40. Each orphan is removed as root (`rm -f`); a failure prints `  failed  <path>` on stderr
  and the rest go on. Then the empty-directory sweep (E22's find), and
  `Prune complete: <removed> of <n> files removed (<size> freed)`. Exit 0.

### FTP-server modes: start

- E41. A server session (default, `--async`, `--async-copy`) prepares in this order, and any
  failure prints its error and then runs the full teardown (E55-E57) before exiting 1:
- E42. `--async-copy`: the remote prerequisites (E23) and the mount path (E24), which must be a
  directory (`ERROR: rclone mount path not found: <path>` and the unit hint).
- E43. `--view`/`--view-jpg`: `geeqie` on PATH, and `WAYLAND_DISPLAY` or `DISPLAY` set; each
  missing is its own error.
- E44. `--hotspot`: the config is read when present; the profile is `HOTSPOT_CONNECTION`, default
  `Hotspot`; `nmcli` must be on PATH. The WiFi device is the first `wifi` device of
  `nmcli -t -f DEVICE,TYPE device` (none: error). AP support: `nmcli -g WIFI-PROPERTIES.AP device show <dev>` containing `yes`, else `no`; when that does not answer and `iw` is on PATH, a line
  `Supported interface modes` in `iw list` followed within twelve lines by the word `AP` is yes,
  else no. Yes prints the capability line; no is the hardware error; unknown prints the
  three-line note and goes on. Then the single-radio note (three lines). The profile must be a
  whole line of `nmcli -t -f NAME connection show` (else the eleven-line error naming the vault
  key and the play). Already active (a whole line of `... --active`): the yellow
  `already active` line, and it is not taken down at the end. Otherwise
  `Activating hotspot '<name>'...`, `nmcli connection up <name>` as root, a two-second wait, and
  the session owns it.
- E45. Hotspot detection (every session): when `nmcli` is on PATH, the first active connection
  whose `ipv4.method` is `shared` (a connection whose method cannot be read prints
  `WARNING: could not read ipv4.method for '<name>': <output>` and is passed over); its device
  (`GENERAL.DEVICES`), the device's first IPv4 address (`ip -4 addr show dev <dev>`) and its
  SSID. Any of the first three missing: no hotspot.
- E46. WiFi context: when `iw` is on PATH and a WiFi device exists, `iw dev <dev> link` gives the
  frequency (MHz, whole) and signal (dBm); below 3000 MHz is 2.4 GHz, below 5926 is 5, else 6.
  Nothing here is an error.
- E47. The address advertised: the hotspot's when one is active; otherwise the device named after
  `dev` on the first line of `ip -4 route show default` (none: the three-line error), and that
  device's first IPv4 address (none: `ERROR: <dev> has no IPv4 address`).
- E48. Then `Starting camera FTP server...` and, as root: copy `/etc/vsftpd/vsftpd.conf` to
  `/run/vsftpd-camera.conf`; append `pasv_address=<ip>`; with `--debug-ftp`, append
  `log_ftp_protocol=YES` and print the six-line tracing note naming
  `CLAUDE/Plan/Cancelled/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash`;
  `mkdir -p /run/vsftpd/empty`; `chmod 0755 /run/vsftpd /run/vsftpd/empty`;
  `mkdir -p /run/systemd/system/vsftpd.service.d`; write `camera-ftp.conf` there
  (`[Service]`, an empty `ExecStart=`, `ExecStart=/usr/sbin/vsftpd /run/vsftpd-camera.conf`);
  `systemctl daemon-reload`; `systemctl start vsftpd`; `firewall-cmd --add-service=ftp --quiet`
  and `--add-port=30000-30100/tcp --quiet`; with a hotspot active, the same two in
  `--zone=nm-shared`.
- E49. Then a blank line and the banner (stdout): mode, hotspot (SSID or `-`, IP, device, and
  whether ftp-camera owns it), viewer (class, the watched folder: today's
  `photos/YYYY/MM/DD/<class>/` in async modes, the upload root in default mode; and whether it
  pre-warms), the remote and mount (`--async-copy`), debug, the laptop's WiFi (when known, said
  to be the laptop's), the upload directory and `ftp://camera@<ip>/`, then the camera settings,
  the landing-zone advisory (E50), and `Monitoring for uploads — press Ctrl+C to stop`.
- E50. Landing-zone advisory: the regular files under the upload directory whose age in whole
  days is above `LANDING_WARN_AGE_DAYS`, summed; at or above `LANDING_WARN_GB` GiB, the
  six-line suggestion with the size to one decimal. Entries that cannot be read are not
  counted. It never deletes anything.
- E51. Viewer pre-warm (async modes with a viewer): today's class folder is created as root and
  shown (E54).
- E52. The log follower: `tail -F -n 0 --pid=<this process> /var/log/vsftpd.log` as root, its
  stderr discarded, each line printed: `FAIL LOGIN` as `[fail]`, `OK LOGIN` `[auth]`,
  `CONNECT` `[conn]`, `OK UPLOAD` not printed, `FAIL UPLOAD` `[fail]`, an `FTP command:` line
  carrying `"PASS` not printed (it holds the password), `FTP command:` or `FTP response:`
  `[verb]`, anything else `[ftp ]`; each as `  <tag> <line>`.
- E53. The monitor. Default mode: `inotifywait -m -r -q -e close_write,moved_to --format "  [%T] %e  %w%f" --timefmt %H:%M:%S <upload>`, printed as it comes; with a viewer, a second
  follower of the log shows each `OK UPLOAD` whose file matches the viewer class (E54). Async
  modes: a follower of the log hands each `OK UPLOAD: Client` line's file (the fourth
  `"`-separated field, its leading `/` removed, under the upload directory) and the fifth field
  (`, <bytes> bytes, <rate>Kbyte/sec`) to the per-upload handler (E59-E63), one at a time in
  arrival order.
- E54. Showing a file: `  [<HH:MM:SS>] view  <name>  → geeqie` (stdout), then
  `setsid -f geeqie --file=<path>` with `GQ_NEW_INSTANCE` set empty and its output discarded.
  The viewer class RAW matches `.arw`, JPG `.jpg` and `.jpeg` (case-insensitive). A geeqie that
  cannot be started ends the session (exit 1).

### FTP-server modes: end

- E55. The session ends on INT or TERM (exit 0 after the teardown), or when a background part
  stops by itself: `ERROR: <part> stopped: <reason>` and exit 1 after the teardown, where the
  part is `the upload monitor` (inotifywait in default mode, the log follower in async modes),
  `the vsftpd log follower` (default mode) or `the viewer` (default mode, E54). An interrupt
  that arrives within a second of the part stopping is an interrupt (Ctrl+C reaches its
  process too). The log is followed once; the async handler and the default-mode viewer take
  the completed uploads from that one follower, one at a time, so a slow sort never holds up
  the printed log.
- E56. Teardown, in order: a blank line and `Stopping FTP server...`; every follower and monitor
  is stopped and waited for; vsftpd is stopped when `systemctl is-active --quiet vsftpd` says it
  runs; as root `rm -rf /run/systemd/system/vsftpd.service.d /run/vsftpd-camera.conf` and
  `systemctl daemon-reload`; the ftp service and the port range are removed when a
  `firewall-cmd --query-...` says they are present (exit 0 present, 1 absent, anything else an
  error); the same in `nm-shared` when that zone is in `firewall-cmd --get-zones`; the hotspot,
  when this session brought it up and it is still active, is brought down
  (`Deactivating hotspot '<name>'...`, `nmcli connection down` as root); `FTP server stopped.`.
  A failing step stops the teardown with its error and the exit is 1.
- E57. Then the upload summary of the regular files modified since the session started (count,
  total in IEC binary units, path, and, for twenty or fewer, one `  <path>  <size>` line each;
  or `(no files received)`); in async modes the async summary (sorted, skipped, sort errors;
  truncated with its three lines; with `--async-copy`, copied and copy errors, the failed files,
  the `--push` line, and `rclone-tail --once <remote name>` when it is on PATH, whose failure
  prints that the status could not be read); in default mode the sort pass (E17-E22), whose
  failure prints `note: the sort pass did not complete — continuing with teardown.` on stderr
  and does not change the exit.

### Weak link

- E58. The weak-link advisory is printed on stderr at most once per run: the headline, the
  detected reason, the explanation, the laptop's band and signal when known (said to be the
  laptop's, not the camera's), the band-specific advice (try 2.4 GHz from 5 or 6; nothing to
  gain on 2.4), and the `--hotspot` and card-reader options.

### Per upload (async modes)

- E59. A file under `photos/` or `videos/` is ignored. Every other arrival is counted by name;
  at `LINK_REPEAT_THRESHOLD` arrivals or more, the advisory for `<name> has now arrived <n> times — the camera is re-sending because no attempt has completed`. When the transfer had at
  least `LINK_RATE_MIN_BYTES` bytes and a rate below `LINK_SLOW_KBS`, the advisory for
  `transfer ran at <rate> KB/s (below <limit> KB/s)`.
- E60. A file that is no longer there (the camera's retry already moved it) is a quiet no-op.
- E61. A truncated file prints `  [<ts>] partial  <name>  (truncated at <bytes> bytes — NOT sorted; camera will retry)` on stderr, counts as truncated, and raises the advisory for
  `<name> arrived truncated — the data connection dropped mid-transfer`. An unverifiable file
  prints `  [<ts>] unverified  <name>  (no completeness check for this type)` and goes on.
- E62. Sorting one file (E20, E21 as root): unsupported type `skip ... (unsupported type, left in place)`, no date `skip ... (no EXIF date, left in place)`, both counted skipped; a failed
  move `fail ... (sort error, left in place)` on stderr, counted as a sort error; moved
  `  [<ts>] sort  <name>  → <relative dir>/`, counted, then shown when it matches the viewer
  class.
- E63. `--async-copy` then copies the sorted file into the mount: the mount path must still be
  inside an rclone mount (`findmnt -T <mount path> -t fuse.rclone --noheadings`); the
  directory is created; the file is written to `.ftp-camera.tmp.<name>` and renamed to
  `<name>` (a failure removes the temporary file). Success: `  [<ts>] copy  <name>  → mount cache (VFS will upload)`, counted. Failure: `  [<ts>] copy fail  <name>  (rc=1; sorted locally, rerun ftp-camera --push)` and the reason indented four spaces, on stderr; counted,
  and the sorted path is listed in the summary.

### Shared

- E64. A command that fails where nothing above names its message prints
  `ERROR: <what> failed: <reason>` and exits 1.
- E65. Sizes are printed as `numfmt --to=iec` (or `--to=iec-i`) prints them: under 1024 the
  whole number; otherwise scaled by 1024 with the unit K, M, G, T, P, E (Ki, Mi, ... for binary
  units), one decimal below 10, rounded away from zero; a value that rounds to 1024 moves up a
  unit.

## 4. Where the Go departs from the bash

| #   | bash behaviour                                                                                                                                     | Go                                                                         | exp.     |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------- |
| B1  | `/etc/ftp-camera/config` was `source`d (any shell in it ran) and an unset key fell back to the environment                                         | read as `KEY=VALUE` data; the environment is not consulted                 | E11      |
| B2  | the JPEG trailer search matched `ffd9` in the hex dump, so a nibble-misaligned `x f f d 9 x` passed                                                | byte-aligned `FF D9`                                                       | E14      |
| B3  | the strip check folded exiftool's stderr into the numbers it parsed                                                                                | stdout only                                                                | E15      |
| B4  | an arrival that had vanished (the camera's retry moved it) was reported as `partial ... truncated at ? bytes`: the "vanished" code was unreachable | a quiet no-op, as intended                                                 | E60      |
| B5  | `--help` and usage errors needed the `camera` account                                                                                              | not needed                                                                 | E1, E7   |
| B6  | a tunable that was not a number made `[ -ge ]` fail inside an `if`, silently disabling the check                                                   | refused at start, exit 2                                                   | E6       |
| B7  | the default route's device was field 5 of the line, wrong for `default dev <x> ...`                                                                | the word after `dev`                                                       | E47      |
| B8  | a device with no IPv4 address advertised `pasv_address=` (empty)                                                                                   | an error                                                                   | E47      |
| B9  | with neither gum nor fzf, the suspend-inhibit re-exec passed no flags, so the re-entry printed the launcher note a second time                     | passes `--no-tui`                                                          | E9       |
| B10 | the launcher replaced the process with itself; gum and fzf were called with their own flags                                                        | continues in-process; the shared `ui` gum and fzf forms                    | E8, D4   |
| B11 | colour escapes were written even to a pipe                                                                                                         | only to a terminal, never with `NO_COLOR`                                  | E10      |
| B12 | the per-session summary lost the two-space indent and collapsed spaces in names (awk field rebuild)                                                | `  <path>  <size>`                                                         | E57      |
| B13 | a monitor that ended by itself (tail or inotifywait dying) ended the session with its status, often 0                                              | `ERROR`, exit 1                                                            | E55      |
| B14 | a `firewall-cmd --query` that failed for any reason read as "absent"                                                                               | 1 is absent, other failures are errors                                     | E56      |
| B15 | `--copy` needed `python3` to read the RC stats                                                                                                     | read in Go; not needed                                                     | E30, D3  |
| B16 | a command that failed under `set -e` exited silently with its own status                                                                           | `ERROR: <what> failed: ...`, exit 1                                        | E64      |
| B17 | plain `sudo`; a credential that expired during a long session prompted from a background job                                                       | `sudo.Ready` (reason, then the prompt) before each root command; `sudo -n` | D5       |
| B18 | the `--debug-ftp` note named the triage script at its old path (the plan was cancelled and moved)                                                  | `CLAUDE/Plan/Cancelled/00066-.../triage.bash`                              | E48      |
| B19 | session counters, the arrival log and the start marker were temporary files                                                                        | in memory; the start is a timestamp                                        | E57, E59 |
| B20 | files were considered in directory order                                                                                                           | name order                                                                 | E18      |
| B21 | a second ERROR path checked the mount source twice; the first answer was already the source                                                        | read once                                                                  | E24      |
| B22 | the log was followed by up to three `sudo tail` processes (printer, async monitor, viewer)                                                         | one follower feeding the printer, the handler and the viewer               | E55      |
| B23 | the RC-address lookup's reason was printed before the ERROR line, prefixed with a bash function name                                               | after it, indented                                                         | E30      |

Kept as they were, and accepted: the capture date is used as exiftool prints it (an odd value
makes an odd folder name, as before); the prune prompt treats anything but `y` as no, the
documented default of a `[y/N]` question; `--copy` polls with no time limit (Ctrl+C is the way
out).

## 5. Go design

- `src-go/cmd/ftp-camera/main.go`: INT and TERM cancel a context; the real environment. `run`
  is tested.
- `src-go/internal/ftpcamera`: `App` and `Env` (runner, execer, streams, getenv, look-path,
  clock, sleep, the process's own path and pid, `/proc` as an `fs.FS`, the terminal seams);
  the command line and launcher; the config file; completeness; the sort pass; the remote modes
  (push, preflight, copy); prune; the server session (start, followers, per-upload handler,
  teardown); the hotspot and WiFi probes; the weak-link advisory; the IEC size format.
  Messages are printed where they arise and the function returns a reported error, so the
  stream and text of each line is the bash's.
- `src-go/internal/rclonerc`: the credential, the RC call, the availability probe, and the RC
  address of a mount (D2).
- `src-go/internal/systemd` gains `UnitActive`, `StartAsRoot`, `StopAsRoot` and
  `DaemonReloadAsRoot` (systemctl's owner; the root ones through `internal/sudo`).
- `src-go/qa/rules/table.go`: `nmcli` gains the owner `internal/ftpcamera`, and the vault
  helper's run-time path is a `dynamicNames` row.
- Concurrency: the log printer, the monitor and the default-mode viewer follower are goroutines,
  each over its own runner call; the per-upload handler runs on the monitor's goroutine, so
  arrivals are handled one at a time; the session's output goes through one locked writer.

## 6. The help text

`src-go/internal/ftpcamera/usage.txt` is the bash's help text, with the launcher paragraph
unchanged in meaning.

## 7. What only a host run proves

- H1. `ftp-camera --sort` on a real upload tree with real exiftool moves files as before.
- H2. `ftp-camera --copy-preflight` on the host prints the RC address of the configured mount.
- H3. A default session with the camera: banner, vsftpd up, the log lines, Ctrl+C tears down
  (vsftpd stopped, `/run` files gone, firewall rules removed) and sorts.
- H4. `--async-copy --view`: per-file sort, copy into the mount, geeqie shows the frame.
- H5. `--hotspot`: the profile comes up, the nm-shared rules are added, and it goes down at the
  end.
