# lxc-attach with a file stream: independent search

The dispatch named the class and hazard only; it did not contain the detector's rule or pattern.
I did not open the excluded paths (src-go/qa/lxcattach/, src-go/qa/cmd/qa-lxc-attach-streams/,
.semgrep/lxc-attach*, scripts/qa-lxc-attach-streams.bash) and did not run the detector.

## Instances (1 real hazard)

### 1. REAL HAZARD (confident; found by reading)
CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:632-635
(in `ask_test_question`):

    out="$(printf ... | session_run "${COORD}" pingbus ask-human --team "${QTEAM}" 2>"${err}")" || rc=$?
    cat -- "${err}" >&2        # line 636; open_duplicate "${err}" at 640 reads it again

- Stream: stderr is the regular file `${PLAN_RUN_DIR}/ask-human.err` (line 623). stdin is a pipe and stdout a command substitution, so those two are fine.
- Wrapper chain: `session_run` (line 115) -> case `lxc)` (line 123) -> `in_guest` (line 560) -> `"${LXC_ATTACH[@]}"` (line 563). `LXC_ATTACH=(sudo -n lxc-attach)` is set in acceptance.bash:132. The `ccy)` branch is podman exec and is not affected.
- Reached when COORD is an `lxc` session. COORD comes from `find_session` (line 237) over SESSIONS, which holds both ccy and lxc kinds (lines 195-197); the comment at 613-614 says "whatever kind". So when the team's coordinator is an online declared LXC member, lxc-attach chowns the err file to the container root, 0600, and the `cat` at 636 and `open_duplicate` at 640 fail with Permission denied.
- The fake test (`fake_ask`, line 1079) sets COORD to a `ccy` session, so the fake run never exercises the lxc branch.
- Found by: reading. A grep for `lxc-attach` does not point at line 635. The redirect shows up in a grep for `2>"` or `session_run`, but only tracing session_run -> in_guest -> LXC_ATTACH shows it reaches lxc-attach.

## Near-misses examined and rejected

| Location | Why rejected |
|---|---|
| _acceptance-stage2.inc.bash:182 `in_guest ... </dev/null \| jq` | stdin /dev/null (skipped), stdout a pipe, stderr inherited (not a file at source) |
| _acceptance-stage2.inc.bash:514 `session_run ... </dev/null` (via `where_in`, called at 543 and 746 inside `$(...)`) | stdin /dev/null, stdout command substitution |
| _acceptance-stage2.inc.bash:589 `printf \| session_run ...` inside `$(...)` | stdin pipe, stdout substitution |
| _acceptance-stage2.inc.bash:739 `session_run ... </dev/null` inside `$(...)` | same |
| _acceptance-stage2.inc.bash:571 `guest_home` (`$(... \| cut)`) | substitution plus pipe; its callers add no file redirect |
| _acceptance-stage1.inc.bash:91 `"${LXC_ATTACH[@]}" ... pingbus version` in `$(...)` | substitution; stderr not redirected |
| _acceptance-stage1.inc.bash:123-131 fake lxc-attach heredoc | a fake script that runs `cat`; `cat >"${FIX}/lxc-attach"` writes the fake, not an attach stream |
| 00211 acceptance.bash `exec` lines and REPORT writes | `exec` only chains to another script; REPORT writes are on `printf`. meta-deploy.bash:221 runs acceptance as `2>&1 \| tee`, a pipe, so inherited stdio is a pipe |
| 00161 _acceptance-u23.inc.bash:162-176 `u23_lxc_attach` (called from `u23_exec` 196, whose output is kept as .out/.err files) | internally `cat \| sudo -n lxc-attach ... 3>&- \| cat 3>&-`: all three streams of lxc-attach are pipes. Deliberately safe even though its caller's output ends up in files |
| 00203 _acceptance-robust.inc.bash:273 | `$(...)` substitution, no file redirect |
| 00213 _acceptance.inc.bash:127 and 141 | substitution (and a pipe on stdin at 141) |
| 00055 acceptance.bash:463 backgrounded `sudo lxc-attach ... &` | no redirect on the command; inherits the script's stdout/stderr, a file only if the caller redirects the whole script. No `exec` redirect in the script. External invocation only (unsure, not counted) |
| 00055 acceptance.bash:192 (via `best_effort_rm`, `"$@" 2>&1` in `$(...)`), 480, 569 | substitution with `2>&1`; stdout is the substitution pipe |
| 00161 triage_probe.py:1623 | `subprocess.run(capture_output=True)`: pipes |
| helpers/agent_bus_lxc/members.py:1459-1461 `LxcGuest._attach` -> `_run` (1435-1441), reached by `home`, `root`, `agent` | `subprocess.run(input=bytes, capture_output=True)`: pipes |
| helpers/containerwatch/core.py:228 | builds a hint string only, never executes |
| files/var/local/docker-in-lxc:482, 497, 520, 646 | no redirect. Line 564 uses `&>/dev/null` (allowed). Callers of `provision_container_direct` (448, 452) add no redirect and the script has no `exec` redirect |
| src-go/internal/lxcfreeze/{lister,renew,attach}.go (`Sudo.Capture("lxc-attach", ...)`) | Capture hands the runner no writers (pipes); `containerTmux.Run` ignores the caller's Stdin/Stdout/Stderr; act_test.go:190 documents it |
| src-go/internal/teams/winddown, lxcadd, picker | reach lxc-attach only through `lxcfreeze.InContainer`/`Capture`, or print a hint string (lxcadd/adder.go:869) |
| src-go non-test code setting `Stdout:`/`Stderr:` to a file (systemd.go:171-172, vmtest/run.go:327, wsi/record.go:54-55) | none of these commands is lxc-attach |
| src-go/internal/execx/session.go:165-192 (`*os.File` handed to the child as is) | generic runner; the only lxc-attach users go through `Capture`, so no file reaches it |
| playbooks/, scripts/, fedora-install/ | no `lxc-attach` or `LXC_ATTACH` match |
| Completed/ plan folders, docs, plan markdown | out of scope or prose only |

