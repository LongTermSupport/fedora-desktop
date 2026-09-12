# Worktree Workflow

> **This file is DAEMON-OWNED.** It is deployed to `CLAUDE/core/Worktree.core.md`
> and overwritten wholesale on every install and upgrade, so a local edit is
> discarded the next time the daemon deploys — never hand-edit it. Your own
> `CLAUDE/Worktree.md` is seeded once, is never touched again, and opens with a
> markdown link to this file. Put anything this project does differently there,
> not here.
>
> A plain markdown link, deliberately, not an `@`-import: `@` auto-resolves
> only inside a `CLAUDE.md` chain, and this document sits outside one, so it
> would not be a mechanism here — while eagerly re-inlining a large file into
> every session is a real cost. The forced read is a convention either way.
>
> Wrapper paths below are written as `.claude/hooks-daemon/bin/hooks-daemon` —
> the location for a standard install. A self-install checkout, where the
> daemon root IS the project root, uses `bin/hooks-daemon` at the project root
> instead; substitute accordingly.

Git worktree workflow for isolated, safe refactoring and development.

```bash
# ✅ CORRECT ORDER:
cd untracked/worktrees/worktree-my-feature
git merge main --no-edit              # 1. Sync worktree with main FIRST
# ... run this project's test/QA suite here ...
cd -
git merge worktree-my-feature         # 3. ONLY THEN merge to main

# ❌ WRONG - Will cause conflicts and lost work:
git merge worktree-my-feature         # DON'T DO THIS WITHOUT STEP 1!
```

## Overview

Git worktrees allow multiple working directories from a single repository,
enabling parallel development without branch-switching disruption. This is
**essential** for running parallel agents on different tasks.

