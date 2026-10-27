# reclaim: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its sixth
tool: `reclaim`, the disk-space menu that reports what is using disk and runs targeted
cleanups, each behind a confirmation. **Method: the legacy bash is not an oracle of wording.**
This document states the intended behaviour as numbered, testable expectations (`E*`). The
bash (`files/home/.local/bin/reclaim`, 456 code lines, 29 functions, at `842d88ddd`) was read
to find the requirements; where it was wrong, the expectation states the right behaviour and
section 5 lists the departure (`B*`). The proof is the Go tests in `src-go/internal/reclaim`
and `src-go/cmd/reclaim`, and the scenario table of section 6, which both the Go and the bash
were run against. What only a host can prove is section 7.

## 1. Why reclaim next among P6

Left in P6 after `setup-rclone`: `reclaim` (456 code lines, 29 functions, interactive, churn
5), `nord` (505 lines, 18 functions, churn 8), `nvidia-status` (430 lines, 4 functions) and the
`ssh-with-key` family (87 lines, 2 functions). Ranked by the inventory's value (interactivity,
size, churn), `reclaim` and `nord` lead; `reclaim` is the one that can be proven without the
owner:

- **`reclaim`** is a menu over programs whose every decision a fake can pin: the commands it
  runs, and in what order, after which answer. Its danger (root and deletion) is exactly what
  a recorded command list proves: no destructive command runs without its yes. The deletions
  it does itself (`~/.cache`, the trash) are proven on temporary trees.
- `nord` drives NordVPN's OpenVPN configs with the account's credentials and changes the
  host's routing: its proof is a live VPN connection, the owner's.
- `nvidia-status` reads the GPU (`nvidia-smi`, sysfs); a fake proves only the formatting.
- `ssh-with-key` is small (87 lines, 2 functions) and is credentials: the least value.

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/reclaim`, built by
  `tasks/build-go-command.yml` from `play-cli-tools.yml` (tag `disk-reclaim`, which now also
  imports `play-golang.yml`), installed at `~/.local/bin/reclaim`, mode 0755. The bash file is
  deleted and its row leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv`
  gets `reclaim	qa-go` (the bash had no suite).
- **D2. External programs stay where they are the tool.** `dnf`, `journalctl`, `podman`,
  `docker`, `flatpak`, `trash-list`/`trash-empty`, `duf`/`df`, `du`, `ncdu` and `baobab` are
  run; done in Go: the biggest-files walk (was `find | sort | head | numfmt`), the sorting and
  byte formatting of `du` figures (`du -B1`, formatted by `rclonecache.IEC`), and the removal
  of `~/.cache`'s and the trash's contents (was `find -exec rm -rf`).
- **D3. One seam for processes, owned programs by their owners.** Everything runs through
  `internal/execx`; root through `internal/sudo` (`Ready` with a reason, then `sudo -n`);
  `ncdu` through `execx.Foregrounder` (it takes the terminal). `reclaim` joins the owners of
  `podman` and `docker` in the project rule's table (`project_seam_commands`): it prunes them,
  a job of its own.
