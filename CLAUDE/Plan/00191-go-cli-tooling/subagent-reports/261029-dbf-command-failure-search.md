# Independent search: a command's failure tested but never carried

Searcher report. The dispatch gave the class and hazard description only (no rule or pattern text in the prompt).
Paths are relative to /workspace/.claude/worktrees/usage-bars. Go paths are under src-go/.

Disclosure: my first grep, run from src-go/ with an exclusion that failed to match (paths print without "./"),
printed three lines of src-go/qa/rules/testdata/... (badrun/bad.go, badexec/bad.go) into my output. I did not open any
rules/ or driver/ file, read no detector config, did not run the detector, and none of those lines is used below.

## Expected forms (written before searching)

Go (execx): `_, err := Runner.Run/Foreground/Exec/Combined/Sudo.Run/Capture`, then
- `if err != nil { return <fixed message / guess> }` with no `%w`, `%v`, `err.Error()`, `Stderr`, `Code`;
- `return false` / `""` / `0` / zero struct / "unknown" sentinel on error;
- `err == nil` used as a bool (`return err == nil`, `if err == nil {..}` with a guessed else);
- error-to-int collapse (exit code only), `continue` on error, a message naming a cause the code never checked.
Go (qa/internal/proc): same, plus `res.Code != 0` reduced to a fixed message without Code/Stderr.
Bash: `if cmd; then ..; else X=<fixed>`, `cmd || X=<fixed>`, `$(cmd 2>/dev/null) || var=""`, `if ! cmd >/dev/null 2>&1; then <guessed cause>`.

## Searches run

1. Go call-site grep: `.(Run|Foreground|Exec)(<ctx-ish>, `, `execx.Combined(`, `.Foreground(`, `.Exec(` over all first-party Go (vendor excluded): 320 raw hits in 154 files; each read with 4 lines of context.
2. Files mentioning `execx.Cmd`/`proc.Cmd` (123 files): every `Run(`/`Foreground(`/`Exec(`/`Capture(`/`Combined(` line diffed against the hits from 1 to find call forms 1 missed (wrappers, `bounded`/`tctx`/`rctx` contexts, `Sudo.Run/Capture`, `(execx.OS{}).Run`).
3. Files mentioning `execx.` or `proc.Runner/OS/Result` but not in 2 (60 files): checked for indirect runner use (systemd, gitx, sessprobe, engine, freeze, lxcfreeze, sudo wrappers).
4. Wrapper callers: every caller of `sessprobe.*`, `Sudo.Capture/AsRoot/Run`, `systemd.*` (UnitActive, HomeserverState/Units, CanReboot, Reboot, DefaultTarget, UserUnitEnvironment, LaunchElement), `gitx.*`, `rclonerc.*`, `tmuxccy.*`.
5. Ignored-error grep `_ := ...Runner/Foreground/...`: no hits.
6. qa module: all `proc.Cmd{` / `Runner.Run(` sites (47), read for how `err` AND `res.Code`/`Stderr` are handled.
7. Bash (files/var/local/claude-yolo/, claude-code/): greps for `|| X=` / `|| echo` / `|| printf`; `if [!] x=$(`; `$?`; `2>/dev/null`, `&>/dev/null`, `>/dev/null 2>&1`; `&& x=.. || x=..`; `if [!] <engine/git/curl/ssh/...> ...; then`; `curl`/`wget`. Each match with a fixed substitute was opened and read.

## Go: instances where I judge the hazard PRESENT

Technique tag: T = the call site was found by text search and the loss was visible there; R = reading the branch body or a wrapper was needed to see the loss.

