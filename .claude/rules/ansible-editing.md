---
paths:
  - "playbooks/**"
  - "tasks/**"
  - "vars/**"
  - "environment/**"
  - "roles/**"
  - "**/*.yml"
  - "**/*.yaml"
---

# You are editing Ansible — read the style rules before you write

Every play declares a `scope`, anchors `root_dir` on the config lookup, and opens every multi-command `shell:` block with strict mode. If the project path is `/workspace/` you are in the CCY container: edit and commit only, never run a playbook.

- [CLAUDE/AnsibleStyle.md](../../CLAUDE/AnsibleStyle.md) — playbook structure, `root_dir`, scope guards, fail-fast annotations, shell blocks, the 2.19 self-default and comment-quote traps
- [CLAUDE/InfrastructureAsCode.md](../../CLAUDE/InfrastructureAsCode.md) — edit, playbook, deploy, test; never a manual system change
- [CLAUDE/ContainerRules.md](../../CLAUDE/ContainerRules.md) — the CCY container boundary
- [helpers/CLAUDE.md](../../helpers/CLAUDE.md) — complex logic belongs in a TDD'd Python helper, not an inline `shell:`
- [CLAUDE/AgentNotes.md](../../CLAUDE/AgentNotes.md) — the ansible-core 2.19 parser gotchas in full
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `./scripts/qa-all.bash` before every commit
