# Research: optimal bash history config and Ctrl+R choice

Scope: Fedora 44, bash 5.3, many concurrent interactive shells (GNOME Terminal tabs, tmux
panes, containers). Baseline being improved on: `histappend`, `cmdhist`,
`HISTCONTROL=ignoredups`, `HISTSIZE=10000`, `HISTFILESIZE=20000`,
`HISTIGNORE="&:ls:[bf]g:exit"`, no `PROMPT_COMMAND` history handling, no
`HISTTIMEFORMAT`, stock readline Ctrl+R.

Sources were checked against the official docs where reachable. Facts marked **(host)**
were read from the installed packages on a Fedora 44 machine (`rpm -ql fzf`,
`dnf repoquery`, `man bash` for bash 5.3.9), not from the web.

---

## 1. Plain-bash history settings

### 1.1 What bash does by default (the root of every multi-terminal problem)

- Each interactive shell reads `$HISTFILE` **once** at startup into an in-memory list, and
  writes back **only on exit**: "When a shell with history enabled exits, bash copies the
  last `$HISTSIZE` entries from the history list to `$HISTFILE`. If the histappend shell
  option is enabled … bash appends … otherwise it overwrites the history file … After
  saving the history, bash truncates the history file to contain no more than
  HISTFILESIZE lines." — `man bash`, HISTORY section (host); online:
  <https://www.gnu.org/software/bash/manual/html_node/Bash-History-Facilities.html>
- Consequences with N open terminals:
  - A command typed in terminal A is invisible to terminal B (and to any new terminal)
    until A exits.
  - If A dies without saving (see 1.7), its whole session is lost.
  - Every exiting shell re-truncates the file, so concurrent exits race on a rewrite.

### 1.2 `history -a` / `-n` / `-c -r` in `PROMPT_COMMAND`

From `man bash`, `history` builtin (host); online:
<https://www.gnu.org/software/bash/manual/html_node/Bash-History-Builtins.html>

- `-a` "Append the 'new' history lines to the history file. These are history lines
  entered since the beginning of the current bash session, but not already appended."
- `-n` "Read the history lines not already read from the history file and add them to the
  current history list."
- `-c` clears the in-memory list; `-r` reads the whole file and appends it to the list.

| Pattern in `PROMPT_COMMAND`          | Durability                           | Cross-terminal visibility                     | Cost / side effects                                                                                                                                                                                                                                             |
| ------------------------------------ | ------------------------------------ | --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `history -a`                         | Every command on disk at next prompt | New shells see everything; open shells do not | Negligible. Up-arrow stays per-terminal (usually what people want)                                                                                                                                                                                              |
| `history -a; history -n`             | Same                                 | Live, every prompt                            | Up-arrow interleaves other terminals' commands. `-n` works by line offset, so it can skip or duplicate lines when another shell appends between this shell's read and its own `-a`, or when anything truncates the file. Recommended by hstr's generated config |
| `history -a; history -c; history -r` | Same                                 | Live and exact (full reload)                  | Re-reads the entire file at **every prompt**, so cost grows with file size. Loses per-terminal up-arrow ordering entirely                                                                                                                                       |

The trade-off that falls out: **`history -a` every prompt** for durability, and do the
cross-terminal merge **only when the user asks to search** (inside the Ctrl+R widget, see
section 3). That gets durability, per-terminal up-arrow and an exact global search.

### 1.3 `PROMPT_COMMAND` array form (bash 5.1+)

- bash 5.1 NEWS: "PROMPT_COMMAND: can now be an array variable, each element of which can
  contain a command to be executed" — <https://tiswww.case.edu/php/chet/bash/NEWS>
- `man bash` (5.3, host): "If this variable is set, and is an array, the value of each set
  element is executed as a command prior to issuing each primary prompt."
- **Fedora already uses the array form (host):** `/etc/bashrc` does
  `declare -a PROMPT_COMMAND`, and `/etc/profile.d/vte.sh` and
  `/etc/profile.d/80-systemd-osc-context.sh` do `PROMPT_COMMAND+=(…)`.