1. internal/ccylaunch/tokenusage.go:433, (*usageReport).ask. Any Run error (start failure, curl exit, stderr) becomes status "000"; only ErrSecretInArgv is returned. Cached 15 min. The origin. T+R. Confident.
2. internal/ftpcamera/upload.go:257, findmnt probe. Any error becomes "%s is no longer served by an rclone mount" (no err, no stderr). T+R. Confident.
3. internal/rclonerc/rclonerc.go:122, (*Client).AddrForMount. Any findmnt error becomes "%s is not inside any mount". T+R. Confident.
4. internal/vmtest/member.go:94, listDomains. Error becomes "virsh -c <uri> list failed"; stderr, exit code and start error dropped (Stderr not streamed here). T+R. Confident.
5. internal/vmtest/build.go:314, blkid label read. `err != nil || label == ""` becomes "could not read the volume label of <iso>"; err dropped (stderr is streamed; exit and start failure are not). T+R. Confident.
6. internal/vmtest/hostonly.go:156, redaction check. Any bash error (including a start failure) becomes "a supplied secret survived redaction ... NOT safe to share": the verifier failing to run is reported as a positive finding. T+R. Confident.
7. internal/wsi/prepare.go:51-59, probeServer. curl error replaced by `code == "" => "connection failed"`; err only reaches a debug suffix when the HTTP code is 2xx. Closest twin of the origin. T+R. Confident.
8. internal/ftpcamera/app.go:310, findUploadDir. Any getent error becomes "'camera' user not found - run play-ftp-camera.yml". T+R. Confident (mild).
9. internal/ftpcamera/network.go:181, default-route lookup. `ip route` error leaves dev "" and the caller prints "No default network interface found AND no hotspot active". T+R. Confident.
10. internal/ftpcamera/network.go:96, firstIPv4. `ip addr` error returns ""; the caller says "<dev> has no IPv4 address". T+R. Confident.
11. internal/ftpcamera/teardown.go:197, rclone-tail --once. Any error becomes "could not read upload status - no matching mount, or the rclone RC is unreachable" (two guessed causes). T+R. Confident.
12. internal/teams/board/collect.go:233, readTeams. `HomeserverUnits` error becomes "systemctl could not list the agent-bus homeserver units." with the cause not printed. R only (the runner call is inside systemd.HomeserverUnits, so a search for the runner never lands here). Confident.
13. internal/teams/hot/list.go:259, up-cache: `cache.up[team] = err == nil && state == up` records a systemctl that could not run as "not up". R. Unsure.
14. internal/teams/hot/move.go:43: `err != nil || state != up` returns "OK, skip", so a systemctl failure reads as "homeserver down". R. Unsure.
15. internal/teams/ops/ops.go:406-417, element play: after the busy-lock check, any other error (including "play binary did not start") becomes "play-agent-bus-element.yml failed (above)"; nothing is above for a start failure. T+R. Unsure (stderr is streamed; exit code and start failure lost).

## Go: BORDERLINE (cause partly lost, or the guess is declared by design)

