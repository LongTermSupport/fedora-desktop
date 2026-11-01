# Independent search: single-architecture artefact references

Method: text search (arch literals, release/URL/repo patterns, flatpak/AppImage/rustup/dpkg/machine-probe forms) over the first-party tree, then reading of each hit and of every download site carrying no literal. Excluded as instructed (vendor, hooks-daemon, skills, untracked, Plan, .semgrep, arch-pin.allowlist, archpin). Not instances: agent-bus-install case arm and tuwunel.pin map keyed by arch, `dpkg --print-architecture` in apt source lines.

## A. Executable instances

| file:line | reference | literal/implicit | kind | found by |
|---|---|---|---|---|
| .claude/ccy/Dockerfile:108 | shellcheck `.linux.x86_64.tar.xz` | literal | executable | text |
| .claude/ccy/Dockerfile:126 | yq `yq_linux_amd64` | literal | executable | text |
| .claude/ccy/Dockerfile:146,148 | gum `Linux_x86_64` (+ GUM_SHA256, per-arch digest) | literal | executable | text |
| .claude/ccy/Dockerfile:158 | go `linux-amd64` (+ GO_SHA256) | literal | executable | text |
| .claude/ccy/Dockerfile:169,171,172 | golangci-lint `linux-amd64` | literal | executable | text |
| .claude/ccy/Dockerfile:187 | ImageMagick `gcc-x86_64.AppImage` | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:12 | rustup target add `x86_64-unknown-linux-musl` | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:16 | cargo build `--target x86_64-unknown-linux-musl` | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:508 | `COPY --from=phpantom-builder .../x86_64-unknown-linux-musl/...` | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:142 | yq `yq_linux_amd64` | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:192,194,200,201 | Chrome for Testing platform `linux64` (that channel has no linux arm64 build, so also implicit) | literal+implicit | executable | text |
| files/var/local/claude-yolo/Dockerfile:251-254 | lightpanda `lightpanda-x86_64-linux` + single LIGHTPANDA_SHA256 | literal | executable | text |
| files/var/local/claude-yolo/Dockerfile:370-374 | opencode `opencode-linux-x64.tar.gz` + single OPENCODE_SHA256 | literal | executable | text |
| .github/workflows/qa.yml:82 | shellcheck `.linux.x86_64.tar.xz` | literal | executable (CI) | text |
| .github/workflows/qa.yml:87,94 | golangci-lint `linux-amd64` | literal | executable (CI) | text |
| playbooks/imports/play-basic-configs.yml:561 | yq `yq_linux_amd64` | literal | executable | text |
| playbooks/imports/play-python.yml:27,211 | golangci-lint `linux-amd64` archive and inner path | literal | executable | text |
| playbooks/imports/play-python.yml:168 | shellcheck `.linux.x86_64.tar.xz` | literal | executable | text |
| playbooks/imports/play-browsers.yml:186 (comment 244) | google-chrome repo baseurl `.../rpm/stable/x86_64` (Chrome publishes no aarch64 rpm, so also implicit) | literal+implicit | executable | text |
| playbooks/imports/play-markless.yml:12,49,51 | `markless-x86_64-unknown-linux-gnu.tar.gz` (+ one marklessSha256) | literal | executable | text |
| playbooks/imports/optional/common/play-cli-tools.yml:51 | `ouchArch: x86_64-unknown-linux-musl` (+ one ouchSha256) | literal | executable | text |
| playbooks/imports/optional/common/play-network-tools.yml:21 | ookla speedtest `linux-x86_64.tgz` | literal | executable | text |
| playbooks/imports/optional/common/play-photography.yml:13 | RapidRAW `ubuntu-22.04_x86_64.rpm` | literal | executable | text |
| playbooks/imports/optional/common/play-photography.yml:25 | ART `x86_64.AppImage` | literal | executable | text |
| playbooks/imports/optional/common/play-qobuz.yml:45,65 | hifi-rs `x86_64-unknown-linux-gnu.tar.gz` | literal | executable | text |
| playbooks/imports/optional/common/play-qobuz.yml:24 | rescrobbled bare release asset `rescrobbled` (upstream believed x86_64 only; unsure) | implicit | executable | reading |
| playbooks/imports/optional/hardware-specific/play-nvidia.yml:20,115-117 | CUDA gpg key and repo baseurl `fedora<N>/x86_64`, repo name/description | literal | executable | text |
| playbooks/imports/optional/hardware-specific/play-nvidia.yml:25 | cuDNN wheel URL (hit matched an x86_64-style platform tag; line truncated in my view, unsure of exact tag) | literal | executable | text |
| playbooks/imports/optional/hardware-specific/play-nvidia.yml:187 | `/usr/local/cuda/lib64` | literal | executable (path, low) | text |
| playbooks/imports/optional/hardware-specific/play-displaylink.yml:106,144 | evdi rpm `...github_evdi.x86_64.rpm` | literal | executable | text |
| playbooks/imports/play-AB-dnf-upgrade.yml:218,220 | rpm query pattern `kmod-*-${v}.x86_64` | literal | executable | text |
| files/usr/local/bin/manage-kernel-versions.py:168,200,219 | `.replace(".x86_64","")`, versionlock `kernel-0:<ver>.x86_64` | literal | executable | text |
| files/var/local/docker-in-lxc:401 | `lxc-create ... -a amd64` | literal | executable | text |
| fedora-install/setup-netinstall-boot.bash:35,621,640,647 | netinst prefix, `/Everything/x86_64/iso/`, `/Workstation/x86_64/iso/`, `.x86_64.iso` regex | literal | executable | text |
| helpers/vmtest/probe_upstream.py:40 | `ARCH = "x86_64"` | literal | executable | text |
| helpers/vmtest/freshness_gate.py:30 | `ARCH = "x86_64"` | literal | executable | text |
| src-go/internal/vmtest/fetch.go:14 | `const arch = "x86_64"` | literal | executable | text |
| playbooks/imports/play-toolbox-install.yml:75,76 | JetBrains release JSON key `downloads.linux` (x86_64 build; arm64 is a separate key) | implicit | executable | reading |
| playbooks/imports/play-comms.yml:43 | flatpak `com.slack.Slack` (believed x86_64 only) | implicit | executable | reading |
| playbooks/imports/optional/experimental/play-virtualbox-windows.yml:30,35 | VirtualBox repo file and kernel-module package, x86 hosts only | implicit | executable | reading |
| playbooks/imports/optional/common/play-agent-bus-element.yml:91 | flatpak `im.riot.Riot` (believed x86_64 only; unsure) | implicit | executable | reading |
| playbooks/imports/play-rpm-fusion.yml:32 | intel-media-driver package (x86 only) | implicit | executable | reading |
| playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml | IPU6 userspace stack (x86 laptop hardware) | implicit | executable | reading |

