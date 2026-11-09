# docs-optimise procedure

Referenced from `SKILL.md`. Run from the project root.

## 1. Pick the ruleset

Read `documentation.enabled` in `.claude/hooks-daemon.yaml`.

- **true**: the daemon ruleset governs (`CLAUDE/DocumentationStrategy.md`).
  A rules file is a pointer target only (R7a). Substance goes to the agent
  tree or a registered module `CLAUDE.md` (R7d). No proposal adds an
  `@`-import outside the resident set (R6).
- **otherwise**: Claude Code's own guidance governs (the loading-rules
  contract in `contracts/claude-code-instructions/`). A `paths:`-scoped rules
  file may carry substantive instructions.

The report's first lines state which ruleset applied and why.

## 2. Measure

Run `.claude/skills/docs-optimise/scripts/footprint.sh` (it wraps
`docs-qa --footprint --json`). Work only from entries with
`proposable: true`. Entries with `proposable: false` (the daemon-injected
`<hooksdaemon>` block, daemon-deployed artefacts, `CLAUDE.local.md`, and
user-scope files under `~/.claude/`) are counted in the totals and named in the
report with their `not_proposable_reason`; they are never moved. Note the
`unmodelled` list and the audited-versus-installed version pair in the report.

## 3. Classify

Read each proposable always-loaded file and place every section in exactly one
class:

| Class         | Meaning                                             | Target                                                                                                                   |
| ------------- | --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| resident      | Needed in nearly every session; stays               | stays in place, tightened                                                                                                |
| path-scopable | Only matters when working on certain files          | `.claude/rules/<name>.md` with `paths:` (claude-code mode); an agent-tree document plus a one-line pointer (daemon mode) |
| nested-module | Belongs to one directory's code                     | a `CLAUDE.md` in that directory                                                                                          |
| on-demand     | Reference depth, read only when a task calls for it | an agent-tree or docs document, reached by a pointer from the resident file                                              |

One canonical home per fact: if a fact already has a home, the proposal
replaces the duplicate with a pointer, it does not copy.

## 4. Write the proposal

Create `untracked/scratch/docs-optimise/<run-id>/` (`<run-id>` is a UTC
timestamp). Never write to a tracked file. Never add a `resident_at_imports`
or `grandfather_allowlist` entry, in either mode. Write:

- the proposed new and replacement files, mirroring their intended repository
  paths under `proposed/`
- `proposal.patch`: a unified patch against the current tree, applicable with
  `git apply untracked/scratch/docs-optimise/<run-id>/proposal.patch`
- `report.md`: ruleset applied and why, the footprint before and after (always
  loaded lines and bytes), each section with its class and target, and the
  files counted but not proposable

Check `git apply --check` on the patch before reporting it.

## 5. Close with the hand-offs

End the report with two next steps and no more:

1. After applying the patch, run `/docs-qa` scoped to the touched files to
   re-verify SSoT.
2. Run `/doctor prompt-audit` for outdated, missing-reference and
   contradictory content. If `/doctor` is unavailable in this Claude Code
   version, say so in one sentence and move on.
