# Plan 00092 — Research facts

What was measured in a CCY container **before** planning, and the deployment chain the
plan had to fit into. Extracted from `PLAN.md` to keep it lean; every fact here was
verified at a `file:line`, not recalled.

The threat model built on these is [SECURITY-MODEL.md](SECURITY-MODEL.md); the choices
they led to are in [DECISIONS.md](DECISIONS.md).

---

## The credential

- The token reaches PID 1 via `-e CLAUDE_CODE_OAUTH_TOKEN` at
  `files/var/local/claude-yolo/claude-yolo:3021`, passed **by name** so it never appears
  in the `podman run` argv.
- Container mode always has a token. The "Desktop" fallback that uses the host's own OAuth
  is host-mode only — `lib/token-management.bash:1024` states there is no Desktop fallback
  in container mode. So `/proc/1/environ` is a reliable source.
- A bare `claude -p` from the Bash tool fails with `Not logged in`. The same command with
  the token recovered from `/proc/1/environ` returns a real completion. A prototype wrapper
  worked on first run and left no token in the calling shell.
- Comparing PID 1's environment against the Bash-tool shell, exactly two names are missing:
  `CLAUDE_CODE_OAUTH_TOKEN`, and `GH_TOKEN` which `entrypoint.sh` unsets itself after
  `gh auth login`. The removal is by name and specific to the credential.

## The opt-in surface

- `ccy.env` is already sourced **as shell** inside the container at
  `entrypoint.sh:336-341`, so the checked-out tree already controls the command that runs.
  Plan 00068 recorded this as `E10`. Putting the opt-in flag there adds no trust the file
  does not already hold.

## Where skills live, and why the wiring is constrained

- Skills are staged in the image under `/opt/claude-yolo/skills/` and copied
  **unconditionally** to `/root/.claude/skills/` at `entrypoint.sh:295-304`, which runs
  *before* `ccy.env` is sourced. Both facts constrain the wiring: opt-in content cannot
  live in that directory, which is why the `optional/` tree is staged separately.
- `/root/.claude` symlinks to `/workspace/.claude/ccy`, so the skills directory is
  **host-persisted** across sessions and gitignored at `.claude/ccy/.gitignore:17`. A skill
  installed by an enabled session therefore survives into a later disabled session unless
  it is actively removed. This is threat T3.

## The deployment chain for any image asset

repo `files/opt/claude-yolo/...` → `play-claude-yolo.yml` copies to the host build
context → `Dockerfile` COPYs into the image → `entrypoint.sh` installs into the session.

Two consequences the plan had to handle:

1. Every step is additive. Deleting a file from the repo does **not** remove it from a
   host's build context, so a removed asset keeps shipping into the image until an explicit
   `state: absent` task clears it.
2. Image content only reaches a session through a rebuild, which only happens when
   `REQUIRED_CONTAINER_VERSION` and the Dockerfile's `claude-yolo-version` label are both
   bumped. An `entrypoint.sh` change without that bump does not ship.

## Dedupe

The dedupe scout checked 58 live plans and found none covering this. Nearest neighbours are
00068 (CCY env config for CI), 00089 and 00048 (token injection), and 00080 (network
isolation); each touches token handling or CCY config, none touches in-container child
processes.
