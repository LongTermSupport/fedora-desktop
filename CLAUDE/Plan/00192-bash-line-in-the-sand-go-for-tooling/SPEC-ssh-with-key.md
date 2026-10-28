# The ssh-with-key family: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md): `ssh-with-key`, `scp-with-key` and
`ssh-copy-id-with-key`, the wrappers that log in to a host offering one private key and nothing
else: no key from the ssh-agent, no other file in `~/.ssh`, no password. **Method: the legacy
bash is not an oracle.** This document states the intended behaviour as numbered, testable
expectations (`E*`, E1-E20). The three bash files (`files/home/.local/bin/{ssh,scp,ssh-copy-id}-with-key`,
87, 85 and 73 code lines, and the agent half of `ssh-with-agent.bash` they source, at
`a08948346`) were read only to find the requirements; where they were wrong, the expectation
states the correct behaviour and section 5 lists the departure (`B*`). The proof is the Go tests
in `src-go/internal/sshkey` and the three `src-go/cmd/*-with-key` packages. What only a real
sshd can prove is section 7.

They are interactive (a key picker, the keys to forward), so
[InteractiveScripts.md](../../InteractiveScripts.md) binds them. They had no test suite.

## 1. Why now

The last of P6 apart from the `ccy` launcher. Small, but the only one whose promise is a
security property, and that property was not true (B1).

## 2. Scope decisions

- **D1. One package, three thin commands.** `internal/sshkey` holds everything; each of
  `cmd/ssh-with-key`, `cmd/scp-with-key` and `cmd/ssh-copy-id-with-key` is a `main` naming its
  `sshkey.Tool`. Not one binary answering to its `argv[0]`: `tasks/build-go-command.yml`
  builds and installs one command per name, so three names fit the shared task as it is, and a
  binary whose behaviour follows the name it was started by changes silently when it is copied,
  linked or run from a test. Three `main`s cost three files of a few lines.
- **D2. How ssh gets the terminal.** Without agent forwarding the program **replaces** this
  process (`execx.Execer`, `execve`): the terminal, every signal and the exit status are ssh's,
  and nothing is left to clean up. With forwarding (`-A`) a dedicated agent must end when the
  connection ends, so the run first replaces itself with `ssh-agent -- <itself> <args>`
  (ssh-agent's command mode: the agent watches its parent and exits when it is gone, even after
  a `SIGKILL`), and inside that the program runs as a **foreground child**
  (`execx.Foregrounder`): it stays in the terminal's foreground process group, so Ctrl-C and
  Ctrl-\\ reach ssh directly, SIGTERM and SIGHUP are passed on to it, and when it ends
  `ssh-agent -k` ends the agent at once.
- **D3. No secret passes through the tool.** It never reads a passphrase: ssh, scp,
  ssh-copy-id and ssh-add ask for one on the terminal themselves. The key's path is in argv as
  before (a path is not a secret). Nothing is logged.
- **D4. One seam for processes.** ssh, scp, ssh-copy-id, ssh-agent and ssh-add run through
  `internal/execx`; the menus are `ui.Lines` on `/dev/tty`. No `~/.ssh/config` is parsed: ssh
  is asked (`ssh -G`, which connects to nothing) what it would do.
- **D5. `ssh-with-agent.bash` stays** for `ssh-with-password` and `scp-with-password`, which
  still source it; only its comment loses the two key wrappers.

## 3. Expectations

`<tool>` is the command's name, `KEY` the login key, `~` the home directory as printed
(`term.AbbrevHome`). Every message is on stderr; stdout carries only `--help` and what the
program itself prints.

### Command line

- E1. `-h` or `--help` as the first argument prints the usage on stdout, exit 0. The usage
  names the key flag, says everything else is passed to the program, describes `-A` (ssh and
  scp), and gives examples with `host.example.com`.
- E2. No arguments: `<tool>: no arguments — see '<tool> --help'.`, exit 64.
- E3. The key flag (`-i` for ssh and scp, `--auth-key` for ssh-copy-id) is recognised only as
  the first argument. Without a following non-empty value: `<tool>: <flag> needs a key path — see '<tool> --help'.`, exit 64. Everything after it is passed to the program unchanged and in
  order.