The daemon protects this workflow with a BLOCKING rule (`worktree_file_copy`):
`cp`, `mv`, and `rsync` between a worktree and this project's main code
directories are denied outright, whichever direction they go. See
[Never Copy Files Between a Worktree and the Main Repo](#6-never-copy-files-between-a-worktree-and-the-main-repo-critical)
below — that rule is the reason this document exists, and it is not
negotiable.

### Two Ways a Worktree Gets Created

Two independent mechanisms produce a git worktree here, and the daemon treats
both the same way for isolation purposes:

- **Manual**, via `git worktree add` (or the setup steps in this document),
  living under `untracked/worktrees/<branch-name>/`. This is the workflow the
  rest of this document walks through: an agent or human explicitly creates,
  names, and manages the worktree, typically for a multi-task plan.
- **Automatic**, via Claude Code's own `isolation: "worktree"` agent option or
  a `--worktree` session. Claude Code hands this entirely to the daemon, which
  creates it at a human-friendly `.claude/worktrees/<slug-of-name>-<shorthash>/`
  path (the `worktree_create` handler; separate guidance for that mechanism is
  injected elsewhere in this project's CLAUDE.md when it is active).

`untracked/worktrees/` and `.claude/worktrees/` are both fixed daemon
conventions — neither is project-configurable — and the file-copy rule
guards both roots identically. Everything below about isolation, cleanup, and
never cross-copying applies no matter which mechanism created the worktree.

### Hierarchical Structure

Worktrees support **parent-child relationships** for complex plans:

- **Parent (Plan) Worktrees**: Top-level worktree for a plan
- **Child (Task) Worktrees**: Individual tasks within the plan

**Merge Rules:**

- ✅ **ALLOWED**: Child → Parent worktree (automatic, no approval needed)
- ❌ **NOT ALLOWED**: Parent → Main project (requires human approval)

## Critical Rules

### 1. Worktree Location

**Manually-created worktree folders MUST be in:**

```
./untracked/worktrees/<branch-name>/
```

✅ **Correct**: `./untracked/worktrees/worktree-my-feature/`
❌ **Wrong**: `../my-feature/`, `/tmp/worktree/`, etc.

**Why**:

- Keeps workspace organised
- Prevents git confusion
- Easy cleanup (just delete `untracked/worktrees/`)
- Excluded from main repo operations (`untracked/` is gitignored)

### 2. Branch Naming

**Parent (Plan) Worktrees:**

- Prefix: `worktree-`
- Format: `worktree-<name>`
- ✅ Examples: `worktree-auth-refactor`, `worktree-plan-00042`

**Child (Task) Worktrees:**

- Prefix: `worktree-child-`
- Format: `worktree-child-<parent-name>-<task-name>`
- ✅ Examples: `worktree-child-auth-refactor-handler-1`, `worktree-child-auth-refactor-config-fix`

❌ **Wrong**: `auth-refactor`, `feature/headers`, `temp-work`, `child-handler-1`

**Why**:

- Clear identification of worktree hierarchy
- Easy filtering in `git branch` output
- Shows parent-child relationships
- Prevents accidental merges to main
- Signals temporary nature

### 3. Daemon Installation Per Worktree (CRITICAL)

**A fresh worktree does not have the daemon installed, and nothing about
`git worktree add` gives it one.** `.claude/hooks-daemon/` (the daemon's own
clone, code, and virtual environment) and everything under `untracked/` are
gitignored — deliberately, so they are never committed and never shared
between checkouts. `git worktree add` only populates TRACKED files, so a new
worktree starts with the project's committed `.claude/hooks-daemon.yaml` and
hook forwarders, but no `.claude/hooks-daemon/` directory at all. Until that
is fixed, the worktree's own `bin/hooks-daemon` wrapper does not exist, and
none of this project's hooks fire inside it.

**Fix it by reinstalling, not by copying.** Re-run this project's daemon
installer (or upgrader) from inside the new worktree, the same way it was
first set up in the main checkout. This provisions a fresh,
`untracked/hooks-daemon/`-scoped copy of the daemon with its own isolated,
fingerprint-keyed virtual environment
(`venv-{slug}-py{MM}-{fingerprint}/`, unique to that worktree's own
interpreter and paths) and its own `bin/hooks-daemon` wrapper.

**Never hand-build the venv, and never copy one in from elsewhere.** A
hand-made `venv/` is the retired pre-fingerprint layout and the daemon's venv
resolver refuses it — every wrapper call then exits with a diagnostic telling
you to reinstall. `bin/hooks-daemon list-venvs` shows what a worktree
actually has, once the wrapper exists to run it.

**Why each worktree needs its own**: the venv is built against that
worktree's own paths and interpreter. Pointing a worktree at another
worktree's (or the main checkout's) daemon install means its hooks would
observe and act on the WRONG tree — silently, since nothing fails loudly
when a hook fires against the wrong `cwd`.

**You never name the interpreter.** Each worktree's own `bin/hooks-daemon`
anchors to its own location and resolves that worktree's venv itself.

### 4. Daemon Process Isolation (CRITICAL)

**Each worktree gets its own daemon process with isolated socket/PID/log files.**

#### How It Works

The daemon CLI discovers the project root by walking up the directory tree to
find `.claude/`. In a worktree, it finds the worktree's own `.claude/`
directory (tracked by git), so the daemon naturally resolves paths relative
to the worktree root:

```
Main checkout daemon:
  Socket: .claude/hooks-daemon/untracked/daemon-{hostname}.sock
  PID:    .claude/hooks-daemon/untracked/daemon-{hostname}.pid
  Log:    .claude/hooks-daemon/untracked/daemon-{hostname}.log

Worktree daemon (isolated automatically):
  Socket: {worktree}/.claude/hooks-daemon/untracked/daemon-{hostname}.sock
  PID:    {worktree}/.claude/hooks-daemon/untracked/daemon-{hostname}.pid
  Log:    {worktree}/.claude/hooks-daemon/untracked/daemon-{hostname}.log
```

No collision occurs because each worktree has a different absolute path for
`.claude/hooks-daemon/untracked/`.

#### Starting a Daemon in a Worktree

```bash
cd untracked/worktrees/worktree-my-feature

# Daemon automatically uses the worktree's own .claude/ for socket/PID
.claude/hooks-daemon/bin/hooks-daemon start
.claude/hooks-daemon/bin/hooks-daemon status
# Expected: Status: RUNNING (with worktree-local socket)
```

#### Explicit Path Overrides (Optional)

For additional control, use CLI flags or env vars to override automatic
resolution.

**Path Resolution Precedence**: CLI flags > Environment variables > Auto-discovery

##### CLI Flags

The most explicit way to control daemon paths is with CLI flags:

```bash
cd untracked/worktrees/worktree-my-feature

# Start daemon with explicit paths
.claude/hooks-daemon/bin/hooks-daemon \
  --pid-file .claude/hooks-daemon/untracked/daemon-wt.pid \
  --socket .claude/hooks-daemon/untracked/daemon-wt.sock \
  start

# All commands support these flags
.claude/hooks-daemon/bin/hooks-daemon \
  --pid-file .claude/hooks-daemon/untracked/daemon-wt.pid \
  --socket .claude/hooks-daemon/untracked/daemon-wt.sock \
  status
```

**Supported CLI Flags**:

- `--pid-file PATH`: Explicit PID file path (overrides env vars and auto-discovery)
- `--socket PATH`: Explicit socket path (overrides env vars and auto-discovery)

**When to use CLI flags**:

- Testing specific path configurations
- Debugging daemon isolation issues
- Temporary path overrides without modifying environment
- Scripted daemon management with custom paths

##### Environment Variables

Alternatively, use env vars to override automatic resolution:

```bash
# Force specific paths (usually not needed - automatic isolation works)
export CLAUDE_HOOKS_SOCKET_PATH={worktree}/.claude/hooks-daemon/untracked/daemon-wt.sock
export CLAUDE_HOOKS_PID_PATH={worktree}/.claude/hooks-daemon/untracked/daemon-wt.pid
export CLAUDE_HOOKS_LOG_PATH={worktree}/.claude/hooks-daemon/untracked/daemon-wt.log
```

**Note**: CLI flags take precedence over environment variables if both are specified.

#### Stopping a Worktree Daemon (MANDATORY Before Cleanup)

**You MUST stop the worktree's daemon before removing the worktree.** Failing
to do this leaves orphaned processes with stale PID files and dangling
sockets.

```bash
# ALWAYS stop daemon BEFORE removing worktree
WT=untracked/worktrees/worktree-my-feature
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop

# NOW safe to remove worktree
git worktree remove "$WT"
git branch -d worktree-my-feature
```

#### Orphaned Daemon Recovery

If a worktree was removed without stopping its daemon:

```bash
# Find orphaned daemon processes
ps aux | grep claude_code_hooks_daemon | grep -v grep

# Kill by PID (check it's the right process first)
kill <PID>

# Clean up stale socket if it exists
rm -f /path/to/.claude/hooks-daemon/untracked/daemon-*.sock
```

### 5. Agent Containment

**Agents working in worktrees MUST stay in their worktree**

When launching sub-agents for worktree tasks:

- Set working directory explicitly
- Verify agent is in correct worktree
- Never `cd` back to the main checkout
- All file operations relative to worktree root
- Use the worktree's own `.claude/hooks-daemon/bin/hooks-daemon`, not the
  main checkout's

**Example agent prompt:**

```
You are working in a git worktree at <project-root>/untracked/worktrees/worktree-my-feature/
DO NOT work in the main checkout - only work in YOUR worktree directory.
All file paths should be relative to that worktree root.
Run the daemon CLI as .claude/hooks-daemon/bin/hooks-daemon from that worktree
— it resolves that worktree's own venv, so you never name an interpreter.
```

### 6. Never Copy Files Between a Worktree and the Main Repo (CRITICAL)

**`cp`, `mv`, and `rsync` between a worktree and this project's main code
directories are BLOCKED outright** — whichever direction they go, and this is
enforced by the daemon, not just a convention.

🔥 **Why this is catastrophic:**

1. Defeats the entire purpose of worktrees (isolation)
2. Destroys branch isolation
3. Loses git history (bypasses git tracking entirely)
4. Can silently nuke untracked work already sitting in the target directory
5. Creates merge conflicts that a normal `git merge` would have caught cleanly

✅ **The correct workflow is always:**

```bash
cd untracked/worktrees/your-branch
git add . && git commit -m 'feat: changes'
cd -                          # back to the main checkout
git merge your-branch
```

**cd into the worktree, commit your changes there, then `git merge` back —
never copy the files across.** Operations that stay entirely within one
worktree branch are fine; it is crossing the worktree/main-repo boundary with
a file copy that is denied.

The main code directories this rule protects are this project's declared
`layout.source_dirs`, `layout.test_dirs`, and `layout.config_dirs`
(`.claude/hooks-daemon.yaml`) — by default `src/`, `tests/` (and its common
aliases `test/`, `__tests__/`, `spec/`), and `config/`.

### 7. Merge Protocol

**Two types of merges with different rules:**

#### Child → Parent Worktree (ALLOWED)

✅ **Can merge automatically** - no human approval needed

```bash
# From parent worktree
cd untracked/worktrees/worktree-plan
git merge worktree-child-plan-handler-1
```

**Why allowed:**

- Isolated to plan worktree
- Doesn't affect the main project
- Part of plan execution workflow
- Easy to rollback if needed

#### Parent → Main Project (REQUIRES APPROVAL)

❌ **MUST ask human approval first**

Before merging parent to main:

1. ✋ **STOP** - Ask human for approval
2. Verify no other agents working in the main checkout
3. Verify no conflicts with the main branch
4. Get explicit "yes" from human
5. Only then proceed with merge

**Why requires approval:**

- Multiple agents may be working simultaneously
- The main checkout might have uncommitted changes
- Conflicts need human resolution
- Risk of losing work

### 8. Cleanup Protocol

**On merge completion, the daemon MUST be stopped, then worktree branches and
folders cleaned up:**

```bash
# After child merges to parent:
# 1. Stop child's daemon
WT=untracked/worktrees/worktree-child-plan-handler-1
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true

# 2. Remove worktree and branch
git worktree remove "$WT"
git branch -d worktree-child-plan-handler-1

# After parent merges to main:
# 1. Stop parent's daemon
WT=untracked/worktrees/worktree-plan
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true

# 2. Remove worktree and branch
git worktree remove "$WT"
git branch -d worktree-plan
```

**Why mandatory:**

- **Daemon stop prevents orphaned processes** with stale PID files and dangling sockets
- Prevents worktree clutter
- Reduces confusion about active work
- Frees disk space
- Keeps git branch list clean

## Standard Worktree Workflow

### Creating a Parent (Plan) Worktree

```bash
# 1. Ensure untracked/worktrees directory exists
mkdir -p untracked/worktrees

# 2. Create parent worktree from main branch
git worktree add untracked/worktrees/worktree-plan -b worktree-plan

# 3. Provision the daemon inside it (see Critical Rule 3)
cd untracked/worktrees/worktree-plan
# ... re-run this project's daemon installer here ...
.claude/hooks-daemon/bin/hooks-daemon status

# 4. Verify creation
cd -
git worktree list
```

### Creating a Child (Task) Worktree

```bash
# 1. Create child from the PARENT worktree branch (not main!)
git worktree add untracked/worktrees/worktree-child-plan-handler-1 \
  -b worktree-child-plan-handler-1 worktree-plan

# 2. Provision the daemon inside it
cd untracked/worktrees/worktree-child-plan-handler-1
# ... re-run this project's daemon installer here ...
cd -

# 3. Verify it's based on the parent
git worktree list
```

**Note**: Child worktree is branched FROM the parent worktree branch, not
from main.

### Working in a Worktree

```bash
# Navigate to worktree
cd untracked/worktrees/worktree-plan

# Work normally - commits, edits, tests
git status
# ... do your work ...

# Run this project's test/QA suite within the worktree

# Verify the daemon loads with your changes
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status

git add <specific-files>
git commit -m "Implement handler"

# Return to the main checkout
cd -
```

### Merging Child → Parent Worktree

```bash
# From the parent worktree directory
cd untracked/worktrees/worktree-plan

# 1. Review child changes
git log worktree-child-plan-handler-1

# 2. Merge child into parent (no approval needed)
git merge worktree-child-plan-handler-1

# 3. Stop child's daemon process
CHILD_WT=untracked/worktrees/worktree-child-plan-handler-1
"$CHILD_WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true

# 4. Immediately cleanup child
cd -
git worktree remove "$CHILD_WT"
git branch -d worktree-child-plan-handler-1
```

### Merging Parent → Main Project

**CRITICAL**: This is a multi-step process that requires careful verification
at each stage.

⚠️ **MERGE ORDER IS CRITICAL** ⚠️
**ALWAYS merge main → worktree FIRST, then worktree → main**
**NEVER merge worktree → main directly!**

```bash
# ===================================================================
# STEP 1: ALWAYS MERGE MAIN INTO WORKTREE FIRST!
# ===================================================================
# This is THE MOST IMPORTANT STEP - sync worktree with main BEFORE merging back
# Prevents conflicts and ensures the worktree has all latest changes from main
cd untracked/worktrees/worktree-plan
git fetch origin
git merge main --no-edit
# ⚠️ If there are conflicts, resolve them HERE in the worktree
# ⚠️ Test thoroughly after merge - the worktree must pass all QA
# ... run this project's test/QA suite ...
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status
# Expected: Status: RUNNING

# STEP 2: Verify the main checkout is clean
cd -
git status  # MUST show "nothing to commit, working tree clean"

# ✋ STOP - If the main checkout has uncommitted changes:
#   - Commit them first OR
#   - Set them aside some other way
#   - DO NOT proceed until main is clean

# STEP 3: ✋ STOP - Ask human for final approval!
# Confirm with human:
#   - Is main branch clean? (no uncommitted changes)
#   - Are all other agents/processes stopped?
#   - Is it safe to merge now?

# STEP 4: Review parent worktree changes
git log worktree-plan --oneline

# STEP 5: Merge parent to main (only after ALL approvals above!)
git merge worktree-plan --no-edit

# STEP 6: Verify merge succeeded
git status  # Should show clean state
# ... run this project's test/QA suite ...
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status
# Expected: Status: RUNNING

# STEP 7: Push to origin
git push

# STEP 8: Stop worktree daemon BEFORE removing worktree
WT=untracked/worktrees/worktree-plan
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true

# STEP 9: ONLY NOW cleanup parent worktree
# (Not before merge push - we need it in case merge fails)
git worktree remove "$WT"
git branch -D worktree-plan

# STEP 10: Final verification
git status  # Confirm everything clean
```

**Why this order matters:**

1. **ALWAYS sync worktree first** (`main → worktree`):
   - Prevents conflicts by updating the worktree with main's latest changes
   - Lets you resolve conflicts IN THE WORKTREE (isolated, safe)
   - Ensures your changes work with the current state of main
   - **If you skip this, the merge WILL conflict and you WILL lose work**
2. **Clean main checkout**: Uncommitted changes in main cause merge failures
3. **Human verification**: Ensures no other work is in progress
4. **Keep worktree until success**: Don't delete until merge is confirmed working
5. **Cleanup last**: Only remove the worktree after everything pushed successfully

**Remember**: The worktree is your isolated workspace. ALWAYS bring main's
changes INTO your workspace BEFORE you merge your workspace back to main!

### Cleaning Up

Cleanup is **mandatory and immediate** after merging. **Always stop the
daemon first.**

```bash
# After merging child to parent:
CHILD_WT=untracked/worktrees/worktree-child-plan-handler-1
"$CHILD_WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true
git worktree remove "$CHILD_WT"
git branch -d worktree-child-plan-handler-1

# After merging parent to main:
WT=untracked/worktrees/worktree-plan
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true
git worktree remove "$WT"
git branch -d worktree-plan
```

**Never leave merged worktrees around** - cleanup prevents confusion,
orphaned daemons, and keeps the workspace tidy.

## Parallel Agent Strategy

### Hierarchical Approach: A Plan With Several Tasks In Parallel

**Step 1: Create Parent (Plan) Worktree**

```bash
# Main orchestrator creates the parent worktree for the plan
git worktree add untracked/worktrees/worktree-plan -b worktree-plan
# ... provision the daemon inside it (Critical Rule 3) ...
```

**Step 2: Wave 1 - Create Task Worktrees (several child agents in parallel)**

```bash
# Create child worktrees from the parent — each provisioned with its own daemon
for task in handler-a handler-b handler-c handler-d; do
  git worktree add "untracked/worktrees/worktree-child-plan-${task}" \
    -b "worktree-child-plan-${task}" worktree-plan
  # ... provision the daemon inside each ...
done

# Launch agents, each in their own child worktree
Agent 1 → handler_a.py in worktree-child-plan-handler-a
Agent 2 → handler_b.py in worktree-child-plan-handler-b
Agent 3 → handler_c.py in worktree-child-plan-handler-c
Agent 4 → handler_d.py in worktree-child-plan-handler-d
```

**Step 3: Merge Children into Parent (sequential, in the parent worktree)**

```bash
# From the parent worktree, merge each child
cd untracked/worktrees/worktree-plan

git merge worktree-child-plan-handler-a
WT=untracked/worktrees/worktree-child-plan-handler-a
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true
cd -
git worktree remove "$WT"
git branch -d worktree-child-plan-handler-a

cd untracked/worktrees/worktree-plan
git merge worktree-child-plan-handler-b
# ... repeat for other children
```

**Step 4: Run Full Verification in the Parent Worktree**

```bash
cd untracked/worktrees/worktree-plan
# ... run this project's test/QA suite ...
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status
# Expected: Status: RUNNING
```

**Step 5: Merge Parent into Main (REQUIRES APPROVAL)**

```bash
# ✋ STOP - Ask human for approval!
cd untracked/worktrees/worktree-plan
git merge main --no-edit
# ... run this project's test/QA suite ...

cd -
git merge worktree-plan
git push

WT=untracked/worktrees/worktree-plan
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop || true
git worktree remove "$WT"
git branch -d worktree-plan
```

### Benefits of the Hierarchical Approach

1. **All plan work isolated**: Parent worktree contains the entire plan
2. **Clean main checkout**: Main repo unaffected until final approval
3. **Easy rollback**: Can abandon the entire plan without affecting main
4. **Parallel within a plan**: Multiple agents work on tasks simultaneously
5. **Sequential integration**: Tasks merge to parent, then parent merges to main
6. **Clear hierarchy**: Easy to see which tasks belong to which plan

## Agent Team Integration

Worktrees are designed to work with Claude Code's **agent team mode**
(`TeamCreate` / `SendMessage` / `Task` with `team_name`). The team lead
orchestrates work from the main checkout while teammates operate in isolated
worktrees.

### Architecture

```
Main checkout (project root) ← Team Lead (orchestrator)
    │
    ├── worktree-plan/ ← Integration worktree (merges happen here)
    │
    ├── worktree-child-plan-handler-a/ ← Teammate "handler-a-dev"
    ├── worktree-child-plan-handler-b/ ← Teammate "handler-b-dev"
    └── worktree-child-plan-handler-c/ ← Teammate "handler-c-dev"
```

### Team Lead Workflow

The team lead operates from the **main checkout** and coordinates all
worktree creation, merging, and cleanup.

1. Create the team and its tasks.
2. Create the parent worktree, then a child worktree per task — provisioning
   the daemon in each (Critical Rule 3).
3. Spawn one teammate per child worktree, each told explicitly to stay in
   their own worktree and to use that worktree's own
   `.claude/hooks-daemon/bin/hooks-daemon`.
4. As each teammate completes: stop its daemon, merge child → parent, remove
   the child worktree and branch, then shut the teammate down.
5. Once all children are merged: run full verification in the parent, sync
   with main, ask the human for merge approval, merge parent → main, stop the
   parent's daemon, remove the parent worktree.

### Teammate Responsibilities

Each teammate MUST:

- **Stay in their worktree** - never `cd` back to the main checkout
- **Use their own `.claude/hooks-daemon/bin/hooks-daemon`** - it anchors to
  its own location, so it automatically uses the worktree-local daemon and
  venv
- **Run this project's test/QA suite before committing**
- **Verify the daemon loads** - `.claude/hooks-daemon/bin/hooks-daemon restart`
- **Communicate via `SendMessage`** - report completion, ask questions
- **Respond to shutdown requests** promptly

### Shutdown Sequence (CRITICAL)

The shutdown order matters to avoid orphaned processes:

```
1. Request each teammate to stop
   ↓ (wait for acknowledgement from each)
2. Stop each child worktree's daemon
   ↓
3. Merge children → parent (if not already done)
   ↓
4. Remove child worktrees and branches
   ↓
5. Run verification in parent, sync with main, merge (with approval)
   ↓
6. Stop the parent worktree's daemon
   ↓
7. Remove the parent worktree and branch
```

**If a teammate is unresponsive:**

```bash
# Find its daemon PID
cat {worktree}/.claude/hooks-daemon/untracked/daemon-*.pid

# Kill the daemon
kill <PID>

# Force-remove the worktree
git worktree remove --force untracked/worktrees/worktree-child-plan-stuck
git branch -D worktree-child-plan-stuck
```

### Avoiding Conflicts Between Teammates

- **Each teammate gets a separate worktree** - no shared files
- **Teammates should NOT modify the same files** - plan tasks to avoid overlap
- **If overlap is unavoidable**, merge children sequentially into parent and
  resolve conflicts there
- **Shared config** (e.g. `.claude/hooks-daemon.yaml`): only the team lead
  should modify it; teammates should not change it in their own worktrees

## Concurrent Verification Across Worktrees

Each worktree's daemon is isolated by design (its own socket, PID file, and
log — see Critical Rule 4), so running one worktree's daemon does not collide
with another's. If this project's own test/QA suite additionally starts
shared services, binds fixed ports, or writes to a shared cache (a type
checker cache, for instance), running that suite in several worktrees at once
can still collide on THOSE resources — that is a property of the suite, not
of the daemon. When in doubt, run one worktree's verification at a time, or
confirm your suite uses per-run-unique paths/ports before running several
concurrently.

