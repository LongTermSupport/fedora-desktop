# CCY Container Environment Rules

## Container Detection

**IF THE PROJECT PATH IS `/workspace/` — YOU ARE IN A CCY CONTAINER**

### What CCY Container IS:

- Development environment for editing files
- Git operations (commit, push, pull)
- File manipulation (read, write, edit)
- Code review and analysis
- Testing bash syntax with `bash -n`

### What CCY Container IS NOT:

- Target system for Ansible playbooks
- Fedora host with real users
- System with systemd services
- Environment with real users/groups

### Absolute Rules

1. **NEVER run Ansible playbooks** — the container does NOT have the target users, groups, or system state
2. **Only edit and commit** — then tell the USER to run the playbook on their HOST system
3. **Correct workflow**:
   ```bash
   # In CCY container (/workspace/):
   vim playbooks/imports/play-something.yml    # Edit
   git add playbooks/imports/play-something.yml
   git commit -m "Update playbook"
   git push

   # Then instruct USER to run on HOST:
   # "On your host system, run:"
   # ansible-playbook ~/Projects/fedora-desktop/playbooks/imports/play-something.yml
   ```

**REMEMBER: In CCY container = EDIT ONLY, DEPLOY ON HOST**

---

## CCY Version Bump Requirement

**ALWAYS bump CCY_VERSION when modifying `files/var/local/claude-yolo/claude-yolo`**

The CCY script has hash validation to detect modifications without version bumps. A pre-commit hook enforces this requirement.

**Rules:**

1. ANY code change requires a version bump (patches are fine for small fixes)
2. Update the version comment to describe what changed
3. Never commit CCY changes without bumping the version

**Version numbering (Semantic Versioning):**

- **Patch (x.y.Z)**: Bug fixes, minor improvements, documentation
- **Minor (x.Y.0)**: New features, backward compatible changes
- **Major (X.0.0)**: Breaking changes, major refactoring

**Example:**

```bash
# Before (version 3.0.0)
CCY_VERSION="3.0.0"  # Removed sessions, simplified state management

# After making a fix (bump to 3.0.1)
CCY_VERSION="3.0.1"  # Fix: persist sessions in .claude/ccy/
```

**What happens if you forget:**

- Pre-commit hook will **REJECT** the commit
- Users will see "DEVELOPER ERROR: CCY script modified without version bump"

**This applies to:**

- `files/var/local/claude-yolo/claude-yolo` (main CCY wrapper)
- **`files/var/local/claude-yolo/lib/*.bash`** — the six libraries the launcher
  sources are part of the same program, and together they are the larger half of
  it. `CCY_VERSION` lives in the launcher, so a `lib/` change must stage the
  launcher too in order to bump it.
- Any file with version tracking

Both the `pre-commit` gate and the runtime `CCY_HASH` used to key on the
launcher **alone**: 71 commits touched `lib/`, 22 of them without touching the
launcher, so no bump was required and none was made — a behaviour change shipped
with an unchanged version and a hash that still matched. Fixed in 3.41.0; see
`docs/ccy-changelog.md`.

---

## ctrl+z SIGSTOP Suppression Is the Supervisor's Job — Do Not Re-Add a Patch

**CCY 3.42.0 deleted the image-level ctrl+z patch.** Do not reintroduce it, and
do not "fix" a ctrl+z freeze by patching Claude Code again — that is the problem
this removal solves, not the solution.

