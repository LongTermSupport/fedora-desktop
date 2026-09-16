# FACTS — what CCY actually does, checked against issue 44's premises

Issue 44 makes concrete claims about how CCY works and designs on top of them. Each is
checked here against the code, with `file:line`. **Three are wrong**, and one hazard the
issue does not mention is the single biggest risk in the feature. The design in
[DESIGN-failure-modes.md](DESIGN-failure-modes.md) is built on what is here, not on the
issue's description.

Everything below was read in the repository, not recalled.

---

## F1 — "one tmux session per project" is WRONG; there can be several

The issue says "Every interactive `ccy` runs in a tmux session on ccy's own server
(`tmux -L ccy`), **one per project**".

The server and socket half is right: `lib/tmux-session.bash:24` sets
`CCY_TMUX_SOCKET="ccy"` and `ccy_tmux()` routes every call through `tmux -L ccy`, so no call
can land on a user's default server. `claude-yolo:900` calls
`ccy_tmux_insulate "$PROJECT_NAME" "$SCRIPT_DIR/$(basename "$0")" "$@"`, which re-executes
the launcher inside a session before the first prompt.

**One per project is not true.** `ccy_tmux_next_name` (`lib/tmux-session.bash:78-86`) walks
`ccy-<project>`, then `ccy-<project>-2`, `-3`, … until it finds a free name, and
`docs/ccy.md:186` documents exactly that: "`ccy` from the project while a session is
attached → Starts a second session, `ccy-<project>-2`".

**Consequence for the design.** A record cannot be keyed on the project directory — two
records can legitimately share one. The record is keyed on the **tmux session name**, which
is unique on the server by construction, and carries the project directory as a field. The
restore reconstructs names through the same `ccy_tmux_next_name` path by letting `ccy` do it,
so a restored session never collides with a live one.

Also relevant: `ccy_tmux_project_sessions` (`:69`) matches on the **directory**, not the
name, and the comment says why — two checkouts of one repo produce the same project name, so
project `app` would otherwise claim `ccy-app-2`, which belongs to a project genuinely called
`app-2`. The same trap applies to any slug this plan derives from a project name, so the
registry slugs the **directory path**, never `PROJECT_NAME`.

## F2 — Claude's state in `.claude/ccy` — CORRECT

`claude-yolo:1888-2001` and `:2029`: all Claude Code state lives in the project's
`/workspace/.claude/`, with user/session state (history, databases, todos) under
`.claude/ccy/`, and the entrypoint symlinks the container's `/root/.claude` to it. The
container is `--rm` (`:3120`) and the state is project-local, so a fresh container in the
same directory does resume — which is what makes `--continue` work at all.

This is the foundation the whole restore rests on, and it holds.

## F3 — `--supervise` exists, and the supervisor consumes session-keyed signal files — CORRECT

`claude-yolo:594` parses `--supervise`; `:3053-3061` resolves it to
`CCY_CLAUDE_WRAPPER="python3 /workspace/.claude/ccy/claude-supervise.py --arm --"`, passed in
at `:3145`. `--no-supervise` sets `CCY_NO_SUPERVISOR=1` (`:3061`, `:3146`), and the two are
mutually exclusive (`:3053`).

The supervisor is deployed by the hooks daemon, not by this repo
(`CLAUDE/ContainerRules.md`), and its own docstring confirms the signal mechanism: it writes
and consumes session-keyed signal files at an idle choke point — a compaction signal, a
`goal-intent` signal, a `model-switch-intent` signal — each unlinked on injection. A
`reboot-warning` signal is one more of the same kind, exactly as the issue says.

