# Plan 00169 research: the ccy base image, its distro, and why its tools fall behind

Evidence for [PLAN.md](PLAN.md). Every version below was measured, from inside a running
ccy container (container 2.49, built from `node:lts-slim`) or from the distributions' own
package APIs (Fedora `mdapi`, Debian `madison`, Docker Hub tag API). Where something could
not be measured from the container it is marked **unmeasured** and a plan task measures it.

## 1. Why the image is Debian today

**No reason is recorded.** Debian came in as a side effect of choosing a Node base image.

- `git log --diff-filter=A` on `files/var/local/claude-yolo/Dockerfile` gives `f0686fbd`,
  the commit that created ccy. Its first line was `FROM node:20-slim`, and its message
  says only "Dockerfile: Node.js 20 base with Claude Code and CLI tools (no git/gh)". The
  base was picked for Node; Debian is what `node:*-slim` happens to be built on.
- `4477eab2` moved it to `node:lts-slim` so Claude Code's rising Node floor (`>=22`) is met
  without hand edits. Its Dockerfile comment adds "node:lts-slim is Debian-based, matching
  the apt layers below", which describes a consequence and does not argue for Debian.
- `7ee4b481` (Plan 00165, container 2.49) installs `tzdata` by name because "the floating
  `node:lts-slim` base will move to a Debian release that does not install it".
- `docs/ccy.md`, `docs/ccy-changelog.md`, `CLAUDE/ContainerRules.md` and the completed
  ccy plans hold no argument for Debian over another distro: no image-size comparison and
  no Chromium-compatibility reason. The Chromium dependency list in the Dockerfile was
  written for apt because the base already used apt.

## 2. What the image actually ships (measured in container 2.49)