- **D4. The menu is a numbered text menu on `/dev/tty`** (as the bash's), not an fzf picker:
  every choice is a visible line with its number or letter (InteractiveScripts.md rule 14), and
  each screen starts with a `ui.Heading` (rule 15).

## 3. Expectations

### Command line

- E1. `reclaim` opens the menu; `reclaim report` prints the report; `-h`/`--help` prints the
  usage on stdout and exits 0; `--version` prints `reclaim 2.0.4` on stdout and exits 0.
- E2. Any other argument, or a second argument, is `reclaim: unknown argument: <arg> — see 'reclaim --help'.` on stderr, exit 64. Every error line is `reclaim: <text>` on stderr.

### The report

- E3. `reclaim report` prints on stdout: `reclaim disk report — <hostname>`, a blank line,
  `Filesystems:` and the filesystem lines, a blank line, `Journal:` and the journal line, a
  blank line, `Notable directories:` and one line per directory, and, when `podman` is
  installed, a blank line, `Podman storage:` and `podman system df`'s output.
- E4. The filesystem lines are `duf --only local`'s output when `duf` is installed and succeeds;
  otherwise `df -h -x tmpfs -x devtmpfs -x efivarfs -x overlay --output=target,size,used,avail,pcent`'s; if that fails, `df -h`'s. If all fail: `  (filesystem usage unavailable)`, the
  last reason on stderr.
- E5. The journal line is `journalctl --disk-usage`'s output; `journal: journalctl not present`
  when it is not installed; `journal: usage unavailable` when it fails, its reason on stderr.
- E6. The notable directories are `~/.cache`, `~/.local/share/Trash`, `/var/log` and
  `/var/cache`, each that exists: `<size>\t<path>`, the size `du -s -B1 <path>`'s figure
  formatted as `numfmt --to=iec --suffix=B` does (`rclonecache.IEC`). A `du` that exits non-zero
  but prints its total (an unreadable entry inside) gives `<size>\t<path>\t(at least: some entries could not be read)`. A `du` with no figure gives `?\t<path>`, its reason on stderr.
  `du`'s permission refusals under one directory (`du: cannot read directory '<p>': Permission denied`, `du: cannot access '<p>': Permission denied`; `/var/log` and `/var/cache` give dozens) are one stderr line,
  `reclaim: du <dir>: <n> entries could not be read (permission denied), the first '<p>'; the size is a lower bound.`; any other line of `du`'s stderr is shown as it is. The same holds for
  every `du` the menu runs (E17, E24, E26).
- E7. A `podman system df` that fails gives `  (podman storage unavailable)`, its output on
  stderr. One refused `permission denied` (a volume's `_data` owned by a container's uid, which
  the user's own uid cannot read) is named on stderr and read again as
  `podman unshare podman system df`, inside podman's user namespace, where those uids are
  mapped; its figures are the section, and only both failing is unavailable, each reason on
  stderr.
- E7a. When the second read is refused `permission denied` too (a volume's `_data` owned by a
  uid outside the user's current subordinate range, left from an older mapping, which no user
  namespace maps), the section is partial, not unavailable:
  `  (partial: podman system df could not read every volume, so there are no totals)`, then the
  volume podman's refusal names (`open <store>/volumes/<name>/_data: permission denied`) as
  `  Unreadable volume <name> (<path>): <why>`, where why is found, never guessed: its owner
  (lstat) against the account's uid and its `/etc/subuid` lines (by name or uid), giving
  outside the current range (`— probably left from an older setup; podman cannot read or size it.`), inside it, the account itself, no range, or which of those could not be read. Then
  `  To look at it without changing anything:` with `podman volume inspect <name>`,
  `sudo ls -lan <path>` and the `grep -E` of the account's `/etc/subuid` lines. A refusal naming
  no volume says so. Then a summary of `podman images`, `podman ps --all --size` and
  `podman volume ls`, each read as `--format json` (none opens a volume's data), each on a line
  `  <Title> (<command>): <summary>`, never the raw listing (a host has hundreds of images):
  images, their count and `total size not known: podman system df could not run, and images share layers` (no summed size: it counts a shared layer in each image, and on a real host equals the whole disk; podman's image JSON carries no per-layer sizes to count each layer once),
  `    Dangling (<none>)[: <n>][, <k> of them used by a container and kept by the prune]` (no name, or podman's `Dangling`), and the largest ten,
  `      <size, right-aligned to 8>  <name, or <none> and the short id>`; containers, their
  count and the largest ten by writable layer, `<name> (<state>)`; volumes, their count (`not sized, since sizing a volume opens its data`) and the first ten by name. A kind with more than
  ten adds `    and <n> more`; a kind with none is `0`. A listing that fails, or whose output is
  not a JSON list, is named on stderr and its line ends `unavailable`. The report still exits 1
  (E8). The volume is never touched.
- E8. The report exits 0 when every section was read, and 1 when any could not be (E4 all
  failed, E5 failed, E6 no figure, E7), after printing every section. A failed write to stdout
  is exit 1.

### The menu

- E9. The menu needs a terminal: stdin a terminal and `/dev/tty` opened. Otherwise
  `reclaim: needs an interactive terminal; run 'reclaim report' for a non-interactive summary.`, exit 1.
- E10. The header is a heading `reclaim` with the context `<hostname> · v2.0.4`, then the
  filesystem lines indented by two spaces, then the journal line. It is drawn when the menu
  opens and again on `r`.
- E11. The menu lists, under `Analyse`, `Reclaim (destructive — each confirms first)` and
  `Other`: `1) Biggest directories` (noting `ncdu` or `du summary`), `2) Biggest files`,
  `3) dnf autoremove + clean all (sudo)`, `4) Remove old kernels (sudo, keep newest 2)`,
  `5) Vacuum systemd journal (sudo)`, `6) Prune containers` (noting `podman`, `docker`, both or
  `-`), `7) Flatpak unused runtimes` (noting `flatpak` or `(flatpak absent)`), `8) Clear ~/.cache`, `9) Empty trash`, `10) Safe sweep (run 3,5,6,7,9)`, `r) Refresh report`,
  `q) Quit`; then `Choose [1-10, r, q]: `.
- E12. A choice runs its action and the menu is shown again. `q`, `Q`, `quit` or `exit` prints
  `Bye.` and ends the run; end of input prints `No more input — exiting.` and ends it. An empty
  line asks again. Anything else is `  Not a valid choice: '<x>'. Pick 1-10, r, or q.`; three
  invalid choices in a row end the run with `Too many invalid choices — exiting.`, exit 1. A
  valid choice resets the count.
- E13. The run that ends by `q` or end of input exits 0, or 1 when any action it ran failed
  (a command it ran failed, or a removal it made failed): `reclaim: <n> action(s) failed in this run; see the messages above.` (B9).
- E14. Each action starts with a heading `reclaim › <action>`.

### Questions

- E15. A confirmation is `<question> [y/N] `; `y` or `yes` in any case is yes; anything else,
  an empty line and end of input are no. A destructive command runs only after a yes to the
  confirmation that names it.
- E16. A value question is `<question> [<default>] `; an empty line takes the default; end of
  input is `  Cancelled.` and the action ends. A value that fails its check is named
  (`  Not a directory: <x>`, `  Not a positive number: <x>`, `  Not a valid size (e.g. 500M, 1G): <x>`) and the same question is asked again, three tries in all; after the third,
  `  Giving up on this action after 3 tries.` and the action ends without failing the run
  (B5).

### Actions

- E17. **Biggest directories** asks `Analyse which directory?` (default `$HOME`, must be a
  directory). With `ncdu` installed it runs `ncdu -x <dir>` in the foreground; a non-zero exit
  is `  ncdu exited <code>.`. Without, it prints `  ncdu is not installed — showing a du summary instead.` and the 20 largest entries of `du -x -B1 --max-depth=1 <dir>`, largest
  first, `  <size, right-aligned to 8>  <path>`; a `du` that exits non-zero after printing
  shows its own stderr (naming what it could not read) and adds
  `  (some entries could not be read)`; one that prints nothing fails the action. Then, when
  `baobab` is installed and `DISPLAY` or `WAYLAND_DISPLAY` is set, it asks `  Also open baobab (graphical analyser) on <dir>?`; yes runs `setsid -f baobab <dir>` with its output to
  `/dev/null` and says `  baobab launched in the background.`
- E18. **Biggest files** asks `Find big files under which directory?` (default `$HOME`) and
  `How many to list?` (default 20, a positive whole number). It walks the directory without
  following symlinks and without leaving its filesystem, and lists the regular files, largest
  first (equal sizes by path), `  <size, right-aligned to 8>  <path>`. Entries it could not
  read are counted: `  <n> entries could not be read (first: <path>: <reason>)`.
- E19. **dnf autoremove + clean all** lists `dnf repoquery --unneeded -q`'s packages, run as
  the user (`  Orphaned packages (<n>):` and `    <pkg>` each, or
  `  (no orphaned packages detected in preview)`), asks `  Run 'dnf autoremove' + 'dnf clean all' now?`; yes gets root (E25) and runs `sudo -n dnf -y autoremove`, then, only if it succeeded,
  `sudo -n dnf clean all`, and says `  Package cleanup complete.`. A failed preview is
  `  dnf could not list the orphaned packages.` and the action fails before asking (B3).
- E20. **Remove old kernels** lists, as the user,
  `dnf repoquery --installonly --latest-limit=-2 -q`. None is `  No superfluous kernels — nothing to remove (keeping newest 2).`; else the list and `  Remove <n> old kernel package(s)?`; yes gets root and runs `sudo -n dnf -y remove <pkg>...`. A failed preview fails the action
  before asking (B3).
- E21. **Vacuum systemd journal**: without `journalctl`, `  journalctl is not available.`.
  Else it shows the journal line (E5), asks `Vacuum journal down to what size?` (default
  `500M`, digits with an optional `K`, `M`, `G` or `T`), then `  Vacuum system journal to <size>?`; yes gets root and runs `sudo -n journalctl --vacuum-size=<size>`, then shows the
  journal line again.
- E22. **Prune containers**: neither `podman` nor `docker` installed is `  Neither podman nor docker is installed.`. For podman, `podman system df` (both streams) shown indented, then
  `  Prune stopped containers, dangling images and unused networks; no volume and no tagged image (podman system prune -f)?`; yes runs it (never `-a`, B16). Then, whatever the answer,
  `podman images --format json` is read and every image no container uses is a second,
  separate question, default no: when a tagged one is among them,
  `  podman image prune -af removes every image no container uses, not only the dangling ones: here <n> tagged image(s); each is pulled or rebuilt when it is next needed.`,
  then the claude-yolo project images (`localhost/claude-yolo:<project>`) among them, each
  rebuilt on that project's next launch, listed largest first as E7a lists, and
  `  and <n> other tagged image(s).`, then `  Also remove all <n> unused podman image(s), tagged ones included (podman image prune -af)?`; yes runs it (`  Unused podman images removed.`, or
  `  podman image prune -a reported an error.` and a failed action), no is
  `  Kept the unused images.`. No tagged image unused is
  `  No other podman image is unused: nothing more to remove.` and no question; an image list
  that cannot be read is `  Removing every unused image is not offered: podman could not list its images (<reason>).`. A `podman system df` that fails is `  Cannot query podman — skipping podman prune. Reason:` and its output, and when that output names `stale temp dir`, the
  hint to repair the store on the host with
  `ansible-playbook CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml`; this is not a
  failed action (nothing was run). A `podman system df` refused `permission denied` is first said
  (`  podman system df was refused permission; reading it again inside podman's user namespace (podman unshare podman system df). Reason:` and its output) and read again
  as E7 does; only a second failure is the cannot-query message, with the second output.
  E22a: a second failure that is a refused `permission denied` too is
  `  Cannot query podman fully: podman system df could not read every volume. Reason:` and its
  output, E7a's volume lines, `  Podman storage, in part (no totals):` and E7a's three summary
  lines, with the reason after `unavailable`. `podman system prune` is not offered
  (`  podman system prune is not offered: ...`): it removes stopped containers, podman could not
  see all of its storage, and a stopped container may be what still holds that volume. Only
  image prunes are offered, which neither read nor remove a volume: first
  `  Prune the <n> dangling (<none>) podman image(s) no container uses (podman image prune -f)?` (`  No dangling podman image to prune.` and no question when there is none); when <k> dangling images are used by a container, which the prune keeps, `  <k> dangling podman image(s) are used by a container and are kept by the prune.` is said first, so the report's count and the question's differ by exactly <k>;
  yes runs it (`  Dangling podman images pruned.`, or `  podman image prune reported an error.`
  and a failed action, with no second question); no is `  Skipped podman image prune -f.`.
  Then E22's question of every unused image, from the list just shown (less the dangling images
  just pruned). When `podman images` failed too,
  `  No podman prune is offered: podman could not list its images either.`. For docker,
  `docker system df`, then
  `  Prune stopped containers, dangling images, unused networks and dangling build cache; no volume and no tagged image (docker system prune -f)?`, then, separately and default no,
  `  docker image prune -af removes every image no container uses, not only the dangling ones; each is pulled or rebuilt when it is next needed.` and
  `  Also remove all unused docker images (docker image prune -af)?`, both run as the user whose
  `docker system df` answered (B4); a `docker system df` that fails is
  `  docker is installed but not usable (daemon down, or not in the docker group) — skipping.`.
- E23. **Flatpak unused runtimes**: without `flatpak`, `  flatpak is not installed.`. Else
  `flatpak uninstall --unused --assumeno` (its output shown; a non-zero exit adds
  `  (nothing unused, or preview unavailable)`), then `  Remove unused flatpak runtimes?`; yes
  runs `flatpak uninstall --unused -y`.
- E24. **Clear ~/.cache**: `~/.cache` as a symbolic link is refused (B13):
  `  ~/.cache is a symbolic link (to <target>); reclaim does not clear through a link.` and
  what to do instead, and the action fails. No directory is `  No ~/.cache directory.`. Else
  the 15 largest entries (as E17's du summary, `du -B1 --max-depth=1 ~/.cache`); a `du` that
  sized nothing is `  Not clearing ~/.cache: its contents could not be listed.` and the action
  fails before asking (B3). Then the note that applications recreate their caches, and
  `  Delete the CONTENTS of ~/.cache now?`; yes removes every entry inside `~/.cache` (the
  directory itself stays) and says `  ~/.cache cleared.`. Each entry that could not be removed
  is named on stderr, the others are still removed, `  Some items in ~/.cache could not be removed.` is said and the action fails. The action's verdict is the removal's: a listing
  that worked and a removal that worked is an action done (B14).
- E25. **Root**: an action that needs root says why and calls `sudo.Ready` (cached or
  passwordless sudo asks nothing; else sudo asks for the password on the terminal), then runs
  its root commands with `sudo -n`. `sudo` not installed is `  This action needs root through sudo, and sudo is not installed.`; sudo not given fails the action. Nothing is run as root
  before that (B2), and root is asked for only after the yes to the command that needs it:
  every preview runs as the user (B12).
- E26. **Empty trash**: with `trash-empty` installed, `trash-list` (its output shown; a
  non-zero exit adds `  (trash empty or unavailable)`), `  Empty the trash now?`, yes runs
  `trash-empty -f`. Without it: `~/.local/share/Trash`, or its `files` or `info` directory, as
  a symbolic link is refused as in E24 (B13); no `~/.local/share/Trash` is
  `  No trash directory.`; else its size (E6's figure, `(at least)` and du's stderr when du
  could not read all of it); a size du could not give is
  `  Not emptying the trash: its size could not be read.` and the action fails before asking
  (B3). Then `  Empty ~/.local/share/Trash now?`, yes removes every entry inside its `files`
  and `info` directories (either may be missing), as E24.
- E27. **Safe sweep** lists its steps, asks `  Begin the safe sweep?`; yes runs E19, E21, E22
  (when `podman` is installed), E23 (when `flatpak` is installed) and E26, in that order, each
  asking its own questions, then `  Safe sweep finished.`. Its E22 is the dangling-only prunes
  alone: the question of every unused image is never asked, and
  `  The safe sweep removes no tagged image; option 6 offers removing every unused one.` is said
  in its place. A step that fails does not stop the
  sweep; it counts towards E13.
- E28. A command's own output (dnf, journalctl, podman, flatpak, trash) goes to stderr as it
  runs; its failure is said in a line naming it.

### Interrupts

- E30. The command runs under `execx.SignalContext`: Ctrl-C (SIGINT, which the terminal also
  sends to the command running), or SIGTERM, SIGHUP or SIGQUIT, ends the run instead of
  killing the process. A command that changes the machine (every root command, the prunes,
  `flatpak uninstall --unused -y`, `trash-empty -f`) is not signalled or killed by reclaim: the
  run waits for it to end, however the Ctrl-C it got from the terminal leaves it, and runs
  nothing after it (`dnf clean all` after an interrupted autoremove, the sweep's next step). A
  command that only reads (du, df, the previews) is stopped. A wait for a line at the menu or
  at a question ends at once. A clear of `~/.cache` or the trash stops before its next entry
  and says `  <~/.cache | The trash> is only partly cleared: cancelled after <i> of <n> entries.`. The run then prints a blank line and `Cancelled.` once, and exits 130
  (`term.ExitCancelled`), in the menu and in `reclaim report` alike.

### Output

- E29. stdout carries only the report, the usage and the version; every menu, question and
  diagnostic is on stderr. Colour (bold headings, cyan, yellow, red and green lines) only when
  stderr is a terminal and `NO_COLOR` is unset or empty; the text is the same either way.

## 4. Kept as it was

The menu's numbers and letters, the report's sections and order, the confirmations' wording
(but E22's, B16) and their default of no, the old-kernel keep count (2) and journal default (500M), the
container stale-temp-dir hint, `ncdu -x` as the deep dive and `baobab` on a desktop, and the
sweep's steps.

## 5. Departures from the bash

- B1. Usage errors exit 64 (the bash: 2); a second argument is an error (the bash ignored it).
- B2. Root through `internal/sudo`: the reason is said before sudo asks, then `sudo -n`, so an
  expired credential fails loudly instead of prompting mid-action. Running as root needs
  `sudo` installed too (the bash ran directly as uid 0); every Fedora install has it.
- B3. A failed `dnf repoquery` preview fails the action. The bash read it through a process
  substitution whose status nobody checked, so a failing preview said "no orphaned packages"
  or "No superfluous kernels — nothing to remove". The listing before a clear (E24, E26) is a
  preview too: one that fails ends the action before asking, so nothing is deleted unseen.
- B4. `docker system prune` runs as the user whose `docker system df` answered (the bash ran
  it under sudo after asking docker as the user).
- B5. A bad value is asked again, three tries in all (InteractiveScripts.md rules 1 and 2;
  the bash returned to the menu on the first).
- B6. The commands' stderr is shown, a partial `du`'s included (E6, E17, E26: its refusals
  counted and the first named, its other complaints as they are) (the bash appended it to a scratch log that it deleted at exit
  without showing, while its messages said "see run notes").
- B7. The report's `du` of a directory the user cannot read in full is printed and marked
  incomplete; the report goes on. In the bash, `du`'s non-zero exit failed the `du | awk`
  pipeline under `pipefail` and `set -e` ended the report there, with status 1 and the stderr
  hidden (B6). A section that could not be read makes the report exit 1 after printing all of
  it.
- B8. Sizes are `numfmt`'s IEC form with a `B` (`4.0KB`; the bash printed `du -h`'s `4.0K`),
  and the biggest-files walk is Go's (regular files, one filesystem, unreadable entries
  counted and the first named).
- B9. A run in which an action failed exits 1 when it ends (E13); the bash exited 0.
- B10. `y` and `yes` are yes in any case (the bash: `y`, `Y`, `yes`, `YES`). Colour honours
  `NO_COLOR`.
- B11. Every screen has a heading (InteractiveScripts.md rule 15).
- B12. The `dnf repoquery` previews of E19 and E20 run as the user, and sudo is readied only
  after the yes (as E21's vacuum already was). The bash ran them as root first, so a person who
  only looked, and answered no, had typed a password and left a cached credential for nothing.
  `dnf repoquery` answers an unprivileged user.
- B13. `~/.cache`, `~/.local/share/Trash` or its `files` or `info` directory as a symbolic link
  is refused, naming the link and its target (E24, E26). The bash's `find "$dir" -mindepth 1`
  did not follow the start symlink, so it removed nothing and still said the directory was
  cleared; following the link instead would clear wherever it points.
- B14. A clear's verdict is its removal's (E24, E26): a listing that failed stops the action
  before asking (B3), so a yes that removed everything is never reported as a failed action,
  as the port first did when the listing had failed and the removal then worked.
- B16. Every prune the menu offers by default is dangling-only (`podman system prune -f`,
  `podman image prune -f`, `docker system prune -f`); removing every image no container uses
  (`podman image prune -af`, `docker image prune -af`) is a second question, default no, that
  names the tagged images it removes and lists the claude-yolo project images among them, and
  the safe sweep never asks it (E22, E22a, E27). The bash's one question ran
  `system prune -af`, sweep included, which removes every tagged image not in use now: on a ccy
  host, each project's `localhost/claude-yolo:<project>` image (several GB, rebuilt on that
  project's next launch). Its question also said volumes, which neither prune removes.
- B15. A Ctrl-C ends the run with `Cancelled.` and 130 after the command it interrupted has
  ended (E30). The bash died with the shell's default handling while a root child such as
  `dnf` could still be running and printing over the prompt, and a half-done clear said
  nothing.

## 6. Proof

- **Expectation tests**: `go test -race` over `internal/reclaim` and `cmd/reclaim`; every
  expectation has a test written first and seen to fail (journal), over `execx.Fake`, a fake
  terminal and temporary trees.
- **Scenario table** (`src-go/internal/reclaim/testdata/scenarios.json`): each scenario is the
  programs installed, what each answers, the lines typed at the terminal, and the expected
  exit status and the commands that may change the machine, in order, with the entries left
  in `~/.cache` and the trash. A command counts as changing when it runs as root (every
  `sudo -n` argv but `internal/sudo`'s own credential calls), or when one of its words (an
  option split at `-` and `=`, never a path) is one of the table's `changingWords` (`rm`,
  `prune`, `remove`, `autoremove`, `clean`, `vacuum`, `uninstall`, `empty`, `reset`), so a
  destructive command nobody listed is still recorded and fails the scenario. In the Go a
  command no scenario scripts fails it too (`execx.ErrUnexpected`), unless it is one of the
  read-only commands the test world answers by default (`readOnlyDefaults`: `du`, `df`, `duf`,
  `journalctl --disk-usage`, `dnf repoquery`, the engines' `system df`, flatpak's `--assumeno`
  preview, `trash-list`). `TestScenarios` runs every one against the Go.
  `CLAUDE/Plan/00192-.../reclaim-parity/parity.py` runs the same table against the bash at
  `842d88ddd` (fake programs on `PATH` that record their argv, a pseudo-terminal for
  `/dev/tty`) and reports every scenario where the bash ran different changing commands or
  exited differently; each such scenario names the departure that explains it
  (`bash_differs`), and the script fails on any unexplained difference. Wording is not
  compared (section 1's method).

## 7. Host checks

Proven only on the host, after `play-cli-tools.yml` (tag `disk-reclaim`) has run:

- H1 (host agent). `~/.local/bin/reclaim` is an ELF; `reclaim --version` prints
  `reclaim 2.0.4`; `reclaim report` prints its four sections with real figures and exits 0, or
  1 naming on stderr what it could not read.
- H2 (host agent, tmux). The menu in a tmux pane, driven by `send-keys` and read with
  `capture-pane`: `1` on a small directory opens ncdu and `q` returns to the menu; `2` lists
  files; `r` redraws; three invalid choices exit 1; Ctrl-D exits 0; Ctrl-C at the menu prints
  `Cancelled.` and exits 130 (E30).
- H3 (host agent). `3` and `4` show their previews with no sudo question (B12), and
  answering No runs nothing (`dnf history` unchanged). `5` with No likewise.
- H4 (host agent). `HOME=<scratch> reclaim`, `8` and `9` answered yes, on a scratch home with a
  `.cache` and a `.local/share/Trash` of throwaway files: the contents go, the directories stay.
  `6` shows `podman system df` and No to both questions prunes nothing.
- H5 (host agent, by screenshot). On the desktop, `1` with yes to baobab opens baobab, and the
  menu stays usable.
- The real cleanups (`3`, `4`, `5`, `6` answered yes) are the owner's to run when wanted; they
  are the same commands the bash ran, which section 6 shows.
