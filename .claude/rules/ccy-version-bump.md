---
paths:
  - "files/var/local/claude-yolo/**"
---

# You are editing CCY — the version bump is MANDATORY

Any code change to the launcher or its `lib/` requires bumping `CCY_VERSION` in the same commit; a Dockerfile or `entrypoint.sh` change bumps the container version too. A pre-commit hook rejects the commit without it. If the project path is `/workspace/` you are inside CCY: edit and commit only, never run a playbook.

- [CLAUDE/ContainerRules.md](../../CLAUDE/ContainerRules.md) — what to bump and where, semantic versioning, the container boundary, the four layers to check before adding a mechanism, and why the ctrl+z patch must not return
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — `./scripts/qa-all.bash` before every commit