- E4. A second identity among the passed arguments is a usage error, exit 64, before anything
  runs: for ssh and scp a `-i` in an option cluster or any `-o IdentityFile` (`-oIdentityFile=…`,
  the key in any case, `=` or a space); for ssh-copy-id only `-o IdentityFile` (its `-i` is the
  key to install). The message says to give the one key as the first argument. ssh-copy-id-with-key
  requires its `-i FILE` (E20): without it (or with `-i` given no value)
  `<tool>: -i FILE, the public key to install, is required — see '<tool> --help'.`, exit 64.
- E5. A given key must be a readable regular file whose first line is a private-key header
  (`-----BEGIN ` … `PRIVATE KEY-----`). A path ending `.pub`:
  `<tool>: <path> is a public key; give its private key <path without .pub>.`; anything else
  not a readable private key: `<tool>: <path> is not a readable private key: <reason>.`;
  exit 1, nothing run.

### The key picker

- E6. With no key flag the person chooses the login key from a numbered menu on `/dev/tty`:
  the regular files in `~/.ssh` (symlinks followed) whose first line is a private-key header,
  by name, numbered from 1, then `q) Cancel`. Heading `<tool> › Login key`, context
  `<n> private key(s) in ~/.ssh`; for ssh-copy-id the context adds `the key to log in with, not the key to install`.
- E7. A bad answer is shown what to type and asked again, three tries in all (`ui.Lines`);
  then `<tool>: no key chosen after 3 tries; nothing was run.`, exit 1. `q`, Ctrl-D or Ctrl-C:
  `Cancelled; nothing was run.`, exit 130.
- E8. No terminal to ask on: `<tool>: no key given and no terminal to choose one on; give it with <flag> KEY.`, exit 64.
- E9. No private key in `~/.ssh` (or no `~/.ssh`): `<tool>: no private keys in ~/.ssh; give one with <flag> KEY.`, exit 1.

### Only that key

- E10. The tool's options come before the passed ones, so ssh's first-value-wins keeps them:
  ssh and scp get `-i KEY -o IdentitiesOnly=yes -o PreferredAuthentications=publickey`, and
  `-o IdentityAgent=none` when not forwarding; ssh-copy-id gets `-f` (unless E20 found the key
  already installed) then `-o IdentityFile=KEY -o IdentitiesOnly=yes -o IdentityAgent=none -o PreferredAuthentications=publickey`.
