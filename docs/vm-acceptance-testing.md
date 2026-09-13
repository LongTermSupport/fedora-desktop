# VM Acceptance Testing

Fresh-install lifecycle acceptance testing for this repo's provisioning, run
against local virtual machines. A freshly installed Fedora guest is snapshotted
once as a **base**, every test run boots a throwaway overlay on top of it,
provisions it with `run.bash`, and returns a machine-readable verdict. The
design and its reasoning live in the plan folder
(`CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/DESIGN.md`); this page is
the operator's view.

## What is in place

| Piece                                | Where                                                    | Job                                                                                                                                    |
| ------------------------------------ | -------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Scenario manifest                    | `vars/vm-test-scenarios.yml`                             | The bases, the scenarios, which base each scenario needs, guest sizing, freshness backstops                                            |
| Manifest parser and check accounting | `helpers/vmtest/scenarios.py`                            | Validates the manifest; derives the bridge allowlist; decides `pass`/`fail`/`error` from a run's check counters                        |
| Upstream signal parsers              | `helpers/vmtest/upstream.py`                             | Parses `COMPOSE_ID`, `.treeinfo`, `releases.json`, Bodhi and `repomd.xml` into a media identity and a revision                         |
| Freshness policy                     | `helpers/vmtest/freshness.py`                            | Decides `current`, `refresh`, `reinstall` or `unknown` for a base, failing closed per signal                                           |
| Upstream probe                       | `helpers/vmtest/probe_upstream.py`                       | Reads the signals live and prints `VMTEST-FRESHNESS-*` marker lines                                                                    |
| Manifest validator                   | `helpers/vmtest/validate_manifest.py`                    | Thin executor the playbook and the QA gate both call                                                                                   |
| Lab playbook                         | `playbooks/imports/optional/common/play-vm-test-lab.yml` | Installs the rootless libvirt/QEMU stack, enables linger, creates the lab tree, renders the manifest and allowlist, deploys the bridge |
| QA gate                              | `scripts/qa-vmtest-manifest.bash`                        | Rejects a malformed manifest on every `./scripts/qa-all.bash` run                                                                      |
| Bridge spool I/O                     | `helpers/vmtest/spool.py`                                | Symlink-safe, read-once access to the shared spool; the request grammar and deny list                                                  |
| Bridge watcher                       | `helpers/vmtest/bridge_watcher.py`                       | One activation: validates every pending request in order, answers each, dispatches accepted ones                                       |
| Bridge run scope                     | `helpers/vmtest/bridge_run.py`                           | Runs the accepted verb, keeps the heartbeat fresh, archives the run, writes the signed finished response                               |
| Response contract                    | `helpers/vmtest/verdict.py`                              | The response state machine, HMAC signing, the heartbeat document and its assessment                                                    |
| Container-side requester             | `scripts/vmtest-request.bash`                            | Writes a request, waits, maps the answer to a distinct exit code; never claims to verify the signature                                 |

The play deploys everything the lab runs from: the CLI, the guest scripts, the
two VM kickstarts, the manifest and the bridge. The three bases (`server-fast`,
`server-full`, `desktop`) are built on the host by `vmtest build-base`, never
by the play.

## Deploying the lab

Optional and rootless. Run on the host, never in a CCY container:

```bash
./playbooks/imports/optional/common/play-vm-test-lab.yml
# or, with the plan's gated wrapper and run log:
./CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/deploy.bash
```

The play refuses to proceed unless `/dev/kvm` is openable read-write by the
user. Fedora's systemd udev rule ships it `0666`, so no group membership or
re-login is needed; the assertion is there for a host where that is not so.

What lands:

