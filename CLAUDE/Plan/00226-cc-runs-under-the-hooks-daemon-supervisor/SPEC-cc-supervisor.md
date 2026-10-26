# SPEC: cc runs under the hooks-daemon supervisor

The behaviour Plan 00226 builds, as numbered expectations. Each one is a test, written before
the code (src-go/CLAUDE.md, "spec first, tests first"). The tests and the package doc use
the `S` numbers as names, the same way the existing C1-C5 are used. The numbers are names,
not the order of evaluation; the order is given under "Order of evaluation".

S25-S32 depend on Decisions 5, 6 and 7 in PLAN.md, which the owner has not ruled on yet.
They are written as each decision's default. If the owner rules otherwise, they change
before Phase 2.

Terms used below:

- **checkout**: cc-claude's working directory. `cc` has already changed to the top of the
  git work tree (cc-project-dir, Plan 00167) before it runs anything, so cc-claude does not
  look the top level up. A working directory with no `.git` entry (file or directory) is
  not a checkout. That only happens when cc-claude is run by hand. In that case S6 refuses,
  and the default runs claude unwrapped with one stderr line saying it is not at the top of
  a git work tree.
- **the project supervisor**: `<checkout>/.claude/ccy/claude-supervise.py`, which the hooks
  daemon deploys when `deploy_supervisor: true`, and its launcher
  `<checkout>/.claude/ccy/claude-supervise`. The launcher is a `sh` script that picks a
  Python of at least 3.11 and execs the `.py`.
- **wrapper**: the words put in front of `claude <args>`, ending in `--`, like
  `CCY_CLAUDE_WRAPPER` in ccy.
- **minimum release**: 3.71.0. It is the first hooks-daemon release whose supervisor counts
  as its own only the sessions in its child process tree (upstream Plan 00551, issue #138
  there).
- **trusted**: Claude Code has recorded that its owner accepted the workspace trust dialog
  for the checkout (S25). The sources are in
  [RESEARCH-claude-code-trust-record.md](RESEARCH-claude-code-trust-record.md).

## Where it lives

S0. The decision is one Go package that two commands call:

- `cc-claude` (`src-go/internal/ccclaude`) is the step that execs `claude` on both launch
  paths: plain (`cc` runs `/var/local/claude-code/cc-claude "$@"`) and `--teams`/`--bus`
  (`pingbus seat exec ... -- /var/local/claude-code/cc-claude ...`). It makes the decision
  and execs the result.
- `cc-project-dir` makes the same decision as a preflight, before cc does anything else
  (S31).

`cc-teams.bash` does not change. `files/var/local/claude-code/cc` changes on one existing
line only: the `cc-project-dir` call gains `--` and cc's arguments (S31). Both files are over the
bash limits and on the ratchet allowlist (`scripts/bash-limits.allowlist`: cc has 182 code
lines; cc-teams has 87 code lines and 7 functions). Their counts must not change, and
`scripts/qa-bash-limits.bash` is the test of that.

The two flags already reach cc-claude unchanged on both paths, because
`cc_teams_split_args` and `ccy_registry_launch_args` pass on every argument they do not
own.

## Options

S1. `--supervise` and `--no-supervise` are cc-claude's own options.

- Each is removed from the arguments wherever it appears as a whole argument before the
  first bare `--`. Nothing after a bare `--` is looked at.
- Every other argument still reaches claude unchanged and in order (C1 holds for them).
- A claude option whose value is exactly `--supervise` (`--append-system-prompt --supervise`) is taken as cc's flag. That is a known limit, and a test pins it, so that
  the behaviour is chosen and not accidental.

S2. Both given: `cc: --supervise and --no-supervise cannot be used together`, exit 64, and
nothing is executed.

S3. The package doc of `internal/ccclaude` stops saying "cc-claude has no options of its
own" and lists these two.

## Order of evaluation

Both commands evaluate in this order and stop at the first step that decides:

1. S1-S2, the flags.
2. S4, off.
3. S5, the operator's wrapper. The trust rule S29 applies when one of its words is a file
   inside the checkout.
4. S6, `--supervise`. A missing supervisor is S6's refusal; then it needs trust (S29).
5. Is the project supervisor present? This is a stat of
   `<checkout>/.claude/ccy/claude-supervise.py`, nothing read and nothing run. If it is
   absent: S10, claude unwrapped. No ccy.env is read and no trust record is read.
6. S28: if the checkout is not trusted, run unwrapped. No project file is read after this
   point.
7. S7-S8, the project's ccy.env wrapper.
8. S9, the default.

The presence check comes before the trust read. It is a stat, not a read or an exec, so it
does not weaken S28. In a checkout with no supervisor, which is most checkouts on a host, cc
therefore never reads `~/.claude.json`, never prints S28's line, and never refuses on a
malformed config file (S27). A ccy.env wrapper in a checkout with no supervisor is not read
either: S8 would refuse it anyway, since it may only name the supervisor, and S10's line
says the supervisor is missing.

S12-S14, the release check, run on whichever wrapper was chosen, before anything is
executed.

## Which wrapper, highest first

S4. `--no-supervise`, or `CCY_NO_SUPERVISOR=1` in the environment cc-claude runs in, means
no wrapper, and claude runs as it does today.

- One stderr line says the supervisor is off and why (`--no-supervise` or
  `CCY_NO_SUPERVISOR=1`). When the operator's `CCY_CLAUDE_WRAPPER` is set, the line names it
  as not used, as ccy's entrypoint does.
- No project file and no trust record is read. ccy's entrypoint also names a wrapper set in
  ccy.env; cc does not. Reading a project file here could only add a failure to the escape
  route: an unreadable or S7-malformed ccy.env must never stop `--no-supervise`.
- Whether an exported `CCY_NO_SUPERVISOR` reaches cc-claude through cc's tmux insulation is
  established by Task 1.2 and settled by Decision 7.

S5. `CCY_CLAUDE_WRAPPER` set and not empty in the environment: that wrapper, as given,
split into words as S8 does.

- It outranks `--supervise`, as in ccy.
- When one of its words resolves to a file inside the checkout (the project supervisor, or
  a `--plugin` path), it runs project code, so S29 applies.

S6. `--supervise`: the project supervisor, armed:
`sh <checkout>/.claude/ccy/claude-supervise --arm --`.

- It needs the checkout trusted (S29).
- With no project supervisor:
  `cc: --supervise needs the hooks-daemon supervisor in this project (<path> is missing)`,
  exit 1. Something asked for explicitly is never quietly dropped.
- It replaces any wrapper the project configures. A project wrapper's `--plugin` words are
  therefore not used (Decision 3), and the docs say so.

S7. A wrapper the project configures for ccy is read from
`<checkout>/.claude/ccy/ccy.env` and then `<checkout>/.claude/ccy/ccy.env.local`, in that
order. Two forms are read, with the meaning bash gives them:

- `CCY_CLAUDE_WRAPPER=<value>` or `export CCY_CLAUDE_WRAPPER=<value>` sets it.
- `CCY_CLAUDE_WRAPPER="${CCY_CLAUDE_WRAPPER:-<value>}"` (with or without `export`) sets it
  only when it is still unset or empty.

So a later plain assignment wins and a later `:-` loses, exactly as sourcing the two files
does. The lines are read as follows:

- A whole-line comment (the first character that is not a space or tab is `#`) is skipped,
  whatever it names. This repo's ccy.env names `${CCY_CLAUDE_WRAPPER:-<default>}` in a
  comment, and that line is not an assignment.
- An assignment starts in column 1. Its value is wrapped in single or double quotes, or is
  unquoted with no space in it.
- After the closing quote, or after an unquoted value, only spaces or tabs may follow, then
  optionally `#` and a comment. A `#` inside the quotes belongs to the value.
