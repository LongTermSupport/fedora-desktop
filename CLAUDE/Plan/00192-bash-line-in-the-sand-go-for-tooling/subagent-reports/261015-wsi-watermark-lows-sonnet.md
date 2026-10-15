# wsi and watermark review Lows: fixes

Branch `go-port/wsi-watermark-lows`, from `go-port/wsi-watermark` at 06bacc37.

- L1: cleanup removes the audio files only once a recorder was started (code now agrees with
  SPEC W6/B11, which says so explicitly). Test: a preflight failure leaves a file at the recording
  path alone. The PID file and IDLE are unchanged.
- L2: the 30 s limit timer is armed at the end of the prepare delay even when a TERM is pending
  (RECORDING is still not sent). Test fires the prepare delay by hand after STOPPING (kit field
  `held`) and expects the limit to stop the recording.
- L3: the `-d` check prints `Install: pip install --user faster-whisper` for the transcriber.
- L4: watermark usage exit code 5 says the unmarked output is removed, even one `--force` overwrote.
- L5: `ydotoolBin`, `wlCopyBin`, `xselBin`, `sessionX11`, `sessionWayland` used at every site.
- L6: the gosec G304 exclusion is `internal/wsi/(app|log)\.go` (the PID file and log reads); the
  wsi test files carry G304 in their own exclusion.
- L7: `CLAUDE/GnomeShell.md` describes the `setsid` and `spawnWatched` launch.

Targeted QA only: `qa-changed.bash` passed (go gate, docs, stop-grace suite 120, qa-changed suite 121).
The deployed `wsi` and `watermark` binaries change; the speech-to-text and image-watermarking
plays are already queued in `meta-deploy.list` on F44, so no entry is added here.
