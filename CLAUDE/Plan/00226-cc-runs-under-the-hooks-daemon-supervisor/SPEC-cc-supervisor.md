# SPEC: cc runs under the hooks-daemon supervisor

The behaviour Plan 00226 builds, as numbered expectations. Each one is a test, written before
the code (src-go/CLAUDE.md, "spec first, tests first"). `S` numbers are the names the tests
and the package doc of `internal/ccclaude` use, in the same way as the existing C1-C5.

Terms used below:

- **checkout**: the git top level of the directory cc-claude starts in
  (`gitx.TopLevel`), the directory ccy would mount at `/workspace`.
- **the project supervisor**: `<checkout>/.claude/ccy/claude-supervise.py`, which the hooks
  daemon deploys when `deploy_supervisor: true`, and its launcher
  `<checkout>/.claude/ccy/claude-supervise` (a `sh` script that picks a Python of at least
  3.11 and execs the `.py`).
- **wrapper**: the words put in front of `claude <args>`, ending in `--`, as
  `CCY_CLAUDE_WRAPPER` is in ccy.
- **minimum release**: 3.71.0, the first hooks-daemon release whose supervisor counts as its
  own only the sessions in its child process tree (upstream Plan 00551, issue #138 there).

## Where it lives

S0. All of it is in `cc-claude` (`src-go/internal/ccclaude`), the step that execs `claude` on
both launch paths: plain (`cc` runs `/var/local/claude-code/cc-claude "$@"`) and
`--teams`/`--bus` (`pingbus seat exec ... -- /var/local/claude-code/cc-claude ...`). Neither
`files/var/local/claude-code/cc` nor `cc-teams.bash` changes: both are over the bash limits
and on the ratchet allowlist (`scripts/bash-limits.allowlist`: cc 182 code lines, cc-teams
87 code lines and 7 functions), so they may not grow, and they need not. The two flags below
already reach cc-claude unchanged on both paths, because `cc_teams_split_args` and
`ccy_registry_launch_args` pass every argument they do not own through. The test is that the
diff of the plan touches neither bash file.

## Options

S1. `--supervise` and `--no-supervise` are cc-claude's own. Each is removed from the
arguments wherever it appears as a whole argument before the first bare `--`; nothing after
a bare `--` is looked at. Every other argument still reaches claude unchanged and in order
(C1 holds for them).

S2. Both given: `cc: --supervise and --no-supervise cannot be used together`, exit 64, and
nothing is executed.

S3. The package doc of `internal/ccclaude` stops saying "cc-claude has no options of its
own" and lists these two.

## Which wrapper, highest first

S4. `--no-supervise`, or `CCY_NO_SUPERVISOR=1` in the operator's environment: no wrapper.
claude runs as it does today. When a wrapper had been configured (operator environment or
project file), one stderr line names the wrapper that is not used, as ccy's entrypoint does.

S5. `CCY_CLAUDE_WRAPPER` set and not empty in the operator's environment: that wrapper, as
given. It outranks `--supervise`, as in ccy.

S6. `--supervise`: the project supervisor, armed:
`sh <checkout>/.claude/ccy/claude-supervise --arm --`. With no project supervisor:
`cc: --supervise needs the hooks-daemon supervisor in this project (<path> is missing)`,
exit 1. Asked for explicitly, it is never quietly dropped.

S7. A wrapper the project configures for ccy: `<checkout>/.claude/ccy/ccy.env`, then
`<checkout>/.claude/ccy/ccy.env.local`, read in that order with the meaning bash gives the
two forms the daemon and ccy write:

- `CCY_CLAUDE_WRAPPER=<value>` or `export CCY_CLAUDE_WRAPPER=<value>` sets it;
- `CCY_CLAUDE_WRAPPER="${CCY_CLAUDE_WRAPPER:-<value>}"` (with or without `export`) sets it
  only when it is still unset or empty.

So a later plain assignment wins and a later `:-` loses, exactly as sourcing the two files
does. Quoting is single or double quotes around the whole value, or none. Any other line that
names `CCY_CLAUDE_WRAPPER` (an `unset`, a `$(...)`, a second variable in the value, a line
continuation) is refused: `cc: <file>:<line> sets CCY_CLAUDE_WRAPPER in a form cc cannot read: <line>`, exit 1. cc-claude never sources or executes either file: on the host that
would run project code with the owner's rights before the session even starts (Decision 2).
Other lines in the files are ignored.

S8. A wrapper from S7 is accepted only when it runs the project supervisor: its command word
(after an optional `sh`, `python3` or `python3.N`) is the project supervisor's launcher or
`.py`, written as `/workspace/.claude/ccy/...`, as a path relative to the checkout
(`.claude/ccy/...`), or as the absolute path inside this checkout; the remaining words are
supervisor options (each starts with `-`, or is the value of `--plugin`); the last word is
`--`. `/workspace/` and a relative path are rewritten to `<checkout>/`. Anything else is
refused, naming the file, the line and the rule, exit 1, and saying that a wrapper of the
operator's own belongs in their environment (S5). The rule exists because a cloned project
could otherwise run any command on the host the moment cc starts (Decision 2).

S9. Default, when none of the above applies and the project supervisor exists:
`sh <checkout>/.claude/ccy/claude-supervise --`, unarmed, and one stderr line:
`Supervisor: on by default (auto-compaction unarmed; cc --supervise arms it, cc --no-supervise turns it off)`.

S10. Default with no project supervisor: claude unwrapped, as today, and one stderr line
saying the project has no supervisor at `<path>`. An absent supervisor is a normal state, so
this is not an error, and it is said, so that silence never reads as supervised (parity with
ccy's entrypoint).

S11. The wrapper starts in the operator's working directory, not the checkout: claude's
project is the directory cc was started in.

## The supervisor must have the child-tree ownership fix

S12. Whenever the chosen wrapper runs a supervisor file (S6, S8, S9, and S5 when one of its
words ends in `claude-supervise` or `claude-supervise.py`), cc-claude reads that supervisor's
release before running it: the one line `__version__ = "X.Y.Z"` of the `.py` (for the
`claude-supervise` launcher, the `.py` beside it). The comparison is numeric on the three
parts; a pre-release suffix is below its release.

S13. Older than the minimum release:
`cc: the hooks-daemon supervisor in this project is <found>; a host session needs <minimum> or later, which keeps the supervisor to its own session (an older one acts on every session it can see on this host). Upgrade the hooks daemon in this project, or start without it: cc --no-supervise`, exit 1, nothing executed. It refuses rather than warns: wrapping with an old
supervisor does harm to other sessions, and quietly running unwrapped is the skip-and-continue
this repo bans (Decision 1). The refusal names its bypass, so the operator is never stuck.

S14. The supervisor file is missing, unreadable, or has no such line (or more than one):
refused the same way, naming the path and what was wrong, exit 1.

S15. The minimum release is one constant in Go; a test pins it to `3.71.0`.

S16. An operator wrapper that names no supervisor file (S5) is run as given with no version
check, and one stderr line says so. It is an explicit instruction.

## What is executed

S17. With a wrapper, the exec is `<wrapper words> claude <args>`: the supervisor runs
`-- claude <args>`, and claude is found on PATH by the supervisor as ccy's is. `sh` is found
on PATH by cc-claude. The supervisor replaces cc-claude (no fork), so on the `--teams`/`--bus`
path the chain is `pingbus seat exec` > supervisor > claude, the same order ccy's entrypoint
uses (`exec "${_ccy_seat_exec[@]}" "${_ccy_wrapper[@]}" "$@"`).

S18. C2 and C3 hold with a wrapper: `CLAUDE_CODE_AUTO_COMPACT_WINDOW` is set the same way, on
the wrapper's environment, so claude inherits it through the supervisor.

S19. C4 holds: the wrapper's command not on PATH, or any exec failure, is exit 1 naming the
command and the error.

S20. All of cc-claude's own output still goes to stderr; stdout stays claude's (or the
supervisor's).

## Verified on the host (acceptance, not unit tests)

S21. In a checkout whose supervisor is at least the minimum release, plain `cc` and
`cc --teams` each show `claude-supervise.py -- claude ...` (unarmed) with a
`claude-supervise.py --worker` sibling in the session's process tree; `cc --supervise` shows
`--arm`; `cc --no-supervise` shows none.

S22. Two cc sessions in the same checkout at once: neither supervisor's `decision.log` records
an action on the other session's id.

S23. A `cc --teams` session still receives a bus message and is woken as before, and
`ccy-teams` still controls it while it runs (the pid pingbus execs is now the supervisor, not
claude).

S24. In a checkout whose supervisor is older than the minimum release, `cc` refuses with S13's
message, and `cc --no-supervise` starts.
