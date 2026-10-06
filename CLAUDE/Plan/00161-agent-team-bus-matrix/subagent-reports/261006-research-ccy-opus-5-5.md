# Plan 00161 research: the ccy integration surface

Scope: how an opt-in "join a team" setting (issue #59 section 7) plugs into ccy. Every claim
cites `file:line` at the tree as read (CCY 3.82.1, container 2.44). Claims tagged
**[UNVERIFIED]** are about podman runtime behaviour, which this container cannot observe.
Each one names the host probe that would settle it. All paths are relative to the repo root.
`files/var/local/claude-yolo/` is shortened to `ccy/`.

## Headline findings

1. **The opt-in must be read on the HOST, before the container exists.** It decides a mount
   and a network, and neither `ccy.env` nor `ccy.env.local` can do that: both are sourced
   in-container, after the mounts are fixed (`ccy/lib/common.bash:416-417`,
   `ccy/entrypoint.sh:411-427`, `docs/ccy.md:1284-1286`). No untracked, host-read,
   per-checkout setting exists today. The one host-side per-project store is
   `~/.claude-tokens/ccy/projects/<sha256(path)[:16]>/` (`ccy/lib/network-management.bash:80-86`).
   The playbook creates its parent mode 0700 (`playbooks/imports/play-claude-yolo.yml:947-953`).
   That directory is the natural home for the team choice and the member credential.
2. **The issue's assumption that "no new networking should be needed" holds only in part.**
   Every rootless podman session already shares one bridge, `podman`
   (`ccy/claude-yolo:3011-3013`; Plan 00080 F17/F25/F29). But that bridge is a side effect,
   kept for `--connect`. Plan 00080 has decided to replace it with a per-session network
   (Plan 00080 `PLAN.md:314-320`, dormant). The bus must not depend on it. The robust shape
   is a **dedicated per-team podman network, attached as a second network**. Today's
   launcher supports only one network per launch, so that is a launcher change.
3. **Two sessions in one checkout would share one member account.** ccy names a second
   concurrent container `<project>_yolo_2` (`ccy/lib/common.bash:952-985`). A credential kept
   per checkout would give both containers the same handle and two syncers on one account.
   Issue section 5 forbids two syncers. The design must either refuse a second team session
   in a checkout or allocate per container slot.
4. **The opt-in already has a template to copy: `CCY_CHILD_CLAUDE`** (Plan 00092). It ships an
   optional bin and skill under `/opt/claude-yolo/optional/<feature>/`, adds a PATH symlink
   only when the feature is on, and removes the skill when it is off. See
   `ccy/entrypoint.sh:429-516`, `ccy/Dockerfile:361-370` and
   `playbooks/imports/play-claude-yolo.yml:281-325`.
5. **The container engine never reaches the container.** `<container-type>` is
   `CONTAINER_ENGINE` on the host (`ccy/lib/common.bash:33`), but no `-e` passes it in
   (`ccy/claude-yolo:3526-3548`). `CCY_HOST_HOSTNAME` does reach the container. Give the
   container a handle allocated on the host, not one it builds itself.

---

## 1. Networking

### What network a session is on today

- **Decision point:** `ccy/claude-yolo:3008-3014`. An explicit or saved network is used if
  there is one. Otherwise, under podman and without `--no-network`, the launcher sets
  `SELECTED_NETWORK="podman"` and `NETWORK_FLAG="--network podman"`. The flag is applied at
  `ccy/claude-yolo:3520`, and it is a **single** network.

- **Why it is the shared bridge:** rootless podman's default is pasta, which isolates
  containers from each other. ccy overrides it because pasta cannot `network connect`
  after start, which `--connect` needs (Plan 00080 `research/findings.md` F1/F3, `PLAN.md:75-90`).

- **`--no-network`** leaves `NETWORK_FLAG` empty, so the session gets pasta and joins no
  bridge (`ccy/claude-yolo:2373-2375`; Plan 00080 F2b).

- **Project networks:** `--network NET` (`ccy/claude-yolo:2274-2372`) replaces `podman` as
  the session's only network. `--connect` attaches a running container with
  `container_cmd network connect` (`ccy/lib/network-management.bash:465,514`) and saves the
  network as the project's default (`:497,525,536`, stored at `:80-96`).

- **The facts measured on the host** (Plan 00080):

  - `podman` is one `/16` bridge with `dns_enabled: false` (F25), so peers resolve by IP only.
  - Sessions on it reach each other's ports (H1, confirmed by F32).
  - It spans projects and GitHub identities (F29).
  - A user-created network gets egress, the `host.containers.internal` alias and DNS
    (F35, F36).
  - `network connect` works from a user-created bridge (F34).
  - The host runs Podman 5.8.7, which is below the netavark 2.0 / Podman 6.0 line, so bridge
    isolation needs an explicit `--opt isolate=` (F24).

- **Two preflights act on the selected network:**

  - `ensure_network_dns` (`ccy/claude-yolo:3017-3018`, `ccy/lib/network-management.bash:1119-1178`)
    *adds 1.1.1.1 and 8.8.8.8* to any DNS-enabled network chosen as primary.
  - The internet preflight (`ccy/claude-yolo:3029-3110`) runs `alpine wget google.com` on
    that network and fails the launch if the network has no egress.

  Both make a team network unsuitable as the **primary** network. An `--internal` team
  network would fail the preflight. A DNS-enabled one would be changed to use public
  resolvers.

- **`allowed-hostnames`** (`ccy/lib/common.bash:661-733`, called at `ccy/claude-yolo:130`)
  is a host gate on where ccy may *run*. It has nothing to do with network reach. It matters
  here only because it means ccy may run inside an LXC, where `uname -n` is the LXC's name
  (see section 5).

- **Docker engine:** the cross-engine warning says Docker and Podman have separate network
  namespaces and cannot see each other's networks (`ccy/claude-yolo:2489-2490,2702-2703`).
  A `docker` member cannot join a rootless podman team network. It would need a published
  host port.

### How a container reaches a service "on a host-local podman bridge address"

- **[UNVERIFIED, high confidence]** In rootless podman (netavark), a bridge network and its
  gateway address live inside podman's rootless network namespace, not in the host's own
  namespace. So "the homeserver listens on host-local bridge addresses" (issue section 1)
  has two different meanings:
  - **(a) The homeserver is itself a rootless container on a bridge.** Members that join
    that bridge reach it at its container IP, or by name if the network has DNS. Host
    processes, such as Element, cannot reach that IP directly. They need a port published
    on host loopback (`-p 127.0.0.1:<port>:8008`).
  - **(b) The homeserver is published on a host address, and members connect to the host.**
    From a bridge container that means `host.containers.internal` (F35). Whether a host
    loopback-only bind can be reached that way depends on podman and pasta's host-loopback
    mapping.
  - Probe: on the host, start a throwaway container on a new network, publish it on
    `127.0.0.1`, then run a ccy-image container on `podman` and on the new network. From
    each one, try `curl` to the container IP, to the container name and to
    `host.containers.internal:<port>`. Record which of them connect.
- **The repo has no prior art.** Nothing under `files/`, `docs/`, `playbooks/` or
  `CLAUDE/*.md` mentions `rootless-netns`, `host.containers.internal`, `host-gateway` or
  pasta's loopback mapping. The only hit is Plan 00080's `triage.bash:456-458` probe.
  Nothing in ccy currently reaches a host-run service.

### Recommended network shape (for DESIGN.md to decide)

1. **One podman network per team** (for example `agent-team-<team>`), created by
   `agent-team create`.

   - Created `--internal`. **[UNVERIFIED]** It is not known whether a published port works
     on an internal network.
   - Created with `--opt isolate=true`, because of F24.
   - The homeserver container joins only this network, so it has no outbound route at all.
     That satisfies the privacy check "No outbound traffic from the homeserver" by
     construction rather than by firewall.
   - Human clients reach it through a port published on `127.0.0.1`. That needs a probe.
     If a published port fails on an internal network, add a second, egress-less publish
     path.

2. **A ccy member keeps `podman` (or its project network) as its primary network** and
   attaches the team network as a second one. There are two ways:

   - Pass `--network podman --network agent-team-<team>` at `run`. **[UNVERIFIED]**: confirm
     that the podman version on the host accepts several bridge `--network` flags.
   - Or call `container_cmd network connect` after start, the way `--connect` does
     (`ccy/lib/network-management.bash:465`; F34 shows it works).

   The run-time flag is simpler because no race with the session start is possible.

3. **Launcher changes this implies:**

   - Keep the team network out of `SELECTED_NETWORK`, so neither the preflight nor
     `ensure_network_dns` touches it (`ccy/claude-yolo:3008-3018`).
   - Never write it into `save_network_preference`, `LAST_NETWORK` or `save_launch_config`
     (`ccy/claude-yolo:3124`, `ccy/lib/network-management.bash:89-96`). If it got there, it
     would become a later launch's *only* network and cut off the Claude API.
   - Make `--disconnect` refuse it, or skip it.
   - Restart (`ccy/claude-yolo:3416-3417`, `ccy/lib/restart-request.bash:332`) and reboot
     restore (`ccy/lib/session-registry.bash:144`) replay arguments. If the team choice is a
     persisted per-project setting rather than a flag, both inherit it with no change.

4. **The CLI's "host-local" check** (issue section 1: refuse plain HTTP that is not
   host-local) must accept the team network's subnet and container name, and not only
   `127.0.0.0/8`. The member config should pin the URL, for example
   `http://<homeserver-name>:8008`, which resolves through the team network's DNS.

