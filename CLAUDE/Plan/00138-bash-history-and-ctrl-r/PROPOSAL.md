# Plan 00138 — proposed enhancements

Grounded in three research documents:

- [RESEARCH-current-config.md](RESEARCH-current-config.md) — what this host does (findings F1–F8)
- [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md) — how bash history and the Ctrl+R tools behave
- [RESEARCH-ranking-and-security.md](RESEARCH-ranking-and-security.md) — cwd-weighted ranking
  (requirement A) and the security of each option (requirement B), from the tools' source

**Decided by the owner** (see the table at the end): P1–P5 adopted; recorder **R2**
(Atuin "sounds dodgy"); fzf accepted as the picker only; Plan 027 (Atuin) cancelled.
Implementation is tracked in [PLAN.md](PLAN.md) Phase 3.

## Owner requirements

- **Rejected:** fzf's stock Ctrl+R. It searches only the current shell's in-memory history.
- **A — weighting, not filtering.** Ctrl+R always searches **all** history from every
  terminal. Commands run in the current directory rank first, then the current git repo,
  then everything else. It must work before a single character is typed.
- **B — no new security holes.**

## Part 1: fix bash history itself (needed whatever Ctrl+R becomes)

These fix the "weird" behaviour. They also feed every ranking option below, and they are
what any shell without the Ctrl+R tool (root, containers, a rescue shell) falls back on.

### P1 — Write every command to disk at the next prompt (F1)

Add a hook to the `PROMPT_COMMAND` **array**, once. Before appending, it drops the newest
entry if it starts with a space. That keeps the leading-space escape hatch working even
if a preexec library strips `ignorespace` (bash-preexec does; research §4.4):

```bash
__history_append() {
    local last
    last=$(HISTTIMEFORMAT='' builtin history 1)
    last=${last#*[[:digit:]][* ] }
    [[ $last == ' '* ]] && builtin history -d -1
    builtin history -a
}
[[ " ${PROMPT_COMMAND[*]-} " == *" __history_append "* ]] || PROMPT_COMMAND+=(__history_append)
```

Every command reaches the file within one prompt, in the order it was run. A shell killed
with SIGKILL loses at most the command in flight. Up-arrow stays per-terminal. **Depends on
P5**, which stops `ps1-prompt` overwriting array element 0.

### P2 — Timestamps and multi-line entries (F2)

`HISTTIMEFORMAT='%F %T  '`, `shopt -s lithist histverify`. With `histverify`, `!!` and `!$`
are shown for review before they run.

### P3 — No size limits; the leading-space escape hatch works (F3, F4)

`HISTSIZE=-1`, `HISTFILESIZE=-1`, `HISTCONTROL=ignoreboth`.

### P4 — A history file a stray shell cannot truncate (F5)

`HISTFILE=~/.local/state/bash/history`, applied only when that directory is owned by the
current user. A root shell that kept your `HOME` (`sudo -E`) therefore cannot write
root-owned files into it. IaC creates the directory `0700` for the user and for root, and
seeds the file `0600` once from `~/.bash_history`. A post-deploy check fails if the
directory is missing, since bash silently saves nothing without it.

### P5 — Fix `PROMPT_COMMAND` (F6)

- Make `ps1-prompt` append to the array idempotently, instead of assigning a scalar.
- Stop sourcing the tweaks file from `~/.bash_profile`, which already sources `~/.bashrc`.
  Confirm root's `~/.bash_profile` first.

### Root

Root gets P1–P5 and **no** Ctrl+R tool. Root's history is the audit trail most worth
keeping, and a recorder running as root adds risk for no gain.

## Part 2: Ctrl+R with directory and repo weighting

### What the research established

- **Atuin cannot do A.** Every current search mode ranks by match quality, recency and
  frequency. The current directory and repo can only *filter*.

  - The one exception, `search_mode = "skim"` on Fedora's 18.12.1, weights by
    directory distance, but only mildly (≤ 1.4×). It does nothing on an empty query and
    has no repo term.
  - Upstream removed `skim` in 18.19.0. When Fedora upgrades, the weighting would vanish
    silently.
  - No upstream issue or PR proposes cwd-weighted ranking.

- **McFly does rank by directory** over all history, even on an empty query. But it is
  unpackaged and thinly maintained. It also takes over the history file and writes it
  without timestamps, undoing P1–P3.