- packages: `libvirt-daemon-kvm`, `libvirt-daemon-config-network`,
  `libvirt-client`, `virt-install`, `qemu-kvm`, `qemu-img`, `edk2-ovmf`,
  `guestfs-tools`, `virtiofsd`, `cloud-utils`, `xorriso`, `osinfo-db`, `lorax`,
  `swtpm`, `swtpm-tools`, `passt` (the guest's SSH port forward) and
  `distribution-gpg-keys` (names only, no pinned versions);
- `loginctl enable-linger` for the user, so the user's systemd manager survives
  a logout;
- `~/.local/share/vmtest/{images,bases,runs}`;
- `~/.local/share/vmtest/scenarios.json`, the manifest as the helpers read it;
- `~/.local/share/vmtest/scenarios.allowlist`, one runnable scenario id per
  line. A scenario is runnable only once its guest script has declared a
  `planned` check count in the manifest; until then the file is absent rather
  than empty;
- the bridge: the spool under `untracked/vmtest-bridge/` in the checkout, the
  per-checkout policy and signing key under `~/.config/vmtest-bridge/<slug>/`,
  the audit log and lock under `~/.local/state/vmtest-bridge/<slug>/`, and the
  `vmtest-bridge@<slug>.path` and `vmtest-bridge-heartbeat@<slug>.timer` user
  units, enabled. `<slug>` is `systemd-escape --path <checkout>`.

## Building a base and running a scenario

Everything below is the deployed `vmtest` CLI, rootless, on the host:

```bash
vmtest fetch server-fast          # download + verify the Cloud Base image (signed CHECKSUM, GPG, sha256)
vmtest build-base server-fast     # boot it once, upgrade, clean, flatten, write base.json
vmtest build-base server-full     # Anaconda from the Server tree via the verified netinst ISO, then the same
vmtest run server-fast-provision  # the lifecycle; exit 0 only on verdict pass
vmtest run server-full-provision  # the same checks on the Anaconda-installed base
vmtest run server-fast-provision --commit <40-hex>   # a specific pushed commit
```

A `full` base is installed by `fedora-install/ks-vm-server.cfg`, a VM-only,
non-interactive Fedora Server kickstart: Anaconda runs in cmdline mode on the
serial console from the one install tree the manifest names, creates the lab's
`fedora` user with the lab key, and powers off; the builder then boots the
result for the same upgrade, cleanup and snapshot as the fast base. The
deployed copy of the kickstart is what is rendered, and it is part of the
base's recipe digest. The install domain boots the installer kernel directly,
which the Secure Boot firmware refuses, so it runs with that firmware feature
off; runs of the resulting base boot through shim with the default firmware.

`fetch` locates the artefact by `releases.json`'s structured fields, downloads
it conditionally, fetches the signed `Fedora-<Variant>-<label>-x86_64-CHECKSUM`
from beside it, verifies the clearsignature against the Fedora release key from
`distribution-gpg-keys` (a hard failure, not best-effort), and requires the
sha256 to agree with both the CHECKSUM and `releases.json`.

`build-base` seeds a build disk from the image, boots it with the lab's own SSH
key injected through cloud-init (then disables cloud-init for good), upgrades
every package, records the updates-repo revision **the guest saw** and the
mirror that served it, runs the in-guest cleanup (DNF cache, cloud-init
instance state, SSH host keys, machine-id, journal, histories, fstrim), powers
off, and moves the flat disk into `bases/<name>/base.qcow2` read-only next to
its `base.json`. That record is re-validated on every read and its identity is
recomputed from its own fields, so a copied or edited record is refused.

`run` is fail-closed at every step:

1. the scenario must be runnable and on the **deployed** allowlist;
2. the base it names must exist, and its disk's size and mtime must match the
   record;
3. the commit to provision is the branch tip **on the remote** (`git ls-remote`), never the checkout, unless `--commit` names a pushed one;
4. the freshness gate runs the live probe and applies the policy: `current` or
   `refresh` proceed, `reinstall` or `unknown` refuse and name the fix;
5. a copy-on-write overlay is created on the read-only base, the guest boots
   with a passt port forward to its SSH, `run.bash` is fetched raw at the
   pinned commit and run headless with no GitHub identity and a per-run random
   vault password that decrypts nothing;
6. the in-guest acceptance script declares its planned check count first,
   runs every check, and prints evidence (boot id, machine id, kernel, the
   guest-seen updates revision);
7. the guest is destroyed, the transcript judged, and
   `runs/<run-id>/response.json` written. A passing run's overlay is removed;
   a non-passing run keeps its overlay and console log for diagnosis.

The verdict is three-valued. `pass` needs every check green, the count equal
to the plan, at least one check passed, skips under the scenario's cap, a
`PLAY RECAP` with work done, and a boot id. `fail` means the product ran and
did not hold (`run.bash` exited non-zero, or a check failed). `error` means the
harness could not complete, and names a stage: `provision`, `assert`,
`collect`. A run that died after four green checks of thirteen is `error`, not
`pass`.

### The desktop base and scenario

`vmtest build-base desktop` installs the repo's own installer **shape** —
netinst Anaconda deploying the Workstation Live filesystem via `liveimg`,
btrfs subvolumes under LUKS2 — from `fedora-install/ks-vm-desktop.cfg`, a
VM-only kickstart that is not the shipped `ks.cfg` (which prompts a human on
tty6). The Live ISO is attached as a second CD-ROM and found by content in
`%pre`; the netinst's volume label pins the installer's stage-2 medium so its
initrd cannot boot the Live squashfs by mistake. The LUKS passphrase is a
throwaway generated by the builder and kept `0600` beside the base; every
boot of the base is unlocked by driving it in over the guest's serial socket
(`helpers/vmtest/serial_console.py`), which also tees the console into the
run's log and names a stall at the prompt (`failure.stage: boot`, the console
excerpt inline) rather than reporting a timeout. That needs `plymouth.enable=0`
on the kernel line, so the base boots without `rhgb quiet`.

