# Rules-file migration report

Branch F44, all changes staged and uncommitted.

## Per rules file

### ansible-editing.md

- Migrated to `CLAUDE/AnsibleStyle.md`:
  - Fail-fast annotation rule (new "Fail-fast annotations" subsection under Error Handling and Validation).
  - Self-default variable prohibition with point-of-use guidance (new "Never self-default a variable" subsection under Variable Naming and Templates, linking the AgentNotes gotcha).
  - No apostrophes/backticks in `# comments` inside `shell:` blocks (appended to External Repository Integration, linking the AgentNotes gotcha).
  - Bonus: the Code Quality example used `ansible_distribution`, contradicting the doc's own facts rule; corrected to `ansible_facts['distribution']`.
- Dropped as copies: `root_dir`/`inventory_dir`, scope guard, shell strict mode (all in AnsibleStyle), CCY container boundary (ContainerRules).
- New pointer list adds ContainerRules and AgentNotes.

### bash-scripts.md

- Migrated to `CLAUDE/AgentNotes.md` (new "Bash conventions the gates only partly enforce" under Project Gotchas): strict mode, errors never hidden, quote every expansion / `local`, no in-place edits (`mktemp` + `mv`, verify rewrite), the `bash -n` exit-0 trap, the `grep \t` trap.
- Migrated to `CLAUDE/SecurityRules.md` Security Principles: secrets never in `argv` (/proc rationale, env or stdin).
- Migrated to `CLAUDE/QA.md` Rules Summary: suppression directives banned.
- Dropped as copies: stderr hygiene, interactive re-prompt, qa-all (already in linked docs).

### ccy-version-bump.md

- Migrated to `CLAUDE/ContainerRules.md` "This applies to" list: Dockerfile (`LABEL claude-yolo-version` must match `REQUIRED_CONTAINER_VERSION`), `entrypoint.sh` (baked into the image). New "Check the layers before adding a mechanism" subsection (four layers, cite file:line).
- Dropped as stale: the `ccy-ctrl-z-patch.js` row; that patch and `qa-ctrl-z-patch.bash` were deleted in CCY 3.42.0 per ContainerRules.
- Dropped as copies: version bump rule, semver, error message, version comment wording, container boundary.

### gnome-extensions.md

- Migrated to `CLAUDE/GnomeShell.md` Common Pitfalls: new pitfall 4 on `metadata.json` declaring this branch's GNOME major and the `check_extension_compat` gate (links QA.md).
- Dropped as copies: ESLint binary command, Wayland freeze rationale (extensions/CLAUDE.md), GNOME 45+ imports, `disable()` cleanup, event status, nested-shell testing (GnomeShell.md).

### python-helpers.md

- Migrated to `helpers/CLAUDE.md` Fail fast: explicit `check=` rule with the narrow `check=False` allowance; pointer to the ruff pin section in QA.md. Cross-references line now includes QA.md and StderrHygiene.md.
- ruff pin content: the rules file claimed the ruleset was ruff's default set; `ruff.toml` enumerates `select` explicitly, so the QA.md version (below) is the corrected one.
- Dropped as copies: stdlib only, test first, namespace packages, `command:` + `argv:`, marker lines, the `discover` trap.

### qa-gates.md

- Migrated to `CLAUDE/QA.md`: new "Changing a Gate" section (scans-nothing failure mode with `$REPO_ROOT` anchoring and the zero-file guard, scans-wrong-things failure mode with shared exclusion list and comment stripping, prove with a control fixture, most gates have no tests with `qa-docs.bash`/`link_check.py` as the model). New "ruff: the Ruleset Is Explicit and the Version Is Pinned" section. Plan-number narrative and the test count were left out.
- Rules file now points at QA.md and AgentNotes.

### secrets-and-vault.md

- Migrated to `CLAUDE/SecurityRules.md`: Vault Management now documents `./vault.bash set` versus `replace` (replacing the raw `ansible-vault encrypt_string` bullet); new "External Posts Are Not Scanned" subsection (gh CLI gap, edit history, `CLAUDE/Plan/` is public) linking the AgentNotes procedure.
- Dropped as copies: never-commit list, placeholder table and private-range note (ExampleValues), variable-level vault and editor rule.

### Unchanged (already thin, daemon-versioned pointers)

agent-docs, claude-agents, claude-skills, human-docs, plan-dir, source-dirs, test-dirs.

## Verification

- `hooks-daemon docs-qa --sweep`: zero `rules-file-shape` findings (was 14 across 7 files).
- `./scripts/qa-all.bash`: passed, 647 files; docs gate 63 files OK (links, anchors, catalogue, index).
- `hooks-daemon docs-qa --check-staged`: 0 findings.
- Pre-existing advisories not addressed (out of scope): `helpers/CLAUDE.md` module-doc-budget, AnsibleStyle duplicate-block against Plan 00061 PROPOSAL.md.

## Files changed (staged)

.claude/rules/ansible-editing.md, bash-scripts.md, ccy-version-bump.md, gnome-extensions.md, python-helpers.md, qa-gates.md, secrets-and-vault.md; CLAUDE/AgentNotes.md, AnsibleStyle.md, ContainerRules.md, GnomeShell.md, QA.md, SecurityRules.md; helpers/CLAUDE.md.
