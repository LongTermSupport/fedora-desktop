# Provision a Checkout

Build the daemon for a checkout of a project that already uses it, at exactly
the version the project names, or bring an existing clone to that version.

## When

A fresh clone carries the tracked hooks, `.claude/init.sh`, `.claude/settings.json`
and `.claude/hooks-daemon.yaml`, but not the daemon: `.claude/hooks-daemon/` is
gitignored and per-checkout. Until it is built, every safety handler is off.

The same command syncs a clone that is already there but at another version than
the project names: a pull changed `daemon.expected_version`, and the gitignored
clone did not follow.

This is not `install` (add the daemon to a project that does not use it) and not
`upgrade` (move the project itself to a new version, which also migrates config
and rewrites tracked files).

## Run

```claude-code
/hooks-daemon provision
```

It runs the project's own tracked `.claude/provision.sh`, which:

1. Resolves the expected version from `daemon.expected_version` in
   `.claude/hooks-daemon.yaml`, else from the `.claude/HOOKS-DAEMON.md` header.
   Unknown or not `X.Y.Z`: it stops and says so. It never guesses and never
   installs `main`.
2. Fetches that one tag into `.claude/hooks-daemon/` from the repository the
   installer trusts. The URL is fixed in the script; no environment variable or
   config value can change it.
3. Builds the venv through the clone's own venv build path and lock.
4. Starts the daemon, as a hook would.

It changes no tracked file, and needs no session restart: the hooks are already
registered in the tracked `.claude/settings.json` and reach the daemon on the
next tool call.

## Syncing a clone that is already there

A clone already at the expected version is left alone ("already at vX.Y.Z").
Otherwise, for a newer or an older expected version:

1. It refuses, changing nothing, when the clone is not a git checkout, its
   `origin` is not the fixed URL, or it has tracked changes or files outside
   `untracked/` (gitignored files do not count).
2. In a stage, `.claude/hooks-daemon/untracked/sync-stage`, it fetches exactly
   that tag (depth 1) from the fixed URL into a clone of its own, and checks
   that the tag's `version.py` names the version. Nothing else of the live
   clone, its git directory included, is written while the old daemon serves.
3. It runs the target release's own pre-deploy gate from the stage, the one
   `upgrade` runs. If the gate wants its reading list confirmed, it prints the
   listing and stops; read it, then re-run with
   `--skip-reading-confirmation=<digest>` (the digest it printed). A target that
   predates the gate has none to run.
4. It builds the target's venv in the stage with the target's own
   `scripts/venv_bootstrap.sh`, then rebinds it to the live path: the venv is
   renamed for the live path and every text file in it that names the stage
   (the editable `.pth`, `direct_url.json`, script shebangs, activate scripts,
   `.daemon-metadata.json`) names the live path instead. A failure up to here
   changes nothing and never stops the running daemon.
5. Then the switch. It stops the old daemon, moves the daemon's state under
   `untracked/` into the stage, and swaps the stage in by renames. Before the
   new daemon starts, the moved venv must pass init.sh's own pre-start check,
   resolve as the live venv and import the package from the live `src`; if it
   does not, it is rebuilt in place by the clone's own build script (the daemon
   stays down meanwhile) and the outcome says so. The old
   clone is kept at `untracked/sync-previous` until the new daemon, started with
   `bin/hooks-daemon restart`, answers; then it is removed. Until then, a hook
   that would start a daemon answers "the daemon is starting; retry". A failure
   during the switch swaps the old clone, its venv and the state back, starts
   the old daemon again, and says so. A switch a killed run left half-done is
   undone the same way by the next run, which says `RECOVERED`.

A sync writes no tracked file, runs no config migration and regenerates no docs.

## In the background, after a pull

You usually do not run the sync yourself. Every session start, the tracked
session-start hook starts `bash .claude/provision.sh --background` detached. It
does nothing unless the clone is OLDER than `daemon.expected_version`; then it
runs the steps above, and the SessionStart version check says so. The sync waits
for the running daemon, which keeps serving until the sync restarts it at the
new version. The output goes to
`.claude/hooks-daemon/untracked/background-sync/v<version>.log`.

- It runs at most once per target version: the outcome is recorded in
  `v<version>.state` beside the log, and a lock stops two syncs overlapping.
- With no daemon running from the clone it changes nothing and claims nothing,
  so a later session start tries again.
- It never downgrades and never provisions a fresh checkout. Both stay a
  deliberate run of the command above.
- A failure, or a stop for the reading list, leaves the previous version in
  place. Every later session start reports it with the reason, the log and
  `bash .claude/provision.sh` to run by hand. To have it retried in the
  background instead, delete the `v<version>.state` file it names.
- The daemon's own repository never syncs: it has no clone to sync.

## When it refuses

| It says                                                                | What to do                                                                           |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| the version is unknown                                                 | Set `daemon.expected_version`, commit it, run provision again                        |
| `daemon.expected_version` is not X.Y.Z                                 | Correct it (for example `3.68.0`: no `v`, no branch name)                            |
| the clone has local changes                                            | Commit, move or discard them yourself, run provision again                           |
| the clone's `origin` is not the fixed URL, or it is not a git checkout | Move `.claude/hooks-daemon/` aside and provision a fresh clone                       |
| the pre-deploy gate asks for reading                                   | Read the listing, re-run with the `--skip-reading-confirmation=<digest>`             |
| a clone exists but has no venv here                                    | `.claude/hooks-daemon/bin/hooks-daemon repair` keeps the clone, then provision again |
| the directory holds something else                                     | Inspect it; move it aside if disposable; run provision again                         |
| the tag cannot be fetched                                              | Check the version is a release and the machine can reach GitHub                      |
| the venv cannot be built                                               | Fix the named condition, then `.claude/hooks-daemon/bin/hooks-daemon repair`         |