| Tool          | In the image                                 | Source                                         |
| ------------- | -------------------------------------------- | ---------------------------------------------- |
| OS            | Debian 12 (bookworm), glibc 2.36             | `node:lts-slim`                                |
| git           | **2.39.5** (`1:2.39.5-0+deb12u3`)            | apt                                            |
| Node / npm    | 24.21.0 / 11.19.0                            | `node:lts-slim`                                |
| Python        | 3.11.2                                       | apt                                            |
| gh            | 2.102.0                                      | GitHub's apt repo                              |
| yq            | 4.53.3                                       | GitHub `releases/latest` download              |
| uv            | 0.12.24                                      | astral install script                          |
| Claude Code   | 2.1.295                                      | npm, plus the launcher's daily in-place update |
| agent-browser | 0.38.2, Chrome for Testing 155               | npm, then `agent-browser install`              |
| Lightpanda    | 0.3.6 (sha256 pinned)                        | GitHub release asset                           |
| shellcheck    | 0.11.0 in the project image (apt's is 0.9.0) | upstream binary, `.qa-versions` pin            |

The fedora-desktop project image (`.claude/ccy/Dockerfile`) already works around Debian
being behind in **three places**, each documented in a comment there:

- Ansible: "Debian 12's package is ansible-core 2.14, while the host … runs ansible-core
  2.19", so it is installed through pipx.
- shellcheck: "the base image's apt build is years behind the host's", so the upstream
  binary is installed.
- ImageMagick: "Debian 12 ships only IM6. Production target is Fedora … which ships IM7
  natively", so an AppImage is extracted and wrapped.

## 3. Option A: stay on Debian

### Getting git 2.48 or later

The relative-worktree extension needs git 2.48 or later. Debian's own archive, measured with
`api.ftp-master.debian.org/madison?package=git`:

| Suite                                 | git                  |
| ------------------------------------- | -------------------- |
| oldstable (bookworm, the image)       | 2.39.5-0+deb12u3     |
| stable (trixie)                       | **2.47.3**-0+deb13u1 |
| testing (forky)                       | 2.53.0-1             |
| unstable                              | 2.55.0-1             |
| bookworm-backports / trixie-backports | **no git package**   |

**No stable Debian release, with or without backports, ships git 2.48 or later.** Moving
the base to trixie does not fix the incident. The routes left are:

1. **Build git from source in a builder stage**, the same way PHPantom is built. This needs
   the build dependencies (`libcurl4-openssl-dev`, `libexpat1-dev`, `gettext`, `zlib1g-dev`,
   `libssl-dev`) and a version: either a pin, which is the X.Y.Z pin the owner does not want,
   or "latest tag" logic. A source build also has to install git's helper programs under
   `libexec/git-core`, `git-remote-https` and the templates. It is a second git to keep
   current, with nobody shipping security fixes for it.
2. **Mix in testing or unstable apt sources with pinning.** This is the classic way to break
   a Debian system: an unrelated later install pulls a testing libc.
3. **A third-party build.** There is no maintained git PPA for Debian; Ubuntu's
   `git-core/ppa` targets Ubuntu releases.

### Which Debian `node:lts-slim` is (measured)

Docker Hub tag API, 2026-10-06 update: `node:lts-slim` and `node:lts-bookworm-slim` have the
**same digest** (`sha256:d6aa754f…`). `node:lts-trixie-slim` is a different digest. The
floating tag floats the **Node major** but keeps the image on **oldstable** Debian. Floating
it does not keep the distro current.

### Why the image stops getting updates even with floating tags

- **The base is not re-pulled.** `build_container_with_hash` (`lib/common.bash:1015`) and the
  play's build task (`play-claude-yolo.yml:1089`) run `podman build` with no `--pull`. Per the
  podman documentation, the default policy then pulls `FROM` only when it is missing
  locally. **Unmeasured on the host**; triage measures it from the image's base-layer
  digest against the registry's.
- **Package layers are cached until the Dockerfile changes.** A rebuild happens only for
  a first build, a container version or hash mismatch (`validate_container_version`,
  `lib/common.bash:934`), or `ccy --rebuild` (full is `--no-cache`, which still does not
  re-pull `FROM`). If the Dockerfile does not change, the `apt-get install` layers are
  reused indefinitely, git included.
- **Only Claude Code is kept current.** `auto_update_claude_code` (`claude-yolo`, around
  line 1805) compares the image's Claude Code with the npm `latest` once a day per image
  and updates it in place. No other tool has an equivalent.

The generic mechanism (Section 5) is needed on either base. On Debian it also needs the
git source build above, because no Debian package will ever satisfy the floor.

## 4. Option B: Fedora base

### Package availability on Fedora 44 (measured, `mdapi.fedoraproject.org/f44/pkg/<name>`)

| Need (Debian name)                        | Fedora 44 package                                | F44 version                                      |
| ----------------------------------------- | ------------------------------------------------ | ------------------------------------------------ |
| git                                       | `git`                                            | **2.56.0**                                       |
| node / npm                                | `nodejs24`, `nodejs24-npm`                       | 24.18.0 / 11.16.0                                |
| python3, python3-pip, python3-venv        | `python3`, `python3-pip` (venv is in the stdlib) | 3.14.8                                           |
| python3-yaml                              | `python3-pyyaml`                                 | 6.0.3                                            |
| pipx                                      | `pipx`                                           | 1.15.0                                           |
| shellcheck                                | `ShellCheck`                                     | **0.11.0** (matches `.qa-versions`)              |
| ripgrep, fzf, jq, tree, htop, less, nano  | same names                                       | rg 15.2.0, fzf 0.74.4                            |
| vim                                       | `vim-enhanced`                                   | 9.2                                              |
| procps                                    | `procps-ng`                                      | 4.0.6                                            |
| openssh-client                            | `openssh-clients`                                | 10.2p1                                           |
| tini                                      | `tini`                                           | 0.19.0                                           |
| tzdata                                    | `tzdata`                                         | 2026a                                            |
| wl-clipboard                              | `wl-clipboard`                                   | 2.2.1                                            |
| poppler-utils, ghostscript, moreutils, pv | same names                                       | present                                          |
| imagemagick                               | `ImageMagick`                                    | **7.1.2.32** (the project image's AppImage goes) |
| sqlite3                                   | `sqlite`                                         | 3.51.2                                           |
| dnsutils                                  | `bind-utils`                                     | 9.18.50                                          |
| netcat-openbsd                            | `nmap-ncat` or `netcat`                          | 7.92 / 1.238                                     |
| libimage-exiftool-perl                    | `perl-Image-ExifTool`                            | 13.50                                            |
| gh (GitHub apt repo)                      | `gh` (Fedora) or GitHub's rpm repo               | 2.97.0 in F44, behind upstream 2.102             |
| yq (curl download)                        | `yq`                                             | 4.53.3                                           |
| uv (curl script)                          | `uv` / `python3-uv`                              | 0.12.23                                          |

The image would install by package four tools that the Debian image installs with `curl`
(yq, uv, and potentially gh), plus two that the project image installs by hand (shellcheck,
ImageMagick 7).

**Name resolution not proven:** `mdapi` answers by binary package name, so a bare `nodejs`
or `npm` returned 400. Fedora is expected to resolve them through `Provides:` on the default
stream. The build spike (PLAN Task 3.1) proves it with `dnf install nodejs npm` and records
what was installed.

### Base image size (measured, registry manifests, compressed amd64 layers)

| Image                                          | Compressed |
| ---------------------------------------------- | ---------- |
| `node:lts-slim` (Debian 12 + Node)             | 77 MB      |
| `registry.fedoraproject.org/fedora:44`         | 68 MB      |
| `registry.fedoraproject.org/fedora-minimal:44` | 54 MB      |

Size does not decide this. The image's bulk is elsewhere: Chrome for Testing is 399 MB and
global `node_modules` 615 MB (measured in the container).

### Chromium for agent-browser, the biggest port risk

- agent-browser downloads **Chrome for Testing**, a distro-neutral `linux64` build. Its
  install directory ships **both `deb.deps` and `rpm.deps`** (measured in
  `/root/.agent-browser/browsers/chrome-155…/`). `rpm.deps` lists the RPM soname provides
  (`libnss3.so()(64bit)`, `libgbm.so.1()(64bit)`, `libgtk-3.so.0()(64bit)`, `liberation-fonts`
  and the rest). dnf accepts those provides as install arguments, so the dependency list
  can come from Chrome's own file instead of a hand-kept list.
- `agent-browser install --with-deps` **does** support dnf. The 0.38.2 binary carries a dnf
  package list (`nss nss-tools atk at-spi2-atk cups-libs libdrm libXcomposite libXdamage libXrandr mesa-libgbm pango libxkbcommon libxcb libX11-xcb libX11 libXext libXcursor libXfixes libXi cairo-gobject google-noto-cjk-fonts google-noto-emoji-color-fonts`) and
  the message "No supported package manager found (apt-get, dnf, or yum)". Two names on that
  list do not exist as binary packages in F44: the emoji font is
  `google-noto-color-emoji-fonts` there, and CJK is `google-noto-sans-cjk-fonts`. The
  installer is documented to exit non-zero when it cannot install every library, and it
  runs its package manager through `sudo`. **Do not rely on `--with-deps`.** Install from
  `rpm.deps`, or from an explicit dnf list as the Debian Dockerfile does today.
- The headed mode forwards Wayland (`--ozone-platform=wayland`). The container's
  distribution does not change that, but it has to be shown working on the host (PLAN
  acceptance).

### Every other Debian-specific thing in ccy (complete grep inventory)

Searched for `apt`, `dpkg`, `debian`, `bookworm`, `update-ca-certificates`, `locale`,
`/etc/ssl`, `/etc/pki`, `os-release`, `/usr/lib/x86_64-linux-gnu`, `dash` and `mawk` over the
Dockerfile, `entrypoint.sh`, the launcher, `lib/`, the browser wrappers, templates and docs.

| Where                                                                                    | What                                                                            | Port                                                                               |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `Dockerfile:47`                                                                          | `APT::Sandbox::User "root"` (rootless apt workaround)                           | delete; dnf has no `_apt` user                                                     |
| `Dockerfile:62-111, 119, 162-181`                                                        | `apt-get install` layers                                                        | `dnf install -y --setopt=install_weak_deps=False` + `dnf clean all`                |
| `Dockerfile:133-139`                                                                     | gh via GitHub apt repo and `dpkg --print-architecture`                          | Fedora `gh` or GitHub's rpm repo (owner decision D5)                               |
| `Dockerfile:142, 153`                                                                    | yq and uv by curl                                                               | Fedora packages                                                                    |
| `Dockerfile:4-6, 7-17`                                                                   | PHPantom musl build because of "GLIBC 2.36"                                     | keep the builder (static musl is portable), fix the comment                        |
| `Dockerfile:19-24`                                                                       | `FROM node:lts-slim`                                                            | `FROM registry.fedoraproject.org/fedora:${FEDORA_RELEASE}` plus Node from dnf (D3) |
| `Dockerfile:58-61`                                                                       | tzdata comment about Debian 13                                                  | reword                                                                             |
| `Dockerfile:291-334`                                                                     | agent-browser wrappers check `*/node_modules/agent-browser/bin/*`               | unchanged if npm's global prefix stays `/usr/local` (measure)                      |
| `Dockerfile:419`                                                                         | `ENTRYPOINT ["/usr/bin/tini", …]`, repeated in `update_claude_inplace --change` | Fedora `tini` installs `/usr/bin/tini` (verify in spike)                           |
| `entrypoint.sh`                                                                          | only `python3` (supervisor `ast.parse`); otherwise distro-neutral               | none expected                                                                      |
| generated `#!/bin/sh` wrappers                                                           | `sh` is dash on Debian, bash on Fedora                                          | stricter to looser, so no breakage                                                 |
| `Dockerfile.project-template`, `Dockerfile.example-ansible`, `Dockerfile.example-golang` | apt with cache mounts                                                           | rewrite for dnf (`/var/cache/libdnf5` cache mount)                                 |
| `lib/dockerfile-custom.bash:230, 270, 371, 445-462, 622, 665`                            | AI generator prompt says "Debian slim" and emits apt snippets                   | rewrite for dnf                                                                    |
| `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`                                      | about 20 apt lines, "Debian slim", the Docker apt repo for bookworm             | rewrite                                                                            |
| `files/opt/claude-yolo/ccy-startup-info.txt:12`                                          | "install packages freely (apt, npm, pip…)"                                      | say dnf                                                                            |
| `CLAUDE/ContainerRules.md:44-46`                                                         | "`apt-get update && apt-get install`"                                           | say dnf                                                                            |
| `docs/containerization.md`                                                               | apt cache-mount examples                                                        | rewrite                                                                            |
| `.claude/ccy/Dockerfile` (this repo's project image)                                     | apt layers, gh apt repo, IM7 AppImage, shellcheck binary                        | port; IM7 and possibly shellcheck come from dnf                                    |
| `.claude/plan/ccy-standalone-extraction.md`                                              | apt mention                                                                     | check                                                                              |

Host-side `lib/` code that reads `/etc/localtime` and `/usr/share/zoneinfo` runs on the
Fedora **host**, so it is unaffected.

### Every client project breaks: the biggest consequence

Each project that uses ccy has its own `.claude/ccy/Dockerfile`, `FROM claude-yolo:latest`.
Every template, the AI generator and the guide teach apt, so in practice **every existing
project image runs `apt-get`**. After a Fedora base lands, the first launch in each such
project fails to build its project image (`apt-get: command not found`). That has to be
caught before the build, with a message that names the file and the fix (PLAN Task 3.8).
A silent fallback to a stale image is not acceptable.

### Node on Fedora

Fedora 44 ships parallel `nodejs22` and `nodejs24`; 24.18.0 is behind upstream LTS 24.21.0.
Claude Code is now a native binary (`bin/claude.exe`) wrapped by npm, so Node mostly serves
the npm-installed LSPs and agent-browser's JS shim. Two routes:

- **Distro Node** (`dnf install nodejs npm`): it floats with Fedora, which is in line with
  the policy. Fedora moves Node's major with the release, not with upstream LTS. A floor
  assertion (`node >= 22`) catches a regression.
- **`COPY --from=node:lts-slim`** of `/usr/local/bin/node` and `/usr/local/lib/node_modules/{npm,corepack}`:
  this keeps upstream's floating LTS. Node's release binaries need only glibc 2.28, so they
  run on Fedora. It does mean a second image to pull and copy paths that upstream can
  change.

## 5. A mechanism that keeps tools current, needed on either base

The incident has three causes, and each needs its own fix:

1. **A stale base and stale package layers** (Section 3). Rebuild on a schedule with a
   fresh pull: `--pull=always` (or `newer`) and `--no-cache`. Run that **as the same recipe**:
   the Dockerfile, the container version and the hash do not change, so
   `validate_container_version` still passes and no version bump is owed. The bump rule in
   `CLAUDE/ContainerRules.md` is about the **recipe**, and a refresh does not change the
   recipe.
2. **No signal when a tool is too old for the host.** The fix is a build-time floor
   assertion. A tracked floors file (for example `git 2.48`, `node 22`) is checked by a
   `RUN` at the end of the base stage, so a refresh that cannot meet a floor **fails the
   build** and keeps the previous image instead of shipping a broken one. On today's
   Debian image this check would fail, which is the point.
3. **No signal at launch when the container cannot read the repository.** The fix is the
   entrypoint preflight `git -C /workspace rev-parse --git-dir`, which fails the session
   start and prints git's own error. It is version-agnostic: it also catches the next
   repository extension that nobody has written a floor for.

**Age needs its own label.** `update_claude_inplace` commits a new image every day, which
resets the image's `Created` time, while `podman commit` keeps labels. So the image needs a
`claude-yolo-built-at` label, set in the final layer next to `claude-yolo-dockerfile-hash`
(PERF-02 placement, so it busts no cache). The refresh check reads that label.

## 6. Relative worktrees

- **Where the rule is written.** At this commit, no "no relative worktrees" rule exists in
  `CLAUDE/Worktree.md` (only the daemon-seeded stub) or in `CLAUDE/core/Worktree.core.md`.
  The only record is the Plan 00166 journal (26-10-09), which documents the incident and
  its repair, plus the coordinator's standing instruction to agents ("never pass
  `--relative-paths`").
- **Lifting it has a precondition beyond git 2.48.** Every git that opens this repository's
  `.git` has to understand `extensions.relativeWorktrees`: the host's, every ccy image on
  every machine that mounts a checkout (all rebuilt past the floor), and any other tool
  that parses `.git/config` itself rather than shelling out to git.
- **What lifting buys.** Container-created worktrees today record `/workspace/...` absolute
  paths, which do not exist on the host. That is why a host `git worktree prune` dropped all
  59 of them (Plan 00166 journal). With relative paths written by git 2.48 or later, the
  same worktree resolves on the host and in the container. The rule against host-side
  prune can then be revisited as well.