## Common Pitfalls

### ❌ Working in Wrong Directory

```bash
# Agent creates a file in the main checkout instead of the worktree
cd /project/root  # WRONG
touch src/new_module.py  # WRONG LOCATION
```

✅ **Solution**: Always verify `pwd` before file operations

### ❌ Running the Main Checkout's Wrapper While Working in a Worktree

```bash
cd untracked/worktrees/worktree-plan
/project/root/.claude/hooks-daemon/bin/hooks-daemon restart  # WRONG — acts on the main checkout's daemon
```

The wrapper anchors to its OWN location, so an absolute path to the main
checkout's `bin/hooks-daemon` always resolves the main checkout's venv AND
daemon — no matter where you are. Being inside the worktree does not
redirect it.

✅ **Solution**: use the worktree's own wrapper. Its location selects both
the venv and the daemon, so an absolute path to it works just as well as a
relative one:

```bash
# From inside the worktree
cd untracked/worktrees/worktree-plan
.claude/hooks-daemon/bin/hooks-daemon restart

# Or from anywhere — the wrapper's location is what counts
/project/root/untracked/worktrees/worktree-plan/.claude/hooks-daemon/bin/hooks-daemon restart
```

### ❌ Creating a Worktree Without Provisioning the Daemon

```bash
# No daemon is installed, so no hooks fire and QA gates never run
git worktree add untracked/worktrees/worktree-plan -b worktree-plan
cd untracked/worktrees/worktree-plan
.claude/hooks-daemon/bin/hooks-daemon status  # No such file!
```

