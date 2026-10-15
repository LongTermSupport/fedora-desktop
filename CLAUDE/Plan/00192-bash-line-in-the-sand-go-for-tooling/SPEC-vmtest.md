# vmtest: specification of the Go port

Programme P5 of [INVENTORY.md](INVENTORY.md). **Method: the legacy bash is not an oracle.** This
document states the intended behaviour of the host CLI of the VM acceptance lab (Plan 00110) as
numbered, testable expectations. The bash (`files/home/.local/bin/vmtest` at `14058c62`, 1685
lines, 67 functions) was read only to find the requirements. Where it was wrong, the expectation
says the correct behaviour and section 4 lists the departure. There is no recording of the bash.
The proof is the Go tests in `src-go/internal/vmtest`, which run every flow over a fake runner,
a fake clock and a temporary lab tree. The behaviours that only a host with KVM can prove are
listed in section 7.

## 1. Role and callers

`vmtest` is a thin executor. Every decision (manifest parsing, freshness, the base record, the
verdict, retention) lives in the unit-tested Python helpers that `play-vm-test-lab.yml` deploys
to `/usr/local/lib/ccy-helpers/helpers/vmtest`. The CLI fetches, verifies, boots, drives a guest
over SSH and calls the helpers. It reads only Ansible-deployed copies (the manifest, the guest
scripts, the helpers, the kickstarts), never the checkout (DESIGN.md §2.3).

Callers:

- a person at the host;
- `helpers/vmtest/bridge_run.py`, which runs `vmtest run <scenario>` with stdout and stderr
  merged into one log. It finds the `VMTEST-RUN` line, and if that line is missing it reads the
  stage from the `==> ` progress lines and the reason from the last `ERROR:` line (section 3);
- `vmtest-nightly.service` (`vmtest nightly`);
- Plan 00161's acceptance (`ensure-base`, `member-up`, `member-ssh`, `member-down`).

## 2. Scope decisions

- **D1. The Python helpers stay Python, behind one seam.** All 21 modules in
  `helpers/vmtest` are imported by more than this CLI. The bridge watcher and runner are Python
  daemons, `play-vm-test-lab.yml` runs `validate_manifest`, and `qa-vmtest-manifest` (Go) calls
  the same validator so that a valid manifest has one definition. They have their own tests
  (`tests/helpers/vmtest`, 21 files). Porting them now would replace a tested library and leave
  the bridge on the old copy. Under the CLAUDE.md rule they move to Go when they are next
  reworked, and the bridge goes with them. The Go reaches them through one function
  (`Lab.helper`): `python3 -P -m helpers.vmtest.<module>` with `PYTHONPATH` set to the deployed
  library. Their stderr streams to the CLI's stderr, and their stdout is parsed or written to a
  file as each call needs.
- **D2. The run-log scrubber stays bash.** `scripts/lib/run-log-scrub.bash` sources the commit
  gate's detection engine (`git-hooks/lib/secret-scan.bash`), so there is one matcher and one
  allowlist. A Go copy would be a second matcher. A small executable front
  (`scripts/lib/run-log-scrub-cli.bash`, `redact` and `verify`, within the bash limits) is
  deployed beside it. The Go runs it as `bash <front> redact|verify ...`. No `bash -c` is used.
- **D3. The guest scripts stay bash.** They run inside the Fedora guest before any Go exists
  there (`guest-*.bash`, `vmtest-session-provision.bash`).
- **D4. Work done natively in Go.** Hashing (sha256), file size and mtime, the free port, the
  run id and timestamps, random secrets (crypto/rand), reading the probe and marker lines, the
  session-environment diff, and every file move and removal are done in Go. External programs
  stay where they are the tool: `curl`, `gpg2`, `qemu-img`, `virt-install`, `virsh`, `ssh`,
  `scp`, `blkid`, `cp --reflink=auto`, `openssl passwd -6`, `systemd-escape`, `python3`, and
  `git ls-remote`, which goes through `internal/gitx`, the owner of `git`.
- **D5. The bash test suites go.** `test-vmtest-member.bash`, `test-vmtest-host-only-gate.bash`,
  `test-vmtest-reboot-dispatch.bash` and `test-vmtest-build-dir-cleanup.bash` source the bash or
  pull functions out of it. Their cases are now Go tests (section 6), so their gate rows are
  removed. `test-vmtest-prepare-record.bash` keeps its row and its guest-script cases. Its one
  case that extracted `prepare_failure_reason` from the CLI is now a Go test (E40).
  `test-vmtest-kernel-selection.bash` tests a guest script and is unchanged. The qa-all golden
  replay restores the removed rows into its table, because they were recorded from the bash
  (`retiredAfterPort`, the mirror of `addedAfterPort`).

