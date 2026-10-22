# Independent search: three checker defect classes in active plan folders

Scope: tracked `.bash`/`.py` under `CLAUDE/Plan/NNNNN-*/` (five digits), excluding `subagent-reports/`, `JOURNAL/`, markdown, Completed/Cancelled/Archive. No detector rule, detector config or detector output was opened or run; the dispatch described the classes only, with no rule text in it.

Forms written down before searching:
- Class 1: `agent-bus add-member`, `seat take`, Matrix `/invite`, `human password` plus login, then `pingbus report|say|send|ask-human|history`, raw `/send`, `/messages`, `/state` as that user; joins by `pingbus recv|wait|watch`, a keeper, or `/join`.
- Class 2: `claude-yolo` / `ccy` / `--headless` / `--teams` launches, `play-claude-yolo`, `podman build`, then `timeout N`, `*_S=` constants, counted `for` loops, `deadline = SECONDS + N`.
- Class 3: `pingbus recv|wait` (3), `login_report` / `probe` (3), `check_freshness` (1), `agent-bus check-rooms` / `health status` (1), `ccy-teams --list` (1), the unit-state queries of the init system (is-active, status, is-enabled), `ssh-add -l|-L`, `firewall-cmd --query`, `cmp -s`, `grep -q`.

## Class 1: INVITE-WITHOUT-JOIN

No new instance. The only add-member user that acts as the added member is the known, already fixed one.

Checked and ruled out (each acts as the member only after a join, or only names it):
- 00211 `_acceptance-stage2.inc.bash:364-411` `a3_take` then `a3_join` (`pingbus recv`, rc 0 or 3 accepted) before `a3_report` (443) and `a3_check`/`a3_status` (458-468). Known fixed; confirmed in HEAD.
- 00161 `_acceptance-steps.inc.bash:170-197` `add_member`, then `place_member` (221-229; first `recv` joins, `expect_status ... 3`) for both a and b, in `place_members` (leg at acceptance.bash:266), before any `send_ping` / `wait` (M1 legs 267+). Fine.
- 00161 `_acceptance-u23.inc.bash:446` `u23_join`: add-member, place, `config check`, `recv` (expect 3, line 469), and only then `u23_ping` (476-490). Fine.
- 00161 human by curl, `_acceptance-steps.inc.bash:306-316` `human_login` posts `/rooms/.../join` before any `human_curl PUT .../send` (check_human_addressed). Fine.
- 00201 `_acceptance-m7.inc.bash:433-444` `m7_add_member` adds m7peer.1 and m7solo.1 (`--type=host`). They are only NAMED (`say --to`, lines 738/767), never a sender; the sender is the real ccy seat session, whose keeper syncs. Not an instance (named-only members). The m7 human logs in and `/join`s at line 495 before reading.
- 00203 `_acceptance-m3.inc.bash:206-231`: `human password`, login, `/join` (213), then `/send` (224) and `/messages` (231). Fine. `_acceptance-m6.inc.bash:270` `seat take` then a launch whose seat keeper syncs. Fine.
- 00194 `_acceptance-goodbye.inc.bash:302-332`: `human password`, `seat take`, login and `/join` (332) before reading. Seats post via keepers. Fine.
- 00211 `spike_probe.py:175-280`: every invited account (worker, coord, human) is joined by `sp.join` before sending. The one deliberately left pending (worker invited to questions, R4, line 268) only syncs the main room, which is the point of R4. Not an instance.
- 00161 `triage_probe.py:1124-1180`: sender/receiver/human invited at 1124, joined at 1156-1158 before any send. The initial sync as receiver (1149) is an intentional probe of the pending invite. Not an instance.
- 00211 `_acceptance-stage2.inc.bash:540-575` (A4 report, ask-human) and 640-660 (A5 history) run via `session_run` in already-seated sessions (keeper-synced) or declared LXC members. Not add-member users.

## Class 2: FIXED WAIT THAT IGNORES AN IMAGE BUILD

Three confirmed instances: one shared helper reused by four plans' legs, plus two independent constants.