**The hazard is real and unchanged.** Ink (Claude Code's terminal UI framework)
intercepts ctrl+z *before* the keybinding system and calls
`process.kill(pid, 'SIGSTOP')` — an unblockable signal, and unrecoverable in a
container with no shell to run `fg` in. `"ctrl+z": null` in `keybindings.json`
does **not** help; the key never reaches the keybinding layer.

**What handles it now:** the hooks-daemon PTY supervisor,
`.claude/ccy/claude-supervise.py` (hooks-daemon **Plan 00173**, daemon ≥ 3.44).
CCY exec's the supervisor instead of `claude`, so it sits between the terminal
and Claude Code's PTY and guards the key from *outside* the application, in two
layers:

- **`strip_suspend()`** removes the `0x1a` SUSP byte from forwarded stdin, so
  the byte Ink's handler keys on never arrives. Ink's `handleSuspend()` is
  therefore never invoked — no patch needed to disable it.
- **`install_input_signal_guards()`** swallows `SIGTSTP` and `SIGQUIT` (and
  `SIG_IGN`s `SIGTTIN`/`SIGTTOU`) belt-and-braces, in case a stop signal is ever
  actually delivered. `SIGINT` (ctrl+C) is deliberately left working.

A rate-limited `⛔ Ctrl+Z ignored — use /exit to quit` notice is posted to the
status line so the keypress is visibly inert rather than silently swallowed.

**Why this is strictly better than the patch it replaces.** The patch had to
find and rewrite an anchor inside a minified upstream artifact — first a
platform boolean in `cli.js`, later a same-length byte edit inside the native
`bin/claude.exe` SEA blob. That anchor churned every few Claude Code releases,
the patch soft-failed when it stopped matching, and the daily in-place
`npm i -g …@latest` re-shipped an unpatched binary that had to be re-patched.
The supervisor needs no knowledge of Claude Code's internals whatsoever, so no
upstream release can break it and an updated binary is protected the moment it
starts.

**The supervisor is ON BY DEFAULT (CCY 3.43.0).** `entrypoint.sh` turns it on
whenever the project ships one at `/workspace/.claude/ccy/claude-supervise.py`
— which the hooks daemon deploys when `ccy.deploy_supervisor: true`. It was
opt-in for one release, and that was wrong the moment 3.42.0 made it the only
ctrl+z guard: every project without a `ccy.env` was freezable by a keypress.

Precedence, highest first: a host `CCY_CLAUDE_WRAPPER` export or
`ccy --supervise` → the project's `ccy.env` → this default.

**The default is UNARMED**, and the split matters. The terminal-key guard is
pure protection and belongs everywhere. Automatic compaction changes what a
session *does* — it injects a real `/compact` — so it stays an explicit opt-in
(`--arm` in `ccy.env`, or `ccy --supervise`). Unarmed, the supervisor injects
one harmless visible marker per session instead.

**Two failure modes are surfaced, not swallowed:**

- A supervisor that does not parse **fails the launch** (`ast.parse` preflight,
  before `exec`) naming the file and the bypass. Running unwrapped instead would
  silently downgrade the only ctrl+z guard — the "skip and continue" this repo
  bans — and it is now the default path, so a corrupt file would otherwise take
  every session in every project down with it.
- An **absent** supervisor prints that ctrl+z is unguarded in this session.
  Absence is a normal state, but silence there reads as "protected" to anyone
  who does not know the patch was removed.

**Opting out**: `ccy --no-supervise` (or `CCY_NO_SUPERVISOR=1`) runs claude
unwrapped — no auto-compaction *and* no ctrl+z guard. The host-side
`stty susp undef` in `claude-yolo` still stops the *kernel* generating SIGTSTP,
but the `0x1a` byte reaches Claude Code, which suspends itself. So if you hit a
freeze: re-arm the supervisor — do not write a new patch.

**Files that changed when this was removed** (`ccy-ctrl-z-patch.js`,
`scripts/qa-ctrl-z-patch.bash` and `scripts/qa-ccy/` are deleted; the Dockerfile
build step, `CCY_DISABLE_SUSPEND`, the `.ctrlz-patch-status` sentinel warning in
`entrypoint.sh`, and the patch re-run in `update_claude_inplace()` are gone).
See `docs/ccy-changelog.md` 3.42.0 and `docs/ccy.md` for the user-facing view.