## 3. Expectations

The tests cite these numbers.

### Command line, output streams, exit codes

- E1. `vmtest` with no argument, `help`, `-h` or `--help` prints the usage to stdout and exits 0.
  An unknown command prints the usage to stderr, then `ERROR: unknown command: <cmd>`, and exits
  64\.
- E2. A wrong argument count, an unknown option, a repeated option, or an option without its
  value prints `ERROR: usage: vmtest <command> ...` (or `ERROR: unknown option: <opt>`, or
  `ERROR: <opt> needs a value`) on stderr and exits 64. Nothing is booted and nothing is written.
- E3. Progress lines go to stderr as `==> <text>`. Failures go to stderr as `ERROR: <text>`, and
  the command then exits 1. stdout carries only the command's marker lines (`VMTEST-BASE-BUILT`,
  `VMTEST-RUN`, `VMTEST-REFRESH-DONE`, `VMTEST-FRESHNESS-STATUS`, `VMTEST-BASE-CURRENT`,
  `VMTEST-MEMBER-UP`, `VMTEST-MEMBER-DOWN`), the usage text, `verify_response`'s output, and
  `member-ssh`'s remote stdout. The output of external programs (virt-install, virsh, curl's
  progress bar, dnf in the guest) goes to stderr.
- E4. SIGINT, SIGTERM or SIGHUP during any command cancels the step in progress. The command's
  teardown then runs (E27, E46, E54), and the exit status is 130, 143 or 129. A signal never
  leads to exit 0.
- E5. The bridge's stage contract. These texts are kept verbatim because `bridge_run.py`
  matches them: `==> freshness: <decision> (degraded=<d>)`, `==> run <id>: booting ...`,
  `==> waiting for SSH ...`, `==> guest answered SSH ...`, `==> fetching run.bash ...`,
  `==> provisioning ...`, `RUN-BASH-EXIT <rc>`, `==> verdict: ...`. A refusal names
  `not in the deployed allowlist`, `is not runnable`, `(stage: base)`, `freshness:` or
  `unknown provenance` as E15, E17, E22 and E23 give them.

### Environment and the lab

- E6. `VMTEST_HOME` (default `~/.local/share/vmtest`) is the lab tree, and `VMTEST_HELPERS_LIB`
  (default `/usr/local/lib/ccy-helpers`) is the helpers' `PYTHONPATH`. An empty value means the
  default.
- E7. `lab.env` is read as data and never executed. It is `KEY=VALUE` lines; blank lines and `#`
  comments are skipped. Any other line, or a repeated key, is
  `ERROR: <lab.env>:<n>: ...`. The keys that matter are `VMTEST_FEDORA_VERSION` (digits only),
  `VMTEST_REPO_URL` (`https://...`), `VMTEST_REPO_BRANCH` (non-empty) and `VMTEST_CHECKOUT`
  (needed only by `verify` and `sweep`, which require it to be an existing directory). Unknown
  keys are allowed.
- E8. Loading the lab requires a readable `lab.env`, `scenarios.json`, the lab key pair
  `ssh/lab-key` and `ssh/lab-key.pub`, `guest-cleanup.bash`, and the directory
  `/usr/share/distribution-gpg-keys/fedora`. Each one missing has its own `ERROR:` line that
  names `play-vm-test-lab.yml`. The lab load then creates `images/` and `bases/`.
- E9. Base facts come from `validate_manifest --fedora-version V --base KEY` with the manifest
  on stdin. The output must be exactly one `VMTEST-BASE` line. Its fields are `key`, `name`,
  `kind`, `profile`, `tree`, `vcpus` and `ram_mib`, each present exactly once. `vcpus` and
  `ram_mib` must be positive integers. An unknown, missing or repeated field, any other output,
  or a non-zero exit from the validator is an `ERROR:` and exit 1.
- E10. Scenario facts come from `--scenario ID` in the same way: one `VMTEST-SCENARIO` line with
  `id`, `base`, `base_name`, `profile`, `planned`, `max_skipped`, `runnable`, `host_only`,
  `reboot_before_checks` and `run_env`. Each of `runnable`, `host_only` and
  `reboot_before_checks` must be exactly `true` or `false`. Any other value is refused:
  `validator reported host_only='<v>', which is neither true nor false`.
- E11. `run_env` is `-` (no overrides) or comma-separated `RUN_BASH_<A-Z_>=<[A-Za-z0-9._,:/=-]+>`
  pairs. Anything else is `malformed run_env entry: <pair>`.

### The upstream probe

