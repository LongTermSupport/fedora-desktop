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

The base builders, the `vmtest` host CLI, the guest acceptance scripts and the
container-to-host bridge are later phases of the same plan and are not deployed
by this playbook yet.

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
