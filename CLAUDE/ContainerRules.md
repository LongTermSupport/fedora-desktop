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

## Known Fragile Patch: Ink ctrl+z SIGSTOP Suppression

**The container image patches Claude Code's `cli.js` to disable ctrl+z suspend.**

**Background:** Ink (Claude Code's terminal UI framework) has a hardcoded input handler checked BEFORE the keybinding system:

```js
// Inside Ink's raw input loop (minified, fG5 name may change between versions):
if (z.name === "z" && z.ctrl && fG5) { A.handleSuspend() }
// where: fG5 = process.platform !== "win32"
```

`handleSuspend()` calls `process.kill(pid, 'SIGSTOP')` — an unblockable signal. In a CCY container, this makes Claude unrecoverable (no shell to run `fg`). Setting `"ctrl+z": null` in `keybindings.json` does NOT fix this — the key is intercepted before keybindings are consulted.

**The patch** (applied after `npm install` via `ccy-ctrl-z-patch.js`):

```js
// Original (variable name is minified, changes between Claude Code versions):
fG5 = process.platform !== "win32"
// Patched to:
fG5 = process.platform !== "win32" && !process.env.CCY_DISABLE_SUSPEND
```

The entrypoint sets `CCY_DISABLE_SUSPEND=1` (this env gating only takes effect on the legacy `cli.js` path; native edits are unconditional — see below). The patch script tries its strategies in order and **soft-fails** (warns but does not break the build) if all of them fail.

### Two packaging formats: legacy `cli.js` and native binary

Claude Code ships in two shapes, and the patch script (`ccy-ctrl-z-patch.js`) handles both:

- **Legacy `cli.js`** (pre-2.1.x): plain JavaScript. The patch appends `&&!process.env.CCY_DISABLE_SUSPEND` to the platform guard (variable-length text replacement).
- **Native binary** (2.1.x+): an ELF with embedded JS (Node.js SEA) at `bin/claude.exe`. All native edits are **same-length byte replacements** (length mismatch is a hard error, never written). Two strategies in order:
  - **PRIMARY — no-op `handleSuspend()` itself.** Claude Code 2.1.198 removed the platform boolean and now gates suspend on a *shared* predicate call — `if(...&&ano()){e.handleSuspend();continue}`, where `ano()` returns false only for `CLAUDE_CODE_SESSION_KIND==="bg"` and is reused elsewhere for unrelated foreground-only behaviour. The *guard condition* churns release-to-release (that churn is what kept soft-failing), but the *method* is single-purpose (its only job is SIGSTOP) and has no other callers. So the patch replaces the method's first statement with an unconditional `return;` padded by a block comment to the same length (`handleSuspend=()=>{if(!this.isRawModeSupported())return;` → `handleSuspend=()=>{return;/*…*/`). We deliberately do **not** flip `ano()`'s body — that would disable unrelated foreground behaviour.
  - **LEGACY fallback** (pre-2.1.198): the platform check was constant-folded to `<var>=!0`; the patch flips it to `<var>=!1`. An unoptimised build is handled too (`!==` → `===`). Absent in newer builds, so it no-matches and we fall through.

The script auto-detects which artifact `npm install -g` produced (`cli.js` present → legacy path; else `bin/claude.exe` → native path) and patches accordingly. `CCY_PKG_DIR` / `CCY_CLI_PATH` override the lookup paths (used by the QA harness).

### Soft-fail is now surfaced at launch (CCY-07)

When the patch soft-fails it writes a sentinel `/opt/claude-yolo/.ctrlz-patch-status` (`failed`). `entrypoint.sh` reads it at container start and prints a one-line warning, so a "ctrl+z freezes the container" outcome is diagnosable instead of being a single buried build-log line. A successful patch writes no sentinel (absent = OK). Override the path with `CCY_PATCH_STATUS_PATH`.

**When Claude Code updates break the dynamic discovery too:**

- Build output will show: `CCY PATCH WARNING: ctrl+z patch target not found - skipping`, and the launch warning above will fire.
- **Legacy cli.js:** find the new minified variable name with
  `grep -o '.\{20\}platform.*win32.\{20\}' /usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js`, then add the pattern to the `knownPatterns` array.
- **Native binary:** inspect the method with
  `grep -ao 'handleSuspend=()=>{.\{0,120\}' /usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`. The PRIMARY patch keys on the method's opening statement (`handleSuspend=()=>{if(!this.isRawModeSupported())return;`); if that opening statement changed, add the new variant to the `firstStatements` list in `tryNoopHandleSuspend()`. Only fall back to the legacy platform-variable shape (`if(<var>){...handleSuspend();continue}`) if the method-noop anchor is truly gone.
- Bump container version (Dockerfile label + `REQUIRED_CONTAINER_VERSION` in claude-yolo).
- Verify against the latest release: `./scripts/qa-ctrl-z-patch.bash` (always reinstalls @latest and, for native builds, executes the patched binary to prove the same-length edit did not corrupt the JS blob).

**Files involved:**

- `files/var/local/claude-yolo/ccy-ctrl-z-patch.js` — the patch script (native primary = `handleSuspend()` no-op via `firstStatements`; legacy cli.js = `knownPatterns`; handles both artifacts; writes the soft-fail sentinel on failure and clears a stale one on success)
- `files/var/local/claude-yolo/Dockerfile` — COPYs the patch script to `/opt/claude-yolo/` (kept in the image, not `rm`'d) and RUNs it at build
- `files/var/local/claude-yolo/claude-yolo` — `update_claude_inplace()` re-runs the patch after the daily in-place `npm i -g …@latest`, so the auto-update to latest never ships an unpatched (freeze-prone) binary
- `files/var/local/claude-yolo/entrypoint.sh` — sets `CCY_DISABLE_SUSPEND=1`; warns at launch if the sentinel says `failed`
- `scripts/qa-ctrl-z-patch.bash` — artifact-aware QA gate; always reinstalls `@latest` and executes the patched native binary to prove the edit is valid