- E12. Every command that probes runs `probe_upstream --fedora-version V --manifest <json>` once
  and keeps its stdout in a private temporary file (the helpers read it by path). The file is
  removed when the command ends, on every path, signals included. If the probe exits non-zero,
  its `VMTEST-FRESHNESS-UNREADABLE` lines are copied to stderr, and the command fails with
  `upstream freshness signal unavailable — cannot prove what the published media is`.
- E13. A single-valued marker (`VMTEST-FRESHNESS-REVISION`, `-COMPOSE-ID`, the `TREE-URL` of
  a tree, a desktop artefact named by prefix) must occur exactly once. None is
  `probe printed no <marker> line`. More than one is a failure naming the marker.

### fetch

- E14. `fetch <base-key>`: load the lab, take the base facts, probe. Then, for each
  `VMTEST-FRESHNESS-ARTEFACT <base-name> <file> <sha> label=<l> variant=<v> url=<u>` of the base,
  in probe order:

  - download it to `images/<file>` with `curl -fL --progress-bar -o <file>.partial`, adding
    `-z <file>` when a local copy exists. A non-empty `.partial` replaces the file
    (`downloaded <file> (<n> bytes)`). An empty one is removed and keeps the current copy
    (`<file> is current (not modified upstream)`). If there is no copy at all, that is a failure.
    A failed curl removes the `.partial` and fails with `download failed: <url>`;
  - download `Fedora-<v>-<l>-x86_64-CHECKSUM` from the same directory of the URL in the same way;
  - verify it with `gpg2` against `RPM-GPG-KEY-fedora-<V>-primary`, imported into a private
    0700 home that is removed afterwards. A failed import or verify prints gpg's output and
    fails. A verify failure says `GPG signature verification FAILED for <checksum> — refusing the artefact`;
  - read the one `SHA256 (<file>) = <64 hex>` line. If it is missing, the run fails. If it
    differs from the probe's sha, the run fails with `releases.json (<a>) and <checksum> (<b>) disagree about <file>`;
  - hash the downloaded file. A mismatch is `SHA-256 MISMATCH for <file>: expected <e>, got <a>`.

  A base that the probe lists no artefact for fails: `the probe listed no artefact for <name>`.

### build-base

- E15. `build-base <base-key>`: the kind must be `fast` or `full`, and a non-server profile must
  be `full`. The command runs `fetch` (E14) and keeps its probe. Every artefact goes into the
  record (`--artefact <file>=<sha>`). The first artefact names the compose label and is the
  file that a fast base is seeded from. The compose id and the revision come from the probe
  (E13).
- E16. Before anything is built: `retention floor --path bases --base-size <n> --step rebuild --guest-mib <ram>`.
  `<n>` is the size of the existing base disk if there is one, otherwise the size of the first
  artefact. A refusal is
  `the host cannot carry a rebuild of <name> (stage: base); see the VMTEST-DISK/VMTEST-RAM lines above`.
- E17. The build domain is `vmtest-build-<name>`, and its directory is `bases/<name>.build`. If
  libvirt already knows the domain, the command fails before any change, and the message names
  the `virsh ... undefine --nvram` command. Otherwise the directory is recreated empty and a
  free loopback port is chosen.
- E18. A fast base: `cp --reflink=auto` the image to `disk.qcow2`, `qemu-img resize -q` it to 20G,
  and `virt-install --import` with cloud-init injecting the lab key, UEFI, a passt forward of
  the port to guest port 22, and the serial port logged to `console.log`.
- E19. A full base renders the deployed kickstart with `render_kickstart`. A server renders it
  with `--set TREE_URL=<probe tree url>`. A desktop has two install media (the netinst it boots
  and the Workstation Live ISO attached read-only), a fresh 32-character LUKS passphrase saved
  0600 in the build directory, and a fresh SHA-512 crypt of a random user password. Both secrets
  reach the renderer on stdin (`--set-stdin`) and are never in argv. The rendered kickstart is
  0600 and is removed once the installer has finished. The installer is `virt-install --location`
  with `inst.stage2=hd:LABEL=<label>`, where `blkid` reads the label from the ISO (empty is a
  failure). It runs with `--noreboot --wait 90`. A desktop's serial port is a unix socket,
  followed into `console.log` (E26). After the installer, the domain must be `shut off` and
  `console.log` must report `Installation complete` or `Running post-installation scripts`.
  Each of these checks has its own failure. The installed system is then started. A desktop
  answers its LUKS prompt (E26).
