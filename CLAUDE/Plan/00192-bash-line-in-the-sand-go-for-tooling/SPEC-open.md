# open: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its first
tool: `open`, the command that opens a file, directory or URL with the right application on a
desktop or over SSH (Plan 00064 built it). **Method: the legacy bash is not an oracle.** This
document states the intended behaviour as numbered, testable expectations (`E*`). The bash
(`files/home/.local/bin/open`, 419 code lines, at `9b9e50e0`) was read only to find the
requirements; where it was wrong, the expectation states the correct behaviour and section 5
lists the departure (`B*`). The proof is the Go tests in `src-go/internal/open` (every flow over
a fake runner, a fake picker and temporary XDG trees) and `src-go/cmd/open`. What only a desktop
session and a terminal can prove is section 7.

## 1. Why open first among P6

P6 lists `gh-account-setup.bash`, `setup-rclone`, `reclaim`, `nord`, the `rclone-cache-*`
pair, `open`, the `ssh-with-key` family and `nvidia-status`. The first port is chosen for value
against risk:

- `gh-account-setup.bash`, `setup-rclone`, `nord` and `ssh-with-key` handle credentials (tokens,
  rclone remotes, VPN logins, SSH keys); a fake cannot show that a secret never reaches argv or
  a log on the real tools, so each needs a host session before it can be trusted.
- `reclaim` needs root (`dnf`, `journalctl --vacuum-size`, `docker`) and deletes (`rm -rf` of
  `~/.cache` children, the trash, kernels): fakes prove the argv but not that a wrong path
  deletes nothing.
- `nvidia-status` reads hardware it cannot fake usefully in the container.
- **`open`** runs as the user, touches no secret, deletes nothing, and is all decisions over
  data (a MIME type, the XDG application directories, the programs on PATH, a choice), which
  is exactly what fakes test well. It has a human-interactive chooser (fzf, or a numbered menu)
  the bash could only be tested through a pseudo-terminal, and its bash uses `bash -c` templates
  and parallel arrays the line in the sand forbids. Neither it nor `reclaim` has a test suite,
  so neither carries an existing oracle; `open` is the one whose whole behaviour a fake can
  pin.

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/open`, built by
  `tasks/build-go-command.yml` from `play-cli-tools.yml` (tag `open-command`), installed at
  `~/.local/bin/open`, mode 0755. The play imports `play-golang.yml` first, tagged
  `open-command` so `--tags open-command` installs the toolchain too. The bash file is deleted
  and its row leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv` gets
  `open	qa-go`.
- **D2. External programs stay where they are the tool.** `mimetype`, `xdg-mime query filetype`
  and `file` find a file's type; `gio` lists a type's applications (`gio mime`), sets a default
  (`gio mime TYPE ID`) and starts a desktop application (`gio launch`); fzf is the chooser (through
  `internal/ui`); the handlers are the programs. Done in Go: the URL test, the readable check
  (`access(2)`), the XDG application-directory search and the desktop entry's `Name`, the
  pipeline into the pager (no shell), shell quoting for `--dry-run`.
- **D3. One seam for processes.** Everything runs through `internal/execx`; fzf only through
  `ui.Picker`. A handler's program is chosen at run time, so the one function that builds its
  command has a `dynamicNames` row in `src-go/qa/rules/table.go`.
- **D4. No shell.** A terminal handler is an argv with the target as its own argument, and an
  optional pager argv; the producer's stdout is joined to the pager's stdin by an `os.Pipe`.

## 3. Expectations

### Command line

- E1. `open [options] TARGET...`; options `-a/--ask`, `-t/--terminal`, `-d/--set-default`
  (implies `--ask`), `-l/--list`, `-n/--dry-run`, `-h/--help`; `--` ends the options. The whole
  command line is read before anything runs. `-h` anywhere prints the usage on stdout and exits
  0\.
- E2. An unknown `-...` option is `open: unknown option '<arg>' — see 'open --help'.`, exit 64.
  No target is `open: nothing to open — see 'open --help'.`, exit 64. Every error line is
  `open: <text>` on stderr.
- E3. Targets are opened in order; the first that fails or is cancelled ends the run with its
  exit code, and later targets are not opened.

### What a target is

- E4. A target is a URL when it starts with a scheme (a letter, then letters, digits, `+`, `-`,
  `.`) followed by `://`, or with `mailto:`; its type is `x-scheme-handler/<scheme>`, the scheme
  lower-cased. Anything else is a path.
- E5. A path must exist (`open: no such file or directory: '<path>'`, exit 1) and be readable by
  this user (`open: not readable: '<path>'`, exit 1). A path that begins with `-` is passed to
  every program as `./<path>`, so no program reads it as an option.