5. **Compatibility with Plan 00080 Option 2** (`ccy-isolate-` per-session networks): this
   design needs no change under it. The team network is an extra attachment, and a team
   network is shared only by its members, which is the intended reach.

## 2. Declaring a per-project setting and mounting a token read-only

| Mechanism                               | Read where / when                                                                                   | Tracked?                                                                      | Can add a mount or network? | Fit for "join team X" + token                                                                                                                                                                            |
| --------------------------------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `.claude/ccy/ccy.env`                   | in-container, entrypoint `ccy/entrypoint.sh:420-426`                                                | yes (`ccy/lib/common.bash:497-508,583-594`)                                   | no (too late)               | Only for in-container toggles, such as the skill or hooks gate. Not the credential.                                                                                                                      |
| `.claude/ccy/ccy.env.local`             | in-container, sourced after `ccy.env` and wins (`ccy/entrypoint.sh:415-416`, `docs/ccy.md:883-890`) | never                                                                         | no                          | Same limit.                                                                                                                                                                                              |
| `.claude/ccy/mounts`                    | host, before the container exists (`ccy/lib/common.bash:425-480`, launcher `:2258-2264`)            | **yes**                                                                       | mount only                  | Poor. It is tracked, so a machine-specific token path would be committed. No SELinux relabel is allowed (`docs/ccy.md:1321-1323`), so on an Enforcing host the token is unreadable inside the container. |
| `CCY_EXTRA_MOUNTS`                      | host env, per launch, unvalidated (`ccy/claude-yolo:2252-2256`)                                     | n/a                                                                           | mount only                  | Debug aid only. It does not survive a reboot restore.                                                                                                                                                    |
| `~/.claude-tokens/ccy/projects/<hash>/` | host (`ccy/lib/network-management.bash:80-86`)                                                      | outside the repo; dir 0700 (`playbooks/imports/play-claude-yolo.yml:947-953`) | the launcher can do both    | **Recommended.** Host-only, never visible inside `/workspace`, per checkout, already used for per-project network state.                                                                                 |