- E20. After SSH answers (E24): `sudo dnf -y --refresh upgrade` runs, then
  `dnf --quiet repoinfo updates`. Its `Revision` (a whole number) and the first word of its
  first `Base URL` must both be present. A desktop gets the session runner installed at
  `/usr/local/bin/vmtest-session-provision.bash` (root, 0755). `guest-cleanup.bash` must print
  `VMTEST-GUEST-CLEANUP-DONE` on a line of its own, or the build fails with
  `guest cleanup did not complete`. Then a poweroff is scheduled with `systemd-run`, the command
  waits up to 300s for `shut off` (E25), and the domain is undefined (`--nvram`).
- E21. The disk becomes `bases/<name>/base.qcow2`, and a desktop's passphrase moves beside it.
  The command hashes the disk and reads its size and mtime. `refresh_state` is `complete` when
  the guest's revision is at least the probe's, otherwise `incomplete`. The recipe digest is
  E30's. A full base adds `--treeinfo-probe <probe> --tree <tree>`. `write_base_record` writes
  `base.json.new`, which is renamed over `base.json`. The disk is made 0444, the build directory
  is removed, and stdout gets `VMTEST-BASE-BUILT <name> <disk> refresh_state=<state>`.

### The guest

- E22. A guest is reached with `ssh -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5 -i <lab-key> -p <port> fedora@127.0.0.1 <command>`.
  `scp` takes the same options without `ConnectTimeout`, and with `-P`.
- E23. `domstate` of the domain reads `shut off` or `crashed`: the guest has stopped, and the
  failure is `guest <domain> is <state> while waiting for <what>; see <dir>/console.log`. A
  failed `domstate` is
  `cannot read the state of guest <domain> while waiting for <what>`.
- E24. The wait for SSH: `==> waiting for SSH on 127.0.0.1:<port> (up to 600s)`. Each try first
  checks that the guest is up (E23), then runs `ssh ... true`. The wait sleeps 5s between tries
  and ends by the clock after 600s with
  `guest did not answer SSH within 600s; see <dir>/console.log`. Success is
  `==> guest answered SSH after ~<n>s`.
- E25. The wait for power-off polls `domstate` every 5s for up to 300s:
  `guest did not shut off within 300s (state: <s>)`. A failed `domstate` is a failure.
- E26. On a desktop, `serial_console --unlock-with <passphrase> --until 'login: '` answers the
  LUKS prompt. Its `VMTEST-CONSOLE` lines go to stderr. If it fails, the command fails with
  `the guest did not reach userspace (stage: boot): reason=...`. A follower
  (`serial_console --follow`) then logs the rest of the boot. It is stopped (SIGTERM, then a
  wait) before a reboot, before a power-off, and at teardown. An exit status other than 0 or
  143 is logged and is not a failure.
- E27. Build teardown, on every exit path: stop the follower. If libvirt knows the domain,
  destroy it unless it is `shut off`, then undefine it with `--nvram`. Each failure there is
  logged, not fatal. A failed build's directory is kept and named
  (`build directory kept for diagnosis: <dir>`). A successful build has already removed its
  own.

### run

- E28. `run <scenario> [--commit SHA] [--github-account U --github-token-file F --github-ssh-passphrase-file F]`.
  The scenario must be `runnable=true`:
  `<id> is not runnable: its guest script has not declared a planned check count`.

- E29. Ordinary scenario. Any `--github-*` option is refused:
  `the --github-* options apply only to a host_only scenario, and <id> is not one`. The id must
  be a whole line of the deployed `scenarios.allowlist`, compared as a literal string. If not:
  `<id> is not in the deployed allowlist <path>; re-run play-vm-test-lab.yml on the host`.

- E30. Host-only scenario: every gate passes before anything boots.

  - `VMTEST_FROM_BRIDGE=1` is refused, and the message names the bridge.
  - The id must be a line of the deployed `scenarios.host-only`. A missing list is a refusal,
    never permission.
  - The id must not also be on the allowlist; the refusal says it is on `both`.
  - The account must match `^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`.
  - The token file and the passphrase file must both be given, and each must be a readable,
    non-empty regular file whose permission bits are exactly 0600 or 0400.

  No refusal message contains a secret value. The order of the checks is the order above.

- E31. The scenario's base must have a readable `base.qcow2` and `base.json`:
  `base <name> is absent (stage: base); build it with: vmtest build-base <key>`. The checker is
  `guest-acceptance-<id>.bash` if it is readable, otherwise `guest-acceptance-<profile>.bash`,
  otherwise the run fails, naming both files.

- E32. The commit is `--commit`, which must be 40 lowercase hex. Without it, the commit is the
  tip of `refs/heads/<branch>` at the repo URL, read by `git ls-remote`, and anything other than
  one 40-hex sha is a failure.

