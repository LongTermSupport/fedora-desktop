---
paths:
  - "files/var/local/claude-yolo/**"
  - "helpers/ssh_agent_filter/**"
---

# You are editing CCY — the version bump is MANDATORY

Any code change to the launcher, its `lib/`, or ccy's one-key agent (`helpers/ssh_agent_filter/`, deployed into `lib/`) requires bumping `CCY_VERSION` in the same commit; a change to anything the image is built from (the Dockerfile, a file it copies, or the `src-go/` code its builder stage compiles) bumps the container version too. A pre-commit hook rejects the commit without it. If the project path is `/workspace/` you are inside CCY: edit and commit only, never run a playbook.

- [CLAUDE/ContainerRules.md](../../CLAUDE/ContainerRules.md) — what to bump and where, semantic versioning, the container boundary, the four layers to check before adding a mechanism, and why the ctrl+z patch must not return
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `./scripts/qa-changed.bash` before each commit, GitHub CI's `qa-all.bash` before a protected branch moves