- So: add history handling with `PROMPT_COMMAND+=(__history_append)`, never
  `PROMPT_COMMAND="history -a; $PROMPT_COMMAND"`. A later tool that assigns
  `PROMPT_COMMAND` as a scalar (a prompt framework such as bash-git-prompt may) only
  replaces element 0, so an appended element survives. Anything that does
  `unset PROMPT_COMMAND` or `PROMPT_COMMAND=(…)` would drop it, so the history snippet
  should be sourced **after** the prompt framework.

### 1.4 Sizes: unlimited, and the 500-line truncation trap

From `man bash` (host):

- `HISTSIZE`: "Numeric values less than zero result in every command being saved on the
  history list (there is no limit). The shell sets the default value to 500 after reading
  any startup files."
- `HISTFILESIZE`: "Non-numeric values and numeric values less than zero inhibit
  truncation. The shell sets the default value to the value of HISTSIZE after reading any
  startup files." Negative-means-unlimited arrived in bash 4.3 (NEWS, link above).

**The gotcha.** Any interactive bash that does not run your `~/.bashrc` ends up with
`HISTSIZE=500`, `HISTFILESIZE=500` and `HISTFILE=~/.bash_history`. When it exits it
appends and then **truncates `~/.bash_history` to 500 lines**, destroying years of
history in one go. Typical triggers:

- `bash --norc`, `bash --noprofile --norc`, `env -i bash -i`
- A container or sandbox that bind-mounts `$HOME` but starts bash with a different (or no)
  rc file
- Privilege tools told to preserve the environment (for example `sudo -E bash`), so a
  root shell runs with your `HOME` and without your rc
- Any other shell (older bash, a rescue shell) that uses the default filename

Non-interactive scripts are normally safe: history is off in non-interactive shells unless
the script runs `set -o history`. Note, though, that since bash 4.2 "the shell saves the
command history in any shell for which history is enabled and HISTFILE is set, not just
interactive shells" (NEWS).

**The fix: use a non-default `HISTFILE`.** A shell that did not read your rc file never
learns the custom name, so it truncates the default `~/.bash_history` and leaves the real
store alone. An XDG-style location such as `~/.local/state/bash/history` is common. Caveats:

- bash does not create the parent directory. If it is missing, history is silently not
  saved ("if the history file is unwritable, the history is not saved"). Create the
  directory in IaC, and do not rely on the shell.
- Migrate once: seed the new file from the old `~/.bash_history` (IaC, `creates:`-guarded),
  then leave the old file to the shells that do not read the rc.
- Tools that assume `~/.bash_history` (hstr defaults to `$HISTFILE`, so is fine) must
  follow `$HISTFILE`.

**In-memory size.** `HISTSIZE=-1` loads the whole file into every shell. That costs memory
and startup time in proportion to file size, but it matters here: fzf's Ctrl+R searches
the **in-memory list** (`fc -l`, see 2.1), so a finite `HISTSIZE` caps how far back
Ctrl+R can see. Use unlimited for both, or a large finite `HISTSIZE` (for example
`100000`) if startup ever becomes noticeable.

### 1.5 `HISTCONTROL`: `ignoreboth` vs `erasedups`

`man bash` (host): `ignorespace` drops lines beginning with a space; `ignoredups` drops a
line matching the previous entry; `ignoreboth` is both; `erasedups` "causes all previous
lines matching the current line to be removed from the history list before that line is
saved."

- **`ignoreboth`: recommended.** A leading space becomes a deliberate "do not record" escape
  hatch, useful for commands that carry secrets.