- E33. The freshness gate (E48) runs. A refusing verdict prints its marker line to stderr and
  fails with
  `base <name> may not be used (freshness: <decision>); rebuild it with: vmtest build-base <key>`.
  Then comes `retention floor --path runs --base-size <size> --step run --guest-mib <ram>`. A
  refusal there names `(stage: clone)`.

- E34. The run id is `<UTC %Y%m%dT%H%M%SZ>-<id>`, the directory is `runs/<run-id>`, and the domain
  is `vmtest-run-<run-id>`. The overlay is `qemu-img create -q -f qcow2 -F qcow2 -b <base>`, and
  `virt-install --import` uses `cache=unsafe` on the overlay. A desktop gets VNC on 127.0.0.1,
  virtio video and a socket serial port. A server gets no graphics and a file serial port. A
  desktop also needs `luks-passphrase` beside the base (otherwise the run fails, naming
  `(stage: boot)`) and then E26. Then E24.

- E35. The transcript `runs/<id>/transcript.log` starts with its two header lines (run id;
  scenario, base, kind, profile, freshness, commit). `run.bash` is fetched in the guest from
  `https://raw.githubusercontent.com/<owner/repo>/<commit>/run.bash`. A per-run random vault
  password that decrypts nothing is made in the guest.

- E36. Provisioning, server: `ssh` with `ServerAliveInterval=30`, bounded at 5400s, runs
  `env RUN_BASH_HEADLESS=1 RUN_BASH_USER_EMAIL='vmtest@example.com'`, the GitHub variables
  (`RUN_BASH_GITHUB_ACCOUNTS=none`, or for host-only the account and the two guest secret
  paths), the vault file, `RUN_BASH_GIT_REF`, `RUN_BASH_PROVISIONING_PROFILE`, then the
  scenario's `run_env` (last, so it wins) and `TERM=dumb bash ~/run.bash`. Its merged output is
  appended to the transcript and copied to stderr. A run that hits the bound has status 124.

- E37. Provisioning, desktop: the same variables as `KEY=VALUE` lines (no quoting) are written
  over SSH to `/var/lib/vmtest/run.env` (umask 077, through a temporary file and a rename). The
  command then waits for `run.exit`, every 15s, for up to 5400s. Each wait first checks that
  the guest is up. There is a log line every 300s. When `run.exit` appears, `run.log` is
  appended to the transcript and to stderr. `run.exit` must be a whole number, or the run fails.
  At the bound, the run log is appended (if it can be read), and
  `ERROR: the session runner did not finish run.bash within 5400s` goes to the transcript and
  stderr. The status is then 124.

- E38. `RUN-BASH-EXIT <rc>` is appended to the transcript and stderr. A non-zero `run.bash` is
  recorded and is never fatal here; the verdict decides.

- E39. If the scenario has `reboot_before_checks=true`, the fixture runs (E40) and then the
  reboot (E41). Otherwise neither runs.

- E40. The fixture is `guest-prepare-<id>.bash`. If it is not readable, the transcript and
  stderr say `# harness: no guest-prepare-<id>.bash under <home>; this scenario prepares nothing`,
  and the run goes on. Otherwise the fixture is announced, copied to `/tmp/guest-prepare.bash`,
  and run with `env TERM=dumb bash`. That run is bounded at 1800s, with output teed. A non-zero
  exit fails the run:
  `the <id> fixture exited <rc> (stage: prepare): <reason>; nothing after it would be judging the scenario it names`.
  `<reason>` is the last line of the fixture's own output that starts with `ERROR: prepare: `;
  if there is none, it is `the fixture printed no ERROR: prepare: line`. An exit of 0 whose own
  output has no `VMTEST-GUEST-PREPARE-DONE` line fails the run:
  `the <id> fixture exited 0 without printing VMTEST-GUEST-PREPARE-DONE (stage: prepare); it did not reach its last step`.

- E41. The reboot follows the profile. A server reads `/proc/sys/kernel/random/boot_id`,
  schedules a reboot, sleeps 10s and waits for SSH (E24). It then polls the boot id every 5s for
  up to 180s until it changes: `the guest still reports boot id <id> 180s after the reboot request; it never rebooted (stage: boot)`.
  It then writes `# harness: boot id <a> -> <b>` to the transcript. A desktop announces the
  reboot, schedules it, sleeps 10s, stops the follower, unlocks (E26) and waits for SSH. It then
  waits up to 180s (polling every 5s, each poll checking that the guest is up) for a `loginctl`
  session of `fedora` on `seat0`, and finally sleeps 15s. Any other profile fails:
  `no reboot mechanics for profile <p> (stage: boot)`.