- internal/ftpcamera/sort.go:57: ExitError tolerated; any start error returns "" (no date).
- internal/ftpcamera/remote.go:78,86 findMount: error returns "", "" (no mount found).
- internal/ftpcamera/remote.go:218 cp: "Failed to copy files to mount" (cp stderr streamed; exit/start failure lost).
- internal/ftpcamera/remote.go:236 and network.go:59: message prints combined output, never err; a start failure prints an empty body.
- internal/ftpcamera/network.go:134 (iw link -> wifiInfo{}), :219/:230 (apSupported -> "" = unknown), verify.go:83/87 (-> `unverifiable`): result type has an honest "unknown" state. Likely absent.
- internal/ftpcamera/wifipass.go:120: first nmcli try dropped; the second try's error is carried. Absent in effect.
- internal/sessions/routes.go:65-68 (ProcessTable): prints the output only, not err.
- internal/sessions/routes.go:238: "signalling X to Y failed." (child stderr streamed; exit code and start failure lost).
- internal/tmuxccy/takeover.go:90-114: message built from output only; a start failure prints "()".
- internal/vmtest/fetch.go:122 (curl: "download failed: <url>", exit lost, stderr streamed), :166/:171 (gpg2 stderr printed; any error reads "GPG signature verification FAILED ... refusing the artefact", including gpg2 not starting).
- internal/vmtest/lab.go:226-230 + helperExitCode (-1 for non-exit errors): callers testing `code > 0` (verify.go:39) treat a start failure as "no failure".
- internal/vmtest/build.go:74, guest.go:88, steps.go:403, teams/login/login.go:504 (wl-copy), :490 (gsettings -> historyUnknown): readiness probes returning bool/unknown. Mostly absent.
- internal/wsi/transcribe.go:127,143: error reduced to an integer by exitCodeOf (127/124/1 for non-exit errors); curl stderr is shown. transcribe.go:56 prints soxi output, not err.
- internal/wsi/dbus.go:113: notification failure logged at debug as "gdbus call failed"; err dropped.
- internal/reclaim/actions.go:147,182-190,222,250: "reported an error" with output streamed above; exit code and start failure lost. reclaim/containers.go:130-143 (`unread` is given only `out`); report.go:102-117 duf/df ladder (final failure is carried).
- internal/ghaccount/app.go:661,717: `exit.Code != 0` with fgErr == nil -> "Authentication failed" (code dropped; output is on the terminal).
- internal/lxcfreeze/engine.go:241 probe: any ExitError is "said no" (container vanished/unreadable); exit and stderr dropped by design.
- internal/pingbus/statusline/statusline.go:246-258: a start failure becomes the `notStarted` status code; err text not kept.
- internal/open/choose.go:140 and internal/ui/gumx.go:54 (tput cols): fallback width constant; cause genuinely irrelevant. Absent in effect.
- internal/systemd/unit.go:16 and systemd.go:88: a non-zero is-active exit is the designed answer; non-exit errors are returned. Absent.
- qa module (qa/internal/proc: a non-zero exit is a Result, err is a start failure only): every site wraps start failures with %w and reports Code/Stderr/Stdout, or runs with Merge so the output is the failure text. Borderline only: qa/godeps/run.go:241,263 print Stdout (merged) but not Code; qa/bash/syntax.go:27-31 `msg = "syntax error"` when exit 2 with empty output. No present instance found in qa/.

Sites read and judged CARRIED: internal/wsitoken/app.go:256 (the reverse of the origin: code, stderr and deadline all carried), nord/connect.go and commands.go, tmuxccy/{panes,tmuxccy,start}.go, sessprobe/collect.go and its callers, sudo/sudo.go, gitx, netpolicy, consumer, ccc, ccclaude, ccylaunch/front.go:510, sshkey, podfreeze, declare, engine, ghlogin, teams/{create,hot,login} (rest), stt, watermark, wsimedia, convert, setuprclone, ui/{choose,picker,ui}, agentbus, vmtest (rest), all of qa/ otherwise.

## Bash (files/var/local/claude-yolo/ and claude-code/)

Present (status tested, replaced with a fixed guess; a cause a person needs is lost):

B1. claude-yolo/lib/token-management.bash:438-457, `_usage_fetch_one`. curl failure (any exit code) becomes "000", cached, shown as "could not reach the API". The origin. Confident.
B2. claude-yolo/lib/token-management.bash:729-737, `validate_token`. Any failure of `container_cmd run ... claude --version` (engine down, image missing, token revoked) becomes "return 1 # Token invalid"; output to /dev/null; callers (:881, :980, claude-yolo:1652) report a failed validation ("api"). Confident.
B3. claude-yolo/claude-yolo:3357-3363. `container_cmd run ... alpine wget` failing (engine, pull, DNS, wget) becomes "NETWORK ERROR: '<net>' has no internet access"; output discarded. Confident.
B4. claude-yolo/entrypoint.sh:231-262. `curl ... 2>/dev/null || github_meta=""`, `jq ... || ""`: any failure prints "Could not fetch GitHub SSH host keys (offline?)". Confident.
B5. claude-yolo/claude-yolo:1910-1927. Version probes capture the cause (`2>&1`) then discard it; "unknown" becomes "Could not determine Claude Code version (offline?)". Confident.
B6. claude-yolo/claude-yolo:2835. `docker network rm "$net" 2>/dev/null && ... || echo "Could not remove (may still have containers)"`. Confident.
B7. claude-yolo/lib/docker-health.bash:221,239,454,472,600,621. `container_cmd stop ... >/dev/null 2>&1` failure becomes "(may already be stopped)". Confident.
B8. claude-yolo/claude-yolo:546-550. `source "$config_file" 2>/dev/null || { "Config file corrupted"; rm -f }`: the shell's error is discarded and the file deleted on that guess. Unsure.
B9. claude-yolo/lib/docker-health.bash:34 (and :82, :377, :538 as unchecked captures): engine failure becomes "" and "no containers found". Unsure (comments call it deliberate).

