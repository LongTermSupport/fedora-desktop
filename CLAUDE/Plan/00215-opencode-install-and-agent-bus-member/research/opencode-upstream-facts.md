# opencode: upstream facts (release, verification, config, credentials, headless modes)

Every fact below was read from upstream at the pinned tag `v1.19.3` unless a line says
otherwise. Doc paths are under `packages/web/src/content/docs/` in
<https://github.com/anomalyco/opencode>; read them at the tag with
`gh api -H "Accept: application/vnd.github.raw" "repos/anomalyco/opencode/contents/<path>?ref=v1.19.3"`.

## Project

- `sst/opencode` now redirects to **`anomalyco/opencode`** (`gh api repos/sst/opencode`
  answers `full_name: anomalyco/opencode`). MIT licence, default branch `dev`.
  The play and `vars/version-pins.yml` name the new repo, so the pin check queries it
  directly rather than through the redirect.
- The npm package `opencode-ai` is the same program, and upstream's `install` script is a
  `curl | bash` installer (`install` at the repo root). Neither is used: the play takes the
  release tarball, as the other pinned-binary plays do.
- Release cadence is fast: six releases between 2026-09-30 and 2026-10-16 (`v1.18.34` to
  `v1.19.3`; `gh api repos/anomalyco/opencode/releases`). The upgrade command also offers
  `opencode upgrade --major`, "Upgrade to OpenCode 2 and remove OpenCode 1" (`cli.mdx`): a
  2.x line exists upstream. This plan pins 1.x; moving to 2 is its own bump, with its own
  look at the server API below.

## Release assets and how they are verified

- Latest at the time of writing: `v1.19.3`, published 2026-10-16
  (`gh api repos/anomalyco/opencode/releases/latest`).
- Linux CLI assets: `opencode-linux-x64.tar.gz`, `-x64-baseline` (no AVX2), `-x64-musl`,
  `-x64-baseline-musl`, `opencode-linux-arm64.tar.gz`, `-arm64-musl`. The `opencode-desktop-*`
  assets are a separate Electron desktop app, not the CLI.
- The tarball holds one file, `opencode` (a 186 MB self-contained Bun executable), at the top
  level (`tar -tzvf`).
- **No checksum file is published.** The sha256 comes from the release API's per-asset
  `digest` field, as `play-cli-tools.yml` does for ouch:
  `opencode-linux-x64.tar.gz  sha256:13a8fb2978302071b86f9d7e00da9c8895f9cd846978379c3bdc6a5333536c9b`.
  A local download hashed to the same value.
- The release is a GitHub **immutable release** (`.immutable: true`) carrying a release
  attestation (predicate `https://in-toto.io/attestation/release/v0.2`) whose subjects list
  every asset's sha256 and the tag's commit. `gh release verify-asset v1.19.3 opencode-linux-x64.tar.gz -R anomalyco/opencode` passed. There is **no SLSA build
  provenance**: `gh attestation verify` answers 404 for the provenance predicate. So the
  digest proves the bytes are what the release published, not how they were built.
- How to bump: take the new tag's `digest` for the same asset name
  (`gh api repos/anomalyco/opencode/releases/tags/vX.Y.Z --jq '.assets[] | select(.name=="opencode-linux-x64.tar.gz") | .digest'`),
  optionally `gh release verify-asset`, and change `opencodeVersion` and `opencodeSha256`
  together (the `update-versions` skill).
- `opencode --version` prints the bare version, `1.19.3`, which the play asserts.

## Where it keeps config, data and credentials

- Global config: `~/.config/opencode/opencode.json` (or `.jsonc`); TUI settings in
  `~/.config/opencode/tui.json` (`config.mdx` "Global"). Agents, commands, plugins, skills,
  tools and themes live in subdirectories of `~/.config/opencode/` (`config.mdx` note).
- Project config: `opencode.json` at the project root, found by walking up to the nearest
  git directory, plus `.opencode/` (`config.mdx` "Per project").
- Precedence, later wins: remote `.well-known/opencode`, global, `OPENCODE_CONFIG`, project,
  `.opencode` dirs, `OPENCODE_CONFIG_CONTENT`, then **managed** config (`config.mdx`
  "Precedence order"). On Linux the managed directory is **`/etc/opencode/`**
  (`config.mdx` "Managed settings"; source `packages/opencode/src/config/managed.ts`,
  `systemManagedConfigDir()` returns `/etc/opencode`; `config/config.ts` loads it after the
  rest).
- Data: `~/.local/share/opencode/`: `auth.json` (API keys and OAuth tokens), `log/` (the 10
  newest logs), `project/<slug>/storage/` session data (`troubleshooting.mdx`). Cache,
  including npm plugins installed with Bun: `~/.cache/opencode/` (`plugins.mdx`).
- **Credentials:** `opencode auth login` stores provider keys in
  `~/.local/share/opencode/auth.json`; providers are also loaded from environment variables
  and a project `.env` (`cli.mdx` "auth login"). Config values may read a secret with
  `{env:NAME}` or `{file:path}` (`config.mdx` "Env vars", "Files"). The install needs no
  credential, so the play writes none (owner decision O1 in PLAN.md).
