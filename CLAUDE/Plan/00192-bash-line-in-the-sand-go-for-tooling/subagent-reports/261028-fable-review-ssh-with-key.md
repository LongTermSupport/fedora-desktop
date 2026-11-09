# Fable review: the ssh-with-key port (`go-port/ssh-with-key`, one commit on `origin/F44`)

Reviewed: the branch diff, the three bash originals and `ssh-with-agent.bash` at `origin/F44`,
`SPEC-ssh-with-key.md`, the worker report, `src-go/CLAUDE.md`, `GoProjectRules.md`. Checked in
the container: `go test` over `internal/sshkey`, `internal/term` and the three `cmd` packages
(all pass); `ssh -G` (OpenSSH 9.2) against a `-F` configuration with `Host *`, `Match final all`
and `%d`/`%h` tokens; `ssh-agent -- <cmd>` command mode (the command keeps the caller's pid, the
agent's `PPid` is that pid, `ssh-agent -k` writes its `unset` lines to stdout, which `Runner.Run`
captures); scp's option parsing (no permutation: an option after an operand is an operand, so
`readSCP` matches scp); and the installed `ssh-copy-id`'s source. Nothing in the branch was
changed.

## BLOCKER

### B1. `ssh-copy-id-with-key` installs nothing: ssh-copy-id's own already-installed check logs in with the login key and skips every new key

- `src-go/internal/sshkey/app.go:183-188` (`ownOptions` for `kindCopyID`), `SPEC-ssh-with-key.md`
  E10/H2, `usage-copy-id.txt`.
- Scenario. `ssh-copy-id-with-key --auth-key KEY -i new.pub host`. ssh-copy-id folds every
  `-o` into `SSH_OPTS` (`/usr/bin/ssh-copy-id:127`), and before installing it runs, for each
  key to install, `ssh -i <new private> ... -o IdentitiesOnly=yes $SSH_OPTS exit`
  (`filter_ids`, lines 180-193) and treats a login that **succeeds** as "this key is already
  installed". With our `-o IdentityFile=KEY` in `SSH_OPTS`, ssh offers the new key, then KEY;
  KEY works (that is the whole premise of the tool), so every new key is filtered out:
  `WARNING: All keys were skipped because they already exist on the remote system.`, exit 0,
  nothing installed (`populate_new_ids`, lines 222-234). Only `-f` bypasses the filter.
- The bash had the identical defect (same four `-o` options), so this is inherited, but the
  port's method is that the bash is a prototype to be fixed, and the spec restates the broken
  promise ("the key that gets installed is chosen the normal ssh-copy-id way"). Worse, H2's
  dry run (`-n`) runs the same filter and prints the same warning with exit 0, so the host
  check as written would record a pass for a tool that cannot do its job.
- Fix. The tool cannot hand ssh-copy-id a login option without that option reaching
  `filter_ids`. Do the check in Go and call ssh-copy-id with `-f`: require `-i NEW` for this
  command (a tool about choosing keys should not install "whatever is default"; spec E4/E10
  change, say so as a departure), probe
  `ssh -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none -o PreferredAuthentications=publickey -i <NEW without .pub> <probe args> <dest> exit`
  through the runner; exit 0 is "already installed, nothing to do" (say it, exit 0); exit 255
  with `Permission denied` is "install it", anything else is ssh's error; then
  `ssh-copy-id -f <four -o options> -i NEW <passed> <dest>`. Rewrite H2 to require ssh-copy-id's
  `1 key(s) remain to be installed` line (or the Go equivalent) and, for the non-dry run, the
  key present in the guest's `authorized_keys` afterwards. Add the departure to section 5.

## FIX-BEFORE-MERGE

### F1. `-A` with a passed `-o IdentityAgent=<socket>` forwards an agent the tool did not start

- `src-go/internal/sshkey/args.go:85-101` (`agentAsk.option`), `app.go:180-194` (`ownOptions`,
  `noAgent=false` when forwarding), spec E13/E18.
- Scenario. `ssh-with-key -i KEY -A -o IdentityAgent=$SSH_AUTH_SOCK host`. Not forwarding, our
  `IdentityAgent=none` comes first and wins. Forwarding, ours is omitted (E18), so the user's
  value stands; ssh puts that socket in `SSH_AUTH_SOCK` and forwards **that** agent (and, with
  `IdentitiesOnly=yes`, could also authenticate with its KEY copy). E13 refuses
  `ForwardAgent=<socket>` for exactly this reason and misses the other spelling of it.
