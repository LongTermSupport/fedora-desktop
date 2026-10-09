# Plan 00169 fact-check (PLAN.md and RESEARCH-base-image.md at aa01e56d)

Read-only check of every claim that can be tested from a ccy container: the repo at
aa01e56d, this container's own tools, `api.ftp-master.debian.org/madison`,
`mdapi.fedoraproject.org/f44`, the Docker Hub tag API, `registry.fedoraproject.org`
manifests, the GitHub releases API and git's own 2.48.0 release notes.

**Tally: 40 CONFIRMED, 4 REFUTED, 8 UNVERIFIABLE.**

## REFUTED

1. **F44 Node name resolution (RESEARCH §4 table, "Name resolution not proven", "Node on
   Fedora"; PLAN Task 3.1, D3).** The bare names resolve to the **22** stream, not 24.
   In F44, the capability `nodejs` is provided by `nodejs22` (22.23.1), and `nodejs24`
   does not provide it. `npm` is provided by all three of `nodejs20-npm`, `nodejs22-npm`
   (10.9.8) and `nodejs24-npm` (11.16.0). F44 also ships `nodejs20` (20.20.2), so there
   are three parallel streams, not two. Correction: `dnf install nodejs npm` should give
   Node 22.23.1 / npm 10.9.8. That still clears the `node >= 22` floor, but to get 24 you
   have to name `nodejs24 nodejs24-npm`. The `/usr/bin/node` alternative comes from
   `nodejs24-bin` (`alternative-for(nodejs-bin)`), which the spike should check. The
   table row "node / npm → `nodejs24` 24.18.0 / 11.16.0" holds only if those names are
   used explicitly.
2. **"yq and uv by curl" (RESEARCH §4 inventory row `Dockerfile:142, 153`; PLAN
   Decision 1 "yq and uv stop being curl installs").** yq is fetched with `wget`
   (`Dockerfile:142`), not curl. Only uv uses curl. The count in RESEARCH §4 is also off:
   it says "four tools that the Debian image installs with `curl` (yq, uv, and potentially
   gh)" but lists three. Correction: "fetched over the network (yq by wget, uv by curl,
   gh from GitHub's apt repo)", count three.
3. **"complete grep inventory" (RESEARCH §4).** It misses one apt reference in the
   launcher: `files/var/local/claude-yolo/claude-yolo:2108`,
   `echo "Tip: apt/npm packages are cached between builds for speed"`. Since it is in
   the launcher, porting it bumps `CCY_VERSION`. Add it to the inventory and to Task 3.6.
4. **PLAN Overview: "Fedora 44 … ships git 2.56, ShellCheck 0.11.0 and ImageMagick 7.
   These are the versions the project image currently fetches by hand."** The project
   image does not fetch git by hand. Only ShellCheck (upstream binary) and ImageMagick 7
   (AppImage) are fetched that way. Also, the project Dockerfile's IM comment says
   "Production target is Fedora **43**". RESEARCH §2 elides that number with "…", so the
   quote is fair, but the comment itself is stale.

## CONFIRMED (grouped)

- This container: `git version 2.39.5`, dpkg `1:2.39.5-0+deb12u3`; Debian 12.15, glibc
  2.36; Node v24.21.0, npm 11.19.0; Python 3.11.2; gh 2.102.0; yq v4.53.3; uv 0.12.24;
  Claude Code 2.1.295 (native `bin/claude.exe`); agent-browser 0.38.2;
  `chrome-155.0.8059.39`; Lightpanda 0.3.6; shellcheck 0.11.0 on PATH, with apt's 0.9.0 at
  `/usr/bin`; npm prefix `/usr/local`; Chrome dir 399M, `node_modules` 615M.
- git 2.39.5 rejects the extension. Reproduced in a throwaway repo with
  `core.repositoryformatversion=1` and `extensions.relativeWorktrees=true`:
  `fatal: unknown repository extension found: relativeworktrees`.
- git 2.48.0 RelNotes add relative worktree linkage and "a new repository extension to
  prevent older Git versions from mis-interpreting worktrees created with relative paths".
- History: `git log --diff-filter=A` on the Dockerfile gives `f0686fbd`. Its first line
  is `FROM node:20-slim`, and the message line is quoted exactly. `4477eab2` moved the
  base to `node:lts-slim` with the quoted "Debian-based, matching the apt layers below"
  comment (now `Dockerfile:19-24`). `7ee4b481` names tzdata for container 2.49 with the
  quoted reason.
- Debian madison: oldstable 2.39.5-0+deb12u3, stable 2.47.3-0+deb13u1, testing 2.53.0-1,
  unstable 2.55.0-1. No backports rows exist for git, so no stable release reaches 2.48.
- Docker Hub, `last_updated` 2026-10-06: `lts-slim` and `lts-bookworm-slim` are both
  `sha256:d6aa754f16b3…`, and `lts-trixie-slim` is `sha256:173f1258…`.
- Image sizes from the registry layer sums, amd64: node:lts-slim 80.8 MB (77 MiB),
  fedora:44 72.0 MB (68.6 MiB), fedora-minimal:44 56.6 MB (54 MiB). These match the
  table when read as MiB.
- F44 mdapi, every other row: git 2.56.0; nodejs24 24.18.0 and nodejs24-npm 11.16.0;
  python3 3.14.8; python3-pyyaml 6.0.3; pipx 1.15.0; ShellCheck 0.11.0 (equals
  `.qa-versions` `SHELLCHECK=0.11.0`); ripgrep 15.2.0; fzf 0.74.4; vim-enhanced 9.2;
  procps-ng 4.0.6; openssh-clients 10.2p1; tini 0.19.0 with `/usr/bin/tini` in its file
  list; tzdata 2026a; wl-clipboard 2.2.1; poppler-utils, ghostscript, moreutils and pv
  present; ImageMagick 7.1.2.32; sqlite 3.51.2; bind-utils 9.18.50; nmap-ncat 7.92 and
  netcat 1.238; perl-Image-ExifTool 13.50; gh 2.97.0; yq 4.53.3; uv and python3-uv
  0.12.23. Bare `nodejs` and `npm` have no `pkg` entry.
- gh upstream latest is v2.102.0, so F44's 2.97 is behind.
- Chrome for Testing ships both `deb.deps` and `rpm.deps`, and `rpm.deps` lists
  `libnss3.so()(64bit)`, `libgbm.so.1()(64bit)`, `liberation-fonts` and a libgtk-3
  provide.
- The agent-browser linux-x64 binary carries the quoted dnf list, the string
  "No supported package manager found (apt-get, dnf, or yum)" and "Running: sudo …".
  `google-noto-emoji-color-fonts` and `google-noto-cjk-fonts` are absent in F44.
  `google-noto-color-emoji-fonts` and `google-noto-sans-cjk-fonts` exist.
- File:line references:
  - Dockerfile `4-6/7-17` (PHPantom, GLIBC 2.36), `19-24`, `47` (APT::Sandbox),
    `58-61` (tzdata / Debian 13), apt layers at 62/100/119/162, `133-139` (gh, dpkg),
    `142`, `153`, `291-334` (wrappers), `419` (tini ENTRYPOINT)
  - `lib/common.bash:934` `validate_container_version`, and `:1015`
    `build_container_with_hash` (no `--pull`)
  - `play-claude-yolo.yml:1089` build task (no `--pull`)
  - `claude-yolo:1805` `auto_update_claude_code`, with `update_claude_inplace`'s
    `--change` tini at `:1934`
  - `--rebuild` full passes `--no-cache` only
  - `lib/dockerfile-custom.bash` 230/270/371/445-462/622/665
  - `ccy-startup-info.txt:12`
  - `ContainerRules.md:44-46`
  - `CUSTOM-DOCKERFILES.txt` (24 apt matches, "Debian slim", Docker bookworm repo)
  - the three templates and `docs/containerization.md` use apt cache mounts
  - `.claude/plan/ccy-standalone-extraction.md` mentions apt
- The base image is built at exactly two sites. The third `build` call
  (`claude-yolo:2122`) is the project image.
- The project image's three workaround comments are present: ansible-core 2.14 vs 2.19,
  shellcheck "years behind", and "Debian 12 ships only IM6".
- The `ContainerRules.md:94-106` bump rules read as the plan states them: a
  Dockerfile/`entrypoint.sh` change bumps the label and `REQUIRED_CONTAINER_VERSION`
  together, and a launcher/`lib/` change bumps `CCY_VERSION`.
- Pins: `vars/version-pins.yml` is read by `check-pinned-versions.bash`, and
  `qa-version-pins.bash` checks that the playbook "declares" the var. Lightpanda 0.3.6 is
  sha256-pinned (Plan 00097 exists). PHPantom's tag is pinned at 0.7.0. ruff's pin cites
  Plan 00071. PERF-02 places the hash label in the last layer.
- `CLAUDE/Worktree.md` at aa01e56d has no relative-worktree rule, and neither does
  `Worktree.core.md`. The Plan 00166 journal (26-10-09) records the
  `--relative-paths` incident, the host prune and the 59 rebuilt worktrees.
- `docs/ccy.md` and `ContainerRules.md` give no argument for Debian.

## UNVERIFIABLE

1. "Plan 00161's acceptance failed (M2.7)" because of this incident. No 00161 file
   records a git or extension failure at M2.7. Its 26-10-09 journal records other M2
   failures and a full M2 pass.
2. "Every new ccy session started with no git and no hooks daemon." This is not recorded,
   though the reproduction above shows git would refuse.
3. podman's default `--pull=missing` and `podman commit` keeping labels. There is no
   podman in the container. These are documented behaviour, and Task 1.1/4.2 measure them.
4. "dnf accepts those provides as install arguments." Note that `rpm.deps` line 2 is a
   rich boolean, `(libgtk-3.so.0()(64bit) or libgtk-4.so.1()(64bit))`, which must be
   passed quoted. Leave this to the Task 3.1 spike.
5. agent-browser `--with-deps` is "documented to exit non-zero" when libraries are missing.
6. Node release binaries need only glibc 2.28 (not fetched).
7. Debian 13 dropped tzdata from its required set (not fetched).
8. "No maintained git PPA for Debian", and the coordinator's standing instruction.
