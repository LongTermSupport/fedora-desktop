Independent search: liveness-claiming status labels fed by non-liveness sources
================================================================================

Dispatch did not contain the rule or its pattern. Not read: src-go/qa/rules/, CLAUDE/GoProjectRules.md.
Detector not run.

Forms written down before searching
- String literals/constants "active", "online", "up", "running", "alive", "listening", "connected", "ready", "healthy", "live"
  in Go, Python, bash, JS, Ansible text; switches/maps turning a registry/declaration/record field into one of them.
- Green colouring of such words; default labels given when a probe fails; cached/last-known facts in present tense;
  TTL-based liveness; "held" seats.

Searches run
1. quoted liveness words over src-go (non-test, non-vendor, excluding qa/rules)
2. same over helpers, files, scripts, extensions, playbooks, tasks
3. echo/printf/print/Printf/Sprintf lines with is running/alive/online/active/listening/up, Online/Active/Running/Connected/Healthy
4. glyph lines (check mark, [ok]) with running/active/online/up/alive/listening/connected
5. git grep 'parked.*active|active.*parked' over helpers files scripts playbooks tasks docs
6. active/online/alive/running/listening in src-go/internal/teams, sessions, pingbus, tmuxccy, sessprobe, restore, registry
7. label/text/status strings in extensions/*.js
8. usage.txt, help.txt, docs/agent-bus.md for the same words
9. git show HEAD:snapshot.go grep active
(Searching the symbol WordNotParked was blocked by the LSP hook; its users were found by reading parse.go and by text greps.)

Method check: originating instance found
- HEAD:src-go/internal/teams/board/snapshot.go:249 `activity[m.Handle] = "active"` (found by search 9/6). The working tree already
  replaces it with memberState/liveWord (snapshot.go:111-136, reading the LIVE line).

CANDIDATES: file:line | word | source | verdict

CARRIES THE HAZARD
1. helpers/agent_bus/admin.py:1246 (doc 1232-1237) | "active" in `MEMBER ... active|parked` | reg.is_parked(handle), the registry park flag only |
   CARRIES the shape (the originating instance's upstream). Docstring and docs/agent-bus.md:668 say "never running", and the working tree adds
   a LIVE line, so the Go consumer is fixed; but `agent-bus list` still prints "active" to a person, and Go keeps it as agentbus.WordNotParked
   (parse.go:41-44). Confident on shape; display harm mitigated downstream.
2. helpers/agent_bus/declare.py:1352-1357, 1832 | "active" | same list output, as --roster input | vocabulary only, not a shown status. Unsure it counts.

UNSURE
3. admin.py:1220-1229; pingbus cli.py:2697; Go graph.go:229, snapshot.go:119-128, table.go:265 (green) | "listening" | a status event with an
   `until` expiry, judged until > now | a heartbeat with a TTL: a session that died just before expiry reads listening until `until`.
   Documented (picker/help.txt:15-19). Probably not the hazard; found by reading.
4. graph.go:204-206 stateOf | "offline" | r.State == wordParked (registry flag), Live not consulted | reverse direction: a parked member whose
   session still listens shows offline. Found by reading only.
5. files/var/local/claude-yolo/lib/docker-health.bash:360-371 | "active" in `ccy --top` STATUS | default when Tty inspect fails or Tty false;
   only Tty=true rows get the pgrep orphan check | mild: container was observed running, but "active" is a default. Text search + reading.
6. playbooks/imports/optional/common/play-qobuz.yml:415 | "installed and running" | qobuz_scrobble flag | low: lines 291-297 assert
   ActiveState == active earlier under the same condition. Does not carry in practice.

CLEARED (liveness source verified)
- systemd.go:28,85-110 StateWord "up": systemd ActiveState (list-units/is-active). Users: board/collect.go:519, snapshot.go:189,
  picker/sessions.go:312, hot/add.go:104, hot/remove.go:31. (active(exited) caveat only.)
- coordinator/coordinator.go:11-91 "(running)/(not running)/(parked)/(not known here)": Seat.Held is a process-held lock; unknown stays unknown.
  checkout.py:797-828 named_elsewhere keeps desired memberships only for system.running_sessions(). Honest.
- choose/choose.go:72 "held by a running session": same Held source. Clear.
- board/collect.go:547-553 session "held"/"free"/"(stopped)": lock state + tmux Live. Clear.
- hot/list.go:227-246, hot/cc.go:84-118 stateWord/rowState "ok": process table keeper checks, homeserver status, seat holds. Clear.
- snapshot.go:17-31 "container stopped at the last apply": last-known, worded as a past fact. Clear.
- sessions/picker.go:166-174 "N running", table.go:52-76: rows from live tmux list. Clear.
- pingbus/statusline/statusline.go:357-398: membership and "(down)" mark only; no liveness claim. Clear.
- wsi-server-manager:44-82 (os.kill 0, PING "alive"), wsi-stream-server:1336,1548 (lock). Clear.
- scripts/check-displaylink-status.sh:141-258 (is-active). Clear.
- helpers/agent_bus/health.py:180,244,315-325; helpers/agent_bus_lxc/members.py:103,1263-1266. Clear.
- lxcfreeze/parse.go:20, containerimages/stale.go:195, podfreeze/engine.go:49, netpolicy/podman.go:9, hot/hot.go:56,531,
  restart/jobs.go:42, registry/restore.go:200, restore/verdict.go:32,111: engine state read at call time. Clear.
- nord/connect.go:218, commands.go:33,109 (nmcli state). Clear.
- claude-yolo/lib/network-management.bash:476,526 (after connect exit status), 1101-1118 (after network_has_running_containers probe). Clear.
- bus/declare/queries.go:422 "Team %s is up": next-steps text after install, not a status column. Clear, low risk.
- extensions/speech-to-text@fedora-desktop/extension.js:284 "Ready." (UI-ready at enable; server polled every 5 s). Clear.
- containerwatch/cli.py:906, displaylink_recovery/recovery.py:41,69: engine/display queries. Clear.
- pingbus config/membership/cli "active team": means configured team; messages are about configuration or a held lock. Not a liveness claim.
- Ansible msg/debug lines with running/active in playbooks/ and tasks/: preceded by systemd asserts or are warnings. Clear.
- Other literal hits (ui key names "up", ftpcamera nmcli "up" command, screen.Ready, stt reply status, vmtest prune action "live", ghlogin Active field): not displayed liveness. Clear.

Attribution to technique
- Text search found, and reading could not have: the complete list of literal sites across languages (searches 1-4), including
  docker-health.bash:360, admin.py:1246, declare.py:1356, play-qobuz.yml:415, and the --roster round-trip of "active".
- Reading found, text search could not: candidates 3 and 4 (TTL "listening"; "offline" from the park flag), the default-label behaviour in
  docker-health.bash, the honest-Unknown design in coordinator.go, and that named_elsewhere filters by running_sessions, which cleared
  "held by a running session". Held semantics (flock, not registry) needed reading checkout.py.

Scope note: the working tree holds an uncommitted fix of the originating instance; the committed HEAD still has it at snapshot.go:249.
