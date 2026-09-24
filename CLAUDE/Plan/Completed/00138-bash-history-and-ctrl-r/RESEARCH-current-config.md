# Plan 00138 — review of the current bash history configuration

Evidence comes from [`triage.bash`](triage.bash) (leg: [`probe-history.bash`](probe-history.bash)),
run on the host. The probe never reads a history line; it reports counts, sizes and
timestamps only. Raw reports stay under `untracked/plan-runs/`.

## Where the configuration lives

| Source                                                                          | What it sets                                                                                                                                               |
| ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `files/etc/profile.d/zz_lts-fedora-desktop.bash` (via `play-basic-configs.yml`) | `histappend`, `cmdhist`, `HISTCONTROL=ignoredups`, `HISTFILESIZE=20000`, `HISTSIZE=10000`, `HISTIGNORE="&:ls:[bf]g:exit"`; sources `/var/local/ps1-prompt` |
| `/var/local/ps1-prompt` (same play)                                             | `PROMPT_COMMAND='ps1Prompt'` — a **scalar** assignment                                                                                                     |
| Fedora `/etc/bashrc`, `/etc/profile`                                            | `histappend`; `HISTSIZE=1000` if unset; builds `PROMPT_COMMAND` as an **array**                                                                            |
| `/etc/profile.d/vte.sh`, `80-systemd-osc-context.sh`                            | append to the `PROMPT_COMMAND` array                                                                                                                       |
| `~/.bash-git-prompt/gitprompt.sh` (`play-git-configure-and-tools.yml`)          | appends `setGitPrompt`, prepends `setLastCommandState`                                                                                                     |

`play-basic-configs.yml` injects `source /etc/profile.d/zz_lts-fedora-desktop.bash` into
**both** `~/.bashrc` and `~/.bash_profile` (and root's). Fedora's `/etc/profile` only
auto-sources `*.sh`, so the `.bash` file is reached through those blocks alone.

The deployed tweaks file is byte-identical to the repo copy, so what follows is a defect in
the repo's configuration, not drift.

## Findings

### F1 — History is written only when a shell exits

Nothing runs `history -a` per prompt. The effective `PROMPT_COMMAND` holds only prompt
functions. At the time of the triage run:

- 51 bash processes were running for the user, across 6 tmux sessions;
- `~/.bash_history` had last been written 44,922 s (about 12.5 h) earlier;
- **27 of those shells had started before that write** — every command typed in them
  since then existed only in memory.

Consequences, all matching "history behaves weirdly":

- **Ctrl+R in a new terminal cannot find anything typed in a terminal that is still open.**
  With many long-lived tmux panes, that is most recent work.
- A shell that dies without a clean exit loses its whole session's history. Whether
  a terminal/tmux teardown counts as clean is in [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md).
- Each shell appends its block when it exits, so the file is in **exit order**, not in the
  order commands were run. Blocks from a week-old pane land after today's.

### F2 — No timestamps

`HISTTIMEFORMAT` is unset: 0 timestamp lines in a 14,228-line file. There is no way to
tell when a command ran, to order the file, or to import it into a timestamped tool with
real times.

### F3 — Ctrl+R cannot reach a third of the file, and a lot of what it can reach is repeats

- The file has 14,228 lines but `HISTSIZE=10000`, so every new shell loads only the newest
  10,000. The oldest ~4,200 are on disk but unsearchable.
- `HISTFILESIZE=20000` will start deleting the oldest lines at the next ~5,800 commands.
- Only 5,722 lines are distinct; 1,234 commands appear more than once.
  `ignoredups` drops only *consecutive* repeats, so the in-memory budget and the Ctrl+R
  stepping (repeated `C-r` walks through every earlier copy) are spent on repeats.

### F4 — Commands with a leading space are still recorded

`HISTCONTROL=ignoredups` lacks `ignorespace`, so the usual "prefix a space to keep a
secret-bearing command out of history" habit does nothing. The `&` in `HISTIGNORE`
duplicates `ignoredups`.

### F5 — One unconfigured shell can truncate the shared file

Every value is set in init files. Any interactive bash that does not read them
(`bash --norc`, a shell from a tool that bypasses `~/.bashrc`) runs with bash's default
of 500 lines and **truncates `~/.bash_history` to 500 lines on exit**. No sign of that
today (14,228 lines), but it is one stray shell away. Recommended mitigations are in the
research document.

### F6 — `PROMPT_COMMAND` is corrupted by `ps1-prompt`; login shells are worse

`/var/local/ps1-prompt` assigns `PROMPT_COMMAND='ps1Prompt'`. On bash 5.3 Fedora has
already made `PROMPT_COMMAND` an array, and a scalar assignment to an array overwrites
**element 0** rather than replacing or appending. Observed:

| Shell                                                                    | Effective `PROMPT_COMMAND`                                                   |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
| `bash -i` (Ptyxis tab)                                                   | `setLastCommandState`, `ps1Prompt`, `setGitPrompt`                           |
| `bash -l -i` (every tmux pane: `default-command ''` starts login shells) | `ps1Prompt`, `ps1Prompt`, `__systemd_osc_context_precmdline`, `setGitPrompt` |

In login shells `~/.bash_profile` sources `~/.bashrc` and then the tweaks file **a second
time**, so `ps1-prompt` runs twice: `ps1Prompt` runs twice per prompt and git-prompt's
`setLastCommandState` is lost. (The probe ran without a VTE terminal, so whether VTE's
own hooks are also overwritten in a real Ptyxis tab is not established.)

This matters here because **any per-prompt `history -a` must survive this.** Put into
`PROMPT_COMMAND` the naive way, it would be overwritten in exactly the shells that need
it.

### F7 — Ctrl+R is stock readline; fzf is installed but not wired

- `"\C-r": reverse-search-history` — the plain incremental search: one match at a time,
  exact substring, no list.
- `fzf` 0.74.4 (Fedora package) is installed (by `play-claude-yolo.yml` and
  `tasks/deploy-freeze-lib.yml`) and ships `/usr/share/fzf/shell/key-bindings.bash`; `fzf --bash`
  is supported. Nothing sources either.
- `atuin`, `mcfly`, `hstr`, `bash-preexec` and `ble.sh` are not installed.

### F8 — An earlier plan already proposes Atuin

[Plan 027](../027-contextual-shell-history/PLAN.md) (Not Started) proposes replacing
Ctrl+R with Atuin. It does not look at the underlying bash settings, so F1–F6 would
remain for `~/.bash_history` and for any shell without Atuin. How the two plans should
relate is a decision in [PLAN.md](PLAN.md).
