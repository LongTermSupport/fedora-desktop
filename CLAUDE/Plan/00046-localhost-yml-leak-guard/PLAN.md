# Plan 00046: localhost.yml Leak Guard Hook Handler

**Status**: Not Started
**Created**: 2026-05-26
**Owner**: joseph
**Priority**: High (safety follow-up to today's leak incident)
**Type**: Hooks-daemon PreToolUse handler

## Overview

On 2026-05-26 a `gh issue create` body leaked private alias/username
content from `environment/localhost/host_vars/localhost.yml` into the
public `LongTermSupport/fedora-desktop` repo. The existing safeguards
(`scripts/git-hooks/pre-commit` and `commit-msg`) only fire on `git commit` and never inspect commands like `gh issue create`, `gh pr create`, `gh gist create`, `curl --data`, or `wget --post-*`. That
class of "post text to an external service" command is the entire
gap.

This plan adds a project-level PreToolUse handler to the hooks daemon
that builds a deny-list dynamically by parsing `localhost.yml`,
subtracts an explicit allowlist of public-by-design tokens, and
blocks any matching content in commands that post text externally.
The deny-list is data-driven (always current as the user adds new
aliases or personas), and the allowlist is the only thing the user
maintains by hand.

## Goals

- Block `gh issue|pr|gist (create|edit|comment)` and HTTP-POST
  commands (`curl -d`, `curl --data*`, `curl --data-urlencode`,
  `curl -X POST`, `wget --post-*`) when the command body contains
  any token from a deny-list derived from `localhost.yml`
- Deny-list is **derived at hook run time** from
  `environment/localhost/host_vars/localhost.yml` so it stays current
  as the user adds aliases/personas without any manual config
- Allowlist of explicitly public tokens (`joseph`, `joseph-uk`,
  `LongTermSupport`, `LTSCommerce`, etc.) lives in a single
  user-curated file
- Block message names the offending token + its source + the
  generic placeholder to use (`<alias-a>` / etc.)
- `--body-file` and heredoc bodies are inspected, not just inline
  `--body "…"` arguments
- Encrypted `!vault` values are silently ignored (already opaque,
  no leak risk)
- TDD: handler ships with a tests/ directory that exercises the
  positive (deny token present → BLOCK) and negative (only public
  tokens → ALLOW) cases, including allowlist subtraction

## Non-Goals

- Not adding a CRITICAL "stop the session" type block — a normal
  PreToolUse block + remediation message is enough
- Not scanning files written by the `Write`/`Edit` tools — that's a
  separate concern (markdown/plan files committed to the public
  repo are caught by the existing `git-hooks/pre-commit`)
- Not handling network-posting via Python `requests`/`urllib`
  inside a script — too broad to detect statically; out of scope
- Not extending to general PII/secret scanning across all bash
  commands — scoped to known external-posting commands only

## Context & Background

### The incident (2026-05-26)

- I posted four real aliases (two private: `<alias-a>`/`<gh-username-a>` and `<alias-b>`/`<gh-username-b>`; two public: `joseph`/`joseph-uk` and `lts`/`LTSCommerce`) into the body of GitHub issue
  `LongTermSupport/fedora-desktop#22` via `gh issue create`.
- The original body remains visible via GitHub's "edited" dropdown
  on the issue even after a scrub-edit — only an issue deletion or
  GitHub Support intervention truly removes it.
- User reaction: *"FFS you have leaked … (PRIVATE) into the public
  fedora desktop repo. do we not have safeguards in place for
  this?"* — then *"just scrub for now … set up real safeguards"*.

### Existing safeguards (and why they didn't help)

- `scripts/git-hooks/pre-commit` — scans staged files. Only fires
  on `git commit`. `gh issue create` doesn't go through git.
- `scripts/git-hooks/commit-msg` — scans commit messages. Same
  gap.
- Hooks daemon has many PreToolUse handlers (security_antipattern,
  ansible_enforcement, system_paths, sed_blocker, …) but none
  scan command bodies for cross-references against `localhost.yml`.

### Existing project handler conventions

- Project handlers live in `.claude/hooks/handlers/<event>/<name>.py`
- Current project pre_tool_use handlers: `ansible_enforcement.py`,
  `system_paths.py`
- Template available at `.claude/hooks/handlers/pre_tool_use/example_handler.py.example`
- Test convention: `.claude/hooks/handlers/<event>/tests/test_<name>.py`
- Front-controller daemon dispatches via `.claude/hooks/<event>` bash entry
- Daemon must be restarted after any handler change
  (`.claude/hooks-daemon/bin/hooks-daemon restart`)

## Tasks

### Phase 1: Research existing handler conventions ✅

Research-only phase already complete (general-purpose research agent
2026-05-26). Key findings recorded here so the implementation phase
does not have to redo the work:

- [x] ✅ **Handler location confirmed**: `.claude/hooks/handlers/pre_tool_use/<snake_name>.py` (this project uses legacy
  explicit-registration style, NOT the `.claude/project-handlers/`
  auto-discovery dir from upstream docs)

- [x] ✅ **Test location confirmed**: `.claude/hooks/handlers/pre_tool_use/tests/test_<snake_name>.py`, pytest
  framework

- [x] ✅ **Base class & imports confirmed** (matches
  `system_paths.py:7-14`):

  ```python
  import sys
  from pathlib import Path
  sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

  from claude_code_hooks_daemon.core import Decision, Handler, HookResult
  from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
  from claude_code_hooks_daemon.core.utils import get_file_path, get_bash_command
  ```

- [x] ✅ **Four abstract methods are mandatory** (per
  `hooks-daemon/src/claude_code_hooks_daemon/core/handler.py:62-205`):
  `matches`, `handle`, `get_claude_md`, `get_acceptance_tests`. The
  acceptance tests list must have ≥1 entry or the daemon refuses to
  load the handler.

- [x] ✅ **Class naming convention**: filename `<snake>.py` →
  class `<PascalSnake>Handler` (loader.py:101-113 tries both
  PascalSnake and PascalSnakeHandler suffixes).

- [x] ✅ **Registration is explicit** in
  `.claude/hooks-daemon.yaml` under `plugins.plugins[]` (this
  project does not auto-discover; append a YAML stanza per new
  handler).

- [x] ✅ **Constructor signature**: `super().__init__(name=…, priority=…, terminal=…)`. Priority bands: 0-19 safety,
  20-39 quality, 40-59 workflow, 60-79 advisory.

### Phase 2: TDD — failing tests first

Test fixtures use **placeholder strings throughout** — no private
identifiers in test data, ever. Example fixture YAML:
`github_accounts: {<alias-a>: "<gh-username-a>", joseph: "joseph-uk"}`.
Tests use these placeholders as the "private tokens" to be blocked
and `joseph`/`joseph-uk` as the "allowlisted public tokens".

- [ ] ⬜ **Create `tests/test_localhost_yml_leak_guard.py`** with
  failing tests for:
  - **Positive — gh issue create with private alias** in body →
    BLOCK with token name + YAML source path in the message
  - **Positive — gh pr create with private username** in body →
    BLOCK
  - **Positive — gh gist create with private token** in body →
    BLOCK
  - **Positive — curl POST with private token in `-d` arg** →
    BLOCK
  - **Positive — `curl -X POST` with private token in heredoc
    body** → BLOCK
  - **Positive — `--body-file` reads file containing private
    token** → BLOCK (handler must inspect the file contents
    when the path argument is a literal string)
  - **Negative — only allowlisted tokens** (`joseph`,
    `LongTermSupport`) → ALLOW
  - **Negative — non-posting command** (`gh repo view`,
    `gh issue list`) → ALLOW (matches() must return False)
  - **Negative — vault-encrypted value substring**: `localhost.yml`
    value is `!vault | …`, command body contains a substring of
    the ciphertext → ALLOW (vault values excluded from deny-list)
  - **Negative — short token** (deny-list filters values \<4 chars
    → no false positive on `et`, `id`, etc.)
  - **Edge — missing `localhost.yml`**: handler logs a warning,
    matches() returns False, ALLOW (fail-soft)
  - **Edge — missing allowlist file**: handler logs a warning,
    treats as empty allowlist (most conservative; deny-list
    not subtracted)
  - **Edge — `--body-file` with shell-substituted path** (e.g.
    `--body-file "$(mktemp).txt"`): handler logs that inspection
    was skipped, ALLOW (defence-in-depth is best-effort here)
  - **Tests for `get_claude_md()`**: returns a non-empty markdown
    string describing the handler's guidance
  - **Tests for `get_acceptance_tests()`**: returns a non-empty
    list of `AcceptanceTest` objects covering both a BLOCK case
    and an ALLOW case

### Phase 3: Implement the handler

- [ ] ⬜ **Create `.claude/hooks/handlers/pre_tool_use/localhost_yml_leak_guard.py`**
  implementing `LocalhostYmlLeakGuardHandler(Handler)` with the
  full four-abstract-method contract:

  - `__init__`: `super().__init__(name="localhost_yml_leak_guard", priority=15, terminal=True)` — priority 15 sits in the safety
    band (0-19), runs before quality/workflow handlers, after
    critical syntax blockers.
  - `matches(hook_input) -> bool`: tool name is `Bash`, command
    matches one of the known external-posting regexes:
    - `\bgh\s+(issue|pr|gist)\s+(create|edit|comment)\b`
    - `\bcurl\b.*(\s-d\s|\s--data\b|\s-X\s+(POST|PUT|PATCH)\b)`
    - `\bwget\b.*\s--post-`
  - `handle(hook_input) -> HookResult`:
    1. Load `localhost.yml` (project root); fail-soft on absence
       — return ALLOW.
    2. Walk the parsed YAML recursively, collect every non-vault
       string scalar of length ≥4 (skip `!vault` tagged nodes by
       custom YAML loader or post-parse type check).
    3. Load `.claude/public-token-allowlist.yml` (top-level
       `public_tokens:` list); subtract from deny-list. Fail-soft
       on missing file — treat as empty allowlist.
    4. Extract the command body from: inline `--body "…"` /
       `-d "…"`, `--body-file <literal-path>` (read the file if
       path is a literal), heredoc blocks (`<<EOF ... EOF`).
       Shell-substituted body paths → log + skip inspection.
    5. For each deny-token, search the body (case-sensitive
       substring match — tokens are usernames/aliases that are
       case-significant).
    6. On match: return `HookResult` with `Decision.BLOCK` and a
       message naming the matched token, its YAML source path
       (e.g. `localhost.yml:github_accounts.<alias-a>`), and the
       recommended generic placeholder (`<alias-a>` etc.) or the
       allowlist-entry remediation.
    7. No match → ALLOW.
  - `get_claude_md() -> str | None`: returns a markdown block
    explaining the handler's purpose and how to remediate a
    block (use placeholder, or add to allowlist if truly
    public). This appears in `CLAUDE.md`'s auto-generated
    hooksdaemon section after daemon restart.
  - `get_acceptance_tests() -> list[AcceptanceTest]`: returns at
    least one BLOCK case (gh issue create with a placeholder
    `<alias-a>` from a test fixture) and one ALLOW case
    (`gh repo view`). Per the daemon's contract, must be ≥1
    entry or load fails.

- [ ] ⬜ **Create `.claude/public-token-allowlist.yml`** seeded
  with the known public identifiers: `joseph`, `joseph-uk`,
  `LongTermSupport`, `LTSCommerce`. Top-level key
  `public_tokens:`, list of strings. Header comment explains
  the allowlist purpose and rules for adding new entries (only
  add a token here if it is *intentionally* public — i.e.
  appears in public commits, public docs, or the public org
  identity).

- [ ] ⬜ **Register the handler in `.claude/hooks-daemon.yaml`**
  by appending under `plugins.plugins`:

  ```yaml
      - path: ".claude/hooks/handlers/pre_tool_use/localhost_yml_leak_guard.py"
        event_type: "pre_tool_use"
        handlers: ["LocalhostYmlLeakGuardHandler"]
        enabled: true
  ```

- [ ] ⬜ **Run the test suite, get all tests green** —
  `pytest /workspace/.claude/hooks/handlers/pre_tool_use/tests/test_localhost_yml_leak_guard.py -v`.

- [ ] ⬜ **Run `./scripts/qa-all.bash`** — must pass.

### Phase 4: Integration & live verification (user does this on host)

CCY container restriction: the daemon cannot be restarted from
inside the container. These steps must be run by the user on the
host after the implementation phase commits land.

- [ ] ⬜ **Restart the hooks daemon** (host):
  `.claude/hooks-daemon/bin/hooks-daemon restart`
- [ ] ⬜ **Confirm daemon is RUNNING and the new handler loaded** (host):
  - `.claude/hooks-daemon/bin/hooks-daemon status` → expect
    `RUNNING`
  - `.claude/hooks-daemon/bin/hooks-daemon logs 2>&1 | grep -i localhost_yml_leak_guard` → expect to see the
    handler being registered during startup
  - If the daemon refuses to start, the handler's
    `get_acceptance_tests()` is the most common culprit — must
    return ≥1 entry, all tests must pass.
- [ ] ⬜ **Live test — should BLOCK**: attempt
  `gh issue create --title test --body "test <real-private-token>"`
  on the host and confirm the handler blocks with a clear message
  naming the token + its YAML source path.
- [ ] ⬜ **Live test — should ALLOW**:
  `gh issue create --title test --body "joseph LongTermSupport hello"`
  → passes (only allowlisted public tokens).
- [ ] ⬜ **Live test — should ALLOW (unrelated)**: `gh repo view`
  → no block (matcher correctly scopes to posting commands only).
- [ ] ⬜ **Live test — heredoc**: a `gh issue edit … --body "$(cat <<'EOF' ... <real-private-token> ... EOF)"` → BLOCK.

### Phase 5: Docs

- [ ] ⬜ **Update `CLAUDE/SecurityRules.md`** — add a
  "Public-surface posting" section pointing to the new handler
  and the allowlist file
- [ ] ⬜ **Document the allowlist format** at the top of
  `public-token-allowlist.yml` itself — what counts as public,
  how to add a new entry, why entries should be reviewed
- [ ] ⬜ **Brief note in `CLAUDE.md`** — one line under the
  Security section referring to the new handler

### Phase 6: QA & validation

- [ ] ⬜ `./scripts/qa-all.bash` passes (Python new handler +
  any shell adjacent to it)
- [ ] ⬜ Daemon restart verifier passes (handler imports
  cleanly, daemon comes up RUNNING)
- [ ] ⬜ The pre-commit `git-hooks/pre-commit` still works as
  before (no interference)

## Dependencies

- **Depends on**: claude-code-hooks-daemon already installed and
  running (it is)
- **Related**: Plan 00045 — when `project_personas` lands and
  replaces `github_accounts`, the deny-list walker must traverse
  `project_personas` too. Phase 3's "walk the parsed YAML, collect
  every non-vault string value" approach naturally handles both —
  no Plan 00045 coupling needed beyond the walk being recursive.

## Technical Decisions

### Decision 1: Block scope (which commands)

**Decided**: External-posting commands only —
`gh (issue|pr|gist) (create|edit|comment)`,
`curl` with POST/PUT/PATCH or `-d`/`--data*`,
`wget --post-*`. Reasoning: every other Bash command (`gh repo view`,
`gh issue list`, `git log`, etc.) doesn't post text externally and
should not be slowed down or risk false-positive blocks.

**Date**: 2026-05-26

### Decision 2: Deny-list source (`localhost.yml` walked at run time)

**Options**:

1. Static deny-list file the user curates by hand
2. Walk `localhost.yml` at hook run time, extract all non-vault
   string values ≥4 chars (this proposal)

**Decided**: Option 2. The user already maintains `localhost.yml` as
the source of truth for identity declarations; making them also
maintain a separate deny-list duplicates effort and drifts the
moment a new alias is added. Run-time walking is also cheap
(parse one YAML file, traverse, build a set).

**Date**: 2026-05-26

### Decision 3: Allowlist file format

**Decided**: `.claude/public-token-allowlist.yml` — single YAML
list of strings, top-level `public_tokens: [joseph, …]`. Lives
in `.claude/` so it's project-scoped, not user-scoped, and gets
version-controlled with the repo. Public tokens ARE intentionally
public — committing the allowlist itself does not leak anything.

**Date**: 2026-05-26

### Decision 4: Token length threshold

**Decided**: Tokens shorter than 4 chars are filtered out of the
deny-list to avoid false positives on very common substrings
(`id`, `et`, `co`, `s3`). This means an alias of 3 chars or fewer
will not be protected — accept that trade-off; the user controls
alias names and can pick ≥4 chars.

**Date**: 2026-05-26

## Success Criteria

- [ ] A `gh issue create` containing a token from `localhost.yml`
  (not in the allowlist) is BLOCKED with a message naming the
  token, its YAML source path, and the recommended placeholder
- [ ] A `gh issue create` containing only allowlist tokens passes
  through unimpeded
- [ ] A `gh repo view` (or any non-posting command) is never
  intercepted
- [ ] Handler tests run green, pytest passes
- [ ] Daemon restarts cleanly with the new handler enabled
- [ ] Live end-to-end verification — at least one positive BLOCK
  and one positive ALLOW case demonstrated on the host

## Risks & Mitigations

| Risk                                                                                                | Impact                                                         | Mitigation                                                                                                                                                                                                                  |
| --------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| False positives on legitimate uses of public-by-design tokens that aren't yet in the allowlist      | Medium — could block valid workflows                           | Allowlist is one-line YAML edits; the block message includes the token, so the fix is obvious (add it to the allowlist if it really is public)                                                                              |
| Handler crashes on malformed `localhost.yml`                                                        | Medium — would block everything                                | Fail-soft: catch any YAML parse error, log it, ALLOW the command. The leak guard is defence-in-depth, not the only safeguard                                                                                                |
| `--body-file` path expansion + shell substitution makes pre-execution body inspection unreliable    | Medium — could miss leaks via dynamically-generated body files | Best-effort: if the path is a literal string, read it; if it's a shell substitution, skip (log a warning that the body couldn't be inspected). User-visible failure mode is the same as today (no protection) — never worse |
| Performance: walking the YAML on every matching Bash invocation                                     | Low — `localhost.yml` is ~100 lines, parse is sub-millisecond  | If profiling shows a hot path later, cache the parsed deny-list per daemon session and invalidate on `localhost.yml` mtime change                                                                                           |
| Handler protection bypassed by piping output through other tools that the matcher doesn't recognise | Medium — can't catch everything                                | Document explicitly that this is defence-in-depth, not a guarantee. The memory rule (`feedback_public_repo_external_posting.md`) is the human-in-the-loop fallback                                                          |

## Timeline

- Phase 1: Research existing handler conventions
- Phase 2: TDD failing tests (must precede implementation)
- Phase 3: Implement handler + allowlist file
- Phase 4: Integration & live verification on host
- Phase 5: Docs (concurrent with Phase 4)
- Phase 6: QA & validation (gates completion)

## Notes & Updates

### 2026-05-26 — plan created

- Direct follow-up to today's leak of `<alias-a>`/`<gh-username-a>`
  and `<alias-b>`/`<gh-username-b>` into public issue
  `LongTermSupport/fedora-desktop#22` via `gh issue create`.
- User requested "real safeguards" after accepting that the leak
  itself was not hugely sensitive.
- Memory rule saved as fallback while this handler is built:
  `feedback_public_repo_external_posting.md` (instructs human-in-
  the-loop scrubbing of `localhost.yml` content before any
  external post).
- Implementation will be delegated to the `hooks-specialist`
  agent given this plan as the brief.