✅ **Solution**: always (re)install the daemon inside a new worktree
immediately after creating it (Critical Rule 3).

### ❌ Branch Name Confusion

```bash
# Creating a branch without the worktree- prefix
git worktree add untracked/worktrees/my-plan -b my-plan  # WRONG
```

✅ **Solution**: Always use the `worktree-` prefix

### ❌ Merging Without Approval

```bash
# Agent automatically merges after completing a task
git merge worktree-plan  # WRONG - no human approval
```

✅ **Solution**: Always ask a human before merging parent to main

### ❌ Skipping Daemon Restart Verification

```bash
# Merging without verifying the daemon loads
git merge worktree-plan  # Merged code with import errors!
```

✅ **Solution**: Always verify the daemon starts in the worktree before merging:

```bash
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status
# Expected: Status: RUNNING
```

### ❌ Removing a Worktree Without Stopping Its Daemon

```bash
git worktree remove untracked/worktrees/worktree-plan  # Orphaned daemon process!
```

✅ **Solution**: Always stop the daemon first:

```bash
WT=untracked/worktrees/worktree-plan
"$WT/.claude/hooks-daemon/bin/hooks-daemon" stop
# THEN remove worktree
```

### ❌ Forgetting Cleanup

```bash
$ git worktree list
/project/root                              abc1234 [main]
/project/root/untracked/worktrees/old-1    def5678 [worktree-old-1]
/project/root/untracked/worktrees/old-2    ghi9012 [worktree-old-2]
```

