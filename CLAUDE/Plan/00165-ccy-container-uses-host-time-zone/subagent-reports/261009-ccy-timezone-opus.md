# Plan 00165 — ccy containers use the host's time zone (implementation report)

## The defect

A ccy session's status line read an hour earlier than the desktop clock. In the
container `date` prints UTC, `/etc/localtime` links to `Etc/UTC` and `TZ` is unset. The
host is on a UK zone in British Summer Time. Containers share the host's kernel clock, so
the instant is right; only the zone differs.

## Mechanism chosen: `-e TZ=<host IANA zone>`

Considered podman's `--tz=local`. Rejected because:

- ccy supports docker as well (`CCY_CONTAINER_ENGINE=docker`, `lib/common.bash`), and
  docker has no `--tz`.
- `TZ` composes with the existing override path: the entrypoint's PROJECT-ENV block
  sources `.claude/ccy/ccy.env` then `ccy.env.local` after the launcher's environment, so
  an `export TZ=...` there wins with no new code.

There is exactly one `container_cmd run` in the launcher, shared by interactive, headless,
`--teams` seat and `ccy --` passthrough launches, so one `-e` line covers every path.
(`token-management.bash`'s `setup-token` run is a one-shot login, not a session.)

### Resolution (host side, `ccy_host_time_zone` in `lib/common-pure.bash`)

1. `timedatectl show -p Timezone --value` (skipped if timedatectl is absent; a failed
   call says so on stderr and falls through).
2. Else the target of the host's `/etc/localtime` symlink, after its last `/zoneinfo/`
   (absolute and relative links both work).
3. A candidate is accepted only if it matches `^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$` (no
   dots, so no `..`; no leading slash; nothing a shell reads) and is a regular file under
   the host's `/usr/share/zoneinfo`. A directory such as `Europe` is refused.
4. Neither accepted: the function prints the reason on stderr and the launch exits 1. It
   never yields UTC by default. A host genuinely on UTC still gets `UTC`.

A host shell's own `TZ` export is deliberately not consulted: the desktop clock follows the
system zone, which is what the owner compares against.

## The image: tzdata

The current image (Debian 12 via `node:lts-slim`) has `tzdata` installed and
`/usr/share/zoneinfo/Europe/London` present; `TZ=Europe/London date` printed BST inside
this container. But it is there only because the base includes it. Debian 13 dropped
tzdata from its required set, and `node:lts-slim` floats. Without zoneinfo, glibc reads
`TZ=Europe/London` as UTC with no error — the exact silent failure this plan exists to
stop. So `tzdata` is now named in the base Dockerfile's first apt list, and the container
version moves 2.48 → 2.49 (label and `REQUIRED_CONTAINER_VERSION`). One rebuild on the
next launch.

## Versions and files

- CCY 3.88.2 → 3.89.0 (minor: new behaviour), container 2.48 → 2.49.
- `files/var/local/claude-yolo/lib/common-pure.bash` — `ccy_host_time_zone`, exported.
- `files/var/local/claude-yolo/claude-yolo` — resolution block after the host-hostname
  block; `-e "TZ=$CCY_HOST_TZ"` on the run; both versions.
- `files/var/local/claude-yolo/Dockerfile` — `tzdata`, label 2.49.
- `scripts/test-ccy-host-time-zone.bash` (new), wired into `scripts/qa-all.bash` as the
  `ccy-host-time-zone` hard gate next to `ccy-host-hostname`.
- `docs/ccy-changelog.md` 3.89.0 entry; `docs/ccy.md` "What the container CAN reach" row.
- Plan `deploy.bash` (play-claude-yolo.yml) and the `meta-deploy.bash` entry, placed after
  00164 and before 00161.

## Tests

Written first; the run before the function existed failed ("ccy_host_time_zone is not
defined"). After: `scripts/test-ccy-host-time-zone.bash` — 26 cases, all pass:

- host zone passed through (timedatectl; timedatectl over a disagreeing link; three-part
  names; `+` names; real UTC);
- link fallback (absolute, relative, invalid timedatectl answer, timedatectl zone missing
  from zoneinfo);
- refusals (nothing; link outside zoneinfo; unknown zone; `..`; leading slash; space;
  `;`; POSIX TZ string; newline; a zoneinfo directory), with the reason on stderr and
  nothing on stdout;
- launcher wiring (the `-e "TZ=$CCY_HOST_TZ"` argv, the resolver call, no `--tz` flag);
- override: the entrypoint's real PROJECT-ENV block, run against a fake workspace with
  `TZ=Europe/London` already set, yields the `ccy.env` value, then the `ccy.env.local`
  value over both.

Also re-run unchanged and passing: `test-ccy-host-hostname.bash`,
`test-ccy-gpu-device.bash`, `test-ccy-project-env.bash`,
`test-ccy-container-version-hook.bash`. shellcheck clean on the new test, the library and
`qa-all.bash`; the launcher shows only its pre-existing info-level notes. `qa-all.bash`
itself was not run (blocked in sub-agents).

## After deploy

The owner runs `./CLAUDE/Plan/meta-deploy.bash`, then exits running ccy sessions and starts
fresh ones (running containers keep UTC until restarted; the first launch rebuilds the
image for container 2.49). `date` inside should match the desktop clock.