- Any other line that names `CCY_CLAUDE_WRAPPER` is refused:
  `cc: <file>:<line> sets CCY_CLAUDE_WRAPPER in a form cc cannot read: <line>`, exit 1.
  That covers an indented line, which could be inside an `if`. It also covers a compound
  line (`;`, `&&`, `||`, `|`, `if`, `then`), which would read a conditional assignment as
  unconditional. It covers an `unset`, a `$(...)` or backquote, a second variable inside
  the value, and a line continuation.
- Lines that do not name the variable are ignored.

cc-claude never sources or executes either file.

A parity-oracle test parses this repository's own `.claude/ccy/ccy.env`. It expects the
`:-` form with the value `/workspace/.claude/ccy/claude-supervise --arm --`.

S8. A wrapper from S7 is accepted only when it runs the project supervisor. Its value is
split into words on spaces, tabs and newlines, as `read -ra` does in ccy's entrypoint. A
word that contains a quote character or a backslash is refused. That includes a quoted
path, which 3.71.0's rewrite keeps quoted.

The first words must be one of these:

- **The launcher alone**: `claude-supervise`, exec'd directly. It must be executable, and
  this repo's copy is (git mode 100755).
- **`sh`, then the launcher.**
- **`python3` or `python3.N`, then the `.py`.**

Python in front of the launcher is refused, because Python handed the sh launcher dies on a
SyntaxError. That is exactly what 3.69 and 3.70 broke. `sh` in front of the `.py` is refused
too.

The supervisor word must equal, as an exact string with no path cleaning, one of these:

- `/workspace/.claude/ccy/<name>`;
- `.claude/ccy/<name>`;
- `<checkout>/.claude/ccy/<name>`.

Here `<name>` is the file the first-words rule requires. So `./.claude/ccy/...` and
`.claude/ccy/../x` are refused. `/workspace/` and the relative form are rewritten to
`<checkout>/`.

The remaining words are the supervisor's own options, as 3.71.0's argument parser defines
them, and the last word is `--`. The accepted options are:

- `--arm` and `--dry-run`;
- `--log <path>` or `--log=<path>`;
- `--disable-plugin <name>`;
- `--plugin <name>=<path>`. `<path>` is rewritten from `/workspace/` to `<checkout>/`. After
  that it must start with `<checkout>/`, contain no `..` segment, and be an existing
  regular file. ccy's lifecycle plugin is an image path the host does not have, so it is
  refused by this rule.

Any other word is refused. A later release that adds options is refused until cc learns
them.

A refusal names the file, the line and the rule, exits 1, and says that a wrapper of the
operator's own belongs in their environment (S5). The rule exists so that a ccy.env line
cannot turn cc into a launcher for any host command.

These are shape checks, not a security boundary. The checks follow symlinks: the launcher,
the `.py` and a `--plugin` path may each be a symlink to anywhere, and cc does not Lstat
them or resolve their prefix. The boundary is trust (S25-S29). S7-S8 are only reached in a
trusted checkout, where Claude Code already runs the repository's hooks, which can run
anything with the owner's rights. A symlink check would add code without closing anything
that trust leaves open.

S9. The default, when none of the above applies and the project supervisor exists, is
`sh <checkout>/.claude/ccy/claude-supervise --`, unarmed. One stderr line:
`Supervisor: on by default (auto-compaction unarmed; cc --supervise arms it, cc --no-supervise turns it off)`.

S10. With no project supervisor (step 5 of the order: no `claude-supervise.py`), and none
of S4-S6 deciding, claude runs unwrapped, as today. One stderr line says the project has no
supervisor at `<path>`. An absent supervisor is a normal
state, so this is not an error. It is said so that silence never reads as supervised
(parity with ccy's entrypoint). An old supervisor is different and is refused (S13,
Decision 1). Absent means that nothing is configured to run. Old means that the project is
configured for a supervisor this host must not run.

