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
  withdrawn capability. 29 − 3 = 26; the measurement says 28. Every assertion below is on
  **tool names**, never on a count.

> **The substitution is why the expected set cannot be derived on paper.** "Default minus denied"
> is wrong: the session's real set is default − denied **+ whatever the CLI substitutes**, and
> which tools it substitutes for which withdrawal is not documented — it was discovered by
> measuring. So §5's assertion 2 diffs against a set that must be **captured per class, with that
> class's `--disallowedTools` string applied**, not computed from a default vocabulary. Plan 00113
> Task 0.3 captures it that way.

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
implied. Decision 9 forbids three things — "never write to it, commit, or push" — and the
credential only reaches the third. **Nothing stops a local `git commit`, and nothing stops a
shell redirect into the working tree**; the runner keeps a *persistent* checkout, so a dirtied
tree or a stray commit outlives the job that made it and lands on the next one. What the
credential does close is push:

> **Required property (to confirm against lts-infra, which has no checkout in this container):
> the token a class-A job receives must carry no push scope.** If it does, the tool surface is
> decorative — the agent can `git push` through `Bash` whatever the tool list says.

The tool list is defence in depth against the *accidental* write — an agent that edits a file
because that is what it usually does. It is not a boundary against a determined one. Say so in
the implementation plan; a boundary described as stronger than it is, is worse than a weak one
described honestly.

For the write and commit halves the credential does not reach, the boundary has to be the
checkout itself — a clean tree asserted after the job, or a checkout the job cannot keep. That
is the runner's side, so it is stated here as a requirement on lts-infra Plan 00030 rather than
specified: **a class-A job must not be able to leave the persistent checkout dirty.**

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

That one place holds two things per class: the **denied names**, which become the
`--disallowedTools` string, and the **expected observed set**, which assertion 2 diffs against.

They are not two views of one list, and treating them as one is the mistake this paragraph
previously made. Because the CLI **substitutes** narrower tools for a withdrawn capability (§1),
the expected set is *not* "the default minus the denied" — removing `Bash` for class B adds
`Glob` and `Grep`, so subtraction would predict 26 names where 28 were measured, and assertion 2
would fail on every run. The expected set is **captured**, per class, from a session launched
with that class's deny string (00113 Task 0.3), and thereafter maintained as a declared artefact.

## 5. The assertions, which must be able to fail

A restriction nobody checks is a comment. Each of these fails the job at startup, before the
agent's first turn:

1. **Flag existence.** Assert the CLI about to run exposes every security-carrying flag this
   design uses. ccy auto-updates Claude Code daily, so the binary is not the one the design was
   written against. A CLI silently lacking `--disallowedTools` would run with the full tool set
   and report success. Assert on the build about to run — not on a pinned version, which is a
   proxy for the property rather than the property.

2. **The whole tool list, diffed against a declared set.** Capture the session's actual tool
   list and assert it **equals** the class's declared expected set — unexpected members fail,
   missing members fail. **Never assert a count** (§1); the comparison is over names.

   > Checking only that the must-be-absent names are absent is the same shape §3 rejects for
   > MCP, and fails open the same way: the vocabulary is Anthropic's, not ours, and assertion
   > 1's own rationale is that the CLI changes underneath this design daily. A write primitive
   > that is renamed, or newly added, is absent-by-name — and every assertion passes green over
   > a class-A job that can now write. A diff against a declared set is the only form where the
   > unexpected member is what fails.
   >
   > The cost is real and is the point: a benign new read tool also fails the job, until someone
   > adds it to the declared set. That is a person reviewing one new tool name, which is the
   > work this assertion exists to force. Fail-closed here, or do not bother.

3. **`Bash` present for class A.** Formally the presence half of assertion 2, named separately
   because its failure mode is the silent one: a class-A job whose `Bash` went missing would
   **skip the suite and pass green** — the failure this whole plan exists to prevent, arriving
   through the mechanism meant to prevent it. An implementer who reads assertion 2 as "the
   denylist" will drop this; the declared set is not a denylist, and dropping it is how
   over-tightening ships. Over-tightening is the likelier mistake once one list feeds two
   classes (§4).

4. **MCP vocabulary.** For class B, assert the tool the job intends to call actually exists in
   the configured server ([DECISIONS.md](../DECISIONS.md) **Decision 7**: *"an unconfigured or
   misnamed MCP tool is silently inert"*). Assert against the server's captured vocabulary, not
   a hand-kept list.

## 6. What this does not settle

- The class-A token's scopes (§2) — a property of lts-infra's token store, unreadable from
  here. It is named as a required property, not asserted as a fact.

- The concrete default tool vocabulary. The 2026-08-10 measurement recorded counts, not names,
  and this specification deliberately does not invent the missing names: it names the
  primitives that must be absent and requires the implementation to capture the real list and
  assert against it. Anything else would be a hand-kept list of exactly the kind §4 forbids.

  This is now a **prerequisite**, not a loose end: assertion 2 diffs against a declared set, and
  that set cannot be declared until the names are captured. Plan 00113 Task 0.3 captures them
  from the binary about to run; Task 2.1 is where they become the one place per class.

- Whether the dirty-tree half of §2's boundary is enforced on the runner. Stated there as a
  requirement on lts-infra Plan 00030; nothing in this repo can assert it.