**Mount validation rules that would apply** if a team path ever went through the mounts file
(`ccy/lib/common-pure.bash:345-439`):

- The host-side deny list (`:399-404`) covers `~/.ssh`, `~/.claude-tokens`, `~/.config/gh`
  and similar. It does **not** cover `~/.config/pingbus/`, so the issue's proposed
  `~/.config/pingbus/<team>/` would be accepted.
- The container side denies `/opt`, `/root`, `/run` and `/var` (`:423-427`), so the mount
  target would have to be under a path like `/ccy/...`.

**The pattern to copy for a 0600 token on an SELinux host is the SSH key staging**
(`ccy/lib/ssh-handling.bash:1168-1189`). When `CCY_SELINUX_MODE` is not `off`:

- The launcher copies the key with `install -m 0600` into an owner-only
  `mktemp -d "$XDG_RUNTIME_DIR/ccy-keys.XXXXXX"` directory.
- It mounts that directory with `:ro,Z`, so the user's own file is never relabelled.
- Cleanup removes the copy.
- Otherwise it bind-mounts the file directly with `:ro`.

The mode is decided once per launch by `ccy_selinux_mode` (`ccy/lib/common.bash:69`, called
at `ccy/claude-yolo:1137`).

A token staged this way is a snapshot. A `rotate-token` takes effect at the next launch.
Restart already re-derives staged state, because it unsets the staging directory before it
execs (`ccy/claude-yolo:3467-3476`).

**Never pass the token value as an environment variable on argv.** BSH-09
(`ccy/claude-yolo:3277-3283`) forwards secrets by name only. A file mount avoids the
question entirely. Pass the token's *path* (for example a `PINGBUS_TOKEN_FILE`) and the team
name inline with `-e`.

