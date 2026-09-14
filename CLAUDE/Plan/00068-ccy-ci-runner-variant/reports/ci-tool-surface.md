# Task 3.4 — the CI tool surface, per event class

Closes the remaining half of Task 3.4. [DECISIONS.md](../DECISIONS.md#decision-9--ci-runs-with-a-restricted-tool-surface)
Decision 9 sets the intent and the mechanism; this specifies **what** is restricted, per event
class, and **how the restriction is asserted**. It specifies a boundary for the implementation
plan (Task 4.1) to build — it is not itself an implementation.

## 1. The mechanism, restated only where it constrains the lists

Measured 2026-08-10 (journal of that date; the table is in Decision 9):

- **`--disallowedTools` removes a tool from the session** and composes with
  `--dangerously-skip-permissions`. It is the only flag that removes anything.
- **`--allowedTools` removes nothing.** It governs prompting. Under `bypassPermissions` it is
  inert; under headless `-p` an unlisted tool is refused *by CLI default*, not by decision.
  Nothing in this specification may rest on it.
- **Removing `Bash` adds `Glob` and `Grep`** — the CLI substitutes narrower tools for a
  withdrawn capability. 29 − 3 = 26; the measurement says 28. Every assertion below is on a
  **tool name being absent**, never on a count.

## 2. The two classes

### Class A — `push`, `pull_request`

The agent runs `./.claude/ccy/ci.bash` (Plan 00030) and reads the tree.

|                     |                                                                      |
| ------------------- | -------------------------------------------------------------------- |
| **Must be present** | `Bash` — without it there is no suite run and the job has no purpose |
| **Must be ABSENT**  | `Edit`, `Write`, `NotebookEdit` — the direct write primitives        |
| **MCP**             | none configured (§3)                                                 |

**`Bash` is a write vector, and removing `Edit`/`Write` does not close it.** Decision 9 already
concedes "read-only cannot be literal" here; this states the consequence rather than leaving it
implied. What actually stops a class-A job committing or pushing is **the credential**, not the
tool list:

> **Required property (to confirm against lts-infra, which has no checkout in this container):
> the token a class-A job receives must carry no push scope.** If it does, the tool surface is
> decorative — the agent can `git push` through `Bash` whatever the tool list says.

The tool list is defence in depth against the *accidental* write — an agent that edits a file
because that is what it usually does. It is not a boundary against a determined one. Say so in
the implementation plan; a boundary described as stronger than it is, is worse than a weak one
described honestly.

### Class B — `issues`, `issue_comment`

No suite runs. This is triage and comment, and a genuinely narrower surface is available.

|                      |                                                                                  |
| -------------------- | -------------------------------------------------------------------------------- |
| **Must be ABSENT**   | `Bash`, `Edit`, `Write`, `NotebookEdit`                                          |
| **Expected present** | the read primitives, including the `Glob` and `Grep` that removing `Bash` *adds* |
| **MCP**              | exactly one server: whatever posts the comment (§3)                              |

Removing `Bash` is what makes this class worth separating — and it is also what removes `gh`.
A class-B job therefore **cannot post its comment through the shell**; the posting capability
has to arrive as an MCP tool. That is not a cost of the design, it is the design: the one
write the class is allowed to do becomes an enumerated server-side tool instead of an
unbounded shell.

## 3. MCP: an allowlist at server granularity, never a denylist

Decision 9: *prefer an allowlist wherever the vocabulary is not ours; a denylist fails open on
a typo.* Measured downstream, not theoretical — earlier revisions denied several tool names
that do not exist while the real write primitive was not listed at all.

The MCP tool vocabulary is the server's, not ours, so **no `mcp__*` name appears in any
`--disallowedTools` list.** Restriction is by which servers are loaded at all:

- `--mcp-config <container-local path>` — never under `/root/.claude`, which
  `entrypoint.sh:183-195` symlinks into the checkout (Decision 7).
- `--strict-mcp-config` — so no user-scoped or project-scoped server is merged in behind the
  config. Without it the allowlist is not an allowlist.
- Class A configures **no** server. Class B configures **one**.

A typo in a server name yields no server and a job that cannot post — it fails closed, loudly.
A typo in a denied tool name would have yielded a job that can do more than intended, silently.

## 4. One list per class; every layer derived from it

Decision 9, sub-item 2. There must be exactly one place per class where the tool names live,
and the flag string, the startup assertion and the documentation must all be generated from
it. Three hand-kept copies of the same list is how one of them ends up wrong, and the one that
ends up wrong is never the one anybody reads.

## 5. The assertions, which must be able to fail

A restriction nobody checks is a comment. Each of these fails the job at startup, before the
agent's first turn:

1. **Flag existence.** Assert the CLI about to run exposes every security-carrying flag this
   design uses. ccy auto-updates Claude Code daily, so the binary is not the one the design was
   written against. A CLI silently lacking `--disallowedTools` would run with the full tool set
   and report success. Assert on the build about to run — not on a pinned version, which is a
   proxy for the property rather than the property.
2. **Tool absence, by name.** Capture the session's actual tool list and assert every name in
   the class's must-be-absent list is not in it. **Never assert a count** (§1).
3. **Tool presence, by name.** Assert `Bash` is present for class A. A class-A job whose
   `Bash` went missing would skip the suite and pass — the failure mode this whole plan exists
   to prevent, arriving through the mechanism meant to prevent it.
4. **MCP vocabulary.** For class B, assert the tool the job intends to call actually exists in
   the configured server (Decision 9 §MCP: *"an unconfigured or misnamed MCP tool is silently
   inert"*). Assert against the server's captured vocabulary, not a hand-kept list.

Assertion 3 is the one an implementer will be tempted to skip as redundant. It is the only one
that catches an over-tightened denylist, and over-tightening is the likelier mistake once one
list feeds two classes (§4).

## 6. What this does not settle

- The class-A token's scopes (§2) — a property of lts-infra's token store, unreadable from
  here. It is named as a required property, not asserted as a fact.
- The concrete default tool vocabulary. The 2026-08-10 measurement recorded counts, not names,
  and this specification deliberately does not invent the missing names: it names the
  primitives that must be absent and requires the implementation to capture the real list and
  assert against it. Anything else would be a hand-kept list of exactly the kind §4 forbids.