S11. The wrapper starts in cc-claude's unchanged working directory, the checkout.

## Only a trusted checkout's code is run

S25. The checkout is **trusted** when Claude Code's global config holds
`projects["<key>"].hasTrustDialogAccepted` as JSON `true`.

- **The config file** is `$CLAUDE_CONFIG_DIR/.claude.json` when `CLAUDE_CONFIG_DIR` is set
  and not empty. Otherwise it is `$HOME/.claude.json`. If both are empty, cc refuses as S27
  does: it cannot tell where the record is.
- **The key** comes from the checkout's own `.git` entry, examined with Lstat. It is never
  taken from a file the checkout can write. There are three cases:
  - **`.git` is a directory**: the key is the checkout itself.

  - **`.git` is a regular file**: the checkout counts as a linked worktree only if all of
    these hold:

    - its `gitdir:` line, resolved against the `.git` file's directory when relative and
      then symlink-resolved, is `<C>/worktrees/<id>`, where `<C>` is named `.git`;
    - that directory is outside the checkout;
    - `<C>/worktrees/<id>/gitdir`, git's own back-link, names this checkout's `.git` file.
      The back-link is resolved against its own directory when relative, which is what git
      writes with `worktree.useRelativePaths`. The two paths are compared after resolving
      their directory parts' symlinks.

    The key is then `<C>`'s parent, the main checkout.

  - **Anything else gives no key**, so the checkout is not trusted. That covers a `.git`
    symlink, a `.git` file whose `gitdir:` is not of that shape (a submodule, a
    `--separate-git-dir` checkout, a forged file), a target or back-link that does not
    exist (a worktree made in a container with a `/workspace/...` path), and a back-link
    that names another checkout. A read error other than "does not exist" is refused as
    S27 does.
- **Why this rule (review r2 B2)**: a `.git` file and the `commondir` it leads to can both
  belong to the tree being launched. A copied or extracted tree holding `.git` =
  `gitdir: .x` and `.x/commondir` = `../../.git` is accepted by git as a worktree of the
  checkout it sits in, so a `commondir` walk would key it on a trusted repository. The
  back-link sits in the main repository's own `.git`, which the launched tree cannot write.
  The "outside the checkout" condition stops a tree from carrying its own `<x>/.git/worktrees/<id>`
  with a back-link it wrote. A directory `.git` keys on the tree's own path, which the tree
  cannot choose.
- **Why not simpler**: trusting only a directory `.git` would be spoof-proof too. But every
  worktree, including the agent worktrees this repository uses, would then run unsupervised,
  and `cc --supervise` would be refused there. The back-link check costs two file reads and
  a path comparison. It replaces the `commondir` read and does not add to it.
- **Known limit**: a submodule and a `--separate-git-dir` checkout are never trusted by cc,
  though Claude Code may trust them. That is the safe direction: unwrapped with S28's line.
  A worktree whose main checkout path contains a symlink is looked up by its resolved form
  only, and the same applies.
- **Path forms**: for a directory `.git`, the checkout path as cc-claude's working directory
  gives it, and its symlink-resolved form, are both looked up. Either one matching counts.
- **No parent walk**: a parent directory's record does not count. Inside a repository
  Claude Code walks up only as far as the repository root, and cc-claude starts at that
  root.
- **The record**: only `projects[key]` as a JSON object whose `hasTrustDialogAccepted` is
  JSON `true` counts. A `projects[key]` that is not an object, or a
  `hasTrustDialogAccepted` that is not boolean `true`, means not trusted. That is how
  Claude Code's `projects?.[key]?.hasTrustDialogAccepted === true` reads them.
- **Nothing is run**: every file is read and decoded in Go. No `git`, no `claude` and no
  project file is executed to answer this.