Checked and not counted: vscode, gh-cli, docker-ce, ddev vendor repos (multi-arch); `/usr/lib64` in extensions/scripts/gnome-shell-extract-js.bash:17,20 and files/var/local/claude-yolo/lib/common-pure.bash:472 (Fedora aarch64 also uses lib64).

## B. Test data and fixtures (literal)

Kernel NEVRA strings `*.fc44.x86_64` and `x86_64: installed` dkms lines in tests/helpers/host_health/*, tests/helpers/version_pins/test_check_pins.py (also speedtest `Linux/x86_64-linux-musl` :675), tests/helpers/vmtest/*, tests/extensions/test-panel-*.mjs, helpers/gnome/check_panel_contract.py:143, scripts/test-vmtest-*.bash, scripts/test-agent-bus-install.bash:87,299,458, src-go/internal/vmtest/*_test.go, src-go/internal/nvidiastatus/*_test.go, src-go/qa/versionpins/run_test.go:466, tests/fixtures/ansible-root-umask/flagged.yml:48. The vmtest fixtures mirror the literals the executable vmtest code in section A hard-codes.

## C. Prose (lower severity)

- docs/README.md:223, docs/containerization.md:92,794: `lxc-create ... -a amd64`
- docs/containerization.md:582, files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt:353: awscli `linux-x86_64.zip` example
- files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt:276,342: go `linux-amd64` example
- docs/vm-acceptance-testing.md:123,457; fedora-install/README.md:312; fedora-install/build-iso.bash:30,31: x86_64 ISO names
- Comments: play-AB-dnf-upgrade.yml:203, play-advanced-kernel-management.yml:29, tasks/boot-kernel-version.yml:5, helpers/host_health/probe_results.py:27, play-browsers.yml:244

## Technique attribution

Text search found: every literal row in A, plus B and C.
Reading found (text could not): rescrobbled bare asset, JetBrains `downloads.linux` key, Slack and Element flatpaks, VirtualBox repo, intel-media-driver, IPU6; that `linux64` and the Chrome repo are single-arch because upstream has no arm64 build; that vendor repos (vscode, gh-cli, docker-ce, ddev) are multi-arch and not instances; that the single sha256 pins (ouch, markless, lightpanda, opencode, GUM, GO) are per-arch values that stay wrong after a URL fix.
Not checkable: upstream arch availability claims (rescrobbled, Element, Slack, Chrome) are from memory, not verified online; the nvidia cuDNN wheel line was truncated in my view.

## Searches run

Arch literals: x86[_-]64, amd64, x64, linux64, linux-64, i[3-6]86, shimx64, grubx64, lib64, manylinux, -musl, intel; 64-bit, x86 word, .64.(deb|rpm|zip|tar|tgz|AppImage), linux-?64, --platform, GOARCH, GOOS, dpkg print-architecture, ansible_architecture, TARGETARCH, BUILDARCH, aarch64, arm64, .deb, dl.google, flatpak/flathub, AppImage, rustup, --arch, machine probes. URL-pattern search (releases/download, .tar, .zip, .rpm, .deb, .repo) excluding lines already carrying a literal, to find arch-less downloads; baseurl/repofile search; flatpak app names; Dockerfile reading. No shimx64, grubx64 or i686 hits in first-party code.
