# VM Acceptance Testing

Fresh-install lifecycle acceptance testing for this repo's provisioning, run
against local virtual machines. A freshly installed Fedora guest is snapshotted
once as a **base**, every test run boots a throwaway overlay on top of it,
provisions it with `run.bash`, and returns a machine-readable verdict. The
design and its reasoning live in the plan folder
(`CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/DESIGN.md`); this page is
the operator's view.

## What is in place

| Piece                                | Where                                                    | Job                                                                                                                |
| ------------------------------------ | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Scenario manifest                    | `vars/vm-test-scenarios.yml`                             | The bases, the scenarios, which base each scenario needs, guest sizing, freshness backstops                        |
| Manifest parser and check accounting | `helpers/vmtest/scenarios.py`                            | Validates the manifest; derives the bridge allowlist; decides `pass`/`fail`/`error` from a run's check counters    |
| Upstream signal parsers              | `helpers/vmtest/upstream.py`                             | Parses `COMPOSE_ID`, `.treeinfo`, `releases.json`, Bodhi and `repomd.xml` into a media identity and a revision     |
| Freshness policy                     | `helpers/vmtest/freshness.py`                            | Decides `current`, `refresh`, `reinstall` or `unknown` for a base, failing closed per signal                       |
| Upstream probe                       | `helpers/vmtest/probe_upstream.py`                       | Reads the signals live and prints `VMTEST-FRESHNESS-*` marker lines                                                |
| Manifest validator                   | `helpers/vmtest/validate_manifest.py`                    | Thin executor the playbook and the QA gate both call                                                               |
| Lab playbook                         | `playbooks/imports/optional/common/play-vm-test-lab.yml` | Installs the rootless libvirt/QEMU stack, enables linger, creates the lab tree, renders the manifest and allowlist |
| QA gate                              | `scripts/qa-vmtest-manifest.bash`                        | Rejects a malformed manifest on every `./scripts/qa-all.bash` run                                                  |

The `server-full` and `desktop` base builders, the desktop guest acceptance
script and the container-to-host bridge are later phases of the same plan and
are not deployed yet. What the playbook deploys today is the `vmtest` host CLI,
the helpers it calls, the server guest scripts and the lab's own SSH key.

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
  `swtpm`, `swtpm-tools` (names only, no pinned versions);
- `loginctl enable-linger` for the user, so the user's systemd manager survives
  a logout;
- `~/.local/share/vmtest/{images,bases,runs}`;
- `~/.local/share/vmtest/scenarios.json`, the manifest as the helpers read it;
- `~/.local/share/vmtest/scenarios.allowlist`, one runnable scenario id per
  line. A scenario is runnable only once its guest script has declared a
  `planned` check count in the manifest; until then the file is absent rather
  than empty.

## Building a base and running a scenario

Everything below is the deployed `vmtest` CLI, rootless, on the host:

```bash
vmtest fetch server-fast          # download + verify the Cloud Base image (signed CHECKSUM, GPG, sha256)
vmtest build-base server-fast     # boot it once, upgrade, clean, flatten, write base.json
vmtest run server-fast-provision  # the lifecycle; exit 0 only on verdict pass
vmtest run server-fast-provision --commit <40-hex>   # a specific pushed commit
```

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

The plan's `acceptance.bash` runs all four scenarios in turn and asserts each
verdict.

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
