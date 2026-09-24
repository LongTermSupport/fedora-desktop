# Plan 00138 — proposed enhancements

> **Under revision.** The owner has rejected P6 (fzf searches only the current shell's
> in-memory history), prefers Atuin, and has set two requirements: Ctrl+R must **search
> all history but rank** commands from the current directory and git repo higher
> (weighting, not filtering), and **no new security holes**. P6/P7 and D2–D4 are being
> rewritten against RESEARCH-ranking-and-security.md. P1–P5 are unaffected.

Grounded in [RESEARCH-current-config.md](RESEARCH-current-config.md) (what this host does,
findings F1–F8) and [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md) (what bash and
the Ctrl+R tools actually do, with sources). Nothing here is implemented; each item names
the decision it needs from the owner.

## The proposals

### P1 — Write every command to disk at the next prompt (fixes F1)

Append `history -a` to the `PROMPT_COMMAND` **array**, idempotently:

```bash
__history_append() { builtin history -a; }
[[ " ${PROMPT_COMMAND[*]-} " == *" __history_append "* ]] || PROMPT_COMMAND+=(__history_append)
```

Every command reaches the file within one prompt, in the order it was run. A SIGKILLed
shell loses at most the command in flight. Up-arrow stays per-terminal. `history -n` per
prompt is **not** proposed: it interleaves other terminals into up-arrow and works by line
offset, so it can skip or duplicate lines.

**Depends on P5.** Without P5, `ps1-prompt`'s scalar assignment overwrites element 0 — and
in a shell where the array was empty, element 0 is this hook.

### P2 — Timestamps and multi-line entries (fixes F2)

`HISTTIMEFORMAT='%F %T  '`, plus `shopt -s lithist` (safe only with timestamps, which
delimit entries) and `histverify` (`!!`/`!$` expansions are shown for review before
running). Older untimestamped lines stay as they are.

### P3 — No size limits, one escape hatch (fixes F3, F4)

- `HISTSIZE=-1`, `HISTFILESIZE=-1`. Ctrl+R (via fzf) searches the in-memory list, so a
  finite `HISTSIZE` is a limit on how far back Ctrl+R can see. The current file is
  ~620 KB; loading it in full is cheap.
- `HISTCONTROL=ignoreboth`: consecutive repeats dropped, and a **leading space keeps a
  command out of history**. Not `erasedups`: it only affects memory, not the file,
  and fzf's Ctrl+R de-duplicates at display time anyway.

### P4 — A history file a stray shell cannot truncate (fixes F5)

`HISTFILE=~/.local/state/bash/history`. A shell that never read the config (`bash --norc`,
`sudo -E bash`, a container sharing `$HOME`) truncates `~/.bash_history` to 500 lines on exit
— with P4 that is the old, abandoned file, not the real one.

IaC needs to: create the directory for the user and for root (bash never creates it and
**silently saves nothing** if it is missing), and seed the new file once from
`~/.bash_history` (`creates:`-guarded). A post-deploy check asserts the directory exists
and is writable, per the fail-fast rule.

### P5 — Fix `PROMPT_COMMAND` handling (fixes F6)

- `/var/local/ps1-prompt`: replace `PROMPT_COMMAND='ps1Prompt'` with an idempotent array
  append, as in P1.
- `play-basic-configs.yml`: stop injecting the tweaks `source` line into `~/.bash_profile`
  (user and root) and remove the existing block. Fedora's `~/.bash_profile` already
  sources `~/.bashrc`, so login shells (every tmux pane) currently load the tweaks twice.
  Whether root's `~/.bash_profile` also sources `~/.bashrc` must be confirmed first —
  triage covers only the desktop user.

Expected result: `setLastCommandState ps1Prompt setGitPrompt __history_append` (plus
Fedora's VTE/systemd hooks) in every kind of shell, each hook exactly once.

### P6 — Ctrl+R: fzf, searching every terminal's history (fixes F7)

Load fzf's packaged `/usr/share/fzf/shell/key-bindings.bash` (fzf is already installed from
Fedora; nothing new), and bind Ctrl+R to a wrapper that merges the file just before the
search:

```bash
__history_merge_then_fzf() { builtin history -a; builtin history -c; builtin history -r; __fzf_history__; }
bind -m emacs-standard -x '"\C-r": __history_merge_then_fzf'   # plus vi-insert, vi-command
```

What you get: a fuzzy list of the whole history, de-duplicated, newest first, with the
current line as the starting query, including what other open terminals have run (P1
put it on disk). Inside the list, Ctrl+R toggles sort order and Shift+Delete removes an
entry from history. Guarded with `command -v fzf` so shells without fzf keep stock
Ctrl+R.

Side effect: after a search, up-arrow in that shell walks the merged global order. If that
proves annoying, `history -n` instead of `-c; -r` in the wrapper is cheaper and keeps it
mostly local.

### P7 (later, if wanted) — Atuin

Atuin 18.12.1 and `bash-preexec` are in the F44 repos. It adds per-directory / per-git-repo
filters, exit status and duration, and optional encrypted sync. The cost is a second store
and a DEBUG-trap hook that must be sourced last and interacts with every other
`PROMPT_COMMAND` user (and drops `ignorespace`). P1–P5 remain necessary with Atuin: they
fix the plain history file that shells without it still use.

## Where it goes in the IaC

All of P1–P4 and P6 replace the `#History` block in
`files/etc/profile.d/zz_lts-fedora-desktop.bash`; P6 goes inside its interactive-only
section, after `ps1-prompt` is sourced. P4's directory and seed, and P5's
`.bash_profile` change, are tasks in `play-basic-configs.yml`, which already owns those
files. No new playbook.

## Decisions for the owner

| #   | Question                                                                                 | Recommendation                                     |
| --- | ---------------------------------------------------------------------------------------- | -------------------------------------------------- |
| D1  | Adopt P1–P5 (durable, timestamped, unlimited, truncation-proof, clean `PROMPT_COMMAND`)? | Yes: these fix the "weird" behaviour               |
| D2  | Ctrl+R tool: fzf (P6), Atuin (P7), or both in sequence?                                  | fzf now; Atuin only if directory context is missed |
| D3  | fzf's Ctrl+T (file picker) and Alt+C (cd picker) bindings too, or Ctrl+R only?           | Ctrl+R only; they override readline keys           |
| D4  | Plan 027 (Atuin): cancel as superseded, or keep as the P7 follow-up?                     | Keep, re-scoped to depend on this plan             |