- Fix. In `option`, refuse any `IdentityAgent` the way `IdentityFile` is refused ("this tool
  sets IdentityAgent; use -A for a new agent"), exit 64; a test beside
  `TestE13AnotherAgentIsRefused`; a line in E4 or E13.

### F2. `meta-deploy.list` does not tell the host agent that `play-claude-yolo.yml` is in this run

- `CLAUDE/Plan/meta-deploy.list:41-47`, `files/var/local/claude-yolo/claude-yolo:17`.
- `changed-plays` judges a play by its inputs (`helpers/play_ledger/changed_plays.py:210`,
  through `affected_plays`), and `play-claude-yolo.yml` deploys the launcher file this branch
  bumps to 3.101.2 and builds the Go front with `-X main.Version`. The entry says "Also
  play-basic-configs.yml ... nothing else in the play changes" and does not mention the
  launcher play, so the host agent will see an unexpected play in the set and has no stated
  expectation for it (the image does not rebuild: the container version is unchanged).
- Fix. One sentence in the same entry: play-claude-yolo.yml also runs, for the 3.101.2 bump,
  and is expected to change only the launcher file and the `claude-yolo` front binary.

### F3. H2 and H4 must be re-stated so a pass means the code path ran

- `SPEC-ssh-with-key.md:188-199`. H2's ssh-copy-id line is covered by B1. H4 checks the agent
  count before and after, which is right, but does not check that the **login** used KEY alone
  while an agent holding other keys was present: add `-v` to the `-A` run and require the one
  `Will attempt key:` line, as H2 does for the plain login. That is the one property the
  forwarding design exists to keep (the agent's keys are reachable by ssh, and
  `IdentitiesOnly=yes` plus E11's refusal is what stops them being offered).

## FOLLOW-UP

### U1. `%h`, `%r`, `%u`, `%p`, `%n` in a configured IdentityFile refuse even when no such file exists

- `probe.go:70-92` (`expand`), E11. `ssh -G` prints identity paths unexpanded (verified:
  `identityfile %d/.ssh/final_%h` under `Match final all`). Any token but `%d` and `%%` counts
  as a key ssh may offer, so `IdentityFile ~/.ssh/%h_key` with no matching file refuses every
  host, and the message tells the person to remove a line that offers nothing. `ssh -G` also
  prints `hostname`, `user`, `port` (and the original host is the destination), which expand
  `%h %r %p %n` (`%u` is the local user); `%C`/`%l`/`%L`/`%i`/`%k` can stay "counts".

### U2. `IdentityFile none` in a configuration is tested as a relative file named `none`

- `probe.go:60` (`mayExist`). `ssh -G` prints `identityfile none` (verified). `none` is a
  keyword that offers nothing; skip it by name.

### U3. A stale `SSH_WITH_KEY_AGENT` in the environment bricks `-A` with a misleading message

- `app.go:141`, `agent.go:50-56`. The outer run goes to `inAgent` whenever the variable is set
  at all, and the only message is "the agent in SSH_AUTH_SOCK is not one this run started".
  The variable is stripped from every child (good), so this needs a hand export, but the
  message should name the variable and say to unset it.

### U4. scp's `-S program` is not what the probe asks

- `args.go:227-242`. A run with `-S other-ssh` asks the system `ssh -G`, not `other-ssh -G`.
  Either pass `-S` through to the probe as the program to run (a `dynamicNames` row) or refuse
  `-S` as E13 refuses the other "a different ssh" spellings.

### U5. A failed probe's message carries ssh's tty warning

- `probe.go:42`, `execx.go:141`. `Runner.Run` gives the child no stdin, and `ssh -G` then
  prints `Pseudo-terminal will not be allocated because stdin is not a terminal.` on stderr
  (verified); on success it is captured and dropped, on failure it leads the message. Pass
  `-T` first in the probe argv (it changes no identity).

### U6. Typed-ahead keys between the menu and the program are lost

- `keys.go:214-225` (`terminal`), `ui/lines.go:216`. `readLines` reads through a
  `bufio.Reader` on `/dev/tty`, so bytes typed after the answer are consumed by the buffer and
  gone when the tty is closed before ssh starts. `internal/ttyecho` exists for this
  (GoProjectRules, "echo held"); `reclaim` and `open` run under it, this family does not.

### U7. After merge, PLAN.md and PORTS.md say "review pending, not merged"

- `PLAN.md:247-251`, `PORTS.md:432-434`. The merge commit must change these lines (the plan
  commit rule), and section 5 of the spec gains B1's departure (B1 above) as B11.

## OK (attacked and held)

- **Agent lifetime.** `ssh-agent -- <self>` is exec'd (`agent.go:37-46`): the inner run keeps
  the outer pid, the agent's parent is that pid (verified), the marker is checked against
  `Getpid` and `/proc/<SSH_AGENT_PID>/stat` (`proc.go`, `parentFromStat` handles a command
  name with spaces or parentheses). A hand-set marker never makes another agent trusted
  (`TestE14AnAgentThatIsNotTheRunsOwnIsRefused`, six ways). `ssh-agent -k` runs on every exit
  path, under `context.WithoutCancel` so a Ctrl-C does not skip it (`agent.go:74-83`); its
  shell lines go to the captured stdout, not the terminal. A SIGKILLed run leaves the agent
  for at most ssh-agent's 10 s parent check (it ignores SIGINT, so the terminal's Ctrl-C never
  reaches it). The user's own agent is never used or killed: command mode replaces
  `SSH_AUTH_SOCK` and `SSH_AGENT_PID`; without `-A`, `IdentityAgent=none` is first.
- **Signals and exit codes.** Without `-A`, `execve` (`app.go:172`). With `-A`,
  `execx.Foreground` keeps ssh in the foreground group, forwards TERM/HUP, and the run's
  status is ssh's (`128+n` for a signal; `TestE18TheProgramsStatusIsTheRuns`); a Ctrl-C the
  child handled is read from `Exit`, not from the cancelled context (`agent.go:233-238`).
  Ctrl-C in `ssh-add`'s passphrase prompt: `readpassphrase` restores the terminal and re-raises,
  so `interrupted` sees the signal and the run says `Cancelled`, 130, after `-k`.
- **No secret through the tool.** `ssh-add KEY` asks on the tty itself; nothing reads a
  passphrase; argv carries paths only; no logging.
- **The probe sees what the connection sees.** For ssh every passed argument is in the `-G`
  argv (`args.go:206`), so `-F`, `-o`, `-J`, `-l`, the destination form and the remote command
  are identical; `-G` applies `Include` and `Match final` (verified); with `-i` given, ssh adds
  no default identities (verified), so a plain configuration is not refused. For scp and
  ssh-copy-id the options that decide configuration are mapped (`-P` to `-p`, user to `-l`,
  `scp://` port). A failed `-G` stops the run with ssh's stderr (`TestE11AFailedProbeStops`);
  the refusal names the host, the keys and both fixes.
- **Argument parsing.** Forwarding only from `-A`/`-a` flags or the first `ForwardAgent`
  option; a letter after an argument-taking letter is a value (`-lAdmin`), a remote command's
  `-A` is not an ask, `--` ends options before and after the destination, `-N/-W/-O/-G/-V/-Q/-s`
  get no remote init, scp's letters are scp's getopt string (which includes `M:`), ssh-copy-id's
  optional `-i` value is handled. A second `-i` or `-o IdentityFile` is refused in any case
  and spelling.
- **`term.ExpandHome` and the CCY bump.** Needed: `project_homeabbrev` flags
  `strings.CutPrefix(path, "~/")` outside `internal/term`, so the inverse of `AbbrevHome` has
  one lawful home, and `term` is in the launcher front's build, so the hook demanded the bump.
  The changelog entry says behaviour is unchanged and why; that is honest.
- **Play wiring.** Three `import_tasks` of `build-go-command.yml` with name, old path and
  mode; `play-golang.yml` is imported at the top of the play; the install is a
  checksum-compared `copy`, so a rerun reports ok. Allowlist rows and the bash files are gone,
  `go-binary-suites.tsv` has the three rows, `ssh-with-agent.bash`'s header names only the
  password wrappers that still source it. No reference to the old script paths remains outside
  the plan tree. Nothing install-specific: examples use `host.example.com`, test keys are a
  split header with a nonce body.
- **Tests.** Every expectation has a test over the fakes; the `ssh -G` fixture matches the
  real output format (`identityfile <as configured>`); the `/proc` reader has its own test;
  the `cmd` tests reach the app. What the fakes cannot prove is section 7's, and B1 shows the
  one place where the real program's behaviour was assumed rather than read.