**Security-model docs to update:** add rows for the team credential and the team network to
the "What the container CAN reach" table (`docs/ccy.md:391-403`).

## 3. Getting the CLI onto PATH, and the version bump

- **Image route (recommended, matching child-claude):**

  1. Put the source under `files/opt/claude-yolo/optional/<feature>/bin/` and `.../skills/`.
  2. The playbook stages it into the build context (`playbooks/imports/play-claude-yolo.yml:281-325`;
     note the warning at `:293-300` that a removed file lingers in the build context unless
     a task removes it).
  3. The Dockerfile copies `optional/` wholesale (`ccy/Dockerfile:369`). The `chmod` at `:370`
     names the child-claude bin explicitly, so a new bin needs its own `chmod` or a mode set
     by the playbook.
  4. When the feature is on, the entrypoint symlinks the bin into `/usr/local/bin`
     (`ccy/entrypoint.sh:468`). That path is on the container's own disposable filesystem,
     so there is nothing to clean up (`:499-500`).

  `python3` is in the image (`ccy/Dockerfile:73`), so a CLI that uses only the standard
  library needs no new package.

- **Build trigger:** the playbook runs `podman build` on every run
  (`playbooks/imports/play-claude-yolo.yml:955-967`), so a changed staged file reaches the
  image through layer invalidation. The *launcher's* staleness check compares only the
  version label (`REQUIRED_CONTAINER_VERSION`, `ccy/claude-yolo:77`, against
  `LABEL claude-yolo-version`, `ccy/Dockerfile:36`).

- **Mount route:** binding a host-installed CLI and exporting `PATH` from `ccy.env`. This is
  possible, but the tracked mounts file has no relabel, so the CLI would be unreadable on an
  Enforcing host. The CLI would also then version with the host, not with the image. It is
  better suited to the issue's section 8 artefact for members ccy does not manage.

- **Version bump rule** (`CLAUDE/ContainerRules.md:59-107`, `.claude/rules/ccy-version-bump.md`):

  - Any change to the launcher or `lib/*.bash` bumps `CCY_VERSION` (`ccy/claude-yolo:17`;
    the pre-commit hook enforces it, and a `lib/` change must stage the launcher).
  - Any change to the Dockerfile or `entrypoint.sh` bumps `LABEL claude-yolo-version` and
    `REQUIRED_CONTAINER_VERSION`. The two must match (`ccy/Dockerfile:30-36`, `ccy/claude-yolo:77`).
  - Add a `docs/ccy-changelog.md` entry.

  This feature touches all four layers, so it needs a minor `CCY_VERSION` bump plus a
  container version bump.

## 4. How skills and hooks reach a session

- **State location:** `/root/.claude` is a symlink to `/workspace/.claude/ccy`
  (`ccy/entrypoint.sh:310-322`). User-scope settings, skills and plugins are therefore
  **host-persisted per project**, inside the project tree.

- **Skills:**

  - Everything in `/opt/claude-yolo/skills/` is copied into every session, unconditionally
    (`ccy/entrypoint.sh:366-383`; `ccy/Dockerfile:350-359`).
  - An opt-in skill therefore has to live under `/opt/claude-yolo/optional/`
    (`ccy/entrypoint.sh:435-438`). It is copied in when the feature is on and **removed when
    it is off**, but only if its `SKILL.md` frontmatter `name:` marks it as the shipped copy
    (`:494-516`).

- **Claude Code hooks:** there are two places to register the Stop and UserPromptSubmit hooks.

  - **User settings**, `/root/.claude/settings.json` (= `/workspace/.claude/ccy/settings.json`).
    The entrypoint already merges keys into it with `jq` (`ccy/entrypoint.sh:331-353`, for
    the LSP flag). A feature gate could merge `hooks.Stop` and `hooks.UserPromptSubmit`
    entries that call the CLI. Because the file is host-persisted, the gate must also
    *remove* its own entries when off, recognising them by an exact command string. This is
    the child-claude lesson again.
  - **Project settings**, `.claude/settings.json`. In a hooks-daemon project, every event is
    routed to `.claude/hooks/<event>` (this repo's `.claude/settings.json`; handlers live in
    `.claude/hooks/handlers/`). A pingbus handler there would only exist in daemon projects,
    and ccy cannot assume a project uses the daemon.

  Claude Code merges hooks from both scopes, so a user-scope Stop hook runs alongside the
  daemon's own Stop handlers, which can block a stop (R-STOP-\*). DESIGN.md must say how a
  "pending ping" Stop result interacts with them. One option is to make the pingbus Stop
  hook report pending pings only, and never block.

