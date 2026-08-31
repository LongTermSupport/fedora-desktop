# Explain a Rule

Get the full, verbatim detail for any daemon rule on demand — independent of
whether it has already fired this session (Plan 00116, Decision F). This is
the drill-down half of the stateful disclosure design: a blocked command's
first fire is verbose, later fires are terse and point back here.

## Usage

```claude-code
/hooks-daemon rule-explain R-GIT-RESET-HARD
/hooks-daemon rule-explain git-reset-hard    # case-insensitive, "R-" optional
/hooks-daemon rule-explain --list            # every known rule ID + handler
```

## Output

A known ID prints the rule ID, its owning handler, and the full verbose
teaching content — the same text you would see on that rule's first fire in
a session:

```
Rule: R-GIT-RESET-HARD
Handler: destructive_git (DestructiveGitHandler)

BLOCKED [R-GIT-RESET-HARD]: `git reset --hard`

Permanently destroys all uncommitted changes...
```

An unknown ID reports the error, suggests close-spelling matches if any
exist, and points you at `--list`:

```
ERROR: unknown rule ID: R-GIT-RESET-HRAD
Did you mean: R-GIT-RESET-HARD?
Run 'hooks-daemon explain-rule --list' to see every known rule ID.
```

## Explaining a whole handler

For a handler's full rule set (IDs + terse reminders) plus its CLAUDE.md
guidance text, use the CLI directly — there is no separate skill subcommand
for this, since it is a less frequent, more exploratory lookup:

```bash
.claude/hooks-daemon/bin/hooks-daemon explain-handler destructive_git
```

## Why this exists

Under the two-tier progressive-disclosure design, always-on `CLAUDE.md`
context holds only a compact rule-ID table; the full rationale is paid once
per rule per session (first fire) and terse thereafter. This command is the
escape hatch: fetch the full detail for any rule at any time, whether or not
it has fired yet, without waiting for — or re-triggering — a block.

## Implementation

Enumeration is fully dynamic: `explain-rule`/`explain-handler` walk the
handlers package directly and require no running daemon, so a rule declared
by any handler's `get_rules()` is found automatically — including handlers
migrated after this skill was written.
