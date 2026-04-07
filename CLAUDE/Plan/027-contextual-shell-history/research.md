# Contextual Shell History — Tool Research

**Date**: 2026-04-04

## Problem Statement

Standard bash history is flat — no directory context, no metadata, no intelligent ranking.
Goal: commands run in a project directory should take precedence when recalling history.

## Tools Evaluated

### 1. Atuin — The Clear Winner

**Repository**: github.com/atuinsh/atuin | **Stars**: ~29k | **Language**: Rust
**Latest release**: v18.13.6 (2026-03-27) | **Actively maintained**: Very active

#### Directory/Project Context

Replaces shell history with a SQLite database storing rich context per command:
working directory, exit code, duration, hostname, session ID, timestamp.

**Five filter modes** (cycle with Ctrl+R):
- **Global** — all history across all machines
- **Host** — current machine only
- **Session** — current shell session only
- **Directory** — commands run in the current working directory
- **Workspace** — commands run anywhere within the current git repo

The **workspace mode** is the killer feature — surfaces commands from any subdirectory
of the current git repo. Exactly what we want.

#### Search Model

Four search modes (toggle with Ctrl+S):
- Fuzzy (fzf-like, default)
- Prefix
- Fulltext (substring)
- Skim

Configurable default `filter_mode` and `search_mode` in `~/.config/atuin/config.toml`.
`cwd_filter` accepts regexes to exclude directories from recording.

#### Sync & Privacy

- E2E encrypted sync (AES) — key never leaves your machine
- Free hosted sync at atuin.sh, or self-host (binary/Docker + PostgreSQL)
- Server sees only encrypted blobs
- Sync is optional — works fine local-only

#### Bash Integration

```bash
eval "$(atuin init bash)"
```

Requires one of two preexec backends:
- **ble.sh** (recommended) — accurate timing, proper `ignorespace` support
- **bash-preexec** — simpler but `ignorespace` not fully honored

This is a notable difference from zsh/fish which have native hooks.

#### Trade-offs

- Bash integration slightly less polished than zsh/fish (needs preexec shim)
- Handles multiline commands correctly (McFly doesn't)
- Statistics and analytics about command usage
- Import from other tools (McFly, bash history, zsh history, fish)

---

### 2. McFly — Neural Network Ranking

**Repository**: github.com/cantino/mcfly | **Stars**: ~7.7k | **Language**: Rust
**Maintenance**: Author seeking co-maintainers

#### Directory Context

Stores CWD, timestamp, exit code in SQLite. Directory is a **first-class ranking signal**
in the neural network — prioritises commands previously run in the same directory.

The neural network ranks by:
1. Current working directory (strong weight)
2. Command sequence context (preceding 2-3 commands)
3. Frequency and recency
4. Historical exit status (deprioritises failures)
5. Previous McFly selections (learns from picks)

#### Key Difference from Atuin

No explicit filter modes — directory influence is **implicit** in neural ranking.
You cannot say "show me only commands from this directory."

#### Trade-offs

- No sync (purely local)
- Maintenance concerns (seeking co-maintainers)
- Multiline commands broken (split incorrectly)
- Fuzzy search disabled by default (requires `MCFLY_FUZZY=2`)

---

### 3. hishtory — Structured Query Language

**Repository**: github.com/ddworken/hishtory | **Stars**: ~3.1k | **Language**: Go
**Latest release**: v0.335 (Feb 2025) | **Actively maintained**: Yes

#### Directory Context

Stores CWD, hostname, exit code, duration, timestamp, username.
CWD is queryable metadata.

#### Search/Query Model (unique strength)

Structured query language for history:
- `psql` — simple text match
- `hostname:my-server` — filter by hostname
- `exit_code:127` — filter by exit code
- `cwd:/path/to/project` — **filter by directory**
- `before:2024-01-01` / `after:2024-01-01` — date ranges

#### Sync & Privacy

- E2E encrypted (AES-GCM)
- Self-hostable server
- Optional AI integration (ChatGPT) for command suggestions

#### Trade-offs

- No git workspace mode (unlike Atuin)
- Go binary (slightly larger than Rust)
- Smaller community than Atuin

---

### 4. hstr — Not Relevant

TUI overlay on standard `.bash_history`. No directory awareness, no metadata.
Only useful as a better Ctrl+R for people who don't want to change infrastructure.

### 5. DIY Per-Directory PROMPT_COMMAND

Uses `PROMPT_COMMAND` to switch `HISTFILE` based on `$PWD`:

```bash
PROMPT_COMMAND='
  if [[ "$PWD" != "$LAST_DIR" ]]; then
    history -w; history -c
    export HISTFILE="$PWD/.bash_history"
    history -r
    LAST_DIR="$PWD"
  fi
'
```

**Trade-offs**: Fragile, loses global context, no metadata, `.bash_history` files
scattered everywhere, potential for history loss with multiple terminals. Abandoned
projects (bash-per-directory-history, last commit 2017).

### 6. fzf-Based Solutions

fzf has **no built-in directory awareness**. Open issue #3539 requests it but
fundamental limitations prevent it (child shell can't access zle widgets).
Not viable as a standalone solution.

### 7. Bashhub — Privacy Concern

Cloud-first, **NOT end-to-end encrypted** — server can read your commands.
Self-hosted server exists but the design is cloud-dependent. Avoid.

---

## Comparison Matrix

| Feature | Atuin | McFly | hishtory |
|---------|-------|-------|----------|
| **Directory filter** | Explicit modes (dir + workspace) | Implicit neural ranking | Queryable CWD field |
| **Git workspace mode** | Yes | No | No |
| **Search model** | Fuzzy/prefix/fulltext + filter modes | Neural network | Structured query language |
| **Sync** | E2E encrypted, self-hostable | None (local only) | E2E encrypted, self-hostable |
| **Bash integration** | Good (needs bash-preexec/ble.sh) | Good (PROMPT_COMMAND) | Good |
| **Metadata** | CWD, exit, duration, host, session | CWD, exit, timestamp, sequence | CWD, exit, duration, host, user |
| **Privacy** | Excellent | Excellent | Excellent |
| **Maintenance** | Very active (~29k stars) | Seeking co-maintainers | Active (~3.1k stars) |
| **Multiline commands** | Correct | Broken | Unknown |

## Recommendation

**Atuin** is the clear choice:
1. Only tool with explicit directory AND git-workspace filter modes
2. Most active development, largest community
3. E2E encrypted sync (optional)
4. Rich metadata for every command
5. Multiple search modes

The workspace mode alone makes it the winner — surfacing all commands from anywhere
in the current git repo is exactly the "folder-based history" requirement.
