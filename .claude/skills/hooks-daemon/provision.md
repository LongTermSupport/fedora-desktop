# Provision a Fresh Checkout

Build the daemon for a checkout of a project that already uses it, at exactly
the version the project names.

## When

A fresh clone carries the tracked hooks, `.claude/init.sh`, `.claude/settings.json`
and `.claude/hooks-daemon.yaml`, but not the daemon: `.claude/hooks-daemon/` is
gitignored and per-checkout. Until it is built, every safety handler is off.

This is not `install` (add the daemon to a project that does not use it) and not
`upgrade` (change the version).

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

## When it refuses

| It says                                | What to do                                                                   |
| -------------------------------------- | ---------------------------------------------------------------------------- |
| the version is unknown                 | Set `daemon.expected_version`, commit it, run provision again                |
| `daemon.expected_version` is not X.Y.Z | Correct it (for example `3.68.0`: no `v`, no branch name)                    |
| a clone is already provisioned         | Change version with `/hooks-daemon upgrade`                                  |
| a clone exists but has no venv here    | `.claude/hooks-daemon/bin/hooks-daemon repair` keeps the clone               |
| the directory holds something else     | Inspect it; move it aside if disposable; run provision again                 |
| the tag cannot be fetched              | Check the version is a release and the machine can reach GitHub              |
| the venv cannot be built               | Fix the named condition, then `.claude/hooks-daemon/bin/hooks-daemon repair` |