✅ **Solution**: Clean up immediately after merging (stop the daemon first)

## Benefits

1. **Parallel Work**: Multiple agents working simultaneously
2. **Isolation**: Changes don't interfere with each other
3. **Safety**: The main checkout remains stable
4. **Speed**: No branch switching overhead
5. **Clarity**: Clear separation of tasks

## When to Use Worktrees

✅ **Use worktrees for:**

- Multi-task plans with parallel phases
- Independent pieces of work that can proceed simultaneously
- Refactoring multiple modules simultaneously
- Any work requiring 2+ parallel agents

❌ **Don't use worktrees for:**

- Single-file edits
- Quick fixes
- Sequential work where context matters
- Exploratory work (use the main checkout)

## Directory Structure

```
<project-root>/
├── .git/                                           # Main git directory
├── .claude/
│   ├── hooks-daemon.yaml                           # Config (tracked)
│   └── hooks-daemon/                                # Daemon clone (gitignored)
│       └── untracked/
│           ├── venv-{slug}-py{MM}-{fingerprint}/   # This checkout's own venv
│           ├── daemon-{hostname}.sock              # Main checkout daemon socket
│           ├── daemon-{hostname}.pid               # Main checkout daemon PID
│           └── daemon-{hostname}.log               # Main checkout daemon log
├── untracked/                                      # Not tracked by git
│   └── worktrees/                                  # All manually-created worktrees here
│       ├── worktree-plan/                          # Parent (Plan) worktree
│       │   ├── .claude/
│       │   │   └── hooks-daemon/                    # Worktree's own daemon clone + venv
│       │   │       └── untracked/
│       │   │           ├── daemon-{hostname}.sock  # Worktree's own daemon socket
│       │   │           ├── daemon-{hostname}.pid   # Worktree's own daemon PID
│       │   │           └── daemon-{hostname}.log   # Worktree's own daemon log
│       │   └── ... (full copy of the repo)
│       ├── worktree-child-plan-handler-a/          # Child (Task) worktree
│       │   ├── .claude/hooks-daemon/               # Child's own daemon clone + venv
│       │   └── ... (full copy, branched from parent)
│       └── worktree-child-plan-handler-b/          # Child (Task) worktree
│           ├── .claude/hooks-daemon/               # Child's own daemon clone + venv
│           └── ... (full copy, branched from parent)
├── (this project's declared source_dirs, e.g. src/)
├── (this project's declared test_dirs, e.g. tests/)
└── ...
```