Borderline:
- claude-yolo/claude-yolo:2830, 2841: "Some containers could not be stopped (continuing)", stderr discarded.
- claude-yolo/lib/network-management.bash:1293 ("Failed to add DNS server", stderr discarded), :842 (inspect failure => count 0 => "network empty"), :295 (preference load, stderr dropped), :1156-1198 (compose probes; "undetermined" returns 1, documented).
- claude-yolo/lib/docker-health.bash:49, 418 (documented race => ""), :126, :145 ("unknown" sentinel).
- claude-yolo/lib/common.bash:942, 944 (annotated FAIL-FAST-OK sentinels that force a rebuild); lib/image-build.bash:15 and claude-yolo:2023-2039, 2109 (image inspect failure == "no image", documented probe).
- claude-yolo/claude-yolo:2093-2097 (inspect failure => "unknown", message discarded); :1975 (debug-only "already removed or never created").
- claude-yolo/lib/ssh-handling.bash:54-64 (git config/remote failure => "" url, with `2>&1` merged into url), :988-992 and :1088-1091 (ssh-keygen failure not mentioning "passphrase" => "needs none"), :1002-1005 (askpass helper failure => "gave no passphrase").

Carried/absent (examples): all `|| rc=$?` captures, agent-bus-*.bash, tmux-session.bash, token-usable.bash, session-lifecycle.bash, restart-request.bash, session-registry.bash jq reads, image-build-prune.bash (exit printed), network-management.bash :182, :194, :1261, :1278, claude-yolo :414, :964, :1887, :2603, :3699, entrypoint.sh :88, claude-code/cc and cc-teams.bash.

## What text search found that reading could not have checked

- The population: every Runner/Foreground/Exec/Combined/Capture call site (about 330 Go, 47 in qa) and every `|| X=`, `2>/dev/null`, `if ! x=$(` form in bash. Search gives the set; it cannot say whether an error is carried.
- The files that use the runner indirectly (diffing the 123 `execx.Cmd` files against the hits).

## What reading found that text search could not have

- Whether the error variable is used in the branch (items 1-11, B1-B7): a search for `err != nil` finds hundreds of look-alikes; only the branch body shows the message is a guess.
- Findings behind wrappers (items 12-14: systemd.HomeserverUnits/State; sessions/routes.go:65: sessprobe.ProcessTable; vmtest helperExitCode): the dropped error is derived from a function that itself runs the command.
- Errors reduced to a different value type (exit-code int in wsi, `notStarted` constant in statusline, "unknown" sentinels), and the qa/internal/proc split where a non-zero exit is a Result.Code and so never appears as `err`.
- In bash, the difference between a documented probe ("does the image exist") and a guessed cause (B3, B6, B7): needs the surrounding message.
- That many apparent candidates are in fact carried (wsitoken readUsage, nord, tmuxccy, sudo): reading removed them.

## Counts

Go present: 15 (items 1-15), Go borderline: about 25 entries. Bash present: 9 (B1-B9), borderline: about 12 entries.