S26. With `CLAUDE_CODE_IGNORE_PERSISTED_TRUST` set and not empty, no record counts, as in
Claude Code. `CLAUDE_CODE_SANDBOXED` is not honoured. It is a claim about the environment,
not the owner's recorded acceptance of this checkout.

S27. A missing config file means not trusted, the normal state on a fresh host. Some cases
are refused instead, naming the file, the error and `cc --no-supervise`, exit 1:

- a file that cannot be read;
- a file that is not JSON;
- a file whose `projects` is not an object.

Claude Code rewrites the file often, and cc itself rewrites it in place (truncate, then
write). A read can therefore land mid-write in another session. So a file that is not JSON
is read once more after a short pause before it is refused, and the refusal says the cause
may be transient: run cc again. A real defect still refuses, on the second read.

`cc --no-supervise` never reads the file (S4), and neither does a checkout with no
supervisor (step 5 of the order).

S28. When the checkout is not trusted on the default path (steps 7-8 of the order),
cc-claude runs claude unwrapped and reads no project file. It prints one stderr line:
`Supervisor: off: Claude Code does not trust <key> yet (no hasTrustDialogAccepted in <config file>). Accept its trust dialog in this session; the next cc, or cc --supervise, then runs the project supervisor.`
This is not skip-and-continue. Running unwrapped is the state the project's files may not
override, and the line says so. claude then shows its own trust dialog, so accepting it
there is the way to trust the checkout.

S29. When the checkout is not trusted, `--supervise`, and an operator wrapper (S5) with a
word that is a file inside the checkout, are refused. Exit 1, nothing executed:
`cc: <--supervise | CCY_CLAUDE_WRAPPER> would run this project's own code (<file>), and Claude Code does not trust <key> yet. Start cc here once and accept the trust dialog, then run it again.`
The trust record is the owner's recorded judgement of the repository. A flag typed out of
habit, an alias, or a restore replaying argv is not a review of the code. Refusing rather
than running unwrapped keeps S6's rule that what was asked for is never quietly dropped
(Decision 5).

## The supervisor must have the child-tree ownership fix

S12. The release check applies whenever the chosen wrapper runs a supervisor file: S6, S8,
S9, and S5 when one of its words ends in `claude-supervise` or `claude-supervise.py`.

- cc-claude reads that supervisor's release before running it, from the one line
  `__version__ = "X.Y.Z"` of the `.py`. For the `claude-supervise` launcher, that is the
  `.py` beside it.
- The comparison is numeric on the three parts. A pre-release suffix sorts below its
  release.

S13. A supervisor older than the minimum release is refused, exit 1, nothing executed:
`cc: the hooks-daemon supervisor in this project is <found>; a host session needs <minimum> or later, which keeps the supervisor to its own session (an older one acts on every session it can see on this host). Upgrade the hooks daemon in this project, or start without it: cc --no-supervise, or export CCY_NO_SUPERVISOR=1 to skip it in every project until they are upgraded`.

It refuses rather than warns. Wrapping with an old supervisor harms other sessions, and
running unwrapped in a project configured for a supervisor is skip-and-continue
(Decision 1). The refusal names both ways round it, the one for this launch and the
persistent one, so the operator is never stuck. If Task 1.2 finds that the exported
variable does not reach cc-claude, the persistent way is the one Decision 7 settles on.

S14. A supervisor file that is missing or unreadable, or has no such line or more than one,
is refused the same way, naming the path and what was wrong, exit 1.

S15. The minimum release is one constant in Go; a test pins it to `3.71.0`.

S16. An operator wrapper that names no supervisor file (S5) is run as given with no version
check, and one stderr line says so. It is an explicit instruction.

## What is executed

S17. With a wrapper, the exec is `<wrapper words> claude <args>`.

- The supervisor runs `-- claude <args>`, and the supervisor finds claude on PATH, as ccy's
  does. cc-claude finds `sh` on PATH.
