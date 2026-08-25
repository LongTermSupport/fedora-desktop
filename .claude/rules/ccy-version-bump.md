---
paths:
  - "files/var/local/claude-yolo/**"
---

# You are editing CCY — the version bump is MANDATORY

**Any** code change to `files/var/local/claude-yolo/claude-yolo` requires bumping
`CCY_VERSION` in the same commit. This is not a convention — the script carries a runtime
self-hash guard, and a **pre-commit hook rejects the commit** without the bump. Get it
wrong and users see `DEVELOPER ERROR: CCY script modified without version bump`.

Full rules: [CLAUDE/ContainerRules.md](../../CLAUDE/ContainerRules.md)

## What to bump, and where

| Changed                                | Also required                                                                      |
| -------------------------------------- | ---------------------------------------------------------------------------------- |
| `claude-yolo` (the launcher)           | `CCY_VERSION` — patch for fixes, minor for features, major for breaking changes    |
| `Dockerfile` (image content)           | `LABEL claude-yolo-version` **and** `REQUIRED_CONTAINER_VERSION` — they must match |
| `entrypoint.sh` (baked into the image) | the container version too, or the change never reaches a running container         |
| `ccy-ctrl-z-patch.js`                  | run `./scripts/qa-ctrl-z-patch.bash` (network required)                            |

Update the version comment to say **what** changed, not just that something did.

## The container boundary

If the project path is `/workspace/`, you are inside CCY right now. **Edit and commit
only** — never run a playbook here. Deployment happens on the HOST.

## Before you assume how it behaves

`claude-yolo` is ~2,850 lines with the launcher, `lib/*.bash`, `entrypoint.sh` and the
`Dockerfile` as four separate layers. A behaviour you are about to describe is usually
already implemented somewhere in them — check before adding a mechanism, and cite
`file:line` rather than describing from memory.