- **RESH** (abandoned, unauthenticated localhost HTTP daemon) and **hiSHtory** (syncs to a
  hosted backend by default) are rejected on security.

- **No off-the-shelf tool meets A.** Meeting it means a small ranker this repo owns. That
  ranker needs two things:

  - a history store that records **the directory each command ran in**, which plain bash
    history does not;
  - a picker that shows a list in the order given.

  fzf (already installed) is that picker. Here it only **draws the list**: the data is the
  full history of every terminal, ranked by us. That is unlike the rejected setup, where fzf
  searched one shell's memory.

### The ranker (common to both options below)

Bound to Ctrl+R with `bind -x`. It reads `directory, exit status, command` rows,
newest first, and scores each distinct command:

1. ran in **this directory**;
2. ran inside **this git repo** (`git rev-parse --show-toplevel`);
3. everything else.

Within each tier, frequency and recency order the rows, and commands that always failed
sink. The rows go to `fzf --tiebreak=index` (or `--no-sort`) with the current line as the
query. The result replaces the line for review. It never runs on Enter.

### Option R1 — Atuin as the recorder

Atuin 18.12.1 and bash-preexec from the Fedora repos. The ranker reads
`atuin search --filter-mode global --include-duplicates --print0 --format '{directory}\t{exit}\t{command}'`.

- **Extras:** duration, session and host are recorded; Atuin's own search screen stays
  available on another key; encrypted sync becomes possible later.

- **Security surface added:**

  - **Every command line is passed as an argument** to a short-lived `atuin` process, and
    other local users can read those through `/proc`. That includes shell-only lines such
    as `export TOKEN=…` and leading-space lines. This is a residual risk to accept, because
    Atuin cannot avoid it.
  - A plaintext SQLite store holding directories, hosts and timings.
  - A DEBUG trap from bash-preexec 0.6.0.
  - `bash-preexec` strips `ignorespace`, which P1 fixes.
  - `HISTIGNORE` must be emptied, or Atuin records the previous command again in the wrong
    directory.