- E11. Before anything connects (and before any agent starts), ssh is asked which identity
  files it would offer: `ssh -G -T` (`-T` keeps ssh's "stdin is not a terminal" note out of a
  failure's message) with the tool's options and the passed options that decide
  ssh's configuration, once per destination. For ssh-with-key that is every passed argument;
  for scp `-F`, `-o`, `-J`, `-4`, `-6`, `-P` given as `-p`, and each remote operand's user and
  host (and a `scp://` operand's port); for ssh-copy-id `-F`, `-o`, `-p` and the destination. An
  `identityfile` line other than KEY that names a file which may exist stops the run. ssh prints
  the paths unexpanded, so `~` and `%d` are made the home directory, `%%` a percent sign, and
  `%h`, `%n`, `%r`, `%p` the `hostname`, `host`, `user` and `port` lines of the same output; the
  file or its `.pub` must exist, a path with any other `%` token (`%u`, `%C`, `%l`...) counts,
  and `none` (the keyword) is no file. The message: `<tool>: ssh's configuration also gives <destination> the key(s) <list>, which ssh would offer too; this tool offers only KEY. Remove those IdentityFile lines for this host, or pass -F with a configuration that has none. Nothing was run.`, exit 1. A probe that fails shows
  ssh's words and exits 1. An scp with no remote operand needs no probe. A forwarded run's probe
  leaves out `IdentityAgent=none`, and an `identityagent` other than `SSH_AUTH_SOCK`, or a
  `forwardagent` that is a socket (it survives a command-line `-A`), stops the run:
  `<tool>: ssh's configuration gives <destination> <line>, so -A would forward that agent, not the one this run starts. Remove that line for this host, or pass -F with a configuration that has none. Nothing was run.`, exit 1.
- E12. Without forwarding the program replaces the process (D2). A program not installed:
  `<tool>: <program> is not installed.`, exit 1.

### Agent forwarding (ssh and scp)

- E13. Forwarding is asked by the options alone, never by a remote command or an option's
  value: a `-A` flag in an option cluster (a letter after an argument-taking letter is that
  option's value), with a later `-a` (ssh) turning it off again; with neither flag, the first
  `-o ForwardAgent` decides (`yes` forwards, `no` does not, keys and values in any case). Any
  other ForwardAgent value (a socket, a variable) is a usage error, exit 64: this tool forwards
  only an agent it started. So is any `-o IdentityAgent` in every tool (with `-A` ssh would
  forward that agent; without, the tool's `none` is the only value it may have), and scp's `-S`
  (another program run as ssh, which E11's probe cannot ask). ssh's argument-taking letters are `BbcDEeFIiJLlmOoPpQRSWw`, and its
  options run to the destination and on from there to the first word of the remote command (or
  `--`); scp's are `cDFiJlMoPSX`, its options ending at the first operand. ssh-copy-id has no
  forwarding (a `-A` reaches ssh-copy-id, which refuses it).
- E14. A forwarded run checks its arguments (E3-E5), chooses the key (E6-E9) and probes
  (E11) first, then replaces itself with `ssh-agent -- <this program> -i KEY <passed args>`
  with `SSH_WITH_KEY_AGENT=<its pid>` in the environment. The run inside (same pid) trusts the
  agent only when that variable is its own pid, `SSH_AUTH_SOCK` is set and `SSH_AGENT_PID` names
  a process whose parent is this process (`/proc/<pid>/stat`). A variable that is not its pid was
  exported by hand: `<tool>: SSH_WITH_KEY_AGENT is set (to <value>) in the environment, but not by this run; unset it and run again. Nothing was run.`, exit 1; any other failure
  `<tool>: the agent in SSH_AUTH_SOCK is not one this run started; nothing was run.`, exit 1. A
  variable set by hand never makes a run trust another agent. The variable is removed from
  every child's environment, and the inside run does not probe again.
- E15. The keys to forward are chosen one menu at a time: heading `<tool> › Keys to forward`,
  context `added: none` or the names added so far; a numbered line for each private key in
  `~/.ssh` not yet added, `a) All the keys not yet added` when more than one is left, `d) Done: forward the keys added` once one is added, `q) Cancel`. A chosen key is added at once by
  `ssh-add KEY` in the foreground (it asks its passphrase on the terminal itself). The menu
  returns until `d`, or until no key is left.
- E16. An ssh-add that fails is said (`ssh-add could not add <name> (exit N).`) and the menu
  returns with that key still offered; after three failures,
  `<tool>: three keys could not be added; nothing was run.`, exit 1. An ssh-add ended by Ctrl-C,
  or `q`, Ctrl-D or Ctrl-C at the menu: `Cancelled; nothing was run.`, exit 130. Every way out
  before the program runs ends the agent (`ssh-agent -k`).
- E17. Forwarding asked with no private key in `~/.ssh`: `<tool>: agent forwarding (-A) was asked, but ~/.ssh has no private key to forward; nothing was run.`, exit 1.
- E18. The program then runs in the foreground (D2) with the agent's `SSH_AUTH_SOCK`, and without
  `IdentityAgent=none` (the agent must be reachable to be forwarded; `IdentitiesOnly=yes` still
  keeps the login to KEY). An ssh login with no remote command and none of `-N`, `-W`, `-O`,
  `-G`, `-V`, `-Q`, `-s` gets `-t` first and the remote command
  `export GIT_SSH_COMMAND="ssh -F /dev/null"; exec "$SHELL" -l`, so `git push` over there uses
  the forwarded key whatever that machine's `~/.ssh/config` pins. Then `ssh-agent -k` ends the
  agent, and the exit status is the program's (128+n for a program killed by signal n).
- E19. An `ssh-agent -k` that fails is said; the run exits 1 if the program had succeeded,
  otherwise with the program's status.

### Installing a key (ssh-copy-id)

- E20. ssh-copy-id's own already-installed check logs in with each new key **and every `-o`
  it was given** (`filter_ids` in `/usr/bin/ssh-copy-id`), so with E10's `-o IdentityFile=KEY`
  the login key always succeeds and every new key reads as installed; only `-f` skips that
  check. So the tool checks itself, then runs ssh-copy-id with `-f`. `-i FILE` is required (E4);
  its private half is FILE without `.pub`, and must be a private key (E5's test), or
  `<tool>: <private> is not a readable private key (<reason>), and the check whether <FILE.pub> is already installed logs in with it. Pass -f to install it without the check. Nothing was run.`, exit 1.
  `ssh -G` with the check's options (below) must show no identity file but the private half (the
  configuration may give the host KEY itself, which E11 allows), or
  `<tool>: ssh's configuration also gives <destination> the key(s) <list>, so a login with <private> alone cannot be tried to see whether it is already installed. Remove those IdentityFile lines for this host, pass -F with a configuration that has none, or pass -f to install it without the check. Nothing was run.`, exit 1.
  Then `ssh -v -T -o BatchMode=yes -o ControlPath=none -o IdentitiesOnly=yes -o IdentityAgent=none -o PreferredAuthentications=publickey -i <private> <-F, -o, -p passed> <destination> exit`:
  a login, or `Server accepts key:` in its output (the host accepts the key, but batch mode could
  not unlock it to sign), is
  `<tool>: <FILE.pub> is already installed on <destination> (the host accepts the key); nothing to do.`,
  exit 0, nothing more run; `Permission denied` runs `ssh-copy-id -f <E10 options> <passed>`;
  anything else (an unknown host key, no route) is
  `<tool>: could not log in to <destination> with <private> alone to see whether it is already installed: <ssh's words without its debug lines> Pass -f to install it without the check. Nothing was run.`,
  exit 1. A passed `-f` skips the check (and is not doubled); a dry run (`-n`) is checked too.

## 4. Kept as it was

The flags and their place (first), the options of E10 and their order, the exclusion of the
agent and of password authentication, the remote git init of E18, and the agent being a fresh
one per run that the person fills.

## 5. Departures from the bash

- B1. **The promise was false.** With `IdentitiesOnly=yes` ssh still offers every
  `IdentityFile` its configuration gives the host (a `Host *` default included), and nothing on
  the command line can take them away (`-o IdentityFile=none` does not). The bash offered them
  silently; E11 asks ssh and stops.
- B2. A `-i` or `-o IdentityFile` later in the arguments added a second key (E4).
- B3. Forwarding was detected by any argument with an `A` after a single dash, the remote
  command's words and option values included (`ssh-with-key host ls -A`, `-oForwardAgent=no`,
  `-lAdmin` all started an agent and asked for keys), and only `ForwardAgent=yes` spelt exactly so
  (E13).
- B4. The bash's list of ssh's argument-taking letters lacked `-P` (tag), so the word after it
  was taken for the destination (E13).
- B5. The picker listed every file not excluded by name (a stray `environment`, `rc` or
  `config~`); now only private keys (E6). A given `.pub` or non-key went to ssh (E5).
- B6. A bad picker answer ended the run; now three tries, and `q` (E7).
- B7. The agent came from `eval "$(ssh-agent -s)"` and an EXIT trap, so a killed wrapper left an
  agent holding the forwarded keys; now ssh-agent's command mode ends it with the run (E14, E18).
- B8. Forwarding with no keys forwarded an empty agent, and a key ssh-add could not add was
  printed and the connection went ahead without it (E16, E17).
- B9. `-A` with `-N` (or `-W`, `-s`…) still got the remote init as a command (E18).
- B10. No arguments printed the usage on stdout with exit 1; now a usage error on stderr, exit
  64 (E2).
- B11. ssh-copy-id-with-key installed nothing: ssh-copy-id's check logged in with the login key
  the tool gave it and skipped every new key as "already installed" (exit 0). The check is now
  the tool's, with the new key alone, and `-i FILE` is required: a tool about choosing keys
  does not install "whatever ssh-copy-id finds" (E4, E20).
- B12. `-o IdentityAgent=<socket>` with `-A`, or a configuration's `IdentityAgent` or
  `ForwardAgent <socket>`, forwarded an agent the tool did not start; scp's `-S` ran a program the
  tool never asked about (E11, E13).

## 6. Proof

Expectation tests in `internal/sshkey` over `execx.Fake` (every unscripted command a failure),
a temporary home with key files, a scripted terminal and a fake `/proc`; the `cmd` packages test
that each `main` runs its tool. No oracle against the bash: these share no on-disk or wire format
with it beyond ssh's own options, which E10 and E18 pin.

## 7. Host checks

The host agent uses a throwaway key (`ssh-keygen -N ''` in a temporary directory) against a
VM-lab guest, or against localhost only when sshd already runs there, with the throwaway key's
line removed from `authorized_keys` afterwards.

- H1 (host agent). The three commands at `~/.local/bin` are ELF binaries; each `--help` prints
  its usage; each with no arguments exits 64; `ssh-with-key -i <throwaway>.pub host.example.com`
  refuses with E5's message and runs nothing.
- H2 (host agent). `ssh-with-key -i KEY -v <guest> true` exits 0, and its `-v` output has one
  `Will attempt key:` line, KEY's; `ssh-with-key -i KEY <guest> 'exit 7'` exits 7;
  `scp-with-key -i KEY` copies a file to the guest and back unchanged. With a second throwaway
  key NEW not on the guest: `ssh-copy-id-with-key --auth-key KEY -n -i NEW.pub <guest>` (dry
  run) exits 0, prints ssh-copy-id's `Would have added the following key(s):` with NEW's line,
  and does not print `already installed`; `ssh-with-key -i NEW <guest> true` still fails
  (nothing changed). The same without `-n` exits 0 with `Number of key(s) added: 1`, after which
  `ssh-with-key -i NEW <guest> true` exits 0; a third run says
  `NEW.pub is already installed on <guest>`, exits 0, and the guest's `authorized_keys` holds
  NEW's line once (`ssh-with-key -i KEY <guest> "grep -c '<NEW's base64>' .ssh/authorized_keys"` prints 1). NEW's line is removed afterwards.
- H3 (host agent). With `-F <temporary config>` giving the guest a second `IdentityFile` that
  exists, the run stops with E11's message naming it, and nothing connects.
- H4 (host agent, in tmux). `ssh-with-key -i KEY -A <guest> 'ssh-add -l'`: the menu lists the
  keys in `~/.ssh`; choosing one (or a throwaway placed there for the check and removed after)
  then `d`, the remote `ssh-add -l` lists it; the same run with `-v` (`ssh-with-key -i KEY -A -v <guest> 'ssh-add -l'`, an agent holding other keys reachable) has exactly one
  `Will attempt key:` line, KEY's; afterwards no `ssh-agent` whose parent was the run
  is left (`pgrep -u "$USER" -x ssh-agent` counts the same before and after). The same with
  `q` at the menu: `Cancelled`, exit 130, no agent left.
- H5 (owner). The owner's own hosts with the owner's keys: an ordinary login, and an
  interactive `-A` login where `git push` works on the far side. If the owner's
  `~/.ssh/config` gives every host an `IdentityFile`, E11 refuses; that is the decision B1
  records, and the owner says whether it stands.

Left by decision: keys typed ahead between the forwarding menu and the program are lost (the
menus read `/dev/tty` through a buffer that is closed before ssh starts). `internal/ttyecho`
would keep them; the menus are short and a lost keystroke there costs a retype, so this family
does not run under it.