**Hierarchy:**

- Main project (project root) ← Parent worktrees merge here (with approval)
- Parent worktrees (`worktree-<plan>`) ← Child worktrees merge here (automatic)
- Child worktrees (`worktree-child-*`) ← Individual tasks worked on here

## Verification Checklist

### Before Creating a Parent Worktree:

- [ ] Directory will be `untracked/worktrees/worktree-<name>`
- [ ] Branch name starts with `worktree-` (not `worktree-child-`)
- [ ] Branching from main

### Before Creating a Child Worktree:

- [ ] Directory will be `untracked/worktrees/worktree-child-<parent>-<task>`
- [ ] Branch name starts with `worktree-child-`
- [ ] Branch name includes the parent's name
- [ ] Branching from the PARENT worktree branch (not main!)
- [ ] Agent knows to stay in their child worktree

### After Creating Any Worktree:

- [ ] Daemon (re)installed inside it, with its own fingerprint-keyed venv
- [ ] `.claude/hooks-daemon/bin/hooks-daemon status` reports RUNNING
- [ ] This project's test/QA suite runs cleanly from inside it

### Before Merging Child → Parent:

- [ ] Working in the parent worktree directory
- [ ] Reviewed child changes
- [ ] No conflicts expected
- [ ] Child teammate has been shut down (if agent team mode)
- [ ] Ready to stop the child's daemon and cleanup immediately after merge