- **Hardening, from IaC before first run:**

  - `update_check = false` (otherwise Fedora's build contacts `api.atuin.sh`);
  - `auto_sync = false`;
  - the daemon off;
  - `enter_accept = false`;
  - extra `history_filter` / `cwd_filter` patterns for passwords, bearer headers, URL
    credentials, `~/.ssh` and `~/.gnupg`;
  - `0700`/`0600` modes;
  - after `atuin import`, an `atuin history prune`, because the import applies no filter;
  - no `atuin-all-users` package, and no Atuin for root.

  The full config is in research §4.8.

- **Maintenance:**

  - Re-audit the config on each Fedora Atuin bump; newer upstream adds AI features, output
    capture and log files.
  - The ranker depends only on Atuin's documented CLI, not its schema.

### Option R2 — a recorder this repo owns, no new binary

One more `PROMPT_COMMAND` element, next to P1. Using only bash builtins, it appends
`epoch <TAB> exit <TAB> cwd <TAB> command` for the command just run to
`~/.local/state/bash/context` (`0600`, in the `0700` directory from P4). The ranker reads
that file.

- **Security surface added:** none beyond a second `0600` file beside the history file.
  - No new binary, network code, daemon, DEBUG trap or preexec library.
  - Command lines never pass through another process's arguments: `printf` is a builtin.
  - `ignorespace` and `HISTIGNORE` keep working unchanged. The entry is taken from the same
    `history 1` that P1 has just cleaned, and it is recorded **only when the history number
    has advanced** since the last prompt. An ignored command does not enter history, so the
    previous command is not recorded again in the wrong directory. This is the trap Atuin
    falls into (research §4.4). Pressing Enter on an empty line does not advance it either.
- **What it lacks next to R1:**
  - no command duration, session or host columns;
  - no sync;
  - no alternative search screen.
  - The directory weighting starts empty: old history has no directory, just as with
    Atuin's import.
- **What the repo takes on:**
  - a short recorder;
  - a rule for exit status: capture `$?` in the first array element, and P5 makes the
    order dependable;
  - the file grows without limit, as in P3.
  - It is tested with the ranker (bats-style tests against fixture files).

### Comparison

|                                        | R1 Atuin + ranker                         | R2 own recorder + ranker |
| -------------------------------------- | ----------------------------------------- | ------------------------ |
| Meets A (weighting, empty query)       | Yes (via the ranker)                      | Yes (same ranker)        |
| New packages                           | atuin, bash-preexec                       | none                     |
| Command lines in other processes' argv | **Yes** (residual risk)                   | No                       |
| DEBUG trap / ignorespace stripped      | Yes / fixed by P1                         | No / no                  |
| Network code on the machine            | Present, disabled by config               | None                     |
| Extra data                             | duration, session, host; sync possible    | —                        |
| Upkeep                                 | config re-audit per Atuin release; ranker | recorder + ranker        |

**Recommendation: R2.** Requirement B is decisive. R1's Atuin would be reduced to a recorder
behind our ranker, and it is exactly the recorder that brings the argv exposure, the DEBUG
trap and the network code. R2 records the three fields the ranker needs, and nothing else.
If duration or sync is wanted later, Atuin can be added **alongside** R2 (Plan 027): the
ranker's input is two columns wide either way.

**Before committing either option, prototype the ranker** against a copy of real history
and time it at 15k and 100k rows. Neither research pass measured it.

## Where it goes in the IaC

- P1–P4, the recorder and the ranker replace the `#History` block of
  `files/etc/profile.d/zz_lts-fedora-desktop.bash`. The ranker is placed in the
  interactive-only section, after `ps1-prompt`.
- The P4 directories and seed, and the P5 `.bash_profile` change, go in
  `play-basic-configs.yml`, which already owns those files.
- The ranker is large enough to live in its own deployed file (a `~/.bashrc-includes`
  snippet or a `/var/local` script), with tests under `tests/`.
- R1 would add packages and an IaC-owned `~/.config/atuin/config.toml`.
- No new playbook for R2.

### Prototype results (Task 2.3)

The prototype ranker, now
[`bash-history-rank`](../../../files/home/.local/bin/bash-history-rank), passed the fixture
checks now kept in
[`scripts/test-bash-history-search.bash`](../../../scripts/test-bash-history-search.bash):

- this directory first, newest first;
- always-failing commands sink within their tier;
- then the same repo, then everything else;
- tabs, multi-line entries and pre-recorder history are all handled;
- no duplicates.

Timings on the host used synthetic records built from a copy of the real history. The
copy's commands were reused, with invented directories:

Two runs of [`ranker-timing.bash`](ranker-timing.bash), the second under load from other
work on the same host:

| Records | Rank only    | Rank + `fzf --filter` |
| ------- | ------------ | --------------------- |
| 15,000  | 153 / 179 ms | ~140 / ~200 ms        |
| 100,000 | 435 / 707 ms | ~440 / ~700 ms        |

`git rev-parse` costs 2–4 ms. At today's size the ranker is fast; at 100k records it is
noticeable. Task 3.5 accepts that for now (YAGNI). The remedy, when it matters, is to
compact the context file to one row per command and directory; there are only about 5,700
distinct commands today. A compiled ranker (Rust/Go) was considered and not chosen: it
would add a build toolchain or pinned binary and a dependency supply chain for a speed-up
compaction delivers without either, and the ranker's plain interface (records in, ranked
list out) keeps a later rewrite cheap.

## Decisions (answered by the owner)

| #   | Answer                                                       |
| --- | ------------------------------------------------------------ |
| D1  | Yes: P1–P5 for the user and root                             |
| D2  | R2, the repo-owned recorder. Atuin rejected ("sounds dodgy") |
| D3  | Yes: fzf as the picker only                                  |
| D4  | Plan 027 cancelled ("atuin is dead")                         |

### The questions as asked

| #   | Question                                                                                                                | Recommendation                                                  |
| --- | ----------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| D1  | Adopt P1–P5 (durable, timestamped, unlimited, truncation-proof history; clean `PROMPT_COMMAND`), for the user and root? | Yes                                                             |
| D2  | Recorder: R1 (Atuin) or R2 (repo-owned, builtins only)?                                                                 | R2, because of requirement B                                    |
| D3  | Accept fzf as the **picker** for a ranked list of all history (not its stock in-memory Ctrl+R)?                         | Yes. It is already installed, and the ranking and data are ours |
| D4  | Plan 027 (Atuin): cancel, or keep as an optional later add-on alongside R2?                                             | Keep, re-scoped to depend on this plan; do not start it now     |
