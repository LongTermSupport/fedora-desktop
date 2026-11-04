# Plan 00192 Task 3.12: upstream issue for the secret-file guard (notes)

Not plan code work. Prepared per `.claude/skills/hooks-daemon/issue-report.md` steps 1-3 and
`.claude/hooks-daemon/BUG_REPORTING.md`. Outcome: STOPPED AT STEP 1. No fields file was
written and the generator was not run, because the source and the handler's own guidance
state the behaviour as intended and configurable.

## Step 1.1: configuration (explain-handler secret_file_guard)

Handler: `secret_file_guard` (rules R-SECRET-READ, R-SECRET-BASH-MENTION,
R-SECRET-SCRIPT-AUTHOR). Options: `protected_paths` (globs), `mode: additive|replace`,
`allowed_consumers` (a command plus the flags its path may follow).

The guidance it prints says, word for word:

> `*.secret*` is intentionally broad (a deliberate project decision): any Bash token merely
> CONTAINING `.secret` trips it, so a repo-wide grep for the string `.secret` can be denied.
> That is the accepted cost. To work around a false positive: ask the user, scope the search to
> exclude the protected file, or have a human narrow the config (`mode: replace` with a tighter
> list).

It also frames the Bash route as "DENY-BY-DEFAULT, NOT A LIST OF BAD READERS", and
"Authoring a script that references a protected path is also denied". The nmcli setting name
(vpn, a dot, then secrets) is exactly "a token containing `.secret`". So the deny is the
documented behaviour.

## Step 1.2: source

- `src/claude_code_hooks_daemon/handlers/pre_tool_use/secret_file_guard.py:1814`
  (through 1819): the same "intentionally broad ... accepted cost" text, as the handler's
  CLAUDE.md guidance.
- `secret_file_guard.py:1421`: the Bash route calls `sfm.find_protected_mention_detail` over
  the whole command's word stream. Its only narrowings come after a match: an allowlisted
  consumer in flag position (`is_exempt_invocation`), an encrypted target
  (`is_encrypted_target_invocation`), and a grep-family search PATTERN
  (`is_grep_pattern_only_mention`, `utils/secret_file_matching.py:3269`, Plan 00466). No
  narrowing looks at whether a token is path-like or a file-reading command's operand.
- `utils/secret_file_matching.py:1461`: `find_protected_mention_detail`, the first
  `(pattern, token)` from `iter_protected_mentions`.

The grep-pattern exemption does show upstream will narrow by argument POSITION when a token
cannot be a read. An nmcli argument is that kind of case. But that would be a design change
against a stated allowlist-not-denylist policy, so it is a feature request, not a defect.
BUG_REPORTING.md: "If reading it explains the behaviour as intended, stop. That is a
documentation gap or a configuration question, not a defect."

## Configurable away?

Yes, but only by weakening it: `mode: replace` with a list that drops `*.secret*`. Any glob
that protects a file named `<x>.secrets` also matches the bare token, so no tighter list keeps
those files covered and lets the nmcli token through. `allowed_consumers` does not help
either: it exempts a path in a consumer's FLAG position, and the nmcli token is a positional
setting name, not a flag value. The plan's decision (part C item 9) keeps the guard as it is
locally, so the workaround in the `nord` code stays: the name is joined from its parts, with a
test pinning the parts.

## Step 1.3: versions

- Installed: v3.71.0 (`git -C .claude/hooks-daemon describe --tags`).
- Latest upstream: v3.71.0 (newest tag from `git ls-remote --tags origin`; `release-notes --latest` heads v3.71.0). Nothing to diff, so there is no `--from/--to` range. The v3.71.0
  notes do not mention secret_file_guard.

## If the owner still wants something filed

It would be a feature request, not a "Daemon defect". The generator's `source_citation` and
`config_considered` fields would honestly have to cite line 1814, which says this is
intended. A suitable title: "secret_file_guard: let a Bash token that is a program's
positional argument (not a path, not a file-reading operand) through, as the grep-pattern
exemption does". Its reproduction would describe the token in words (vpn, a dot, secrets)
given as an `nmcli connection modify` setting name, under `untracked/scratch/`, with no file
involved.
