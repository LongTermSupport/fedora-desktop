# CCY — Claude Code YOLO

`ccy` runs [Claude Code](https://claude.com/claude-code) inside a disposable, rootless
Podman container with `--dangerously-skip-permissions` enabled — so the agent works
without stopping to ask permission for every file write and every command, while the
filesystem it can damage is limited to the project you launched it in.

That is the whole idea in one sentence: **YOLO mode is only reasonable if it is
contained, so contain it.**

- **On the host**, Claude Code asks before each action, because a mistake reaches your
  whole home directory.
- **In CCY**, it does not need to ask, because a filesystem mistake reaches
  `/workspace` — the one project directory you launched it from — and the container is
  thrown away afterwards.

Containment is not total, and the limits matter: the container also holds your SSH key,
your GitHub token, and unrestricted network access. Read
[The Security Model](#the-security-model) before you rely on it.

CCY also carries the surrounding machinery that makes this practical day to day: a named
OAuth token pool separate from desktop Claude Code, per-project container images, SSH
keys mounted read-only for `git push`, container-network attachment for talking to your
app's services, and an optional in-container supervisor that compacts long sessions
before they stall.

This guide covers CCY itself. Adjacent topics have their own homes — custom Dockerfiles,
extra debug mounts, the engine choice, and the rules for agents working inside a
container — all linked from [See Also](#see-also) at the end.

---

## Quick Start

> Assumes CCY is already installed. It ships with `playbook-main.yml`; to install it on
> its own see [Installation](#installation) below.

```bash
cd /path/to/your-project
ccy
```

The first launch in a project will ask you to pick an SSH key and to create a token (see
[Tokens](#tokens)). After that, `ccy` starts in a couple of seconds.

```bash
ccy                                  # start a session in the current directory
ccy "implement the search endpoint"  # start with an opening instruction
ccy --resume                         # resume the previous conversation
ccy --top                            # manage/stop running CCY containers
ccy --help                           # full flag list (plus claude's own --help)
```

`ccy` is a shell alias to `/var/local/claude-yolo/claude-yolo`, defined in a bashrc
include deployed by Ansible. Anything `ccy` does not recognise is validated against
Claude Code's own `--help` and forwarded — so `--resume`, `--model` and friends are
Claude Code's flags, not CCY's, and pass straight through. Use `--` to force-forward a
flag without validation.

---

## Installation

CCY is a **core** part of this repo — `playbooks/playbook-main.yml` installs it. To
install or update it on its own:

```bash
ansible-playbook playbooks/imports/play-claude-yolo.yml
```

**Prerequisites** (the playbook asserts these and fails with instructions if missing):

| Requirement                 | Notes                                                                                   |
| --------------------------- | --------------------------------------------------------------------------------------- |
| A container engine (Podman) | `ansible-playbook playbooks/imports/play-podman.yml`                                    |
| `podman-compose`            | Same playbook — needed for compose-network integration                                  |
| The engine is reachable     | Rootless Podman needs no daemon; verify with `podman ps` (an empty table, not an error) |

The default engine comes from `container_engine` in `vars/container-defaults.yml`
(`podman`). Override per shell with `export CCY_CONTAINER_ENGINE=docker`, or per launch
with `ccy --engine docker`. Podman is strongly preferred — see
[Container Engines](../CLAUDE/ContainerEngines.md).

**What the playbook deploys**

| Path                                   | Purpose                                            |
| -------------------------------------- | -------------------------------------------------- |
| `/var/local/claude-yolo/claude-yolo`   | The launcher (host-side)                           |
| `/var/local/claude-yolo/lib/*.bash`    | Shared libraries (SSH, tokens, networking, health) |
| `/opt/claude-yolo/Dockerfile`          | Base image build context                           |
| `/opt/claude-yolo/entrypoint.sh`       | In-container entrypoint                            |
| `/opt/claude-yolo/docs/`               | Guides readable *from inside* the container        |
| `/opt/claude-yolo/custom-dockerfiles/` | Project Dockerfile templates                       |
| `~/.claude-tokens/ccy/tokens/`         | The named token pool                               |

The playbook then builds the base image, tagged `claude-yolo:latest`.

---

## What Happens When You Run `ccy`

Understanding this sequence makes almost every question about CCY answer itself. This is
the real order the launcher executes in.

01. **Host checks.** The container engine is verified, the project's `.claude/ccy/`
    directory is checked for accidentally-committed state (see
    [State](#state-and-the-claudeccy-directory)), and
    `.claude/ccy/allowed-hostnames` — if present — is matched against the current
    hostname.
02. **SSH selection.** You pick a key (or pass `--ssh-key` / `--no-ssh`). It is mounted
    **read-only**, and a matching `gh` token is resolved.
03. **Token selection.** A long-lived OAuth token is chosen from the pool and its expiry
    checked; an expired one forces a renewal prompt. It is passed in as an environment
    variable.
04. **Safety guard.** The launcher refuses to continue if you are running as root.
05. **Version check.** If the base image's version label is older than the launcher's
    `REQUIRED_CONTAINER_VERSION`, a rebuild is forced. Claude Code inside the image is
    also updated if it is behind — see [Keeping CCY Current](#keeping-ccy-current).
06. **Image resolution.** If `.claude/ccy/Dockerfile` exists, the image becomes
    `claude-yolo:<project-name>` and is rebuilt when the Dockerfile's hash changes.
    Otherwise the base `claude-yolo:latest` is used.
07. **Network.** Any project compose network is detected and offered, or attached with
    `--network`.
08. **State permission repair.** Any file or directory under the project's `.claude/`
    tree carrying group or other permissions is restricted to you, and the count is
    reported. See [State](#state-and-the-claudeccy-directory). This is advisory — it never
    blocks a launch — and it runs on **every** launch by design.
09. **Launch.** The container starts with your project bind-mounted at `/workspace`.
10. **Entrypoint.** Inside, the umask is set to `077` so all new session state is
    owner-only, `/root/.claude` is symlinked to `/workspace/.claude/ccy/`,
    `.claude/ccy/ccy.env` is sourced if present, and `claude` is exec'd — optionally
    wrapped by a [supervisor](#the-supervisor).

`<project-name>` is your project directory's name, lowercased with unusual characters
replaced by `_`. If the parent directory is not a generic container (`projects`, `repos`,
`work`, `src`, `code`, `dev`, `home`), it is prefixed too — so `~/Projects/my-app`
becomes `my-app`, but `~/clients/acme/my-app` becomes `acme-my-app`.

The container itself is ephemeral. Everything that must survive lives in
`/workspace/.claude/`, which is your project directory on the host.

---

## The Security Model

CCY disables Claude Code's permission prompts. That is a deliberate trade: instead of
gating each action, the isolation bounds where the damage from any action can land.

**Threat model.** This design contains an agent's own mistakes and over-broad actions
inside a disposable filesystem boundary. It is **not** a sandbox against a deliberately
adversarial process, a container or kernel escape, or a prompt-injection or
supply-chain attack that exfiltrates the credentials it is given over the network. Those
risks are bounded and named below, not eliminated.

### What the container CAN reach

| Exposed                                 | How                                                                                        |
| --------------------------------------- | ------------------------------------------------------------------------------------------ |
| Your project directory                  | Bind-mounted read/write at `/workspace`                                                    |
| One or more SSH private keys            | Mounted **read-only** at `/root/.ssh/key_N` (individual keys, not all of `~/.ssh`)         |
| A Claude OAuth token                    | Environment variable — no credential files are mounted                                     |
| A GitHub token for `gh`                 | Environment variable, from your existing `gh` login                                        |
| Your git identity                       | A read-only copy of `~/.gitconfig`, to set `user.name` / `user.email`                      |
| Your Wayland or X11 display socket      | Mounted read-only and auto-detected, so the agent can open browser windows on your desktop |
| The host GPU render device              | `--device /dev/dri` — always attached, for accelerated browser rendering                   |
| The network                             | Normal outbound; optionally a named container network                                      |
| Anything you add via `CCY_EXTRA_MOUNTS` | Explicit opt-in — see [debug mounts](ccy-debug-mounts.md)                                  |

### What it CANNOT reach

| Not exposed                 | Why                                                                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Your home directory         | Not mounted. Other projects, `~/Documents`, `~/.ssh` as a whole, browser profiles — invisible                                   |
| Desktop Claude Code's state | `~/.claude/` on the host is never read or modified; CCY's tokens live under `~/.claude-tokens/ccy/`                             |
| Root on the host            | Podman is rootless — the container's `root` is your unprivileged user in a user namespace, so files it creates are owned by you |
| The container/host boundary | No `--privileged`, no added capabilities, and no Docker/Podman socket is mounted                                                |
| The host system             | No host `systemctl`, no host package manager                                                                                    |

### What this means in practice

The realistic worst case is **damage to this project's working tree and git history**,
plus **anything your mounted SSH key and `gh` token can reach on GitHub** — which,
unless you use a project-scoped deploy key or token, is not limited to this one repo.

The residual risks worth naming honestly:

- **Your SSH key is in there** (read-only), and its push access is not limited to this
  repository. Use a dedicated key if that matters, or `--no-ssh` when you do not need
  git remote access.
- **Your Claude and GitHub tokens are live inside the container.** Combined with
  unrestricted network access, a misled or compromised agent process could exfiltrate
  them, not merely misuse them locally.
- **Uncommitted work in the project can be destroyed.** Commit before long unattended
  runs.
- **The display socket is not confined the way the filesystem is.** A process with it
  can open windows and interact with your live desktop session while the container runs.

For projects where CCY should never run, or should only run on one machine, see
[allowed-hostnames](#3-allowed-hostnames--restricting-where-ccy-can-run).

---

## State and the `.claude/ccy/` Directory

Claude Code's **user-level** state — history, sessions, tasks, its own databases — is
redirected into `/workspace/.claude/ccy/` via the `/root/.claude` symlink. Because
`/workspace` is your project, that state is project-local: each project has its own
history and its own sessions.

**Project-level** Claude Code config — `settings.json`, `agents/`, `hooks/`, `commands/`
at the top of `.claude/` — lives directly in the bind-mounted project and is unaffected
by the symlink.

```
.claude/
├── ccy/
│   ├── Dockerfile           # TRACK — project container definition
│   ├── ccy.env              # TRACK — per-project CCY config (see below)
│   ├── claude-supervise.py  # TRACK — vendored supervisor, if deployed
│   ├── allowed-hostnames    # TRACK — host restrictions, if used
│   ├── .gitignore           # generated; whitelists the tracked files above
│   ├── history.jsonl        # runtime state — not tracked
│   ├── sessions/ tasks/ …   # runtime state — not tracked
│   └── …
├── agents/                  # TRACK — custom subagents
├── hooks/ commands/         # TRACK — hooks and slash commands
└── settings.json            # runtime preferences
```

CCY generates `.claude/ccy/.gitignore` so runtime state stays out of git while the
handful of files that *should* be shared with your team are whitelisted.

It also enforces this: if any file under `.claude/ccy/` **other than** the known-safe
whitelist (`.gitignore`, `Dockerfile`, `allowed-hostnames`, `ccy.env`,
`claude-supervise*`) is tracked in git, CCY prints a security alert and **refuses to
start** until you untrack it. Session history and token metadata committed by accident
are exactly what this catches.

### Permissions — this state is plaintext

Everything above is stored **unencrypted**. Anthropic documents this directly: session
files are not encrypted at rest and OS file permissions are their only protection
([Plaintext storage](https://code.claude.com/docs/en/claude-directory)). The material is
sensitive in practice — full conversation transcripts, verbatim copies of files Claude
read before editing them (`file-history/`), shell snapshots, and every prompt you have
typed (`history.jsonl`, which Claude Code's retention sweep never deletes).

CCY therefore does two things, and both are needed:

- **`umask 077` inside the container**, so every file Claude Code creates is owner-only
  from the start. Without it the default `022` leaves all of the above readable by every
  local user.
- **A repair pass on every launch** over the whole project `.claude/` tree, clearing group
  and other permissions from anything that has them. Owner bits are left alone, so
  executables keep working, and symlinks are skipped.

The repair is **not** a one-off migration, and must not be removed once the umask has
shipped. **A umask governs creates and retro-fixes nothing**, so every file written before
one shipped keeps the mode it was born with. The hooks daemon is the worked example: until
daemon 3.53.0 it called `os.umask(0)` when it daemonized — overwriting its own umask
rather than inheriting the container's — and created essentially everything under
`.claude/hooks-daemon/untracked/` world-writable (`0666`, directories `0777`), including
the verdict log, captured hook payloads, the per-session sidecars and the PID file. Daemon
3.53.0 sets `umask 0o077` instead, so *new* artefacts are owner-only; the ones already on
disk keep `0666` until something tightens them. List them with
`.claude/hooks-daemon/bin/hooks-daemon check-permissions` (`--fix` tightens them) — CCY's
repair pass catches them on the next launch regardless, which is exactly why it stays.

(The daemon's transcript archiver was removed in 3.53.0, so
`.claude/hooks-daemon/untracked/transcripts/` is no longer written to. An existing copy is
left in place by the upgrade and is safe to delete: the archives were copies of Claude
Code's own transcripts, which remain at `~/.claude/projects/<project-slug>/<session-id>.jsonl`
on the same storage — deleting the copies loses nothing that the originals do not still
hold.)

This protects against another local user reading your state, and against a copy of the
project tree leaking it — `.gitignore` stops git, but does nothing for `rsync`, `restic`,
`borg`, Dropbox or Syncthing, and `.claude/ccy/` sits **inside** the working tree. It does
**not** protect against anything running as you. If an attacker has code execution under
your account while a session is live, they read this state exactly as Claude Code does.
For that threat the answer is to keep secrets out of the context in the first place, and
to shorten retention (`cleanupPeriodDays`), not file permissions.

---

## Tokens

CCY authenticates with **long-lived OAuth tokens** (`sk-ant-oat01-…`), the same kind used
in GitHub Actions. `ccy --create-token` drives Claude Code's own `claude setup-token`
flow for you and saves the result — you never need to run `setup-token` by hand.

Tokens are stored in a pool completely separate from desktop Claude Code:

```
~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token
```

The expiry date is in the filename, so the launcher checks it before every session and
forces a renewal when a token is expired or expiring today.

```bash
ccy --create-token           # create and name a new token
ccy --list-tokens            # list tokens and expiry dates
ccy --token work             # use a specific named token for this session
ccy --update-token=work      # replace one (e.g. invalidated before expiry)
ccy --export-token           # interactive: pick token(s) to export
ccy --export-token work      # export a specific token
```

Named tokens let you keep separate Claude accounts for separate contexts — a personal
account and a work account, say — and pick per project or per session.

### Seeing usage limits before you pick

When several accounts are in play, the useful question at selection time is *which one
still has headroom*. The token menu answers it on request:

```
  1) work     (expires: 2026-11-02)
  2) personal (expires: 2026-12-14)

  u) Show usage limits (costs 1 small API call per account)
```

Press `u` and the menu redraws with a bar per limit — green, orange or red by how much is
used — and the reset time in plain words:

```
  1) work       expires 2026-11-02
       5-hour limit   ███░░░░░░░░░░░░░░░░░   15%   resets in 2 hours
       weekly limit   ███░░░░░░░░░░░░░░░░░   15%   resets in 4 days
       binding limit: 5-hour limit

  2) personal   expires 2026-12-14
       5-hour limit   ██░░░░░░░░░░░░░░░░░░   12%   resets in 2 hours
       weekly limit   ███████████░░░░░░░░░   57%   resets in 44 hours
       binding limit: 5-hour limit
```

A figure that is non-zero but rounds to zero shows `<1%` rather than `0%`, so the display
never claims an account is untouched when it is merely barely touched.

**What `binding limit` does and does not tell you.** It is the API's own
`-representative-claim` header, reported rather than inferred — ccy does not work it out
by comparing the two percentages. That distinction matters because the two can disagree:
account 2 above is at 57% weekly against 12% for five hours, and the API still names the
5-hour bucket. So this line is **not** "the limit you are closest to". Read it as the
bucket the API considers governing, and read the bars for how much is left.

It is also the answer to a question the bars cannot settle on their own. The weekly
buckets are **per-model**, and the probe deliberately uses Haiku; a claim naming
`weekly limit (Opus)` or `weekly limit (Sonnet)` therefore tells you the weekly figure
shown above it is not the allowance being reported against. Unknown bucket names print
verbatim rather than being dropped, so a bucket the API adds later is visible instead of
silently missing.

**It is never fetched automatically, and the reason is the cost.** These figures are only
available as rate-limit headers on a real API response, so reading them means making a
genuine billed request per account — one that consumes a sliver of the very allowance it
reports. A launch-time fetch would spend quota you never asked to spend, so it is a
keypress instead. The request uses Haiku with a one-character prompt, and the weekly
buckets are per-model, so it does not touch your Opus or Sonnet allowances.

The result is cached for 15 minutes, so pressing `u` again in the same sitting costs
nothing. An account that cannot be read says so on its own row and the others still
render. Set `CCY_TOKEN_USAGE=0` to remove the option entirely.

**The utilisation scale, and why it is worth knowing about.** The API expresses
utilisation as a fraction (`0`–`1`) and documents that nowhere. ccy shipped briefly with
the other reading, so a real 41% arrived as `0.41`, rendered as 0.41%, and displayed as
`<1%` — on every account, for a week. The `<1%` guard above, which exists so the display
never claims an account is untouched, was quietly doing all the work.

It was settled by measurement rather than argument, and it cost nothing: the cache stores
values exactly as the API sent them, so the raw numbers were already on disk from a fetch
already paid for. Eight samples across four accounts, every one between `0.04` and `0.41`.
Read as fractions those are ordinary mid-week figures; read as percentages the same four
accounts had used a twentieth of one percent of their allowances.

That is strong evidence rather than proof — no sample above `1` has been observed, and one
would settle it outright. So the assumption **announces itself if it is wrong**: a raw
value the assumed scale cannot produce (above `1` under `fraction`, above `100` under
`percent`) prints `SCALE MISMATCH` naming the value and the switch, instead of clamping
the bar to full and showing a confident wrong number. That guard is the point — the
original error was invisible *by construction*, because a bar clamped to 100% looks
exactly like a healthy one.

```bash
CCY_USAGE_DEBUG=1 ccy       # each line also shows the raw value the API sent
CCY_USAGE_SCALE=percent ccy # override, if the API ever changes to 0-100
```

Nothing else in the display depends on it, and the switch applies to values already
cached — flipping it costs no extra request.

`--export-token` writes a self-contained import script, so you can move a token onto a
second workstation without repeating the `setup-token` browser flow there.

**`~/.claude/` on the host is never touched.** Desktop Claude Code and CCY do not share
credentials or state.

---

## Command Reference

Run `ccy --help` for the authoritative list — it also prints Claude Code's own help from
inside the image. Flags not listed here (`--resume`, `--model`, …) are Claude Code's and
are forwarded unchanged.

### Session

| Flag              | Effect                                                                |
| ----------------- | --------------------------------------------------------------------- |
| `ccy`             | Start a session in the current directory                              |
| `ccy "task"`      | Start an interactive session with an opening instruction              |
| `--prompt "text"` | Start with a preseeded prompt                                         |
| `--headless`      | Run non-interactively — requires `--prompt` (not the positional form) |
| `--supervise`     | Wrap `claude` in the in-container supervisor                          |
| `--top`           | Container manager: list and stop running CCY containers               |
| `--debug`         | Interactive debug-layer selection (CCY, entrypoint, Claude Code)      |
| `--`              | End of CCY options; everything after is forwarded raw to `claude`     |

### Image and updates

| Flag                      | Effect                                                     |
| ------------------------- | ---------------------------------------------------------- |
| `--rebuild`               | Full clean rebuild (always builds without cache)           |
| `--rebuild=claude`        | Fast: npm-update Claude Code in the base image only        |
| `--rebuild=project`       | npm update **and** rebuild the project image               |
| `--custom`                | Create/edit a project Dockerfile from a template           |
| `--custom-docker`         | AI-guided Dockerfile creation                              |
| `--disable-custom-docker` | Skip the project image — useful to fix a broken Dockerfile |

### Auth, SSH and network

| Flag               | Effect                                                             |
| ------------------ | ------------------------------------------------------------------ |
| `--token NAME`     | Use a specific named token                                         |
| `--create-token`   | Create a new named token                                           |
| `--update-token=N` | Replace an existing named token                                    |
| `--list-tokens`    | List tokens with expiry                                            |
| `--export-token`   | Export token(s) as a portable import script                        |
| `--ssh-key PATH`   | Mount a specific key (repeatable)                                  |
| `--no-ssh`         | Mount no key (git push will not work)                              |
| `--github-443`     | Route GitHub SSH over `ssh.github.com:443` when port 22 is blocked |
| `--network NET`    | Auto-connect to a container network on launch                      |
| `--no-network`     | Skip network auto-detection                                        |
| `--connect [NET]`  | Connect an already-running container to a network                  |

### Engine and policy

| Flag              | Effect                                                             |
| ----------------- | ------------------------------------------------------------------ |
| `--engine ENGINE` | `podman` (default) or `docker`                                     |
| `--prevent`       | Disable CCY for this project (writes `never` to allowed-hostnames) |
| `--version`, `-v` | Show the CCY version                                               |
| `--help`, `-h`    | Full help                                                          |

### Claude Code environment CCY sets

CCY forwards a few Claude Code environment defaults into the container. Each uses the
`${VAR:-default}` idiom, so exporting the variable on the host before launching overrides
it; a project `ccy.env` that sets it explicitly overrides both, because the entrypoint
sources that file after this environment is forwarded.

| Variable                               | CCY default | Why                                                          |
| -------------------------------------- | ----------- | ------------------------------------------------------------ |
| `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` | `1`         | Enables the agent-teams features CCY sessions use            |
| `MAX_THINKING_TOKENS`                  | *(unset)*   | Forwarded only if you export it; CCY does not impose a value |
| `TERM` / `COLORTERM`                   | inherited   | Falls back to `xterm` / `truecolor` if unset on the host     |
| `FORCE_COLOR`                          | `1`         | Keeps colour output intact inside the container              |

CCY deliberately sets **no** sub-agent fan-out limits — the section below explains why.

### Sub-agent limits in long unattended sessions

Long autonomous sessions delegate heavily: QA runners, parallel triage sweeps, read-only
code archaeology. Three separate Claude Code dials govern that fan-out, and only two of
them still exist:

| Dial                                    | Default | Can it be turned off?                    | Status                                       |
| --------------------------------------- | ------- | ---------------------------------------- | -------------------------------------------- |
| `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS`  | `20`    | No — adjustable only                     | Live (v2.1.217+)                             |
| `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`  | `3`     | No — `1` disables *nesting*, not the cap | Live (v2.1.219+ default; `1` in 2.1.217–218) |
| `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` | —       | n/a                                      | **Removed in v2.1.224 — now a no-op**        |

The third one was the problem dial. Between v2.1.212 and v2.1.223 it capped the
*cumulative lifetime* count of sub-agents a session could ever spawn at 200 — not how many
ran at once. A session that delegated steadily for hours accumulated 200 spawns doing
ordinary work, hit the wall mid-task, and could not delegate again for the rest of its
life; the work fell back into the driver's own expensive context. `/clear` reset the budget,
which is no help to an unattended session whose whole value is its accumulated context.

Anthropic removed it in v2.1.224: *"Removed the 200-subagent-per-session spawn cap;
long-running sessions no longer refuse new agents (concurrency and depth limits still
apply)."* CCY tracks Claude Code `@latest`, so CCY sessions are past that version and the
variable does nothing — do not set it.

The two surviving dials are the ones worth tuning, and both are the right *kind* of guard:
concurrency protects CPU, memory and API rate limits at any instant, and depth bounds
recursive fan-out. Neither penalises a session simply for living a long time. Raise the
concurrency cap per project if your box can take it:

```bash
# .claude/ccy/ccy.env
export CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS=32
```

One sibling cap of the *old* design is still live: `CLAUDE_CODE_MAX_WEB_SEARCHES_PER_SESSION`
(default 200) is a cumulative session-lifetime count of WebSearch calls, raisable to any
value but not disableable. A research-heavy unattended session can exhaust it the same way.

---

## Per-Project Configuration

Three tracked files in `.claude/ccy/` shape how CCY behaves for a project. Commit them so
your whole team gets the same environment.

### 1. `Dockerfile` — project-specific tools

The base image ships a general-purpose toolset — see
[what's included](containerization.md#custom-dockerfiles). When your project needs more
(a specific Python version, Go, PHP, a database client), add `.claude/ccy/Dockerfile`:

```bash
ccy --custom          # pick a template (Ansible / Go / generic)
ccy --custom-docker   # AI-guided: Claude inspects the project and proposes tools
```

The image is cached as `claude-yolo:<project-name>` and rebuilt automatically when the
Dockerfile changes. The full workflow, templates and cache-mount patterns are documented
in [containerization.md](containerization.md#custom-dockerfiles).

### 2. `ccy.env` — per-project environment

Sourced by the entrypoint **inside the container**, immediately before `claude` runs:

```bash
# .claude/ccy/ccy.env
export CCY_CLAUDE_WRAPPER="${CCY_CLAUDE_WRAPPER:-/workspace/.claude/ccy/claude-supervise.py --arm --}"
```

Two properties matter:

- **It is never sourced on the host.** A project's `ccy.env` cannot execute host code —
  cloning an untrusted repo does not hand it your host shell.
- **Host settings win.** The `${VAR:-default}` idiom means a value set on the host, or by
  `ccy --supervise`, overrides the project default; an empty value falls through to the
  project's.

#### `CCY_CHILD_CLAUDE` — let a session spawn child `claude` processes

Off unless a project asks for it:

```bash
# .claude/ccy/ccy.env
export CCY_CHILD_CLAUDE=1
export CCY_CHILD_CLAUDE_MAX_DEPTH=1   # optional; a child may not spawn its own child
```

The wrapper counts nesting in `CCY_CHILD_CLAUDE_DEPTH`, which it sets itself and which a
caller should never need to touch. The parent session is depth 0; a child is depth 1; the
wrapper refuses once the count reaches the maximum. It is an accident guard against a
runaway recursive fan-out, not a control — the counter is an ordinary variable.

Claude Code strips `CLAUDE_CODE_OAUTH_TOKEN` from the environment it gives Bash
subprocesses, so a child `claude` launched from an agent's shell answers `Not logged in`.
With this flag set, the entrypoint puts a `ccy-claude` wrapper on `PATH` that reattaches
the session's own credential, plus a `child-claude` skill telling the agent the capability
exists and when the `Agent` tool is the better choice.

Arguments pass through to `claude` verbatim. The wrapper injects nothing — no model, no
settings file, and deliberately no `--dangerously-skip-permissions`.

Removing the line genuinely turns it off: the entrypoint deletes the skill it installed.
That step matters because `/root/.claude` is a symlink to `/workspace/.claude/ccy`, so
session state persists across containers and a stale skill would otherwise linger.

**This adds no security boundary and does not claim one.** Inside CCY the agent already
runs as root and the token is already in PID 1's environment, so the credential scrub is
accident prevention rather than a control. The design goal was to add no new exposure
surface: no copy on disk, none in a command line, none in an inherited variable. Two
easier designs were rejected for failing that test. Putting the token in `settings.json`'s
`env` key writes a live credential into the host-mounted project tree, and aliasing it to
a variable name Claude Code does not scrub exposes it to every command in the session.
The full threat model, the seven invariants and their executable gate were worked out in
Plan 00092; find it by number under `CLAUDE/Plan/`, wherever the plan lifecycle has moved
it.

#### `AGENT_BROWSER_IDLE_TIMEOUT_MS` — how long an abandoned browser lingers

The image sets this to five minutes. `agent-browser` runs a daemon per session that shuts
itself down after that much inactivity; upstream's default is an hour, which meant a
headed Chrome window an agent opened and forgot stayed on your desktop for an hour.

```bash
# .claude/ccy/ccy.env — only if someone will be watching a headed session that long
export AGENT_BROWSER_IDLE_TIMEOUT_MS=900000
```

This is the safety net, not the cleanup. The `browsing` skill requires the agent to close
every session it opens in the same command chain that finishes the task, and to check
`agent-browser session list` is empty before reporting done.

### 3. `allowed-hostnames` — restricting where CCY can run

Optional. If the file does not exist, CCY runs anywhere. If it exists, the current
hostname must match an entry or CCY exits.

```bash
ccy --prevent                                          # disable CCY here entirely (writes 'never')
echo "$(hostname)" >> .claude/ccy/allowed-hostnames    # allow this machine
echo "build-server-*" >> .claude/ccy/allowed-hostnames # globs are supported
rm .claude/ccy/allowed-hostnames                       # remove the restriction
```

`*` alone allows any host (documents intent without blocking); `never` alone disables CCY
unconditionally. Comments (`#`) and blank lines are ignored.

---

## The Supervisor

This is the piece that turns CCY from "a safe sandbox" into something that can run long,
unattended sessions — and it is the least obvious part of the system, because it is
supplied by the [hooks daemon](https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon)
rather than by CCY itself.

### The problem it solves

A long Claude Code session eventually fills its context window. Historically that meant a
session degraded or stalled until a human noticed and ran `/compact`. If you are running
an agent overnight or across a large refactor, "until a human notices" is the weak link.

### What it does

`claude-supervise.py` is a standalone, stdlib-only **PTY supervisor**. CCY exec's it
instead of `claude`, and it spawns `claude` on a pseudo-terminal, forwarding
stdin/stdout and window resizes transparently — you cannot tell it is there.

From that position it does two useful things:

1. **Automatic compaction.** On each idle poll it reads the context sidecar — a small
   status file the hooks daemon writes recording how much of the session's context
   budget is left. When that status goes **red** while the session is idle, it injects
   `/compact` and then `continue`. The session is compacted and resumed instead of
   stalling. This is best-effort and bounded by a cooldown and an injection cap; once
   the cap is exhausted the session still needs a human.
2. **Terminal-key guarding.** `Ctrl+Z` (SUSP) is stripped from forwarded input, and
   `SIGTSTP`/`SIGQUIT` are swallowed if ever delivered. `Ctrl+C` deliberately still
   works. Since CCY 3.42.0 this is the **only** ctrl+z defence — see
   [ctrl+z and the supervisor](#ctrlz-and-the-supervisor).

It is deliberately dependency-free — it imports nothing from the hooks daemon and runs
under the container's system `python3` — so a broken hooks-daemon venv cannot break every
`ccy` launch.

### Turning it on

**Per project (recommended).** This requires the hooks daemon to already be installed in
the project (see `.claude/hooks-daemon/CLAUDE/LLM-INSTALL.md`, or the `hooks-daemon`
skill). It is driven by `.claude/hooks-daemon.yaml`:

```yaml
ccy:
  deploy_supervisor: true
```

| Value      | Behaviour                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------ |
| `true`     | On install/upgrade, deploy `claude-supervise.py` into `.claude/ccy/` and **arm** it              |
| *(absent)* | Same as `true` — deploys and arms. Not a safe no-op; the daemon recommends setting it explicitly |
| `false`    | Never deploy or arm (the only real opt-out)                                                      |

Arming means writing a `CCY_CLAUDE_WRAPPER` export into `ccy.env`. It is idempotent and
respects you — an existing `CCY_CLAUDE_WRAPPER` (set *or* commented out to disable) is
left alone.

**It is already on.** Since CCY 3.43.0 the entrypoint enables the supervisor by default
whenever the project has one at `.claude/ccy/claude-supervise.py` — you do not have to
ask for it. That default is **unarmed**: the ctrl+z guard runs, and the compaction
trigger injects a harmless visible marker rather than a real `/compact`.

Automatic compaction is the part you opt into, because it changes what a session does:

```bash
ccy --supervise      # force it on and ARMED for this launch
```

or per project, which is what `deploy_supervisor: true` writes into `ccy.env`, and which
outranks the in-container default:

```bash
export CCY_CLAUDE_WRAPPER="/workspace/.claude/ccy/claude-supervise.py --arm --"
```

**Opting out.**

```bash
ccy --no-supervise   # or: CCY_NO_SUPERVISOR=1 ccy
```

This runs `claude` unwrapped — no auto-compaction **and no ctrl+z guard**, so ctrl+z can
freeze the session. See [ctrl+z and the supervisor](#ctrlz-and-the-supervisor).

**If the supervisor is missing or broken.** A project with no
`.claude/ccy/claude-supervise.py` gets a one-line notice at launch saying ctrl+z is
unguarded — absence is normal, but silence there would read as "protected". A supervisor
that does not parse **fails the launch** rather than quietly running unwrapped; the error
names the file and tells you to use `--no-supervise` if you need a session right now.

### Dry-run vs armed

The supervisor takes a mode flag:

| Mode        | Behaviour                                                                                                                                    |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `--dry-run` | The script's own default. Injects a harmless **visible marker** instead of compacting — lets you confirm the trigger path fires as expected. |
| `--arm`     | Injects the real `/compact` (and `continue`).                                                                                                |

**Note the asymmetry:** although `--dry-run` is the script's default, the per-project
deploy writes an **already-armed** line, so a project set up via `deploy_supervisor`
starts in `--arm`, not dry-run. To watch the harmless marker first, edit
`.claude/ccy/ccy.env` and swap `--arm` for `--dry-run` before your first session, then
switch back once you have seen it fire.

### Keeping it healthy

The daemon's `ccy_supervisor_integrity` check runs at session start and warns — never
blocks — about brick-risky setups: a missing or non-executable `claude-supervise.py`, a
git-ignored supervisor that teammates will not receive, `deploy_supervisor: false` while
the supervisor is armed (which silently freezes it at a stale version), or a running
supervisor older than the one now on disk. Act on those warnings — a supervisor that
fails to start prevents the session from starting at all.

---

## Networking

When your agent needs to reach your app's database or services, attach the container to
the same network:

```bash
ccy --network myproject_default   # attach at launch
ccy --no-network                  # skip auto-detection entirely
ccy --connect                     # interactive: attach an already-running container
ccy --connect myproject_default   # attach a running container to a specific network
```

CCY auto-detects a project compose network and offers it, and can start the compose
services if they are not already up. `--connect` is run from a *second* terminal while a
session is live — useful when you realise mid-session that the agent needs database
access.

---

## Container Labels

From **CCY 3.40.0** every session container is labelled at launch, so host-side tooling
can find and group sessions without parsing container names:

| Label          | Value                                               |
| -------------- | --------------------------------------------------- |
| `ccy`          | always `true`                                       |
| `ccy-project`  | the project directory name                          |
| `ccy-github`   | the GitHub account this session is authenticated as |
| `ccy-token`    | the stored Anthropic token label you picked         |
| `ccy-ssh-keys` | the mounted SSH key basenames, space-separated      |

The last three are `none` when the axis does not apply, so "no GitHub identity" is stated
rather than looking like an unlabelled container.

Do **not** use the image's `claude-yolo-version` label to find sessions. Image labels are
inherited by anything built FROM the CCY image, so it identifies a lineage, not a session
— a throwaway `podman run <ccy-image> …` carries it too. The run-time labels above cannot
be inherited.

```bash
podman ps --filter label=ccy=true                       # every live session
podman ps --filter label=ccy-github=<gh-username>       # …for one GitHub account
podfreeze freeze --github <gh-username>                 # freeze them all at once
```

`podfreeze` (see [playbooks.md](playbooks.md#play-podfreezeyml)) is the intended consumer.
Nothing inside the container reads these labels, so they change no session behaviour.

---

## SSH and GitHub

On launch you pick which SSH private key to mount. It is mounted read-only at
`/root/.ssh/key_N`, and CCY resolves the matching GitHub account and token so `gh` works
inside the container.

```bash
ccy --ssh-key ~/.ssh/id_ed25519_work   # specific key (repeatable)
ccy --no-ssh                           # no key at all
ccy --github-443                       # tunnel GitHub SSH over port 443
```

`--github-443` is for networks that block outbound port 22 — it routes Git-over-SSH via
`ssh.github.com:443`. You can also enable it host-wide with `export GITHUB_SSH_443=1`
before launching; CCY inherits that and shows it in the launch banner, and falls back to
443 automatically when port 22 fails. Full background:
[github-ssh-over-443.md](github-ssh-over-443.md).

For juggling several GitHub identities, see
[github-multi-account.md](github-multi-account.md).

---

## Extra Mounts

`CCY_EXTRA_MOUNTS` appends read-only bind mounts for debugging host state that is not
part of the project:

```bash
export CCY_EXTRA_MOUNTS="-v $HOME/.local/bin:/host-bin:ro"
ccy
```

Full details and the `scripts/desktop-symlinks` wrapper:
[ccy-debug-mounts.md](ccy-debug-mounts.md).

---

## Keeping CCY Current

### Two version numbers

| Version                      | Meaning                                       |
| ---------------------------- | --------------------------------------------- |
| `CCY_VERSION`                | The host launcher script's version            |
| `REQUIRED_CONTAINER_VERSION` | The minimum image version that launcher needs |

When the launcher requires a newer image than you have, the next `ccy` triggers a
one-time rebuild automatically. You do not normally manage this by hand.

The launcher also carries a **self-hash guard**: if its content changes without a version
bump, it says so. Contributors must bump `CCY_VERSION` when editing the script — a
pre-commit hook enforces it. See
[ContainerRules.md](../CLAUDE/ContainerRules.md#ccy-version-bump-requirement).

Release notes for both version numbers live in [the CCY changelog](ccy-changelog.md). The
one-line comment on the `CCY_VERSION` line describes only the current release — the rebuild
banner prints it verbatim, so it must stay short.

### Claude Code auto-update

Once per 24 hours, per image, CCY compares the bundled Claude Code against npm and runs a
fast in-place update when behind. It is a no-op when current, soft-fails offline, and the
second session on the same day costs nothing.

```bash
export CCY_AUTO_UPDATE=0   # notify only, never update automatically
ccy --rebuild=claude       # force the fast update now
```

### ctrl+z and the supervisor

Claude Code's terminal UI intercepts `Ctrl+Z` and sends itself `SIGSTOP` — unrecoverable
in a container with no shell to run `fg` in.

Until CCY 3.42.0 the image build patched that handler out of the Claude Code binary. That
patch is **gone**. It had to match an anchor inside a minified upstream artifact, so it
broke every few releases, and the daily in-place update re-shipped an unpatched binary.
The job now belongs to [the supervisor](#the-supervisor), which strips the `Ctrl+Z` byte
from forwarded input and swallows `SIGTSTP`/`SIGQUIT` from outside Claude Code — nothing
upstream can break it, and you get a `⛔ Ctrl+Z ignored — use /exit to quit` notice on the
status line instead of a frozen session.

**The supervisor is on by default** (CCY 3.43.0) whenever the project ships one, precisely
because it now carries the only ctrl+z guard. The two ways to end up unguarded are an
absent supervisor and `--no-supervise`; both announce themselves at launch. See
[Turning it on](#turning-it-on).

---

## Troubleshooting

| Symptom                                       | Cause / fix                                                                                                                                                                                                                                        |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ccy: command not found`                      | Shell has not picked up the bashrc include. Open a new shell, or run the playbook.                                                                                                                                                                 |
| Refuses to start, mentions hostname           | `.claude/ccy/allowed-hostnames` restricts this project. Add your hostname or remove the file.                                                                                                                                                      |
| Refuses to start, "sensitive files tracked"   | Runtime state under `.claude/ccy/` has been committed. Untrack it (`git rm --cached`) and re-launch.                                                                                                                                               |
| Container image build fails                   | CCY prints an AI-assisted fix prompt. Recover with `ccy --disable-custom-docker` to get a session in the base image.                                                                                                                               |
| Token expired / rejected                      | `ccy --update-token=NAME`, or `ccy --create-token` for a fresh one.                                                                                                                                                                                |
| `git push` fails inside the container         | No key mounted, or the wrong one. Relaunch and select the right key, or use `--ssh-key`.                                                                                                                                                           |
| Git-over-SSH hangs on a restricted network    | Port 22 blocked — use `ccy --github-443`.                                                                                                                                                                                                          |
| `NETWORK ERROR: '...' has no internet access` | The reachability preflight needs an `alpine` pull and plain-http egress to `google.com`. If the network is known-good by other means (e.g. a fenced CI runner that proves its own egress beforehand), skip it: `CCY_SKIP_NETWORK_PREFLIGHT=1 ccy`. |
| Agent cannot reach the database               | Not on the network. `ccy --network <net>`, or `ccy --connect` from another terminal.                                                                                                                                                               |
| `Ctrl+Z` freezes the session                  | No supervisor in this project, or launched with `--no-supervise` — it owns the ctrl+z guard since CCY 3.42.0. See [ctrl+z and the supervisor](#ctrlz-and-the-supervisor).                                                                          |
| Session stalls with a full context window     | Enable [the supervisor](#the-supervisor) so it compacts automatically.                                                                                                                                                                             |
| Stale/orphaned containers                     | `ccy --top` to list and stop them.                                                                                                                                                                                                                 |
| Need to see what CCY itself is doing          | `ccy --debug` for interactive debug-layer selection.                                                                                                                                                                                               |

---

## Working Inside a CCY Container

Two things are worth knowing if you (or an agent) are working *in* a session:

- **This repo's rule: edit and commit only.** Never run Ansible playbooks from inside the
  container — it does not have the target users, groups, or system state. Deployment
  happens on the host. See [ContainerRules.md](../CLAUDE/ContainerRules.md).

- **The container documents itself.** An agent inside a session can read:

  ```bash
  cat /opt/claude-yolo/docs/CCY-GUIDE.txt          # container internals, file locations
  cat /opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt # Dockerfile authoring guide
  ```

---

## See Also

- [Containerization Technologies](containerization.md#custom-dockerfiles) — the
  custom-Dockerfile workflow and what the base image includes
- [CCY Debug Mounts](ccy-debug-mounts.md) — read-only host access for debugging
- [Claude Devtools (`ccdt`)](features/claude-devtools.md) — the separate helper for
  inspecting Claude Code sessions
- [Container Engines](../CLAUDE/ContainerEngines.md) — why Podman is the default
- [Container Rules](../CLAUDE/ContainerRules.md) — agent rules, version bumps, the retired ctrl+z patch