- The supervisor replaces cc-claude (no fork). On the `--teams`/`--bus` path the chain is
  therefore `pingbus seat exec` > supervisor > claude. That is the same order ccy's
  entrypoint uses (`exec "${_ccy_seat_exec[@]}" "${_ccy_wrapper[@]}" "$@"`).

S18. C2 and C3 hold with a wrapper. `CLAUDE_CODE_AUTO_COMPACT_WINDOW` is set the same way
on the wrapper's environment, so claude inherits it through the supervisor.

S30. With a wrapper, `CLAUDE_PROJECT_DIR=<checkout>` is set on the wrapper's environment,
replacing any inherited value.

- The supervisor reads it before its working directory, for its decision log and sidecar
  lookups (claude-supervise.py, `DecisionLog._default_path` and the sidecar path).
- cc sessions run inside tmux, which hands them the tmux server's environment. A value
  left there by another project would point the supervisor at that project.

S19. C4 holds: the wrapper's command not on PATH, or any exec failure, is exit 1 naming the
command and the error.

S20. All of cc-claude's own output still goes to stderr; stdout stays claude's (or the
supervisor's).

## Checked before anything else happens

S31. `cc` passes `--` and its arguments to `cc-project-dir`:
`CC_PROJECT_DIR=$(/var/local/claude-code/cc-project-dir -- "$@")`. This changes the
existing call on the same line, and nothing else in `cc`.

- **cc-project-dir's argument contract (P1) changes.** Today P1 rejects any argument with
  exit 64, and prints its own usage on stdout for `-h`/`--help`. If cc passed bare `"$@"`,
  `cc --model x` would stop with exit 64, and `cc --help` would `cd` into the usage text.
  So everything after cc-project-dir's first `--` is cc's arguments, taken verbatim for
  the preflight and never parsed as cc-project-dir's own. S1 then looks only before the
  first bare `--` inside them. Before the `--`, P1 holds as today: `-h`/`--help` alone
  prints the usage, and anything else is a usage error. The package doc (`app.go`'s P1)
  says this, and the `cmd/cc-project-dir` test keeps `--nope` → exit 64.
- `cc-project-dir -- -h` and `cc-project-dir -- --help` print only the directory on stdout,
  so `cc -h`/`cc --help` still reaches claude.
- cc-project-dir finds the directory as today. It then runs the decision above on that
  directory, with cc's arguments and its own environment, and executes nothing.
- A refusal is printed to stderr, and cc-project-dir exits 1 and prints no directory.
- cc's `set -e` then stops cc on that line. That is before the tmux re-exec, the token
  chooser, the credential parking, `seat take` and `membership-init`.
- cc-project-dir's stdout stays the directory alone. Its stderr lines (S4, S9, S10, S28) are
  not printed twice: the preflight prints only refusals, and cc-claude prints the
  informational lines.
- **Re-attaching (review r2 m7)**: this check comes before `ccy_tmux_insulate`'s offer to
  re-attach a detached `cc-<project>` session, so a refusal also stops `cc` from
  re-attaching one. That is accepted, not worked around: the refusal is about starting a
  supervisor, and the session already running is not touched. Every refusal of the
  decision (exit 1; not S2's usage error) ends with one more sentence: `A cc session already running here can still be attached with ccy-sessions.`

S32. **Which environment decides.** cc re-executes itself inside tmux (cc:67), so the
decision is made up to three times:

1. the outer preflight, in the launching shell's environment;
2. the inner preflight (cc:42 again, inside the pane);
3. cc-claude at exec.

A session created on a tmux server that is already running takes that server's global
environment, not the shell's. The server is often started by the boot restore unit or by an
earlier launch, and the pane forwards only `CCY_GH_TOKEN_HANDOFF`. So runs 2 and 3 see the
pane's environment, and run 1 sees the shell's. They differ systematically, not only when
something changes between them. The inputs that can differ are `CCY_NO_SUPERVISOR`,
`CCY_CLAUDE_WRAPPER`, `CLAUDE_CONFIG_DIR` and `CLAUDE_CODE_IGNORE_PERSISTED_TRUST`. `HOME`
is the same user's on both sides.

- **cc-claude's decision is the one that counts.** What runs is decided by the pane's
  environment, which is the one claude inherits.
- **Any refusal stops the launch**, and the outer one stops it before any side effect. The
  outer preflight can refuse where the pane would not, for example when a server holds a
  `CCY_NO_SUPERVISOR=1` the shell lacks. That direction is safe, and the refusal says how
  to proceed.
- **A refusal inside the pane comes after the side effects.** Examples are an exported
  `CCY_NO_SUPERVISOR=1` the server never received, or a `git checkout` after the
  preflight. The tmux session and its registry record already exist. The registry's
  trampoline holds the pane on the failure and removes the record. On the plain path cc's
  EXIT trap restores the parked credential: cc does not `exec` cc-claude, so the trap
  fires. On `--teams`, the seat and membership are pingbus's to release when its child
  exits. Acceptance S24 checks these.
- **The reverse leak**: a `CCY_NO_SUPERVISOR` or `CCY_CLAUDE_WRAPPER` in the server's
  environment applies silently to every later cc session in every project. Only Decision
  7's option A closes this and the case above. Until the owner rules, it is a recorded
  risk, and Task 1.2 establishes it on the host.

## Verified on the host (acceptance, not unit tests)

Most of what follows is established by driving `/var/local/claude-code/cc-claude` directly
in a temporary git fixture, with a stub `claude` on PATH that records its argv and its
parent pid and then waits. The fixture is trusted through a temporary `CLAUDE_CONFIG_DIR`
holding a `.claude.json`, so no person has to stand at the token chooser. What needs the
real `cc` is named NOT ESTABLISHABLE in `acceptance.bash` and listed for the owner.

S21. Process trees:

- **This checkout** is armed by its ccy.env (`claude-supervise --arm --`). Plain `cc` and
  `cc --teams` each show `claude-supervise.py --arm -- claude ...` with a
  `claude-supervise.py --worker` sibling. `cc --supervise` shows `--arm`, and
  `cc --no-supervise` shows no supervisor. These need the real cc and are listed for the
  owner. They also confirm that S25 reads the trust record where the owner's Claude Code
  wrote it.
- **A fixture with the supervisor and no ccy.env wrapper, trusted**: the default shows the
  unarmed `claude-supervise.py -- claude`.
- **The same fixture, untrusted**: claude runs unwrapped and prints S28's line.

S22. Two sessions in the same checkout at once: neither supervisor acts on the other
session's id.

- The default decision log is one shared file,
  `<CLAUDE_PROJECT_DIR or cwd>/untracked/supervise/decision.log`. Its lines are
  `<timestamp> <message>` with no pid or session id, so they cannot be attributed to a
  session.
- So acceptance runs the two through operator wrappers (S5), each with its own
  `--log <run dir>/<A|B>.log`, and checks each log against the other session's id.

S23. A `cc --teams` session still receives a bus message and is woken as before, and
`ccy-teams` still controls it while it runs. The pid pingbus execs is now the supervisor,
not claude. This needs the real cc and pingbus and is listed for the owner.

S24. A trusted fixture whose `.claude/ccy/claude-supervise.py` declares
`__version__ = "3.70.0"` is built by `acceptance.bash` with `git init` in a temporary
directory:

- cc-claude refuses with S13's message, exit 1, and the stub claude is not started.
- `--no-supervise` and `CCY_NO_SUPERVISOR=1` each start the stub.
- Run through the real `cc`, the refusal comes from the preflight (S31): the credential is
  back in place and no seat was taken. This is listed for the owner.
- A non-interactive `ccy-sessions` restore of a cc session recorded in such a checkout
  fails visibly. The pane is held and the restore reports it; it does not hang or vanish.
  This needs the real registry and tmux server and is listed for the owner.