- E6. A directory's type is `inode/directory`. Any other path's type is the first answer of
  `mimetype --brief -- <path>`, `xdg-mime query filetype <path>`, `file --brief --mime-type -- <path>`, in that order, from a detector that is installed, succeeds and prints one
  `type/subtype` word. A detector that is installed but fails or prints anything else is named on
  stderr (`open: mimetype could not type '<path>': <reason>; trying the next detector`) and the
  next is asked. None answering is `open: could not determine the file type of '<path>'.`, the
  detectors' reasons, and the play that installs them, exit 1.

### Who can open it

- E7. A graphical session is `WAYLAND_DISPLAY` or `DISPLAY` set and not empty, and `-t` not
  given. Only in a graphical session are desktop applications offered.
- E8. In a graphical session, `gio` must be installed (`open: gio (glib2) is not installed; it lists and starts desktop apps. 'open -t' offers terminal handlers only.`, exit 1). `gio mime <type>` runs with `LC_ALL=C.UTF-8`, so its headings are the untranslated ones: the
  default is the ID on the `Default application for “<type>”: <id>` line, the other applications
  are the tab-indented IDs under `Registered applications:` and `Recommended applications:`.
  The candidates are the default first, then the others in order, each ID once. A failing
  `gio mime` is `open: gio mime <type> failed: <reason>`, exit 1.
- E9. An application ID is found as a desktop entry: in `$XDG_DATA_HOME/applications` (default
  `~/.local/share/applications`) then each `$XDG_DATA_DIRS` entry's `applications` (default
  `/usr/local/share:/usr/share`), the first directory that has it winning. Within a directory the
  ID is the file's path below it with `/` read as `-` (`kde/foo.desktop` is `kde-foo.desktop`).
  A symlink is followed; the entry must be a regular file. An ID found in no directory is not
  offered (gio and this search read the same directories, so this is an environment that
  changed between the two, and nothing the person can act on). A missing applications
  directory is skipped; one that cannot be read is `open: cannot read <dir>: <reason>`, exit 1.
- E10. A desktop application's label is the `Name` key of the entry's `[Desktop Entry]` group
  (the unlocalised one) and ` [GUI]`; an entry with no such `Name` is labelled by its ID without
  `.desktop`.
- E11. Terminal handlers are offered for every target, after any desktop applications, each only
  when its program (and its pager, when it has one) is on PATH, in this order by type:
  - `inode/directory`: `yazi T`, `ranger T`, `nnn T`, `lf T`, `ls -lAh --color=always -- T | less -R`,
    `tree -C -- T | less -R`;
  - `x-scheme-handler/*`: `w3m -- T`, `lynx -- T`, `links -- T`, `curl -fsSL -- T | less`;
  - `text/html`, `application/xhtml+xml`: `w3m -- T`, `lynx -- T`, then the text viewers;
  - `application/pdf`: `pdftotext -layout -- T - | less`, `pdfinfo -- T`;
  - `image/*`: `chafa -- T`, `viu -- T`, `identify -- T`, `exiftool -- T | less`;
  - `audio/*`, `video/*`: `mpv -- T`, `ffplay -autoexit -- T`, `ffprobe -hide_banner -- T`;
  - `application/json`: `jq . T | less -R`, then the text viewers;
  - archives (`application/zip`, `x-tar`, `gzip`, `x-gzip`, `x-bzip2`, `x-xz`, `zstd`,
    `x-7z-compressed`, `vnd.rar`, `x-rar`, `x-compressed-tar`, `x-archive`):
    `ouch list -- T | less`, `atool --list -- T | less`, `bsdtar -tvf T | less`, `7z l -- T | less`;
  - `application/vnd.sqlite3`, `application/x-sqlite3`: `sqlite3 T`;
  - `application/x-executable`, `x-sharedlib`, `x-pie-executable`: `readelf -h -- T | less`,
    `nm -- T | less`;
  - `text/*`, `application/xml`, `javascript`, `x-shellscript`, `x-perl`, `x-python`, `toml`,
    `yaml`, `x-yaml`, `x-desktop`, `message/rfc822`: the text viewers.
- E12. The text viewers are the editor (the first of `$VISUAL`, `$EDITOR`, `nvim`, `vim`,
  `nano`, `vi` whose first word is on PATH; its words, then T; label `<editor> (edit)`),
  `bat --paging=always -- T` and `less -- T`.
- E13. A type not in E11, or one in it for which nothing is installed, is unknown: it is offered
  `xxd -- T | less`, `hexdump -C -- T | less` and the text viewers. Every type ends with
  `file -- T` (`file (identify only)`).
- E14. No candidate at all is `open: no handler available for '<target>' (<type>). Install a viewer, or run 'open --list'.`, exit 1.

### Choosing

- E15. The first candidate is opened without asking when it is the registered default, or when
  there are no desktop applications and the type is known (E11) with an installed handler.
  Otherwise, and always with `--ask`, the person chooses.