### Before Merging Parent → Main:

- [ ] ✋ **STEP 1**: ⚠️ **MERGED MAIN INTO WORKTREE FIRST** ⚠️ (`cd worktree && git merge main`)
- [ ] ✋ **STEP 2**: Resolved any conflicts in the parent worktree (NOT in main!)
- [ ] ✋ **STEP 3**: This project's test/QA suite passes in the worktree
- [ ] ✋ **STEP 4**: Daemon restarts successfully in the worktree (`restart && status`)
- [ ] ✋ **STEP 5**: Verified the main checkout is clean (`git status` shows clean)
- [ ] ✋ **STEP 6**: Committed or otherwise set aside any uncommitted changes in main
- [ ] ✋ **STEP 7**: Asked human for final approval
- [ ] ✋ **STEP 8**: Got explicit "yes" from human
- [ ] ✋ **STEP 9**: Confirmed no other agents/processes working in the main checkout
- [ ] ✋ **STEP 10**: Reviewed changes one last time (`git log worktree-<name> --oneline`)

**REMINDER**: The merge order is ALWAYS: `main → worktree` FIRST, then `worktree → main`

### After Merging Child → Parent:

- [ ] Stopped the child worktree's daemon
- [ ] Removed the child worktree folder immediately
- [ ] Deleted the child branch immediately
- [ ] Verified the parent worktree still works

### After Merging Parent → Main:

- [ ] ✋ **STEP 11**: Verified the merge succeeded (`git status` shows clean)
- [ ] ✋ **STEP 12**: This project's test/QA suite passes
- [ ] ✋ **STEP 13**: Daemon restarts successfully (`restart && status`)
- [ ] ✋ **STEP 14**: Pushed to origin successfully (`git push`)
- [ ] ✋ **STEP 15**: Stopped the parent worktree's daemon
- [ ] ✋ **STEP 16**: ONLY NOW remove the parent worktree folder
- [ ] ✋ **STEP 17**: ONLY NOW delete the parent branch
- [ ] ✋ **STEP 18**: Final verification (`git status` clean)
- [ ] ✋ **STEP 19**: If this project uses the plan workflow, updated plan status to completed
- [ ] ✋ **STEP 20**: If agent team mode, cleaned up team resources

**CRITICAL**: Never remove a worktree/branch before the merge is pushed
successfully!

