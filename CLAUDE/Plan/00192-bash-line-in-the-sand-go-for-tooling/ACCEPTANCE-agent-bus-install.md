# agent-bus-install: host acceptance (the owner's step)

`agent-bus-install` became a Go binary (`src-go/cmd/agent-bus-install`). It runs as root, builds a
homeserver host, and its decisions only show on a real one, so this is the check the suite
(`scripts/test-agent-bus-install.bash`, 309 cases under stubs) cannot give. The mechanical part
runs through `meta-deploy.bash`; the rest is yours.

Nothing here is run by hand to change the host. The play is the only install route
([InfrastructureAsCode.md](../../InfrastructureAsCode.md)).

## What changed on the host

- `play-agent-bus.yml` now builds the installer from this checkout as the login user (the shared
  `tasks/build-go-command.yml`, with the source digest baked in) and installs a root-owned copy at
  `/usr/local/sbin/agent-bus-install`, mode 0755, over the bash script that was there. Every later
  step of the play runs that installed copy. What this narrows: root no longer runs the installer
  as a script from the checkout. What it does not: the installer still runs the checkout's Python
  as root (the zipapp bundler, with the checkout as its working directory) and installs the
  checkout's admin wrapper, which root runs later, and the binary is copied from the build
  directory in the user's cache. Whoever can write the checkout can still get code run as root by a
  play run; that is the existing boundary of every play in the repository, not new here. The path, the mode, the units'
  `ExecStart=/usr/local/sbin/agent-bus-install backup-now --team %i` and the
  `sudo -n /usr/local/sbin/agent-bus-install ...` argv of ccy-teams are the same as before.
- A team stamped by the bash installer is "out of date" in `ccy-teams` after this change, once:
  the stamp's software digest was the hash of the bash file and is now the hash of the Go source
  (`go-source digest`, package `src-go/qa/gobuild`). The play restamps each declared team.
- Deliberate behaviour changes (spec section 11, each pinned by a suite case): a zone id or an
  IPv4-mapped `--bus-address` is refused (B2); a curl that exits 78 is exit 1, not a
  configuration error (B3); the restore stage is never under `TMPDIR` (B4); a symlink planted in
  a team's `secrets/` fails `backup-now` (B6); a shared secret with the wrong mode is repaired and
  reported (B7); another team's malformed `team.json` is exit 78 naming the file (B10); a bad
  `--bus-address` is refused before the host is looked at (B11); a relative `--source` is a usage
  error.

## 1. Run `./CLAUDE/Plan/meta-deploy.bash`

Its entry for this plan runs `deploy.bash`, which runs `play-claude-yolo.yml`,
`play-agent-bus.yml`, `play-podfreeze.yml` and `play-lxcfreeze.yml`, then `acceptance.bash`. For the
installer the play does, in order: computes the source digest, builds the binary into
`~/.cache/ccy-go-build/`, installs it at `/usr/local/sbin/agent-bus-install`, then runs
`software`, `remove` for each team declared absent, `team` for each declared present.

Read the play recap. On the first run the build, the copy and (when teams are declared) the
`team` steps report changed. A team whose homeserver is running is restarted only if its rendered
files changed; the stamp alone does not restart it.

`acceptance.bash` checks, as PASS or FAIL lines under "the installed agent-bus-install":

1. `/usr/local/sbin/agent-bus-install` is a compiled program, owned `root:root`, mode 755.
2. `agent-bus-install --version` prints the same digest that
   `go run ./cmd/go-source digest --source <checkout> --command agent-bus-install` (in
   `src-go/qa`) computes. A mismatch means
   the binary was built from other source than the checkout holds now.
3. `files/usr/local/sbin/agent-bus-install` is gone from the checkout.

It also writes `agent-bus-install-probes.txt` into its run directory (under
`untracked/plan-runs/`), the raw evidence for step 3 below.

## 2. Run it again

`./CLAUDE/Plan/meta-deploy.bash` a second time: the `play-agent-bus.yml` build, install, `software`
and `team` steps report ok, not changed (the build is reproducible, the install compares
checksums, the installer changes nothing that is already right). Any `CHANGED` line the installer
prints on a converged host is a finding; keep the output.

## 3. B12 and B13: the port fixed both; the probes confirm it on a real host

The spec left two bash behaviours to be measured (B12, B13). The port did not keep them: it fixed
both, and the suite pins each under stubs (`scripts/test-agent-bus-install.bash`, "Go port: B13"
and "Go port: B12"). What a real host can still show is whether the real tools say what the stubs
say. Read `agent-bus-install-probes.txt`.

- **B13, IPv6 on `agentbus0`.** `nmcli -g` (terse) escapes every `:` in a value as `\:`, and several
  addresses come comma-separated. The installer normalises: `netprobe.AddressesMatch` unescapes
  `\:`, splits on `,` and `|`, drops `--` and compares parsed prefixes, so an IPv6 bus address
  equals what nmcli prints. The first section of the probes file is the raw terse output. If the
  host's `agent_bus_address` is IPv6, the second run in step 2 must print no `CHANGED` for
  `agentbus0`; a `CHANGED` there is a finding (the real nmcli prints something the parser does not
  expect: keep the first section). For an IPv4 host there is nothing to settle.
- **B12, an interface with no firewalld zone.** The text `no zone` is authoritative:
  `netprobe.ParseZone` treats it as "no zone" whatever the exit status, and the rule then goes to
  the default zone. The second section lists each interface as `name<TAB>exit status<TAB>text`. An
  interface with no zone is expected to show a non-zero status and the text `no zone`; a line with
  status 0 and that text is handled the same way. A line that shows neither a zone name nor
  `no zone` (a different message) makes the installer fail with that text, which is the intent.
  Report any such line.

## 4. A team, if the host has one declared

`ccy-teams` (or `agent-bus-install check --team <team>` through `sudo`) for a declared team shows
it up to date, its homeserver running, and `check` prints no `FAIL` fact. A backup and a restore are
the destructive part: do them on a throwaway team, not a real one.

- `sudo agent-bus-install backup-now --team <throwaway>` prints `CHANGED backup 1 of team <throwaway>`;
  `sudo agent-bus-install backups --team <throwaway>` lists it; `sudo agent-bus-install restore --team <throwaway> --backup 1` restarts the homeserver and prints `CHANGED restored team ...`.
- `systemctl list-timers 'agent-bus-backup@*'` still lists the daily timer, and its service's
  `ExecStart` is the installed binary.

## If something is wrong

The previous bash installer is in git history, but do not copy it back by hand: fix the cause in the
checkout and run the play again, or tell the agent what the run printed.