- E16. Choosing needs stderr to be a terminal and the controlling terminal (`/dev/tty`) to open;
  otherwise `open: no default handler for '<target>' (<type>), and no terminal to ask on.`, then
  how to proceed (`open --list`, and `open --set-default` from a terminal), exit 1. Nothing is
  read from stdin.
- E17. With fzf installed the chooser is the house picker (`ui.Picker`): titled `open`, heading
  `open › <name>` with the type as its context (and `target i of n · <type>` when several targets
  are given), the candidates' labels as rows (told apart by a hidden tag, so two equal labels
  are two rows), and a last row `Cancel — open nothing`. The header names the keys
  (`Enter open   Esc or q cancel`). Leaving it is `open: cancelled — nothing opened.`, exit 130.
- E18. Without fzf the chooser is a numbered menu on stderr: the same heading, then
  `  N) <label>` per candidate, then `Choice [1-N, q to cancel]: `, read from `/dev/tty`. A
  number in range chooses. `q`, `Q`, `quit` and end of input cancel (exit 130, as E17).
  Anything else is `  Not one of the options — enter a number between 1 and N, or q to cancel.`
  and the prompt again, at most 3 times; then `open: giving up after 3 attempts — nothing opened.`, exit 1.
- E19. Ctrl-C at the numbered prompt cancels (exit 130, as E17).

### Listing and dry run

- E20. `--list` prints, on stdout, `<target> (<type>)` and then `  N) <label padded to 42> <kind>`
  per candidate (`gui` or `term`), or `  (no handler available)`, and opens nothing; exit 0.
- E21. `--dry-run` prints, on stdout, the command that would run, shell-quoted, and runs
  nothing: `gio launch <desktop file> <target>`, or the handler's argv, with `|` and the pager's
  argv when it has one. With `--set-default` on a desktop application the line
  `gio mime <type> <id>` comes first. The choice (E15-E19) is made as without `--dry-run`.

### Opening

- E22. Before a candidate runs, stderr says `→ <target> — <label>`.
- E23. A desktop application: with `--set-default`, `gio mime <type> <id>` runs first (failing:
  `open: could not set <id> as the default for <type>: <reason>`, exit 1, nothing started),
  then stderr says `  Registered <id> as the default for <type>.`. Then
  `gio launch <desktop file> <target>`, with stdin from `/dev/null`; failing:
  `open: gio launch failed: <reason>`, exit 1.
- E24. A terminal handler: with `--set-default`, stderr notes
  `  Note: --set-default applies to GUI apps only; '<label>' is a terminal handler.`. The handler
  runs on this process's stdin, stdout and stderr. With a pager, the producer's stdout feeds the
  pager's stdin, the producer's stdin is `/dev/null`, and the pager has the terminal.
- E25. A handler that exits non-zero is `open: <label> exited <n>`, exit 1. With a pager: the
  pager's failure counts; the producer's failure counts only when the pager had not ended first
  (a person quitting the pager early stops the producer, which is not a failure).
- E26. While a handler runs, an interrupt or quit from the terminal (Ctrl-C, Ctrl-) is the
  handler's: `open` neither dies nor stops it, so the terminal is never left to an orphaned
  program; `open` ends when the handler does.
- E27. A handler that is not installed by the time it runs is `open: <program> is not installed`,
  exit 1.

### Output

- E28. stdout carries only the usage, the `--list` text and the `--dry-run` lines (and what a
  terminal handler itself prints); everything else is stderr. A write to stdout that fails is
  `open: could not write the output: <reason>`, exit 1.

## 4. Exit codes

0 opened (or listed, or a dry run); 1 a failure; 64 a usage error; 130 cancelled.

## 5. Where the Go departs from the bash

