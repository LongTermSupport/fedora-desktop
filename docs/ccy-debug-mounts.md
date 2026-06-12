# CCY Debug Mounts — Read-Only Host Access

When debugging speech-to-text from inside a CCY container, you may need
read-only access to host files that are not part of the project workspace:

- `~/.local/share/speech-to-text/` — runtime debug logs
- `~/.local/bin/` — deployed scripts (e.g. `wsi-stream`)
- `~/.config/speech-to-text/` — runtime configuration

## How It Works

CCY reads the `CCY_EXTRA_MOUNTS` environment variable (added in CCY 3.18.0,
BSH-12). When set, it splits the value on whitespace and appends the tokens to
its internal `DOCKER_MOUNTS` array before launching the container. The
`scripts/desktop-symlinks` wrapper automates this.

## Using the Wrapper Script

Run from the project root on the **host** (not inside a container):

```bash
./scripts/desktop-symlinks
```

The script:

1. Detects the container engine (Podman first, Docker fallback).
2. Builds the `CCY_EXTRA_MOUNTS` value with three read-only bind mounts:
   - `$HOME/.local/share/speech-to-text` → `/host-debug-logs`
   - `$HOME/.local/bin` → `/host-bin`
   - `$HOME/.config/speech-to-text` → `/host-config`
3. Exports `CCY_EXTRA_MOUNTS` and calls `exec ccy "$@"`, so any additional
   arguments you pass are forwarded to `ccy`.

All mounts use the `:ro` flag — you cannot accidentally modify host files from
within the container.

If run from inside a container (detected by `$PWD == /workspace`), the script
prints the available mount paths and exits without launching another container.

## CCY Tokens

CCY tokens are stored as named files under `$HOME/.claude-tokens/ccy/tokens/`,
with the filename pattern `<name>.<YYYY-MM-DD>.token`. Manage them with:

```bash
ccy --create-token        # create a new named token
ccy --list-tokens         # list tokens and expiry dates
ccy --token <name>        # launch CCY with a specific token
```

The token directory path is defined in `claude-yolo` as
`CCY_ROOT="$HOME/.claude-tokens/ccy"` / `TOKEN_DIR="$CCY_ROOT/tokens"`.
There is no `~/.config/claude-code/oauth_token` path — that location does not
exist in this repo.

## Passing Extra Arguments

Because `desktop-symlinks` ends with `exec ccy "$@"`, you can pass any `ccy`
flag through it:

```bash
./scripts/desktop-symlinks --token mytoken
./scripts/desktop-symlinks --debug
```