- E42. The checker is copied to `/tmp/guest-acceptance.bash`. A desktop's checker gets the
  session bus variables. For a host-only run, the delivered secrets are removed first, then the
  needles are delivered (E44), and the checker gets `VMTEST_GITHUB_USER` and
  `VMTEST_NEEDLES_FILE`. The checker runs with `VMTEST_COMMIT` and `VMTEST_USER_EMAIL`, and its
  output is teed. A non-zero exit is logged; the transcript decides.

- E43. A desktop's evidence goes into the run directory: `session.env`, the transient unit's
  environment, and `VMTEST-EVIDENCE session_only_vars=<names>,` in the transcript. The names
  are those in the session that the unit lacks, sorted, each followed by a comma. That line is
  written only when both files are non-empty. Then `virsh screenshot` runs and records
  `VMTEST-EVIDENCE screenshot=runs/<id>/screenshot.png (evidence only: ...)`; a failed
  screenshot is logged as absent.

- E44. Host-only secrets go to tmpfs in the guest (`/dev/shm/vmtest-secrets`, 0700, files
  0600). They are copied by `scp` from the paths given and are never on any command line. The
  needles are the two secrets with their trailing newlines removed, one per line. They are
  written over SSH on stdin to `needles` under umask 077.

- E45. For a host-only run, the transcript and `console.log` are scrubbed before the run is
  judged: `redact` for the token and for the passphrase, then `verify` (D2). A missing scrubber
  front or library fails the run, naming `play-vm-test-lab.yml`. A failed redact or verify fails
  the run with
  `a supplied secret survived redaction in <file>; the run's artefacts are NOT safe to share (they remain under <dir>)`.
  The transcript must exist. A missing `console.log` is logged and skipped.

- E46. The follower is stopped, then the domain is destroyed and undefined. `judge_run` writes
  `runs/<id>/response.json` from its full argument set (as the bash passes it). Its divergences
  are `throwaway-vault-password,lab-ssh-key-baked`, and a desktop adds
  `luks-unlock-automated,plymouth-disabled,gdm-autologin-enabled-by-harness,session-runner-autostart`.
  An exit of 2, or a response with no string `verdict`, fails with
  `could not judge the run; see <dir>`. `refresh certify` must succeed; its line is logged, and
  a `stale` outcome adds the `vmtest refresh-base <key>` hint. Then come
  `==> verdict: <v>; response <path>; transcript <path>` and, on stdout,
  `VMTEST-RUN <run-id> verdict=<v> response=<path>`. The exit is 0 only for `pass`.

- E47. Run teardown, on every exit path: stop the follower, destroy and undefine the domain if
  libvirt still knows it (as E27), and remove the overlay of a passing run. A failed run keeps
  its directory and says so.

### Freshness

- E48. The verdict for one base. `freshness_gate --record-only` (with the fast server recipe
  digest and a zero hash) must print one `VMTEST-BASE-RECORD` line. Its `kind`, `profile`,
  `base_size`, `base_mtime`, `base_sha256` and `name` fields are read from it. The disk's size
  and mtime must equal the record's:
  `base.qcow2 for <name> differs from its record (size/mtime); refusing a base of unknown provenance`.
  A quick pass hands the gate the record's own hash. A deep pass hashes the disk
  (`==> hashing <disk> (deep check; this reads the whole base)`). The gate is then run with the
  recipe digest for the record's kind and profile. Exit 0 or 1 must come with exactly one
  `VMTEST-FRESHNESS-VERDICT` line, with tab-separated `decision`, `degraded` and `divergences`.
  Any other exit, or a missing verdict line, fails. Exit 1 is a refusal and is data, not a
  failure.
- E49. The recipe digest is the sha256 of `build-recipe-version=1\n`, followed by
  `guest-cleanup.bash`. A full base adds its kickstart, and a desktop also adds the session
  runner. A missing file fails, naming it.
- E50. `freshness-status [--deep]` probes once. It then writes `freshness-status.txt.new` with
  `written_at=<UTC ISO>`, `integrity=<quick|deep>`, `probe_revision=` and `probe_compose_id=`,
  followed by one tab-separated line per base (`base= decision= degraded= integrity= divergences=`).
  It also prints `VMTEST-FRESHNESS-STATUS <name> decision= degraded= integrity=` for each base,
  in name order. A base with a record and no disk fails the command. When every base is done,
  the file is renamed into place. The exit is the worst gate status (0 or 1). It never builds
  anything.
- E51. `sweep` runs `retention sweep --lab-dir <home> --checkout <checkout> --log <home>/retention.log`
  and then names the log.
- E52. `nightly` runs `freshness-status --deep` and then `sweep`, always both. The exit is 0 only
  if both succeed. Otherwise it is the status's own exit if that is non-zero, or else 1.