`vmtest run desktop-fresh-install` boots an overlay with a local VNC display,
unlocks it, and hands `run.bash` to a session runner that the kickstart's GDM
autologin launches through a GNOME autostart entry: the provisioning run
therefore executes **inside the real session**, inheriting its environment.
When `run.bash` returns, the run does what it tells a user to do — reboots the
guest (a harness step in the transcript), unlocks it again and waits for the
autologin session to be back on seat0 — because GNOME only loads the
extensions the play installed when a new session starts. The guest checks then
speak to that session's GNOME Shell (a local, active Wayland session; the
session bus; every extension the repo deploys `State: ACTIVE`).
The run collects the session environment and its diff against a transient
user unit, and a `virsh screenshot` of the framebuffer, as evidence only. The
response lists the divergences from a real install: `luks-unlock-automated`,
`plymouth-disabled`, `gdm-autologin-enabled-by-harness`,
`session-runner-autostart`, plus the throwaway vault password and the lab key.

### The negative scenarios

Three scenarios exist to go red on purpose, so the harness is known to be able
to fail. Each must produce `verdict: fail` at stage `provision`:

| Scenario                         | What it sets                                          | What it proves                                      |
| -------------------------------- | ----------------------------------------------------- | --------------------------------------------------- |
| `server-main-playbook-fails`     | `RUN_BASH_PROVISIONING_PROFILE=not-a-profile`         | a failed play in `playbook-main.yml` propagates out |
| `server-optional-playbook-fails` | `RUN_BASH_OPTIONAL_PLAYBOOKS=play-nvidia.yml`         | a failed optional play propagates out               |
| `server-optional-play-missing`   | `RUN_BASH_OPTIONAL_PLAYBOOKS=play-does-not-exist.yml` | argument validation refuses before anything runs    |

A scenario's `run_env` in the manifest is a closed allowlist of `run.bash`'s
non-secret knobs; nothing secret-bearing can be set from a scenario.

The plan's `acceptance.bash` runs all six scenarios in turn (the four server
fast legs, `server-full-provision` and `desktop-fresh-install`) and asserts
each verdict.

## Asking the lab from inside the sandbox

A CCY container cannot reach the hypervisor, and must not be able to run
anything on the host. The bridge is a file spool inside the bind-mounted
checkout, watched by a `systemd --user` path unit on the host; the container
writes a request, the host answers with a signed response, and nothing else
crosses.

```bash
./scripts/vmtest-request.bash list-scenarios
./scripts/vmtest-request.bash run-scenario server-fast-provision
./scripts/vmtest-request.bash run-scenario server-fast-provision --timeout 5400
```

Exit `0` only on a finished `pass`. Every other outcome is a distinct non-zero
code with its reason on stderr: `1` fail, `2` error (with the stage), `3`
rejected (with the check that refused it), `4` no answer at all, `5` bridge not
running (stale or absent heartbeat), `6` bridge wedged (a unit is failed or the
path unit is not active; the remedy line is printed), `7` the host run process
died mid-run, `8` the response was malformed or its shape unknown, `64` usage. The requester reads the heartbeat **before** it
writes, so a dead bridge is reported as dead, never as a timeout.

### Verbs

Five verbs and nothing else. `run-scenario <id>` (the id must be on the
**deployed** allowlist), `list-scenarios`, `lab-status`, `refresh-base server|desktop|all`, `abort-run`. Of these, `lab-status` and `abort-run` are
in the verb set but not yet implemented by the run scope, so the shipped
policy denies them (a request is rejected by policy, never answered "not
implemented"); `refresh-base` ships denied by choice. A hardcoded deny list (`exec`, `shell`,
`bash`, `run`, `eval`, `ansible`, …) is checked before the verb set. No verb
takes a path, a command or free text; `run-scenario`'s argument must match
`^[a-z][a-z0-9_-]*$` **and** be in the allowlist the playbook rendered.