Note the precedence the issue does not state: a host `CCY_CLAUDE_WRAPPER` export outranks
`--supervise`, which outranks the project's `ccy.env`, which outranks the default-on-unarmed
behaviour. A restored session passing `--supervise` therefore gets an **armed** supervisor
unless the operator has exported a wrapper — which is what the issue wants ("the supervisor
nudges it back to work"), and is worth stating in the docs because arming changes what the
session *does*.

## F4 — "`ccy` passes arguments through verbatim" is WRONG

`claude-yolo:523-645` is a hand-rolled parser. Every `ccy` flag is **consumed** by it;
anything unrecognised falls through to `CLAUDE_ARGS` and is then validated against
`claude --help` before any container work (`:673-720`), with a hard failure on a flag neither
tool knows. Only arguments after a literal `--` are forwarded raw (`:524-530`).

So `--supervise --continue` works, but for two different reasons: `--supervise` is eaten by
`ccy`, `--continue` is forwarded to `claude`. It is not passthrough.

**Consequence for the design, and it is the better answer anyway.** The issue proposes
recording "launch arguments minus one-shot ones". Argv is the wrong source: the **quick-launch
path** (`:915-950`) supplies the token, SSH keys and network from `.claude/ccy/.last-launch.conf`
with no argv at all, so a session started by accepting quick launch has an argv that describes
none of its configuration. The record therefore stores the **resolved** configuration by
value, captured at `:2870-2881` where `save_launch_config` already assembles exactly those
values, and the restore reconstructs flags from them. Argv is still recorded, as evidence
only.

## F5 — `cc` is not in this repository

`lib/tmux-session.bash:31` and `docs/ccy.md:182` describe a host wrapper `cc` that sets
`CCY_TMUX_SESSION_PREFIX=cc` and puts `cc-<project>` sessions on the same server. There is no
`cc` script or alias anywhere under `files/` — `files/home/bashrc-includes/claude-yolo.bash.j2`
defines only `alias ccy='/var/local/claude-yolo/claude-yolo'`.

**Consequence.** Nothing in this repository can write a registry record for a `cc` session, so
restoring them is a non-goal, stated as such rather than silently absent. `ccy-sessions`
continues to *list* them, as it does today.

Note also that `ccy` is an **alias**, so it is not on `PATH` for systemd. The restore service
invokes `/var/local/claude-yolo/claude-yolo` directly, which is what `ccy-sessions` already
does (`CCY_LAUNCHER`).

## F6 — THE HAZARD THE ISSUE DOES NOT MENTION: a detached session has a pty, so a prompt hangs for ever

This is the finding that most changed the design.

The issue's restore step is "start the session detached on the ccy tmux server, in the
recorded directory, with the recorded arguments plus `--supervise --continue`". A detached
tmux session still allocates a **pty**, so inside it `[ -t 0 ]` and `[ -t 1 ]` are both true.
The launcher's own terminal checks (`lib/tmux-session.bash:266-274`) therefore pass, and every
interactive prompt behaves as if a human were watching.

There are **twenty-one** `read` sites in `claude-yolo` after the tmux re-exec point at `:900`,
of which these are prompts a restored session can plausibly reach:

| Line                                        | Prompt                          | Reached when                               |
| ------------------------------------------- | ------------------------------- | ------------------------------------------ |
| `:928`                                      | `Use same configuration? [Y/n]` | **always**, whenever a saved config exists |
| `:1104`, `:1137`, `:1158`, `:1249`, `:1266` | token creation / recovery       | the token is missing or expired            |
| `:1978`                                     | `Press Enter to continue...`    | `--debug`                                  |
| `:2098`, `:2483`, `:2522`, `:2557`, `:2590` | network questions               | network detection is ambiguous             |
| `:2260`, `:2279`                            | container cleanup choices       | the engine reports a health problem        |
| `:2347`, `:2681`                            | `Start compose services?`       | the project has a compose file             |
| `:3180`                                     | `Stop compose services? [Y/n]`  | compose was started this session           |

The very first one fires on essentially every restore. With nobody to answer, the session sits
at the prompt for ever: it **appears** in `ccy-sessions` as a live session, `claude` never
starts, `--continue` never happens, and the supervisor that was supposed to nudge it back to
work is not running. A restore that silently produces a parked shell is worse than no restore,
because the operator believes their work resumed.

Closing stdin instead (`< /dev/null`) is worse still: several of those prompts sit in
`while true` loops that re-prompt on an unrecognised answer, so EOF would spin one of them at
100% CPU rather than hang quietly.

**Consequence.** D6: one `read()` shadow keyed on the presence of `-p`. The seven non-prompt
`read` calls in the launcher — `read -ra` splitting a string (`:2016`, `:2298`), `while read`
loops over pipes — have no `-p` and pass straight through to the builtin. Enumerating the
seventeen prompt sites above and guarding each one individually would leave the eighteenth,
added later, unguarded; keying on `-p` cannot go stale.

## F7 — the `reboot-warning` signal and its CLI DO NOT EXIST — the dependency is real

> **SUPERSEDED as of hooks daemon v3.65.0 — do not act on the reading below.**
> The finding was correct when taken and is kept verbatim, because the command list it
> quotes is the evidence for why no local substitute was invented. It is no longer true.
>
> `hooks-daemon signal {reboot-warning,shutdown-warning,reboot-cancelled}` now exists, and
> the supervisor carries the reader half that renders it. Re-measure rather than trusting
> either this block or that sentence: **`./triage-signal.bash`** takes the readings, and
> `JOURNAL/00123-Journal-26-09-16.md` records what they were. The shape the issue's security
> note asked for is what shipped — the payload's whole key set is
> `kind, minutes, session_id, source, ts`, with no free-text field, and the wording an agent
> sees is composed on the reader side from fixed templates.
>
> Two readings change decisions rather than merely unblocking them, so they are named here
> where a reader arrives looking for the dependency:
>
> - `--all-sessions` **exits 1** when no session is live, rather than succeeding vacuously.
>   So `reboot --in N` on an idle machine now gets a failure from the signal step and must
>   decide deliberately whether that refuses the reboot or proceeds with it.
> - An unrecognised kind exits **2** (the argument parser refuses it) while a well-formed but
>   invalid request exits **1**. A caller treating "non-zero" uniformly discards that.

The installed hooks daemon's full command list (`hooks-daemon --help`) is:

```
start stop status restart logs health check get-mode set-mode handlers config repair
list-venvs disk-usage check-permissions prune-venvs write-venv-metadata init-config
generate-playbook generate-docs regenerate-docs config-diff config-merge settings-merge
release-slate-check config-validate check-config-migrations audit-handler-keys
check-worktree-seed check-truth-changes plan-qa tool-report block-report explain-rule
explain-handler docs-qa remote-docs find-comment-blocks skill-scan housekeeping
worktree-reap harvest-background inject-goal clear-goal verdicts delete-branch
release-notes init-project-handlers validate-project-handlers test-project-handlers
format-markdown secret-meta contract-status record-config-optimisation-run
transport-probe transport reconcile-settings deploy-plan-workflow agents bug-report
```

There is **no** `notify`, no signal-raising command, and no `reboot-warning` anywhere in the
daemon tree or the supervisor. `Edmonds-Commerce-Limited/claude-code-hooks-daemon#39` is a
genuine blocker for that half, precisely as the issue states.

`inject-goal` is the closest existing mechanism and is the shape the upstream command will
take. It is **deliberately not** used as a substitute: the issue is explicit that the reboot
helper "never composes a message; it only names a signal kind and a number", and
`inject-goal` takes free text. Routing a reboot warning through it would turn a fixed-kind
signal into a prompt-injection channel — the exact property the issue's own security note
relies on. So the seam fails fast instead.

## F8 — the launcher already has an EXIT trap

`claude-yolo:1940` installs `trap cleanup EXIT`, and `cleanup()` (`:1913-1939`) restores the
terminal suspend character, removes `CONFIG_TEMP` and the staged SSH key directory, and
prints the debug log.

**Consequence.** Record removal **extends** `cleanup()` rather than installing a second EXIT
trap, which would silently replace the first and leak the temp directories that hold copies of
the user's gitconfig and SSH keys. The removal is `rm -f` on a path — `rm -f` succeeds on an
absent file, and absence is the correct terminal state, so it cannot change the exit status
the trap is preserving.

## F9 — `ccy` refuses to run outside a git repository root

`claude-yolo:119` — `check_git_repo || exit 1`, before anything else.

**Consequence.** A record whose project directory is no longer a git work tree can never be
restored: `ccy` would refuse. The restore checks this itself and retires the record as
`not-a-git-checkout` with the reason recorded, rather than starting a session that dies
instantly and leaving the operator to work out why (D4).

## F10 — the tmux server must live outside the starting process's cgroup

`ccy_tmux_insulate` (`lib/tmux-session.bash:322-330`) starts the server under
`systemd-run --user --scope --quiet --collect`, and the header comment (`:16-18`) says why: the
scope keeps the server's cgroup out of the terminal tab's scope, so the tab dying does not take
the server with it. The same library **refuses** to wrap a session when it finds itself inside a
tmux server whose cgroup matches `*-spawn-*.scope` (`:250-258`), because such a server dies with
its tab.

**Consequence.** D9. A `Type=oneshot` service's leftover processes are killed when the service
completes (default `KillMode=control-group`), so a `tmux new-session` forked directly from the
restore service would have its server killed seconds after starting it — a restore that
destroys exactly what it restored. The service uses the same `systemd-run --user --scope --collect` mechanism, which is both correct and already proven in this codebase.
</content>
