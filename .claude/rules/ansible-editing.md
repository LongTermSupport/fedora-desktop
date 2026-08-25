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

These are pointers, not copies. Each linked document is the single source of truth; this
file exists so that touching an Ansible file **triggers** reading it.

| Read this                                                              | For                                                                    |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| [CLAUDE/AnsibleStyle.md](../../CLAUDE/AnsibleStyle.md)                 | Playbook structure, `root_dir`, markers, scope guards, shell blocks    |
| [CLAUDE/InfrastructureAsCode.md](../../CLAUDE/InfrastructureAsCode.md) | Edit → playbook → deploy → test. **Never** a manual system change      |
| [helpers/CLAUDE.md](../../helpers/CLAUDE.md)                           | Complex logic belongs in a TDD'd Python helper, not an inline `shell:` |
| [CLAUDE/QA.md](../../CLAUDE/QA.md)                                     | `./scripts/qa-all.bash` before every commit                            |

## The things that bite most often

- **`root_dir` is `{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`.**
  Never `{{ inventory_dir }}/../../` — `inventory_dir` is undefined during the early
  `vars_files` pass, so those entries **silently skip**.
- **Every play declares `scope: general | gnome | server`.** `gnome`/`server` plays carry
  the exact 2-task guard, byte-identical (the QA gate compares the text).
- **`failed_when: false` / `ignore_errors: true` are prohibited** without a same-line
  `# FAIL-FAST-OK: <reason>`.
- **Every multi-command `shell: |` starts `set -euo pipefail`** with
  `args: executable: /bin/bash` — the module only propagates the *last* command's status.
- **Never write `x: "{{ x | default(...) }}"`.** Under ansible-core 2.19 it recurses at
  runtime, and **neither `--syntax-check` nor `qa-all.bash` used to catch it** — apply the
  default at the point of use instead.
- **Avoid apostrophes and backticks in `# comments` inside `shell:` blocks.** The 2.19
  argument splitter is not bash-aware and fails on unbalanced quotes even inside comments.

## In the CCY container

If the project path is `/workspace/`, you are in a container: **edit and commit only.**
Never run a playbook here — tell the user to run it on the HOST.