The host policy, `~/.config/vmtest-bridge/<slug>/policy`, holds one
`MODE_<verb>=auto|deny` line per verb. A missing file, a missing line or an
unrecognised value is `deny`. `refresh-base` ships denied; set
`vm_test_bridge_modes` for the host to change the defaults and re-run the
play.

### The spool

```
untracked/vmtest-bridge/
├── tmp/          the container stages a request here, then renames it into requests/
├── requests/     <UTC stamp>-<verb>-<16 hex nonce>.json   e.g. 20260913T114500Z-run-scenario-0123456789abcdef.json
├── processing/   claimed by the watcher (atomic rename) before it is answered
├── responses/    <request name>.response.json — signed by the host
├── archive/      <run-id>/{transcript.log,response.json,console.log} for a run
├── quarantine/   every refused request file, never deleted silently
└── diagnostics/  bridge-heartbeat.json, written every minute by the timer
```

A request body is exactly `{"verb": …, "argument": … | null, "nonce": …}`, with
the nonce equal to the one in the file name. The watcher judges the bytes it
read once, not the file, so swapping the file after the read changes nothing.
Every directory is reached by a component-wise no-follow walk and pinned as a
descriptor: a spool directory replaced by a symlink is **refused** (logged off
the mount, nothing written anywhere), which is a different outcome from a
request being **rejected** (answered, and the file quarantined).

### The response

```json
{
  "schema": 1,
  "request": "20260913T114500Z-run-scenario-0123456789abcdef.json",
  "verb": "run-scenario",
  "argument": "server-fast-provision",
  "state": "accepted | running | finished | rejected",
  "verdict": null,
  "run_id": "20260913T114500Z-server-fast-provision",
  "accepted_at": "…", "started_at": null, "heartbeat_at": "…", "finished_at": null,
  "checks": { "planned": 13, "total": null, "passed": null, "failed": null, "skipped": null },
  "failure": null,
  "evidence": { "transcript": "untracked/vmtest-bridge/archive/<run-id>/transcript.log", "base": { "kind": "fast", "name": "server-fast-44" } },
  "signature": { "alg": "hmac-sha256", "nonce": "0123456789abcdef", "value": "…" }
}
```

`verdict` stays `null` until `state` is `finished`, so a reader that skips the
state check gets a falsy value. `accepted` is written **before** the run is
dispatched and `rejected` for every refusal, so silence is never an outcome;
`heartbeat_at` is refreshed every minute while running, so a run whose host
process died is detectable. `failure.stage` names where a non-pass happened:
`freshness | allowlist | base | clone | boot | ssh | provision | assert | collect | aborted`.

The signature is an HMAC over the body **and the request nonce**, keyed by
`~/.config/vmtest-bridge/<slug>/response.key`, which never leaves the host.
The container therefore cannot verify it and does not pretend to; the
requester says so and prints the host command:

```bash
vmtest verify <run-id>     # on the host: VMTEST-VERIFY <run-id> signature=ok|bad …
```

The off-mount audit log, `~/.local/state/vmtest-bridge/<slug>/service.log`, is
the verdict of record; both tools print its path.

### Liveness and the remedy

`diagnostics/bridge-heartbeat.json` carries the wall clock, both units'
`ActiveState`/`Result`, the run in flight and the remedy as a literal string.
A failed unit is **reported, not repaired**: a limit that was hit is a defect
worth seeing. Rate limiting is the watcher's (ten answers per minute, the
eleventh is answered `rejected: rate-limited`), because systemd's own limits
put a unit into `failed` silently. If the heartbeat says wedged, run the line
it prints:

```bash
systemctl --user reset-failed vmtest-bridge@<slug>.path vmtest-bridge@<slug>.service && systemctl --user start vmtest-bridge@<slug>.path
```

The plan's `selftest-bridge.bash` and `selftest-liveness.bash` exercise every
rejection path, the hostile-spool refusal, the rate limit and the wedged
report against the live bridge, and clean up after themselves.

## Has upstream moved?

The freshness probe answers that directly, from any checkout with network
access:

```bash
python3 -c 'import json, sys, yaml; json.dump(yaml.safe_load(open("vars/vm-test-scenarios.yml")), sys.stdout)' > /tmp/manifest.json
python3 -m helpers.vmtest.probe_upstream --fedora-version 44 --manifest /tmp/manifest.json
```

It prints one marker per fact:

```
VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20260422.1
VMTEST-FRESHNESS-REVISION 1789172543
VMTEST-FRESHNESS-BODHI F44 current
VMTEST-FRESHNESS-TREE Everything build_timestamp=1776865868
VMTEST-FRESHNESS-TREE-CHECKSUM Everything images/install.img c2571f26…
VMTEST-FRESHNESS-ARTEFACT desktop-44 Fedora-Workstation-Live-44-1.7.x86_64.iso 1620295f… label=44-1.7
VMTEST-FRESHNESS-DONE unreadable=0
```

Two facts drive two different decisions and are never pooled:

- the **artefact identity** (compose label, artefact hashes, and the install
  tree's installer hashes for an Anaconda base) answers "is the base built from
  the currently published media?" and drives a **reinstall**;
- the **package revision** (the updates repo's `repomd.xml` `<revision>`)
  answers "are the packages on top of it current?" and drives a **refresh**.

A signal that cannot be read prints `VMTEST-FRESHNESS-UNREADABLE` with the URL
and the error, the rest still print, and the exit status is non-zero. The
policy never substitutes a guess: an unreadable identity blocks the lab, and an
unreadable revision falls back to a time-to-live backstop with the verdict
stamped `degraded`.

For a GA release the identity signals are correctly inert (the F44 install tree
has not moved since compose), so the reinstall trigger fires on a Fedora version
change or the rebuild backstop, and the check's value is proving that the media
and the branch still agree.

## Keeping a base fresh without a refresh boot

Every run performs the product's own package upgrade on its overlay, and the
transcript records whether that transaction changed anything and which
updates revision the **guest** saw. Judged against the revision the probe read
from the canonical host, the run's `evidence.refresh.state` is one of:

| State        | Meaning                                                                                      | What happens                                                           |
| ------------ | -------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `current`    | the guest had caught up with the probe and the upgrade changed nothing                       | a **passing** run advances `base.json` to that revision; no extra boot |
| `stale`      | the guest had caught up and the upgrade changed packages                                     | the run says so; refresh with `vmtest refresh-base <base-key>`         |
| `incomplete` | the guest's mirror was behind the probe, so nothing was checked against the current revision | nothing is certified; the base stays as it was                         |
| `unknown`    | the transcript did not carry the upgrade result or a revision                                | nothing is certified                                                   |

A refresh is a rebuild from the published artefacts: a fast base is re-imported
from its image (minutes), a full base is reinstalled from its verified media
(the same Anaconda run as `build-base`). The overlay of a provisioned run is
never flattened into a base: a base is a fresh install plus updates, and a
provisioned system is not one.

## Nightly report and retention

`vmtest-nightly.timer` runs `vmtest nightly` at 03:30: a deep freshness report,
then the retention sweep. Neither rebuilds anything.

```bash
vmtest freshness-status          # one probe; one line per built base into ~/.local/share/vmtest/freshness-status.txt
vmtest freshness-status --deep   # the same, re-hashing every base disk against its record (the nightly form)
vmtest sweep                     # apply the retention policy; every eviction goes to ~/.local/share/vmtest/retention.log
```

A run checks a base's size and mtime against its record before booting it and
refuses a mismatch; only the deep pass reads the whole disk and compares the
hash, so the status line says which (`integrity=quick|deep`).

`freshness-status` exits with the worst verdict it found, so a base that needs
a rebuild shows up as the nightly unit failing in `systemctl --user list-units --failed`, not as an unattended rebuild on a workstation. The
sweep keeps the newest ten passing runs and **every** run that did not pass
(its overlay and console are the diagnosis), removes a leftover fast-base
build directory but keeps a desktop one, and bounds `quarantine/` and
`responses/` on the shared mount to their newest two hundred entries, through
the same pinned descriptors the watcher uses.

Before every run and every base build the CLI refuses when the disk cannot
carry the step (an overlay may grow to the base's size; a rebuild holds two
bases until the rename; two gigabytes of headroom on top) or when the guest's
RAM exceeds three quarters of the host's. It never cleans up and continues.

## The manifest

`vars/vm-test-scenarios.yml` is the single tracked source. Each base declares
its kind (`fast` is imported from a published Cloud image and never runs
Anaconda; `full` is an Anaconda install), its one install tree, the published
artefacts it is built from (selected by `releases.json`'s structured fields plus
a filename prefix and suffix, never a full filename that embeds a compose label),
and its guest sizing. Each scenario names the base it requires, and a request
whose base is absent is an error, never a substitution. Base names carry the
Fedora version from `vars/fedora-version.yml`, e.g. `server-fast-44`.

Edit it and run `./scripts/qa-all.bash`; the manifest gate rejects unknown keys,
orphan bases, ids the bridge could never accept, and a skip cap that would let a
run pass having asserted nothing.