- **`erasedups`: not recommended here.**
  - It acts only on the **in-memory** list. With `history -a` the file still accumulates
    duplicates, so it does not deliver a deduplicated file.
  - It rewrites positions in the list that `history -a` / `-n` bookkeeping depends on, so
    it adds another source of the line-offset imprecision described in 1.2.
  - It destroys the chronological record ("when did I last run X, and what came before
    it").
  - fzf's Ctrl+R deduplicates at display time anyway (see 2.1), so the search UX gets the
    benefit without the cost.

### 1.6 `HISTTIMEFORMAT`, `cmdhist`, `lithist`, `histverify`, `histreedit`

- `HISTTIMEFORMAT`: "If this variable is set, the shell writes time stamps to the history
  file". "When present, history timestamps delimit history entries, making multi-line
  entries possible" (`man bash`, host). Setting it (for example `'%F %T  '`) adds a
  `#<epoch>` line before each entry. Benefits: `history` shows when; entries survive a
  reload intact. Cost: the file roughly doubles in line count, and naïve tools that
  `cat ~/.bash_history` see `#169…` lines. fzf's widget reads through `fc`, so it is
  unaffected.
- `cmdhist` (already on): saves a multi-line command as one entry.
- `lithist`: with `cmdhist`, keeps embedded newlines instead of `;`. **Only safe together
  with `HISTTIMEFORMAT`**: without timestamps a reload splits the entry back into separate
  lines. fzf's Ctrl+R renders multi-line entries properly (fzf CHANGELOG: "CTRL-R properly
  supports multi-line commands", and multi-line display since 0.53).
- `histverify`: `!!` / `!$` expansions are loaded into the line for review instead of
  executed immediately. Cheap safety, recommended.
- `histreedit`: a failed history substitution is kept for re-editing. Harmless, optional.

### 1.7 What kills history (and what does not)

bash source, `sig.c`, `termsig_handler`
(<https://cgit.git.savannah.gnu.org/cgit/bash.git/tree/sig.c>):

```c
/* If we don't do something like this, the history will not be saved when
   an interactive shell is running in a terminal window that gets closed
   with the `close' button. ... */
if (interactive_shell && interactive && (sig == SIGHUP || sig == SIGTERM) && remember_on_history)
  maybe_save_shell_history ();
```

- **Closing a GNOME Terminal tab or window, or a crash of the terminal:** the pty master
  closes, so bash gets **SIGHUP** and saves history via the code above. The condition
  also requires `interactive`, so a shell busy with a foreground command at that moment
  is the least reliable case.
- **tmux `kill-pane` / `kill-session` / `kill-server`:** the pane's pty is torn down, so
  bash gets SIGHUP and behaves as above.
- **SIGTERM:** "When bash is interactive, in the absence of any traps, it ignores SIGTERM"
  (`man bash`, host). A logout that only SIGTERMs shells therefore relies on the pty
  hangup (SIGHUP) that follows when the terminal itself dies.
- **SIGKILL of bash itself** (`kill -9`, the OOM killer / systemd-oomd killing the scope,
  `podman kill` / container stop escalating to SIGKILL, power loss): **nothing is
  saved**. Without `history -a` in `PROMPT_COMMAND` the whole session is lost. With it,
  at most the command in flight is lost.
- **Concurrent exits with a finite `HISTFILESIZE`:** each shell truncates the file after
  appending, which means a read-and-rewrite. Several shells exiting together (tmux
  kill-server, logout) race on that rewrite. An unlimited `HISTFILESIZE` removes the
  truncation step, and with it the race.

Net: `history -a` every prompt plus an unlimited `HISTFILESIZE` makes the exit-time save a
formality, and the SIGHUP/SIGKILL questions stop mattering.

---

## 2. Ctrl+R options, ranked for this user

### 2.1 Rank 1: fzf's shipped bash key-bindings (already installed)

- **Fedora packaging (host, `fzf-0.74.4-1.fc44`):**
  - `/usr/share/fzf/shell/key-bindings.bash` (also `.zsh`, `.fish`, `.nu`)
  - `/etc/bash_completion.d/fzf` — this is fzf's `completion.bash` (`**<TAB>`), loaded by
    bash-completion
  - **No `/etc/profile.d` file**, so the key bindings are **not auto-sourced**, which is
    why Ctrl+R is still stock readline.
- **Loading.** Since fzf 0.48.0 the scripts are embedded in the binary:
  `eval "$(fzf --bash)"` (fzf CHANGELOG 0.48.0,
  <https://github.com/junegunn/fzf/blob/master/CHANGELOG.md>; README
  <https://github.com/junegunn/fzf#setting-up-shell-integration>).
  - On Fedora, `fzf --bash` emits key-bindings **and** completion, and completion is
    already loaded from `/etc/bash_completion.d/fzf`. Sourcing only
    `/usr/share/fzf/shell/key-bindings.bash` avoids loading completion twice.
  - Either route is fine. The packaged file is always version-matched to the binary
    because it comes from the same RPM.
- **Behaviour (read from the installed script, host):**
  - Deduplicates (`!$seen{$_}++`).
  - Starts with the current command line as the query.
  - `--scheme=history`.
  - `ctrl-r` inside the widget toggles sort; `alt-r` toggles raw mode.
  - Multi-select, and `shift-delete` deletes the entry from history. With `histappend`
    it rewrites the file (`history -w`), per fzf 0.71/0.72 CHANGELOG.
  - Uses `bind -x` on bash ≥ 4.
- **It searches the in-memory list** (`fc -lnr`), not the file. Commands from other open
  terminals are therefore only visible if this shell has re-read the file (see 1.2 and the
  wrapper in section 3).
- **Options:**
  - `FZF_CTRL_R_OPTS` for extra fzf flags. The README example adds a `ctrl-y` copy binding
    and a header. A `--preview 'echo {2..}' --preview-window down:3:wrap` style preview
    helps with long commands.
  - `FZF_CTRL_R_COMMAND=` (empty) opts out of the Ctrl+R binding (0.66.0). Likewise
    `FZF_CTRL_T_COMMAND=` / `FZF_ALT_C_COMMAND=` drop the Ctrl+T (overrides
    transpose-chars) and Alt+C (overrides capitalize-word) bindings if they are not
    wanted.
- **Pros:**
  - Zero new dependencies: already installed, one package, from Fedora.
  - The store stays **`$HISTFILE`**, plain bash history, with no database.
  - Degrades gracefully: guard with `command -v fzf` and shells without fzf (containers)
    keep stock readline Ctrl+R.
  - Tiny blast radius: only key bindings, no DEBUG trap, no `PROMPT_COMMAND`
    manipulation.
- **Cons:**
  - No per-directory, exit-status or duration context.
  - Cross-terminal freshness is the user's job (solved by the wrapper in section 3).
- **Risk of breaking the shell:** very low.

### 2.2 Rank 2: Atuin

- **Fedora availability (host `dnf repoquery`):** `atuin-18.11.0` (fedora) and
  `atuin-18.12.1` (updates) in the official F44 repos; `bash-preexec-0.6.0` is also
  packaged. Docs: <https://docs.atuin.sh/main/guide/installation/>
- **Store:** a SQLite database with extra context (cwd, exit status, duration, session,
  host). `atuin import auto` imports the existing history, and "old history file is not
  replaced" (<https://github.com/atuinsh/atuin>). bash keeps writing `$HISTFILE` in
  parallel, so the plain file remains a fallback.
- **Filter modes:** Ctrl+R cycles global / host / session / directory. Search is
  cross-terminal out of the box, because every shell writes to the DB immediately.
- **Sync:** optional and end-to-end encrypted, and self-hostable. Nothing leaves the
  machine unless you register or log in.
- **bash hooks:** bash has no native preexec, so Atuin needs one of these:
  - **ble.sh ≥ 0.4** ("Atuin works best in bash when using ble.sh").
  - **bash-preexec**, which uses the DEBUG trap and `PROMPT_COMMAND`. Documented
    limitations: "may experience some minor problems with the recorded duration and exit
    status", it "will stop honoring ignorespace" (space-prefixed commands can still reach
    bash history), and it "can't properly invoke the preexec hook for subshell commands,
    function definitions …". From 18.18.0, `atuin init bash` auto-loads a bundled
    bash-preexec; `ATUIN_NO_BUILTIN_PREEXEC=1` disables that. The Fedora repos currently
    carry 18.12, so the packaged `bash-preexec` would be sourced explicitly.
  - bash-preexec "must be the last thing imported in your bash profile"
    (<https://github.com/rcaloras/bash-preexec>). That ordering constraint clashes with
    other things that manage `PROMPT_COMMAND` (a prompt framework, VTE, systemd OSC
    context).
- **Up-arrow hijack:** `atuin init bash --disable-up-arrow` (and `--disable-ctrl-r`)
  (<https://docs.atuin.sh/cli/configuration/key-binding/>). There are historic bugs
  about up-arrow still being bound (atuin issue #971).
- **Pros:**
  - Best search UX and context.
  - Cross-terminal and cross-machine.
  - Packaged in Fedora.
- **Cons:**
  - A second store and a background-free but always-on hook on every command.
  - DEBUG-trap fragility in bash.
  - Needs installing inside each container to work there.
  - A heavier mental model.
- **Risk:** medium, because the DEBUG trap and `PROMPT_COMMAND` ordering interact with
  other prompt tooling.

### 2.3 Rank 3: hstr

- **Fedora availability (host):** `hstr-3.1` in the official repos.
  <https://github.com/dvorka/hstr>
- **Store:** reads `$HISTFILE` directly, so plain bash history stays the store. Favourites
  and a blacklist live in its own small files.
- **Setup:**
  - Its generated config adds `history -a; history -n` to `PROMPT_COMMAND`, with the
    offset imprecision described in 1.2, and binds Ctrl+R
    (<https://github.com/dvorka/hstr/blob/master/CONFIGURATION.md>).
  - hstr historically inserted the chosen command via the `TIOCSTI` ioctl. Linux ≥ 6.2
    can disable that, and it **is disabled on this host (`dev.tty.legacy_tiocsti = 0`)**.
    hstr's source then warns that "Your bash config is missing required HSTR function"
    and requires the regenerated function-based config (source: `src/hstr.c`).
- **Pros:**
  - Packaged.
  - Plain-file store.
  - Simple curses UI with favourites.
- **Cons:**
  - Less capable matching than fzf.
  - The TIOCSTI caveat.
  - Duplicates what fzf already gives for free.
- **Risk:** low to medium.

### 2.4 Rank 4: ble.sh

- A pure-bash replacement for GNU Readline: syntax highlighting, autosuggestions, vi mode,
  and history sharing via config (<https://github.com/akinomyoga/ble.sh>).
- Install is from source (`make`, needs gawk) or a nightly tarball. It is in AUR, Nix and
  Guix, but **not packaged in Fedora**.
- It must be sourced at the top of `.bashrc` with `--attach=none`, with `ble-attach` at the
  end.
- It **overrides builtins** (`trap`, `bind`, `history`, `read`, `exit`) and changes
  `PIPESTATUS` semantics. fzf needs its contrib integration.
- **Pros:** the richest UX, and the best hook host for Atuin.
- **Cons:** the largest blast radius (it replaces the line editor itself), is unpackaged,
  and affects every keystroke.
- **Risk:** high for a "just fix Ctrl+R" goal.

### 2.5 Rank 5: McFly

- A neural-network-ranked Ctrl+R with a SQLite store (<https://github.com/cantino/mcfly>).
- **Not packaged in Fedora**: it installs via brew, a release tarball, or cargo.
- The README says "Seeking co-maintainers: I don't have much time to maintain this project
  these days."
- On kernels with TIOCSTI disabled it needs a dummy-keybinding workaround.
- It hooks `PROMPT_COMMAND` to feed its DB.
- **Verdict:** not recommended (unpackaged, maintenance risk, overlapping hooks).

### Summary table

| Option | Fedora pkg (F44) | Extra deps                   | Store                       | Cross-terminal search           | Break risk |
| ------ | ---------------- | ---------------------------- | --------------------------- | ------------------------------- | ---------- |
| fzf    | yes (installed)  | none                         | `$HISTFILE`                 | with reload-on-Ctrl+R wrapper   | very low   |
| Atuin  | yes (18.12.1)    | bash-preexec (pkg) or ble.sh | SQLite (+ `$HISTFILE` kept) | native                          | medium     |
| hstr   | yes (3.1)        | none                         | `$HISTFILE`                 | via `history -a; -n` per prompt | low–medium |
| ble.sh | no               | gawk/make to build           | `$HISTFILE`                 | via config                      | high       |
| McFly  | no               | none (binary)                | SQLite                      | native                          | medium     |

---

## 3. Recommendation

**Ctrl+R: fzf's packaged key-bindings, with a wrapper that merges other terminals' history
just before searching.** Reasons:

- Already installed, no new packages.
- Keeps plain bash history as the single store.
- Smallest blast radius, and degrades to stock Ctrl+R wherever fzf is absent.
- Addresses the one real gap (cross-terminal visibility) without the per-prompt
  interleaving of `history -n` or the DEBUG-trap machinery Atuin needs in bash.

Atuin stays the upgrade path if directory, exit-status or cross-machine context becomes a
real need (YAGNI until then).

```bash
# Bash history: durable across many concurrent terminals.
# A non-default HISTFILE means a shell that skipped this file (bash --norc, a
# container sharing $HOME, sudo -E) truncates ~/.bash_history, not this one.
# The directory must already exist (created by IaC); bash will not create it.
HISTFILE="${XDG_STATE_HOME:-$HOME/.local/state}/bash/history"
HISTSIZE=-1
HISTFILESIZE=-1
HISTCONTROL=ignoreboth
HISTIGNORE="&:ls:[bf]g:exit"
HISTTIMEFORMAT='%F %T  '
shopt -s histappend cmdhist lithist histverify

__history_append() { builtin history -a; }
if [[ " ${PROMPT_COMMAND[*]-} " != *" __history_append "* ]]; then
    PROMPT_COMMAND+=(__history_append)
fi

# Ctrl+R: fzf over the full, freshly merged history. Guarded so shells without
# fzf (containers) keep stock readline Ctrl+R.
if command -v fzf >/dev/null && [[ -r /usr/share/fzf/shell/key-bindings.bash ]]; then
    FZF_CTRL_T_COMMAND= FZF_ALT_C_COMMAND= source /usr/share/fzf/shell/key-bindings.bash
    __history_merge_then_fzf() {
        builtin history -a
        builtin history -c
        builtin history -r
        __fzf_history__
    }
    bind -m emacs-standard -x '"\C-r": __history_merge_then_fzf'
    bind -m vi-insert      -x '"\C-r": __history_merge_then_fzf'
    bind -m vi-command     -x '"\C-r": __history_merge_then_fzf'
fi
```

Notes on the snippet:

- **Load order.** Source it **after** any prompt framework that assigns `PROMPT_COMMAND`,
  so the appended array element is not wiped (1.3).
- **Why a full `-c; -r` reload in the wrapper, not `-n`.** The reload is exact, because
  it does not use line-offset bookkeeping. Its cost is paid only when Ctrl+R is pressed,
  not at every prompt. The side effect is that up-arrow in that shell shows the merged
  global order after a search. If that proves annoying, swap the three lines for
  `builtin history -a; builtin history -n`, which is cheaper but less exact. The reload
  cost with a very large file has not been measured here.
- **Ctrl+T and Alt+C.** They are disabled via the empty `FZF_*_COMMAND` variables
  (supported opt-out). Remove those two assignments if those widgets are wanted.
- **`FZF_CTRL_R_OPTS`.** Add it to taste, for example a wrapped preview for long
  commands.
- **Migration, IaC only.** Create the history directory, and seed the new `HISTFILE` from
  `~/.bash_history` once (guarded with `creates:`). Leave `~/.bash_history` in place as
  the file that rc-less shells may truncate harmlessly.
- **Fail fast.** A missing history directory makes bash silently not save. The IaC task
  that creates it is the guarantee, and a verification step should assert the directory
  exists and is writable.