- **Supervisor seam (issue section 6, #31):** the project supervisor wraps `claude`. ccy
  turns it on by default when `/workspace/.claude/ccy/claude-supervise.py` exists
  (`ccy/entrypoint.sh:518-567`). Plugins are staged at `/opt/claude-yolo/supervisor-plugins/`
  (`ccy/Dockerfile:372-381`), with the lifecycle plugin as the worked example
  (`ccy/entrypoint.sh:574-660`). A wake-on-ping supervisor plugin is the alternative to a
  background `pingbus wait`, but it only works where the daemon's supervisor is present.

## 5. Session identity, for `<repo>.<n>+<host>.<container-type>`

| Part               | Available today                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Gap                                                                                                                                                                                                                                                                                       |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `<repo>`           | Two sources, neither ready-made. `PROJECT_NAME` (`ccy/claude-yolo:193`, from `get_project_name`, `ccy/lib/common-pure.bash:445-456`) is the *directory* name, `<parent>-<dir>` unless the parent is generic, lowercased, with other characters turned into `_`. It labels the container `ccy-project` (`ccy/claude-yolo:3516`), but it is not the repository's name. The remote URL is available on the host from `get_project_remote_url` (`ccy/lib/ssh-handling.bash:45-69`). | Decide which one. The repository basename from the remote, lowercased, falling back to the directory name, is closer to "the repository". Map the result onto the localpart charset (`a-z 0-9 . _ = - / +`). Avoid `+` and `.` inside `<repo>`, or the handle becomes ambiguous to parse. |
| `<n>`              | Nothing in ccy.                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Provisioning allocates it (issue section 2). It lives with the credential in host state. See finding 3: concurrent `_yolo_N` containers in one checkout.                                                                                                                                  |
| `<host>`           | `CCY_HOST_HOSTNAME`, derived from `uname -n` by `ccy_host_hostname`: first label, RFC 1123 grammar enforced, launch aborts on failure (`ccy/lib/common-pure.bash:458-488`, `ccy/claude-yolo:3355-3369`). It is passed with `-e` (`ccy/claude-yolo:3536`) and documented (`docs/ccy.md:402`).                                                                                                                                                                                    | **Case is preserved.** It must be lowercased for the handle. Inside an LXC, it is the LXC's name, not the physical machine's. That is probably correct, since the handle names where the session runs, but DESIGN.md should say so.                                                       |
| `<container-type>` | `CONTAINER_ENGINE`, `podman` or `docker` (`ccy/lib/common.bash:33`, set from `CCY_CONTAINER_ENGINE` or the engine argument at `ccy/claude-yolo:596`).                                                                                                                                                                                                                                                                                                                           | **Not passed into the container.** It appears only as a placeholder in the startup prompt (`ccy/claude-yolo:3153-3154`). Inside the container, `DEVCONTAINER=true` (`ccy/Dockerfile:387`) shows only that the session is in some container.                                               |

**Recommendation:**

- The launcher already resolves the member on the host. Have it pass the *finished* handle
  in as an environment variable (for example `PINGBUS_HANDLE`), together with the team name
  and the token file path.
- The CLI can check that the handle matches the credential's user ID, but should not build
  the handle itself.
- Add a run-time label `ccy-team=<team>` beside the existing ones (`ccy/claude-yolo:3325-3353,3514-3519`;
  `docs/ccy.md:1174-1202`), so host tooling (`podfreeze`, `agent-team list`) can find the
  members.

## Open questions for DESIGN.md

1. Three podman behaviours need a host probe before DESIGN.md commits to a network shape:
   - several `--network` flags at `run`;
   - a published port on an `--internal` network;
   - reaching a homeserver from a ccy session by container IP, by DNS name and by
     `host.containers.internal`.
2. The CLI flags: an explicit `--team NAME` / `--no-team`, and whether the flags or the
   persisted host setting is the source of truth. `--no-team` mirrors `--no-network`.
3. One member per checkout, with a refusal for a second concurrent session, or one member
   per container slot (`_yolo`, `_yolo_2`, and so on).
4. How the Stop hook interacts with the hooks daemon's Stop handlers.
5. Docker members, which cannot join a podman network: support them through a published
   port, or leave them out of version 1.