## Search commands (from the worktree root)

1. `grep -rIn --exclude-dir=Completed --exclude-dir=.git --exclude-dir=node_modules -e 'lxc-attach' -e 'LXC_ATTACH' .` with the excluded detector paths filtered out. A second pass added `lxc_attach|LxcAttach|lxcAttach` and `--exclude=*.md --exclude=*.json` for code only.
2. Wrapper hunt: `grep -rnE '(in_guest|session_run|u23_(lxc_attach|run|guest|member)|provision_container_direct|LXC_ATTACH|lxc_exec|guest_run|run_in_(guest|member|container))' -I .` (Completed/, subagent-reports, .git and detector paths filtered out).
3. Redirect and exec hunt in the plan acceptance scripts: `grep -nE '^\s*exec\b|exec [0-9]*>|> *"?\$\{?(LOG|REPORT)'`; `grep -nE '(session_run|in_guest|guest_home|where_in|LXC_ATTACH|ask_test_question|a4_check)'` on the 00211 files; `grep -n 'COORD='`; `grep -nE 'acceptance' meta-deploy.bash` for how acceptance output is wired.
4. Go: `grep -rnE 'Stdout *[:=]|Stderr *[:=]|Stdin *[:=]|\*os\.File' src-go --include=*.go` (non-test), then read sudo/sudo.go (Capture, Run), lxcfreeze/{lister,renew,attach}.go and execx/session.go.
5. Python: read members.py `_run` and `LxcGuest`, and triage_probe.py `run`.
6. Reading: stage2.inc.bash 100-135, 170-240, 500-700; u23 inc 155-200; docker-in-lxc 475-660; 00055 acceptance 176-200 and 452-572; stage1.inc 85-135.

## Attribution by technique

- Text search alone found: every lxc-attach / LXC_ATTACH site (the candidate set), `docker-in-lxc:564 &>/dev/null`, and the pipe-only Python and Go sites. It cannot show what kind of stream each call has.
- Reading was needed for the one real instance (stage2:635): the hazard sits two call layers away from the `lxc-attach` text. Reading also judged `u23_lxc_attach` safe (internal pipes), the Go `Capture` seam, and that COORD can be an lxc session.