### 2.1 00203 `_acceptance-m2.inc.bash:146-165` `m2_container_up` (120 iterations of about 1 s; "120 s at most")
The function header comment reads "the running container labelled ccy-session=NAME; 120 s at most." The body is a counted loop of 120 iterations that lists running containers with that label, returns on the first hit (after a 5 s settle), fails early if the tmux session has gone, otherwise sleeps 1 s, and ends with the message "no running container labelled ccy-session=NAME after 120 s".
- Launch it waits on: `m2_launch` (125-130) runs `/var/local/claude-yolo/claude-yolo --headless --no-restore --no-supervise ...` in a tmux pane. A launch that first rebuilds the project/ccy image (about 16 min, under `$XDG_RUNTIME_DIR/ccy/image-builds/*.lock`) has no container for that time, so the loop expires and a correct product fails.
- `m2_prepare` (101-107) only tests that the launcher is executable. Nothing prebuilds the image or waits on the lock. The loop has no lock awareness (compare m7's `m7_image_build`).
- Callers (all inherit the 120 s cap): `m2_start` 171-172 (two sessions launched back to back, waited in turn); `_acceptance-m1.inc.bash:30` `m1_start`; `_acceptance-m3.inc.bash:167` `m3_start`; `_acceptance-m4.inc.bash:215` `m4_launch` (called twice from `m4_start`).
- Confidence: high. Same shape as the known m7 defect, with a tighter cap.
- The `M3_TURN_S` / `M4_TURN_S` (240) polls start after the container is up, so they are not build-exposed.

### 2.2 00161 `_acceptance-u20.inc.bash:65-67, 477-480` `U20_START_S=600`
Verbatim comment: "Bounds, in seconds. A ccy launch validates the token in a container and, on the day's first launch, may update Claude Code in the image first." Then `readonly U20_START_S=600`, used by `u20_poll` for "session's ccy container running" (477) and "seat held by session" (480).
- Launch: `u20_launch` (362-381) execs the launcher with `--headless --no-restore --no-supervise --teams ...`. The comment anticipates an in-image Claude Code update, not a project-image rebuild (about 16 min, over 600 s). `u20_prerequisites` (232-262) checks only the base image's `claude-yolo-version` label, not the per-project image or the build lock. `u20_poll` (104-128) has no lock awareness.
- Used for every U20 launch (`u20_start`, 468-480), the plain and named sessions included.
- Confidence: medium-high (cap 10 min vs about 16 min build; whether a given run hits a rebuild depends on the host).

### 2.3 00194 `_acceptance-goodbye.inc.bash:43-44, 519-522` `GB_START_S=600`
Verbatim comment: "Bounds, in seconds. A launch may update Claude Code in the image first." Then `readonly GB_START_S=600`, used by `gb_poll` for "its container running" (520).
- Launch: `gb_launch` (494-531) runs the launcher with `--teams ... --no-restore` in its own tmux. `gb_poll` (92-116) is a fixed deadline with no build-lock check, and `gb_starting` (439-455) only detects the launcher waiting at a question. A first launch in a scratch checkout is exactly when a project-image build is likely.
- Confidence: medium-high. Same reasoning as 2.2.

### Near-misses ruled out (class 2)
- 00169 `acceptance.bash:104,158` `LAUNCH_TIMEOUT_S=1800` with `timeout`: 30 min, and the code says "an image rebuild is slow", so sized for a build. Not an instance. Weak caveat: it does not wait out another process's build lock (a queued ~16 min wait plus its own build could pass 30 min). Low confidence it matters.
- 00201 `_acceptance-m7.inc.bash:69-70,589-614` `M7_START_S` / `m7_poll_launch`: the known fix (restarts the clock while a build lock is live). Fine.
- 00203 `_acceptance-m6.inc.bash:56,266` `M6_UP_S=60` waits for a seat keeper of a cc stand-in claude (cc launcher, records written by hand, no ccy image launch). Not an instance.
- 00167 `check-cc-project-dir.bash:44,52,60` `timeout 10` runs `/var/local/claude-code/cc-project-dir` (a refusal before any container). Not a ccy launch.
- 00161 `triage-ssh-agent-probe.bash:29,41` `ATTEMPT_SECONDS=40`: times `ssh-add`/agent probes, not a ccy launch.
- 00209 `_acceptance-s1.inc.bash:258` `timeout --foreground 300` on the ccy-teams TUI (Go binary, no container). 00211 `JOIN_WAIT_S=180` waits for keeper joins in already-running sessions. 00213 and 00211 stage legs only run plays and read files.
- 00063 / 00073 `run_headless` is `run.bash --headless`, not the ccy launcher; no image.
- 00054, 00062, 00092, 00098, 00134, 00137, 00139, 00158, 00160, 00165, 00173, 00196 `deploy.bash`/`update.bash` legs run `plan_ansible_playbook play-claude-yolo.yml` (no `timeout` in `_planlib.inc.bash`) or only read the launcher/Dockerfile. 00080 triage runs a throwaway `podman run` of a probe image after `podman image exists`; it never builds.

## Class 3: DOCUMENTED NON-ZERO RESULT TREATED AS FAILURE

No unfixed forward instance in the working tree. One known instance (fixed in an uncommitted edit; the committed version still has it) and two weaker "reverse" notes.

### 3.1 00214 `check-changed-plays.bash` `check_report` (known)
- Committed version, lines 95-96: the `login_report` call (`--repo-root ROOT --no-notify`, stdout to collection.out) ends in `|| fail "the collection exited <status>; its output is in collection.out"`. `login_report` exits `EXIT_FINDINGS=3` when it has findings (`helpers/host_health/login_report.py:71`), the normal outcome whenever a play has to run. The leg whose purpose is the "play to run" case therefore failed on exactly that case. Confidence: high.
- The working tree (uncommitted edit) reads `EXIT_FINDINGS` from the product (89-93, 100), captures `status` and accepts 0 or 3 (104-107), and line 112 requires 3 when `plays_to_run`. That is correct for this class. I found no second spot in the file: `check_same` (62-63) accepts `check_freshness` 0 and 1 (`EXIT_FINDINGS=1`, `check_freshness.py:44`) and fails on 2; `changed_plays` fails on any non-zero (its only non-zero is EXIT_NO_ANSWER=2), correct. `login_message` and `fedora-desktop-health` have no documented non-zero normal outcome.

### 3.2 Reverse notes (a non-zero accepted without distinguishing it), low severity
- 00109 `triage.bash:111-117`: an `if` around `python3 -m helpers.host_health.login_report --no-notify --no-handoff` whose else branch prints "(exit non-zero - the findings above are what a login would surface)". Every non-zero (a crash, exit 1, included) is read as "findings", where the contract is 3 = findings. A crash would be mislabelled. It is a probe, not a failing leg. Confidence: medium that it is the reverse form. (00109 `acceptance.bash:662-686` and `deploy.bash:150` do it right: they separate 0, 3 and other.)
- 00201 `_acceptance-m7.inc.bash:681-683` `m7_status_ready` (`m7_bus_exec status /dev/null status || return 3`) and 00161 `_acceptance-u20.inc.bash:537-545` `u20_status_field` (the host `pingbus status` call followed by `return 3` on any failure): any non-zero from `pingbus status` becomes "not yet" and is polled again, so exit 10 (untrusted), 78 (config), 64 (usage) or a crash is retried until the timeout and reported as a timeout, not as the real code. The checker still fails in the end. Confidence: low-medium; only the diagnosis is hidden.

### Correct handling seen (ruled out)
- 00161 `_acceptance-steps.inc.bash:224-229,356-398,413,441,509` and `_acceptance-u23.inc.bash:469,476-492`: `pingbus recv`/`wait` status captured and judged by `expect_status ... 0`, `... 0 3` (b's recv-human accepts 3) or `... 3` (the first join).
- 00211 `a3_join` (402-409) accepts 0 or 3. 00201 `m7_say_solo` / `say-outcome` (738-748) treats 3 as "not yet", other as fail; `m7_check_ambiguous` expects 64; `m7_rule_present` (396-402) separates `firewall-cmd --query` 0/1/2.
- 00203 `_acceptance-robust.inc.bash:79-86` `health_status <expected exit>`: `agent-bus health status` exits 1 on problems (`helpers/agent_bus/health.py:69`), R6/R7 pass 1, R2 passes 0. `run_health_check` starts the `check` unit, which does not exit non-zero (only `status` does). R7 (line 201) captures `ccy-teams --list` rc (`listed`; 1 means a team's members could not be read). `_acceptance-m4.inc.bash:82-90` `m4_run <label> <exit>` takes the expected exit (64, 75, 1, 0). `ccy-teams session list` exits 0 normally (`src-go/internal/teams/hot/list.go:115`).
- 00201 `m7_check_list` (451-461) fails on any non-zero `ccy-teams --list`; that is exit 1 only when a team's members cannot be read, an error in its setting (freshly created, up teams). Near-miss, not an instance.
- 00139 `acceptance.bash:125-159` handles `ssh-add -L` rc 1 (no keys) and other. 00109 `deploy.bash:150` (`login_report || [[ exit-status -eq 3 ]]`) is the right form.
- Triage `probe` wrappers (00134 `triage.bash:83-90`, 00203 `_triage-hs.inc.bash:11-15`) record rc as a fact (`if out=...; then rc=0; else rc=status`), so the unit-state queries that end `host_memory` (_triage-hs:146, is-active, exit 3 when inactive) and `lxc_reconcile_state` (00134 triage:391, status, exit 3 for an inactive oneshot) do not fail the leg. Ruled out.
- 00161 `ssh-add -l/-L` (u20:277, 654) and 00109 unit-state queries (707, 928, 945-948): there the non-zero is exactly what is being checked for. Ruled out.
- `agent-bus check-rooms` has no caller in any active-plan script (00211 stage-2 `fake_a12` only models rc 1 = problem). No `pingbus` exit 4-10 is expected or tolerated anywhere beyond the cases above.

## What each technique found

Text search (grep over all active plan scripts) found by itself:
- Every `add-member` / `invite` / `/join` site (class 1 inventory); the `*_START_S=600` constants and `LAUNCH_TIMEOUT_S=1800`; the `login_report` / `EXIT_FINDINGS` sites; the `|| return 3` mappings.
- Searches run: `add-member|add_member|/invite|invite`; `pingbus ... recv|wait|watch|report|say|send|ask-human|history`; `/join|/send/|human password`; `claude-yolo|ccy|--headless|podman build|play-claude`; `_S=|timeout|deadline|SECONDS|sleep`; `m2_container_up|image-build|build-lock`; `EXIT_NOTHING|login_report|check-rooms|health status|seat check|--list`; unit-state queries, `--query-`, `ssh-add`, `cmp -s`; `subprocess.run|check=True` across plan `.py`; EXIT_ constants in `helpers/pingbus/cli.py`, `helpers/agent_bus/cli.py`, `helpers/host_health/*.py`, `helpers/play_ledger/*.py`.

Reading was needed for (text search could not decide these):
- That `m2_container_up` is the shared choke point for four plans' launches (m1, m2, m3, m4) and has no build-lock awareness. A grep for `timeout` or `_S=` finds nothing in that file: the cap is a loop count, stated only in a comment ("120 s at most").
- That the `U20_START_S` / `GB_START_S` comments mention an in-image Claude Code update yet the cap is 600 s, and that `u20_prerequisites` checks only the base image version label.
- Class 1 verdicts: that `m7peer`/`m7solo` are targets only, that each human flow joins before sending, that `spike_probe.py`'s deliberately pending invite only syncs the main room, and that 00161 joins both a and b before any M1 leg (ordering in `acceptance.bash:265-267`).
- Class 3 verdicts: that `health_status`/`m4_run` take an expected exit, that `probe` wrappers record rc as a fact, that `m7_status_ready` / `u20_status_field` flatten any non-zero to "not yet", and the committed vs working-tree difference in 00214.

Text search matched things that reading showed are not instances: 00063/00073 `--headless` (run.bash flag), 00167 `timeout 10` (cc-project-dir), the `ccy` token in many `deploy.bash` files (Ansible plays, no fixed wait), unit-state queries in triage probes.

Not checked: Completed/Cancelled/Archive trees, non-plan scripts (`scripts/`, `files/`), `_planlib.inc.bash` beyond `plan_gather_leg` and a `timeout` grep, markdown acceptance docs. Only tracked files were read; the one untracked plan file (00214 `test_check_changed_plays.py`) is not an acceptance checker.