- E53. `ensure-base <key>`: if the base is present and the gate accepts it, the command prints
  `VMTEST-BASE-CURRENT <name> decision=<d>` and changes nothing. If the gate refuses it, or the
  base is absent, the command logs why and runs `build-base`.
- E54. `refresh-base <key|server|desktop|all>`: for each `bases/*/base.json` in name order, the
  key is the directory name without `-<fedora version>`. A named key skips every other
  directory before the manifest is asked. `all` and a profile ask the manifest for every
  directory and fail on one it does not know. Each match is built (E15) after a line that says
  what kind of refresh it is. No match fails:
  `no built base matches <target>; build one with: vmtest build-base <base-key>`. The command
  prints `VMTEST-REFRESH-DONE refreshed=<n>`.

### Members

- E55. A member name matches `^[a-z0-9][a-z0-9-]{0,39}$` and is checked before anything else:
  `a member name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got '<name>'`.
- E56. `member-up <name> <key>`: the base must be `server`, and present
  (`build it with: vmtest ensure-base <key>`). A member whose directory or domain
  `vmtest-member-<name>` exists is refused, with the `member-down` hint. The gate must accept
  the base (on a refusal the message names `vmtest ensure-base <key>`). Then come the retention
  floor under `members/`, the overlay in `members/<name>/`, and the boot (a server boot, as in
  E34). The port goes to `ssh-port` before the wait for SSH. On success the command prints
  `VMTEST-MEMBER-UP <name> domain=<domain>` and leaves the guest running.
- E57. A member-up that does not finish (any failure or signal after its directory exists)
  removes the domain, the overlay and `ssh-port`. It keeps the directory and its console log,
  says so with the `member-down` hint, and exits non-zero (E4 for a signal).
- E58. `member-ssh <name> <command...>` needs no lab load. It needs `members/<name>/ssh-port`
  holding a number, the lab key, and `VMTEST_MEMBER_SSH_TIMEOUT` (default 1800) as a positive
  whole number of seconds. It runs ssh with the E22 options, `ServerAliveInterval=15` and
  `ServerAliveCountMax=4`, with stdin, stdout and stderr passed through. It exits with the remote
  command's status. At the bound it exits 124 with
  `ERROR: member-ssh <name>: the command did not finish within <n>s (VMTEST_MEMBER_SSH_TIMEOUT)`.
- E59. `member-down <name>`: if libvirt lists the domain (`virsh list --all --name`, matched as a
  whole line), it is destroyed unless `shut off`, then undefined. A failed `list` is a failure,
  never "absent". The directory is removed. Nothing there is not an error. The command prints
  `VMTEST-MEMBER-DOWN <name>`.

### verify

- E60. `verify <run-id>`: needs `VMTEST_CHECKOUT` (E7). The slug is `systemd-escape --path <checkout>`,
  and the key is `~/.config/vmtest-bridge/<slug>/response.key` (it must be readable). The
  command runs `verify_response --checkout --key --run-id`, then names the service log of
  record. `verify_response`'s exit is the command's exit.

## 4. Where the Go departs from the bash

| #   | bash behaviour                                                                                                                                 | Go                                                         | exp.          |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ------------- |
| B1  | `lab.env` was `source`d: any shell in it ran as the user                                                                                       | parsed as `KEY=VALUE` data                                 | E7            |
| B2  | `python3 -m` puts the working directory first on `sys.path`; run from a checkout, its `helpers/` shadowed the deployed copies (breaks §2.3)    | `python3 -P -m`                                            | D1            |
| B3  | build and run teardown exited with the last command's `$?` on a signal, often 0                                                                | 130/143/129                                                | E4            |
| B4  | the argument-missing path (`${1:?usage}`) printed a bash error and exited 1                                                                    | `ERROR: usage: ...`, exit 64                               | E2            |
| B5  | a repeated `run` option silently overwrote the first                                                                                           | usage error                                                | E2            |
| B6  | allowlist and host-only lists matched with `grep -qx`, a regular expression                                                                    | a literal whole-line match                                 | E29, E30      |
| B7  | a secret "file" could be a FIFO or device that `-r`/`-s` accept                                                                                | a regular file required                                    | E30           |
| B8  | the fixture's completion marker and failure reason were read from the whole transcript, so a marker printed earlier (by run.bash) satisfied it | read from the fixture's own output only                    | E40           |
| B9  | the probe temp file leaked from `fetch`, and once per extra build of `refresh-base`                                                            | removed on every path                                      | E12           |
| B10 | a single-valued probe marker appearing twice became a two-line value                                                                           | refused                                                    | E13           |
| B11 | the verdict was read from `response.json` with a regex; an unparseable response printed `verdict=` and exited 1                                | read as JSON; no verdict is a failure                      | E46           |
| B12 | waits counted their own sleeps, so the time SSH attempts took was not counted                                                                  | measured on the clock                                      | E24, E25, E37 |
| B13 | `nightly` skipped the retention sweep whenever `freshness-status` died (upstream unreachable), so the disk could fill                          | sweep always runs                                          | E52           |
| B14 | `vcpus`/`ram_mib`/the revisions were used unchecked (a non-number became a shell arithmetic error)                                             | whole numbers required                                     | E9, E20       |
| B15 | a missing transcript was skipped by the scrubber without a word                                                                                | the transcript is required; a missing console log is named | E45           |

