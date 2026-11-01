# Plan 00243 sweep: architecture assumptions

Task 1.1. Every reference that fixes a download, package or image to one CPU architecture, with
whether an aarch64 build exists and the fix. Two sources, kept apart as Defence Before Fix asks:

- **The rule** `arch-pinned-artefact` (`.semgrep/arch-pin.yml`, gate `scripts/qa-arch-pin.bash`),
  run over every file git would publish. Its known instances outside the two ccy images are the
  ratchet `scripts/arch-pin.allowlist`.
- **The independent search** (text search and reading, without the rule):
  [subagent-reports/independent-search.md](subagent-reports/independent-search.md).
  Availability evidence (release asset lists, digests, repo indexes):
  [subagent-reports/aarch64-availability.md](subagent-reports/aarch64-availability.md).

The sweep found 64 rule findings in 23 files: 20 in the two ccy images (fixed in this dispatch)
and 44 in 21 files on the ratchet. The search found every literal the rule reports, and a set of
implicit references no literal rule can see (section C).

## A. The ccy images (fixed: issues #119, #120, #121)

| file:line                                        | what                                                 | aarch64 build?                                                                   | fix                                                                                                                               |
| ------------------------------------------------ | ---------------------------------------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| files/var/local/claude-yolo/Dockerfile:12,16,508 | phpantom_lsp rust target `x86_64-unknown-linux-musl` | yes: `aarch64-unknown-linux-musl`; rust:slim and musl-tools are arm64v8 too      | target from `uname -m`; the binary is copied to a fixed path in the builder                                                       |
| files/var/local/claude-yolo/Dockerfile:142       | yq `latest` amd64, no checksum                       | yes: v4.53.3 `yq_linux_arm64`                                                    | pinned v4.53.3, asset and sha256 per arch                                                                                         |
| files/var/local/claude-yolo/Dockerfile:192-201   | Chrome for Testing `linux64`                         | **no**: CfT has no linux-arm64; agent-browser refuses `install` on linux aarch64 | arm64: Debian trixie `chromium` (150, `/usr/bin/chromium`), named by `executablePath` in agent-browser's config; x86_64 unchanged |
| files/var/local/claude-yolo/Dockerfile:253       | Lightpanda 0.3.6 `lightpanda-x86_64-linux`           | yes: `lightpanda-aarch64-linux`                                                  | asset and sha256 per arch                                                                                                         |
| files/var/local/claude-yolo/Dockerfile:373       | opencode 1.19.3 `opencode-linux-x64.tar.gz`          | yes: `opencode-linux-arm64.tar.gz`                                               | asset and sha256 per arch                                                                                                         |
| .claude/ccy/Dockerfile:108                       | shellcheck `linux.x86_64`, no checksum               | yes: `linux.aarch64`                                                             | asset and sha256 per arch (sha pinned beside `.qa-versions`' SHELLCHECK)                                                          |
| .claude/ccy/Dockerfile:126                       | yq v4.53.3 amd64, no checksum                        | yes: `yq_linux_arm64`                                                            | asset and sha256 per arch                                                                                                         |
| .claude/ccy/Dockerfile:134                       | gitleaks 8.30.1 `linux_x64`, no checksum             | yes: `linux_arm64`                                                               | asset and sha256 per arch                                                                                                         |
| .claude/ccy/Dockerfile:146,148                   | gum 0.17.0 `Linux_x86_64`                            | yes: `Linux_arm64`                                                               | asset and sha256 per arch                                                                                                         |
| .claude/ccy/Dockerfile:158                       | Go 1.26.10 `linux-amd64`                             | yes: `linux-arm64`                                                               | asset and sha256 per arch                                                                                                         |
| .claude/ccy/Dockerfile:169-172                   | golangci-lint `linux-amd64`                          | yes: `linux-arm64`                                                               | asset and sha256 per arch (pinned beside `.qa-versions`' GOLANGCI_LINT; was upstream's checksums file)                            |
| .claude/ccy/Dockerfile:187                       | ImageMagick 7 AppImage `gcc-x86_64`                  | **no** Linux arm64 build (the arm64 assets are Windows `.exe`/`.7z`)             | arm64: the base image's Debian trixie `imagemagick`, which is IM7 (7.1.1.43) with `magick`; the layer asserts it is 7             |

## B. On the ratchet (next dispatches)

| file:line                                                                                     | what                                           | aarch64 build?                                                    | fix or alternative                                                                                        |
| --------------------------------------------------------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| .github/workflows/qa.yml:82,87,94,197                                                         | CI shellcheck, golangci-lint, gitleaks         | yes                                                               | runners are x86_64 `ubuntu-latest`; select by `uname -m` so the rule is satisfied and an arm runner works |
| fedora-install/build-iso.bash:30,31                                                           | usage text with x86_64 ISO names               | yes                                                               | show `$(uname -m)`                                                                                        |
| fedora-install/setup-netinstall-boot.bash:35,621,640,647                                      | netinst/Workstation ISO paths and regex        | yes, same layout under `aarch64/`                                 | one `ARCH` from `uname -m`                                                                                |
| files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt:276,342,353                                 | go and AWS CLI examples (shipped in the image) | yes                                                               | examples select by `dpkg --print-architecture` / `uname -m`; ccy version bump                             |
| files/usr/local/bin/manage-kernel-versions.py:38,168,200,219                                  | kernel NEVRA `.x86_64` in dnf versionlock      | n/a (logic)                                                       | `platform.machine()`                                                                                      |
| files/var/local/docker-in-lxc:401                                                             | `lxc-create -a amd64`                          | yes: Fedora 44 arm64 LXC image                                    | map `uname -m` to amd64/arm64, fail otherwise                                                             |
| helpers/gnome/check_panel_contract.py:143                                                     | sample kernel string                           | n/a (data)                                                        | build it from the running arch, or move to test data                                                      |
| helpers/vmtest/freshness_gate.py:30, probe_upstream.py:40, src-go/internal/vmtest/fetch.go:14 | VM lab guest arch                              | yes: Fedora Cloud aarch64 qcow2                                   | the host's arch (an arm lab also needs qemu-system-aarch64 and edk2-aarch64)                              |
| playbooks/imports/optional/common/play-cli-tools.yml:51                                       | ouch musl target                               | yes                                                               | map by `ansible_architecture`, sha256 per arch                                                            |
| playbooks/imports/optional/common/play-network-tools.yml:21                                   | Ookla speedtest                                | yes                                                               | same                                                                                                      |
| playbooks/imports/optional/common/play-photography.yml:13,25                                  | RapidRAW rpm, ART AppImage                     | yes (RapidRAW's arm name adds `-arm`)                             | map the whole file name, sha256 per arch                                                                  |
| playbooks/imports/optional/common/play-qobuz.yml:45,65                                        | hifi.rs (latest, no checksum)                  | yes                                                               | map; pinning is a separate defect                                                                         |
| playbooks/imports/optional/hardware-specific/play-displaylink.yml:106,144                     | DisplayLink/evdi rpm                           | yes                                                               | `.{{ ansible_architecture }}.rpm`                                                                         |
| playbooks/imports/optional/hardware-specific/play-nvidia.yml:20,25,115-117                    | CUDA repo, cuDNN wheel                         | **no** CUDA toolkit for Fedora aarch64 (`fedora44/sbsa` has none) | skip on aarch64 naming the missing toolkit; fail if CUDA is asked for explicitly                          |
| playbooks/imports/play-AB-dnf-upgrade.yml:220                                                 | `rpm -qa kmod-*-${v}.x86_64`                   | n/a (logic)                                                       | `ansible_architecture`                                                                                    |
| playbooks/imports/play-basic-configs.yml:561                                                  | yq latest amd64, no checksum                   | yes                                                               | pin, map, sha256 per arch                                                                                 |
| playbooks/imports/play-browsers.yml:186                                                       | Google Chrome rpm repo                         | yes: `stable/aarch64` (google-chrome-stable 156)                  | `{{ ansible_architecture }}` in the baseurl                                                               |
| playbooks/imports/play-markless.yml:12,49,51                                                  | markless                                       | yes                                                               | map, sha256 per arch                                                                                      |
| playbooks/imports/play-python.yml:27,168,211                                                  | host QA shellcheck, golangci-lint              | yes (`aarch64` for shellcheck, `arm64` for golangci-lint)         | map both vocabularies                                                                                     |

## C. Implicit (no literal; the rule cannot see them, the search found them by reading)

Each needs checking upstream and a per-play decision; none is on the ratchet.

| file:line                                                                 | what                                          | to establish                                |
| ------------------------------------------------------------------------- | --------------------------------------------- | ------------------------------------------- |
| playbooks/imports/play-toolbox-install.yml:75,76                          | JetBrains JSON key `downloads.linux` (x86_64) | arm64 is the `linuxARM64` key               |
| playbooks/imports/optional/common/play-qobuz.yml:24                       | rescrobbled bare release asset                | whether an aarch64 asset exists             |
| playbooks/imports/play-comms.yml:43                                       | flatpak `com.slack.Slack`                     | Flathub aarch64 availability                |
| playbooks/imports/optional/common/play-agent-bus-element.yml:91           | flatpak `im.riot.Riot`                        | Flathub aarch64 availability                |
| playbooks/imports/optional/experimental/play-virtualbox-windows.yml:30,35 | VirtualBox repo and kernel module             | x86 hosts only: skip on aarch64 loudly      |
| playbooks/imports/play-rpm-fusion.yml:32                                  | intel-media-driver                            | x86 hardware: skip on aarch64 loudly        |
| playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml         | IPU6 stack                                    | x86 laptop hardware: skip on aarch64 loudly |

## D. Out of the rule's scope

Test data (kernel NEVRAs, dkms lines, ISO names in `tests/`, `scripts/test-*.bash`, `*_test.go`)
installs nothing, and prose (`docs/`, `fedora-install/README.md`, comments) is listed in the
search report, section C; the docs are corrected with the code they describe. `/usr/lib64` is
the same on Fedora aarch64. No `grubx64`/`shimx64`/`BOOTX64` names exist.