- **It reads Claude Code's files by default:** a project `CLAUDE.md` when there is no
  `AGENTS.md`, `~/.claude/CLAUDE.md`, and `.claude/skills/` (`rules.mdx`, `cli.mdx`
  `OPENCODE_DISABLE_CLAUDE_CODE*`). In this repository an opencode session would read
  `CLAUDE.md` and load every skill under `.claude/skills/`, `pingbus` included.

## Defaults that matter for an unattended worker

- **Permissions default to allow** for nearly everything; only `doom_loop` and
  `external_directory` default to `ask`, and `.env` reads are denied (`permissions.mdx`
  "Defaults"). `--auto` on `run` and the TUI approves every `ask` that is not an explicit
  `deny` (`cli.mdx`).
- **None of this repo's Claude Code hooks run in opencode.** The hooks daemon's guards (git
  destructive-command blocks, secret-read blocks, sed block, plan rules) are Claude Code
  `PreToolUse` hooks; opencode has its own plugin hooks (`tool.execute.before` and others,
  `plugins.mdx`) and its own `permission` rules, and reads neither `.claude/settings.json`
  nor the daemon.
- Sharing: `share` is `manual` by default, `auto` uploads every session, `disabled` turns it
  off (`share.mdx`). The play sets `disabled` in the managed config.
- Update checks: `cli/upgrade.ts` says checking never installs; updates happen only on
  `/update` or `opencode upgrade`. The check is skipped when `autoupdate` is `false` or
  `OPENCODE_DISABLE_AUTOUPDATE` is set, **but it reads `cfg.getGlobal()`, the user's global
  file only**, so the managed `autoupdate: false` the play writes does not silence the
  notice in 1.19.3; it is set so every reader of the merged config sees it. A notice is
  harmless: it installs nothing.
- **Upgrading is what to avoid.** `installation/index.ts` names the install method `curl` when
  the executable's path holds `.opencode/bin` or `.local/bin`, else, past the package-manager
  checks, `unknown`. For `curl`, `opencode upgrade` fetches `https://opencode.ai/install` and
  runs it, and that script installs to `$HOME/.opencode/bin` (`install`, `INSTALL_DIR`). An
  upgrade would leave a second, unpinned copy beside the pinned one, so every run of the play
  removes `~/.opencode/bin/opencode`. Which method the play's symlinked binary reports is not
  established here (it depends on how Bun resolves `process.execPath` through the link); the
  rule in `docs/playbooks.md` is not to run it.

## Non-interactive and server modes (what Phase 2 builds on)

- **`opencode run [message..]`**: one prompt, non-interactive. `--format json` prints raw
  JSON events; `--session`/`--continue` resume a session; `--attach <url>` runs against an
  already running server; `--agent`, `--model`, `--dir`, `--auto` (`cli.mdx` "run").
- **`opencode serve`**: a headless HTTP server, default `127.0.0.1:4096`, OpenAPI 3.1 spec at
  `/doc` (`server.mdx`). Basic auth only when `OPENCODE_SERVER_PASSWORD` is set (user
  `opencode` or `OPENCODE_SERVER_USERNAME`); **with no password it is unauthenticated** to
  anything that can reach the port. The TUI itself runs the same server on a random port, or
  a fixed one with `--port`/`--hostname`, so a TUI the owner watches can also be driven.
  - `POST /session` creates a session; `GET /session/status` gives every session's status.
  - `POST /session/:id/message` sends a message and waits for the reply;
    **`POST /session/:id/prompt_async`** sends one and returns `204` at once; body
    `{ messageID?, model?, agent?, noReply?, system?, tools?, parts }`. `noReply: true` adds
    context without starting a turn.
  - `POST /session/:id/abort` stops a running turn.
  - `GET /event` (and `/global/event`): a server-sent events stream; the first event is
    `server.connected`, then bus events such as `session.idle`, `session.status`,
    `message.updated`, `permission.asked`.
  - `POST /session/:id/permissions/:permissionID` answers a permission request.
  - `/tui/append-prompt`, `/tui/submit-prompt`, `/tui/show-toast` drive a running TUI.
- **`opencode attach <url>`** puts a TUI on a running server, so a human can watch and type
  into the same session an adapter drives (`cli.mdx` "attach").
- **SDK**: `@opencode-ai/sdk` (JS/TS) generated from the OpenAPI spec:
  `session.prompt`, `session.promptAsync`-equivalent, `event.subscribe()` (`sdk.mdx`). No Go
  SDK upstream; a Go client would be written against the OpenAPI spec.
- **Plugins**: JS/TS modules in `~/.config/opencode/plugins/` or `.opencode/plugins/`, loaded
  at startup, given an SDK `client` and Bun's `$`; hooks include `event` (every bus event,
  e.g. `session.idle`), `tool.execute.before`/`after`, `shell.env`, and custom `tool`s
  (`plugins.mdx`). A plugin can add a tool the model calls, which is how a bus `say` could be
  offered as a tool instead of a shell command.
- **`opencode acp`**: an Agent Client Protocol server over stdin/stdout nd-JSON (`acp.mdx`),
  for editors. Not used by this design.
- **`opencode github run`**: upstream's own GitHub Actions agent (`github.mdx`). It runs in
  CI with a token; not the team-worker route the owner asked for.