Kept as they were, and accepted. The free port is chosen, released and then handed to
virt-install, so there is a race between them, as in the bash. `ssh` host keys are not checked
(the guest is new on every run).

## 5. Go design

- `src-go/cmd/vmtest/main.go`: signals (INT, TERM and HUP cancel a context, and the first one
  sets the exit status) and the real environment. `run` is tested.
- `src-go/internal/vmtest`: `App` (dispatch, usage, exit status), `Env` (runner, streams,
  getenv, clock, sleep, free port, random source), the lab and its files, the facts and probe
  readers, the helper seam, the guest (ssh, scp, domain state, waits, console), and one file
  per command family: `fetch`, `build`, `run` with its steps, `hostonly`, `freshness`, `member`
  and `verify`. A failure is a typed error carrying its message, so a failure deep in a step
  unwinds through the deferred teardowns before the `ERROR:` line is printed. A status that is
  not a failure (member-ssh, freshness-status) is a typed exit.
- The only processes started are through `execx.Runner`, and `git` through `gitx`
  (`RemoteBranchTip`, new). The follower is a runner call in a goroutine with its own cancel.
- Deployment: `play-vm-test-lab.yml` imports `play-golang.yml` and builds
  `~/.local/bin/vmtest` with `tasks/build-go-command.yml`. It also deploys
  `lib/run-log-scrub-cli.bash` beside the scrubber. `scripts/go-binary-suites.tsv` gets a
  `vmtest qa-go` row. The bash, its allowlist row and the four suites go (D5).

## 6. Disposition of the bash suites' cases

| Suite                           | Cases                                                                                                                                                                                                                     | Now                                                                                 |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `test-vmtest-member`            | names refused; member-down on nothing, running, stopped; member-up boots, exists, no key, gate refuses, guest stops before SSH, virt-install fails, SIGTERM in the SSH wait; ensure-base three ways; member-ssh five ways | `member_test.go`, `freshness_test.go` (E53-E59, E4)                                 |
| `test-vmtest-host-only-gate`    | the acceptance case first; the bridge; the two lists; missing account, token, passphrase; unreadable, empty, wrong mode; bad account grammar; no secret echoed                                                            | `hostonly_test.go` (E30)                                                            |
| `test-vmtest-reboot-dispatch`   | server, desktop, unknown profile; no fixture, a fixture, a failed fixture, a fixture without its marker; SSH answered, a guest that stopped                                                                               | `steps_test.go`, `guest_test.go` (E23, E24, E40, E41)                               |
| `test-vmtest-build-dir-cleanup` | two successful builds then a failed one in one process                                                                                                                                                                    | `build_test.go` (`refresh-base all` over three bases, E21, E27)                     |
| `test-vmtest-prepare-record` §7 | the fixture's reason is its last `ERROR: prepare:` line; none is said, not invented                                                                                                                                       | `steps_test.go` (E40); the suite's other cases are about the guest scripts and stay |

## 7. What only a host run proves

Booting is never exercised here, so these are proven only on a host with KVM, by the owner. The
plan's `meta-deploy.list` queues the play, which builds and installs the binary. The checks:

- H1. `vmtest fetch server-fast` and `vmtest build-base server-fast` succeed (curl, gpg2, sha256,
  virt-install, cloud-init, dnf, cleanup, power-off, record).
- H2. `vmtest run server-fast-provision` reaches `VMTEST-RUN ... verdict=pass`, and the bridge
  (`run-scenario`) reports the same.
- H3. A desktop base build and a desktop run: the LUKS unlock, the session runner, the reboot
  into the session, and the evidence.
- H4. `vmtest member-up m1 server-fast`, `member-ssh m1 true`, `member-down m1`, and Ctrl-C
  during a `member-up` leaves no guest.
- H5. `vmtest nightly` from its timer: the status file is written, and the sweep log is written.
- H6. A host-only scenario run with a throwaway account: secrets delivered, scrubbed and
  verified.
