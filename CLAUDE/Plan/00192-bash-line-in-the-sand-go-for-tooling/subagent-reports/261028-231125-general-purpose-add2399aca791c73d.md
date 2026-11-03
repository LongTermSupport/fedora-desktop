The play no longer asks for the camera hotspot Wi-Fi password: when none is saved it makes a three-word one (like `maple-river-candle`), vaults it, and keeps it on later runs. `ftp-camera` then shows the network name and password whenever the hotspot is up. Go tests and `./scripts/qa-changed.bash` pass; nothing has run on the host yet.

**Branch and commit:** `agent-add2399aca791c73d-e5c104bd`, one commit `fd9cf358b` (code, spec, docs, Plan 00192 Task 3.16 and its journal entry). Not pushed or merged.

**Generation, in Go:**
- A new mode, `ftp-camera --new-hotspot-password`, in `src-go/internal/ftpcamera/wifipass.go`. It draws three words from `crypto/rand` out of a list built into the binary (`words.txt`, 824 common words of 3 to 9 letters), so every password is 11 to 29 characters, inside WPA2's 8 to 63.
- The play runs it right after building the binary, with `no_log`; if it fails, only the exit code and stderr are shown.
- I moved the vault-save steps out of `tasks/ask-and-save-secret.yml` into a new `tasks/save-secret.yml`. Both the existing prompt and the new generation step use it, so there is only one save path.

**Display:** `server.go` and `hotspotPassword` in `wifipass.go`. The password is read from the NetworkManager hotspot profile with `nmcli -s`: as the user first, and through the session's existing sudo only if NetworkManager withholds it. I chose the profile because:
- it is what the camera actually has to match, including a hotspot made by hand in GNOME;
- it needs no vault password file;
- it puts no new plaintext copy on disk.

If even root cannot read it, the session stops before the FTP server starts and cleans up.

**How a saved password is kept:** the generation step only runs when `ftp_camera_hotspot_password` is not defined, so a saved one is never replaced. The early length check (8 to 63 characters) still runs on a saved value before anything changes. An unattended run no longer stops.

**Tests:**
- Tests were written first and failed to compile before the code existed.
- `go test -race -shuffle=on -count=2` passes for `internal/ftpcamera` and `cmd/ftp-camera` (93.0% coverage); golangci-lint finds nothing.
- The new tests cover the word list rules, the uniform draw, a failing random source, the mode end to end, and each display case (read as user, root fallback, unreadable, no password set, wired, no hotspot).
- `qa-changed.bash` failed once (a `no_log` on a check in `save-secret.yml` would have hidden its failure reason); I fixed that before committing. I did not run `qa-all.bash`.

**The two hang findings:**
- The hotspot password now has no interactive path at all, so its prompt cannot hang.
- The camera FTP password prompt does share that code path (`ask-and-save-secret.yml` → `ask-and-save-secret-attempt.yml` → `pause`), as do the MOK, nordvpn, qobuz, self-update, github-cli-multi and claude-yolo plays. They can hang the same way. I left that unchanged, as instructed.

**Needs action:**
- **`meta-deploy.list` needs `play-ftp-camera.yml` queued:** the play and the binary both changed. I did not edit the list.
- After that run, the new host checks H6 and H7 in the spec need doing: an unattended run with no saved password succeeds and vaults one, a second run changes nothing, and `ftp-camera --hotspot` shows the network name and password. Also note whether the read as the user worked or the root fallback was needed.
- **Decision for the owner:** three words from 824 is about 29 bits. Someone who captures the hotspot's Wi-Fi handshake could crack that offline in minutes to hours on one GPU. Four words would give about 39 bits at no other cost (a one-constant change).

Full report: `<worktree>/untracked/scratch/261028-hotspot-pass-report.md`