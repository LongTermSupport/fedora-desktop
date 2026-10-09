# Plan 00169 planning report: ccy image keeps its tools current

Planning only; nothing in ccy was changed. Evidence is in
[../RESEARCH-base-image.md](../RESEARCH-base-image.md) and the tasks are in
[../PLAN.md](../PLAN.md).

## The question the owner asked: why Debian?

There was no reason. `f0686fbd`, the commit that created ccy, wrote `FROM node:20-slim`
and described it as "Node.js 20 base". `4477eab2` moved it to `node:lts-slim` to follow
Claude Code's Node floor. Debian is what the official Node images are built on, and that
is the whole history. No doc, changelog entry, journal or completed plan compares
distros, image sizes or Chromium support.

## What was measured

- **Container git is 2.39.5** (`1:2.39.5-0+deb12u3`) on Debian 12. glibc is 2.36.
- **Debian cannot supply git 2.48 or later from any stable suite.** bookworm has 2.39.5
  and trixie 2.47.3, and neither backports git. Only testing (2.53) and unstable (2.55)
  are new enough. Changing the Debian base alone does not fix the incident.
- **`node:lts-slim` is bookworm.** Its Docker Hub digest equals `lts-bookworm-slim`'s
  (2026-10-06 update). The floating tag keeps Node current and leaves the distro on
  oldstable.
- **Fedora 44** has git 2.56.0, ShellCheck 0.11.0 (the exact `.qa-versions` pin),
  ImageMagick 7.1.2, yq 4.53.3, uv 0.12.23, gh 2.97.0, nodejs24 24.18.0, Python 3.14.8,
  and every other package in today's apt lists under a known Fedora name. RESEARCH
  Section 4 has the table.
- **Base sizes do not decide it.** Compressed: `node:lts-slim` 77 MB, `fedora:44` 68 MB,
  `fedora-minimal:44` 54 MB. The image's bulk is Chrome (399 MB) and global
  `node_modules` (615 MB).
- **Chromium port risk is low.** agent-browser runs Chrome for Testing, a distro-neutral
  build whose install directory ships an `rpm.deps` dependency list.
  `agent-browser install --with-deps` has a dnf branch, but two of its package names do
  not exist in F44 and it goes through `sudo`. The plan installs from `rpm.deps` instead.
- **Why the image goes stale on any base.** Builds pass no `--pull`, so the podman
  default pulls `FROM` only when it is missing (from the documentation; triage measures
  it). The package layers are reused until the Dockerfile changes. The only refresh is
  Claude Code's daily in-place npm update.

## Recommendation

Move ccy to Fedora at the host's release (`registry.fedoraproject.org/fedora:<host VERSION_ID>`). Debian was incidental. Getting git 2.48 or later on it means owning a
source build. And the project image already works around Debian lag three times
(ansible-core, shellcheck, ImageMagick).

Separately, and before the base switch, ship two safeguards:

1. An entrypoint preflight: `git -C /workspace rev-parse --git-dir`, which fails the
   launch with git's own message.
2. A build-time floors file (`git 2.48`, `node 22`).

Then add a scheduled same-recipe refresh, `ccy --refresh-images` with `--pull=always --no-cache`, triggered by a user timer, with an inline fallback when an image is older
than twice its maximum age.

## Version policy and the bump rules

- **Float on distro packages**: git, openssh, python, curl, jq, rg, ShellCheck, ImageMagick,
  yq, uv, Node (with a floor). A rebuild picks up their updates.

- **Float on upstream latest at build time**: Claude Code (plus the daily update),
  agent-browser, Chrome for Testing, the npm LSPs.

- **Pin where compatibility or integrity demands it**:

  - Lightpanda, sha256: its silent screenshot and geometry failures were measured on 0.3.6.
  - The PHPantom tag: a reproducible source build.
  - The project image's QA tools in `.qa-versions`: the QA verdict must not depend on the
    machine it runs on.

  The plan registers the Dockerfile pins in `vars/version-pins.yml` so drift is reported.

- **The bump rules still hold.** The container version names the **recipe**. A refresh
  rebuilds the same Dockerfile, so the version label and hash still match,
  `validate_container_version` passes and no bump is owed. Every edit to the Dockerfile
  or `entrypoint.sh` still bumps both version fields, and a launcher or `lib/` edit still
  bumps `CCY_VERSION`. The age check reads a new `claude-yolo-built-at` label, because
  `podman commit` from the daily Claude update resets the image's `Created` time but keeps
  labels.

## Main risk

Every client project's `.claude/ccy/Dockerfile` runs `apt-get`: the templates, the AI
generator and the guide all teach it. After a Fedora base lands, each of those project
images fails to build on its next launch. The plan treats this as a major container
version. A launcher guard detects `apt-get` or `dpkg` in a project Dockerfile and stops
with the file, the line and the fix, and the templates and docs are rewritten. Owner
decision D6 covers an optional transitional Debian tag.

## Relative worktrees

No "no relative worktrees" rule exists in `CLAUDE/Worktree.md` at this commit. The Plan
00166 journal and the coordinator's instructions are its only record. The lifting
condition: every git that opens the repository must be 2.48 or later, including every
ccy image on every machine that mounts a checkout. What lifting gains: worktrees created
in a container would then resolve on the host as well, which removes the cause of the
59-worktree loss from a host-side prune.

## Owner decisions in the plan

| ID  | Decision                 | Recommendation                                         |
| --- | ------------------------ | ------------------------------------------------------ |
| D1  | Base image               | Fedora                                                 |
| D2  | Fedora release           | The host's release, not `latest`                       |
| D3  | Node source              | Fedora `nodejs`                                        |
| D4  | Refresh trigger          | Timer, plus inline fallback past twice the maximum age |
| D5  | gh source                | Fedora `gh` or GitHub's rpm repo                       |
| D6  | Client-project migration | Hard guard; no legacy tag unless the owner wants one   |