## Troubleshooting

### "Fatal: invalid reference: worktree-plan"

**Cause**: Branch doesn't exist yet
**Solution**: Use `-b` flag when creating the worktree

### "Lock file exists"

**Cause**: Previous worktree operation was interrupted
**Solution**: `git worktree prune` to clean up

### "Already exists"

**Cause**: Worktree folder wasn't properly removed
**Solution**: Manual cleanup:

```bash
rm -rf untracked/worktrees/worktree-plan
git worktree prune
```

### The Daemon Won't Start Inside a Worktree

**Cause**: The worktree has no daemon installed at all (nothing carries it
across `git worktree add` — see Critical Rule 3), or it has a hand-made
`venv/` that the fingerprint-keyed resolver refuses.

**Solution**: (re)install the daemon inside the worktree — the same
installer/upgrader used for the main checkout:

```bash
cd untracked/worktrees/worktree-plan
# ... re-run this project's daemon installer here ...
.claude/hooks-daemon/bin/hooks-daemon status
```

Then run the daemon CLI as `.claude/hooks-daemon/bin/hooks-daemon` from
inside the worktree — it resolves that worktree's own venv itself, so there
is no interpreter to name.

### Orphaned Daemon After Worktree Removal

**Cause**: Worktree removed without stopping its daemon first
**Symptoms**: `ps aux | grep claude_code_hooks_daemon` shows a process for a
deleted worktree
**Solution**:

```bash
# Find the PID from the stale PID file (if the worktree still partially exists)
# Or find it from the process list
ps aux | grep claude_code_hooks_daemon | grep -v grep

# Kill the orphaned process
kill <PID>

# Clean up any stale socket files
rm -f /path/to/.claude/hooks-daemon/untracked/daemon-*.sock
```

### Agent Working in the Wrong Place

**Symptoms**: Files appearing in the main checkout instead of the worktree
**Solution**:

1. Stop the agent immediately
2. Verify the agent's working directory
3. Move files to the correct worktree — using `git`, never a raw `cp`/`mv`
   across the worktree boundary (see Critical Rule 6)
4. Remind the agent of the worktree location

### Child Worktree Created from Main Instead of Parent

**Symptoms**: Child worktree doesn't have the parent's changes
**Solution**:

1. Remove the incorrect child worktree
2. Recreate the child from the parent branch:
   ```bash
   git worktree remove untracked/worktrees/worktree-child-plan-task
   git branch -D worktree-child-plan-task
   git worktree add untracked/worktrees/worktree-child-plan-task \
     -b worktree-child-plan-task worktree-plan
   ```

### Trying to Merge Parent to Main Without Approval

**Symptoms**: Agent attempts `git merge worktree-plan` from the main checkout
**Solution**:

1. Stop immediately
2. Undo the merge if it happened: `git merge --abort`
3. Ask the human for approval
4. Only proceed after an explicit "yes"

## Quick Reference

### Worktree Hierarchy

```
Main Project (project root)
    ↑
    │ (merge with human approval)
    │
Parent Worktree (worktree-plan)
    ↑
    │ (merge automatically)
    │
Child Worktrees (worktree-child-plan-*)
```

### Key Rules Summary

| Action                                                   | Approval Required        | Cleanup               |
| -------------------------------------------------------- | ------------------------ | --------------------- |
| Create parent worktree                                   | No                       | After merge to main   |
| Create child worktree                                    | No                       | After merge to parent |
| Merge child → parent                                     | **NO**                   | Immediate             |
| Merge parent → main                                      | **YES**                  | Immediate             |
| `cp`/`mv`/`rsync` across the worktree/main-repo boundary | N/A — **always blocked** | N/A                   |

### Naming Cheat Sheet

```bash
# Parent (Plan) Worktree
worktree-auth-refactor
worktree-plan-00042

# Child (Task) Worktree - must include the parent name!
worktree-child-auth-refactor-handler-a
worktree-child-auth-refactor-config-fix
```

## References

- Git worktree docs: `git help worktree`
- The rule this document exists to satisfy: the `worktree_file_copy` handler
  (`cp`/`mv`/`rsync` between a worktree and the main repo)
- This project's own layout configuration: `layout.source_dirs` /
  `layout.test_dirs` / `layout.config_dirs` in `.claude/hooks-daemon.yaml`
- Plan workflow (if this project uses it): `CLAUDE/core/PlanWorkflow.core.md`,
  reached via `CLAUDE/PlanWorkflow.md`

---

**Remember**:

- Worktrees are for **parallel execution** on complex plans
- **Parent worktrees** isolate entire plans from the main project
- **Child worktrees** allow parallel work within a plan
- **Every worktree needs its own daemon installation**, with its own venv
- **Every worktree gets its own daemon process** (socket/PID/log isolated automatically)
- **Always stop the daemon before removing a worktree** (prevents orphaned processes)
- **Always verify the daemon restarts** before merging
- **Always cleanup** immediately after merging
- **Always ask a human** before merging parent to main
- **Never `cp`/`mv`/`rsync` between a worktree and the main repo** — `cd` in, commit, `git merge` back