| #   | bash behaviour                                                                                                                                          | Go                                                                                         | exp.     |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | -------- |
| B1  | in a graphical session, any registered application made it "confident": with no default set, the first registered app was launched without asking       | only a registered default is opened unasked; otherwise the person chooses                  | E15      |
| B2  | the desktop-entry search used `find -type f`, so a symlinked entry (every Flatpak export, many user entries) was never found and its app never offered  | symlinks followed, the target must be a regular file                                       | E9       |
| B3  | an ID was matched to any file of that base name at any depth (`foo.desktop` matched `kde/foo.desktop`, whose ID is `kde-foo.desktop`)                   | the desktop-entry spec's ID: the path below the directory with `/` read as `-`             | E9       |
| B4  | the label was the first `Name=` line anywhere in the file                                                                                               | the `[Desktop Entry]` group's `Name`                                                       | E10      |
| B5  | the default came from `xdg-mime query default`, whose failure was discarded; `gio mime`'s failure in a process substitution was discarded too           | `gio mime` alone (C locale), a failure is exit 1; `xdg-mime` is no longer asked a default  | E8       |
| B6  | `--set-default` registered with `xdg-mime default`                                                                                                      | `gio mime <type> <id>`, failure named, nothing started                                     | E23      |
| B7  | without `gio`, only the `xdg-mime` default was offered and `gtk-launch` started it                                                                      | in a graphical session `gio` is required; `-t` works without it                            | E8       |
| B8  | a terminal handler was a `bash -c` template                                                                                                             | an argv and an optional pager argv, joined by a pipe; no shell                             | D4, E24  |
| B9  | a handler whose pager (`less`) was not installed was still offered, and failed                                                                          | offered only when the pager is installed too                                               | E11      |
| B10 | a handler's non-zero exit ended `open` with that status and no message; in a pipeline only the pager's status was seen                                  | `<label> exited <n>`, exit 1; a producer's failure counts unless the pager ended first     | E25      |
| B11 | a failing type detector was discarded without a word before the next was asked                                                                          | named on stderr; output that is not one `type/subtype` word is a failure too               | E6       |
| B12 | `--list` printed to stderr                                                                                                                              | stdout, its payload                                                                        | E20, E28 |
| B13 | `--dry-run` printed `bash -c '<template>' open <target>`                                                                                                | the argv itself, shell-quoted, with the pager after `\|`; and the `gio mime` line for `-d` | E21      |
| B14 | every error exited 1, a usage error and a cancel included; options were read one by one, so `open --bogus -h` failed and `open -h --bogus` printed help | usage 64, cancel 130; the whole command line first, `-h` anywhere wins                     | E1, E2   |
| B15 | any target containing `://` was a URL, a file path with `://` in it included; the scheme kept its case                                                  | a scheme by RFC 3986's grammar, lower-cased                                                | E4       |
| B16 | a path beginning with `-` reached `jq`, `sqlite3` and `bsdtar`, which take no `--`, as an option                                                        | passed as `./<path>`                                                                       | E5       |
| B17 | the "no terminal" advice suggested `open --ask`, which needs the terminal that is missing, and `mimeopen --ask-default`                                 | `open --list`, and `open --set-default` run from a terminal                                | E16      |
| B18 | Ctrl-C while a handler ran killed the bash too, and two equal labels in fzf always chose the first                                                      | the handler's alone (E26); each row carries its own hidden tag (E17)                       | E17, E26 |
| B19 | the fzf chooser's only way out was Esc, a key with no visible row                                                                                       | a `Cancel — open nothing` row, as the house picker has                                     | E17      |

## 6. Go design

- `src-go/cmd/open`: a thin main. It subscribes to SIGINT and SIGQUIT before anything runs (so
  the Go runtime does not die on them, E26; a caught signal is reset to the default in each child,
  so a handler still gets its Ctrl-C) and hands the channel to the flow.
- `internal/open`: `App` over an `Env` of seams (runner, picker, streams, environment, PATH lookup,
  the readable check, the terminal test, the controlling terminal, the signal channel). `args.go`
  the command line, `mime.go` E4-E6, `desktop.go` E8-E10 (the `gio mime` reader and the XDG
  search), `handlers.go` E11-E13 as a table, `choose.go` E15-E19, `launch.go` E21-E27.
- `scripts/go-binary-suites.tsv`: `open	qa-go` (its oracle is the Go tests).
- The usage text, `src-go/internal/open/usage.txt`, is embedded: the bash's, with `--list` and
  `--dry-run` said to print on stdout and the default registered by `gio`.

## 7. What only a host run proves

Plan 00064's Phase 4 desktop and terminal checks, on the Go binary:

- H1. After `play-cli-tools.yml`: `open --help` prints the usage, and `file ~/.local/bin/open`
  says ELF (the Go binary replaced the bash).
- H2. Desktop: `open <pdf>` starts the registered viewer without asking; `open .` starts the file
  manager; `open -a <image>` shows the fzf chooser with the image viewers and a
  `Cancel — open nothing` row, and the row chosen starts.
- H3. Desktop: `open -d <image>` and choosing an app registers it (`gio mime image/<type>` names
  it as the default afterwards), and a later `open <image>` starts it unasked. A Flatpak app
  registered for the type is among the rows (B2).
- H4. Terminal (over SSH, or `open -t`): `open <text>` opens the editor; `open .` lists the
  directory in `less`; `open <image>` renders through `chafa`; `open <pdf>` pages `pdftotext`
  output, and `q` in `less` returns to the prompt with exit 0 (E25); an unknown type shows the
  chooser.
- H5. Terminal: Ctrl-C inside `less` or `mpv` started by `open` is that program's; the shell
  prompt comes back only when the program has ended, and the terminal is in its normal state
  (E26).
