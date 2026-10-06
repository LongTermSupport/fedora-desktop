# Research: rootless podman services in plays (Plan 00161, Task 1.1)

Scope: how this repo's plays run rootless podman services under `systemd --user`, firewall
and bridge facts, user-CLI and helper installation, and the engine choice rule. Evidence is
`file:line` against the F44 checkout. Nothing was run on a host: the container has no
`podman` or `firewall-cmd`, so every runtime claim marked **VERIFY** needs a probe in the
plan's `triage.bash`.

## 1. Headline facts

1. **There is no Quadlet anywhere in the repo.** No `*.container`, `*.network`, `*.pod`,
   `*.volume` or `*.kube` file under `playbooks/`, `files/`, `environment/`; no mention of
   `quadlet` or `containers/systemd`; no `podman generate systemd`; no unit whose
   `ExecStart` runs podman. The homeserver play would be the **first** Quadlet user. It
   sets the pattern, so get it right.
2. **No podman network with a fixed subnet exists.** `podman network create` appears only
   in user-facing hint text (`files/var/local/claude-yolo/lib/network-management.bash:375`,
   `files/var/local/claude-yolo/claude-yolo:2992`).
3. **The only long-running rootless container service is UniFi, and it is not a systemd
   service.** `playbooks/imports/optional/common/play-unifi-controller.yml` writes a compose
   file (:62-106), pre-pulls (:112-119) and ships an on-demand launcher in `~/.local/bin`
   (:150-233). It publishes on every interface (:98-105) and opens the ports host-wide with
   raw `firewall-cmd` in a `shell:` (:125-145). **Do not copy it:** it is the opposite of
   "bound to a host-local address, unreachable from the network".
4. **`systemd --user` services exist, for non-container processes**, and they give the
   mechanics to copy:
   - `play-container-watch.yml`: unit files from `files/home/.config/systemd/user/`
     (:81-98), UID resolved and asserted (:107-128), enable as the user with
     `scope: user` + `XDG_RUNTIME_DIR` (:146-156), and a comment on why the enable is
     unconditional and its own probe (:130-145).
   - `play-rclone.yml`: **one templated unit per item from a host_vars list**
     (`rclone_mounts`): render per item (:361-398), daemon-reload + enable per item
     (:402-415), restart handler (:608+). Secrets kept out of `ExecStart` via
     `EnvironmentFile` (:275, :311-326). This is the closest "one unit per configured
     thing" precedent.
   - `play-vm-test-lab.yml`: instance-templated units `vmtest-bridge@.path`/`.service`
     (:524-538) enabled per instance with `systemd-escape`d slug (:438-445, :548-562),
     `restarted` when the unit files changed (:555).
   - `files/home/.config/systemd/user/ccy-sessions-restore.service:8-19`: user unit
     `WantedBy=default.target`, relying on linger to start at boot.
5. **Linger is owned by a core play.** `playbooks/imports/play-systemd-user-tweaks.yml`
   (imported at `playbooks/playbook-main.yml:15`, before podman at :33) does
   `loginctl enable-linger` (:32-40) **and** starts `user@UID.service` explicitly to close
   the async race (:42-50); its handler does a `scope: user` daemon-reload with
   `XDG_RUNTIME_DIR` and `DBUS_SESSION_BUS_ADDRESS` (:248-257). `play-rclone.yml:171-176`
   and `play-vm-test-lab.yml:101-107` repeat the `enable-linger` for standalone runs. An
   optional homeserver play should depend on the core play having run and fail loud if the
   user manager is unreachable (the `play-podman.yml:24-46` reasoning), not add a third copy
   of the linger task.
6. **Firewalld handling exists only for LXC (and a VPN rule).**
   `playbooks/imports/play-lxc-install-config.yml`: `firewalld` + `python3-firewall`
   packages (:54-55), wait until firewalld answers, not merely active (:100-132), keep SSH
   allowed in the default zone (:142-192), and **bind `lxcbr0` to the `trusted` zone with
   `ansible.posix.firewalld`** (:194-202). `play-vpn.yml:20-21, :37` uses the same module.
   `ansible.posix.firewalld` is the house style; raw `firewall-cmd` is not.
7. **Rootless podman does not touch host firewalling.** `playbooks/playbook-main.yml:31` and
   `play-lxc-install-config.yml:17` say so: podman uses slirp4netns/pasta and no host
   iptables. A rootless podman bridge (including the default `podman` network) lives in
   podman's rootless network namespace, **not** the host's, so there is no host interface
   for a firewalld zone to bind (**VERIFY** with `ip -br link` on the host while a rootless
   bridge container runs).
8. **ccy containers sit on the default rootless `podman` network** unless a project network
   is chosen: `claude-yolo:3009-3014` sets `--network podman`; a saved or `--network`
   choice joins a compose network instead (:2274-2290). So "the host-local bridge address
   ccy members use" is **not** a podman bridge address; it has to be an address in the
   host's own network namespace that pasta/slirp4netns can reach from inside the rootless
   namespace (**VERIFY** per network type, as issue §7 asks).
9. **`/usr/local/lib/ccy-helpers` is host-only.** Despite the name, nothing in
   `files/var/local/claude-yolo/` or `play-claude-yolo.yml` mounts it into a ccy container.
   A CLI that must run inside ccy containers (pingbus) cannot rely on it there.

## 2. Engine choice (`container_engine`)

- `vars/container-defaults.yml:9`: `container_engine: podman`, overridable in host_vars
  (:4-7); valid values `podman`, `docker`.
- `CLAUDE/ContainerEngines.md:28` (variable drives the engine), `:47` (new playbooks use the
  variable, never hardcode), `:48` (if podman does not fit, document why). Docker is
  rootful compatibility only (:50-87).
- Loading pattern: `include_vars` of the file then `which {{ container_engine }}` and a
  reachability check (`play-claude-devtools.yml:15-40`, `play-claude-yolo.yml:46-95`).
- **Recommendation:** Quadlet is podman-only, and the bus's privacy model needs rootless.
  The play should `include_vars` the defaults and **assert** `container_engine == 'podman'`
  with a fail message saying why (rootless + Quadlet; Docker is root-equivalent and would
  put the homeserver under a root daemon). That honours the variable rule (no hardcoded
  engine choice) while failing fast instead of half-supporting Docker.

## 3. CLI into `~/.local/bin` and Python helpers

- Pattern A (host-only tool, deployed library), `play-container-watch.yml`:
  - helper modules copied by an **explicit list** (never a glob, Plan 00055 D8) into
    `/usr/local/lib/ccy-helpers/helpers/<pkg>/` (:30-57);
  - a thin bash wrapper `files/home/.local/bin/container-watch` copied to
    `~/.local/bin` mode 0755 (:62-76); the wrapper is
    `exec env PYTHONPATH=/usr/local/lib/ccy-helpers python3 -m helpers.containerwatch.cli "$@"`
    (`files/home/.local/bin/container-watch:21`). Same shape in
    `files/usr/local/bin/github-ssh-443:25`, `files/home/.local/bin/vmtest:98`.
- Pattern B (runs from the checkout): `PYTHONPATH="{{ root_dir }}" /usr/bin/python3 -P -m helpers.host_health...` (`files/home/bashrc-includes/host-health-report.bash.j2:37-48`,
  `files/home/.local/bin/fedora-desktop-health.j2:145-168`), with the reasons for `-P` and
  for not exporting `PYTHONPATH` in the comments there.
- Pattern C (play-time helper): `command:` + `argv:` + `chdir: "{{ root_dir }}"`, marker
  lines on stdout (`helpers/CLAUDE.md:39-67`; live example
  `play-lxc-install-config.yml:172-183`).
- Helper rules: stdlib only, namespace packages without `__init__.py`, pure logic split
  from a thin executor, test first under `tests/helpers/<pkg>/test_*.py`, run with
  `scripts/qa-helper-tests.bash` (`helpers/CLAUDE.md:23-89`).
- **Fit for this plan:** `agent-team` (host-only provisioning) fits Pattern A. `pingbus`
  runs on the host, inside ccy containers and in containers ccy does not manage (issue §8:
  "a single file or wheel"), so it should be **one stdlib-only file** with its pure
  validator unit-tested; the host copy goes to `~/.local/bin` and the ccy integration
  mounts or bakes the same file (owned by the ccy-integration research, not this one).

## 4. Closest play to copy

No single play does "one rootless container service per item, bound to a host-local
address". Assemble it from:

| Need                                          | Copy from                                              |
| --------------------------------------------- | ------------------------------------------------------ |
| Optional play shape, header, `scope`          | `play-container-watch.yml:1-26` (opt-in, not in main)  |
| One unit per configured item from host_vars   | `play-rclone.yml:361-415` (+ restart handler :600-620) |
| Instance template units + per-instance enable | `play-vm-test-lab.yml:524-562`                         |
| UID resolve + assert + `scope: user` enable   | `play-container-watch.yml:107-156`                     |
| Linger / user manager up                      | depend on `play-systemd-user-tweaks.yml:23-50` (core)  |
| Engine variable                               | `play-claude-devtools.yml:15-40`                       |
| Firewalld module, readiness wait              | `play-lxc-install-config.yml:54-55, 100-132, 194-202`  |
| Helper + `~/.local/bin` wrapper               | `play-container-watch.yml:30-76`                       |
| Secrets out of unit text                      | `play-rclone.yml:275-326` (`EnvironmentFile`, 0600)    |

## 5. Recommended layout for the homeserver play

`playbooks/imports/optional/common/play-agent-team-homeserver.yml` (optional; not imported
by `playbook-main.yml`, like `play-container-watch.yml:17-19`).

1. `include_vars` `vars/container-defaults.yml`; assert `container_engine == 'podman'`.
2. Resolve and assert the user's UID (container-watch :107-128); no linger task of its own.
3. Pin the image by digest in a play var with a `@see` to the Tuwunel tag page (the repo's
   pinned-version convention, see the `update-versions` skill), never `:latest` (contrast
   `play-unifi-controller.yml:12`).
4. Per team, from an untracked list (`agent_teams` in
   `environment/localhost/host_vars/localhost.yml`, which `.gitignore:1` keeps out of git;
   add a commented placeholder to `localhost.yml.dist`), or from the untracked team registry
   the issue proposes; see decision D1 below. For each team:
   - data dir `~/.local/share/agent-teams/<team>/` (0700) and config dir
     `~/.config/agent-teams/<team>/`;
   - Tuwunel config rendered from a template (`allow_federation = false`,
     `trusted_servers = []`, token-only registration, port 8008 inside);
   - registration token generated once on the host (`creates:`), mode 0600, passed with
     `EnvironmentFile=`/`Secret=`, never in the unit text or task output (`no_log: true`);
   - a Quadlet file `~/.config/containers/systemd/agent-team-<team>.container` with
     `Image=`, `Volume=…:Z`, `PublishPort=<bind-addr>:<port>:8008` (an explicit address,
     never a bare port), `EnvironmentFile=`, `AutoUpdate=` unset, and
     `[Install] WantedBy=default.target`;
   - the warden as a second Quadlet or user unit per team (`agent-team-<team>-warden`),
     `Requires=`/`After=` the homeserver's generated `.service`.
     An alternative to per-team files is one template `agent-team@.container` with
     `%i`-derived paths (vm-test-lab's `@` pattern). **VERIFY** the host's podman supports
     Quadlet templates before choosing that.
5. `systemd` `daemon_reload: true`, `scope: user`, `XDG_RUNTIME_DIR` env (Quadlet generates
   the `.service` on reload), then start each `agent-team-<team>.service`; `restarted` when
   its Quadlet or config changed (vm-test-lab :555). Quadlet units are generated, so they
   are started, not `enabled`; `[Install]` handles boot.
6. Assert, do not assume: probe the published socket is on the bind address only
   (`ss -ltnH` as an assert), and fail if it is listening on `0.0.0.0`/`::`.
7. Deploy `agent-team` and `pingbus` per section 3; the plan's `acceptance.bash` owns the
   privacy checks (issue "Privacy: acceptance checks").

## 6. Bind address and firewall: open decisions for DESIGN.md

- **D1. Where team definitions live.** The issue has `agent-team create` writing an
  untracked registry and "bringing up the homeserver". The repo's IaC rule says system
  changes go through Ansible. Two consistent shapes: (a) teams are a host_vars list and the
  play creates the units (rclone precedent), `agent-team` only does accounts and
  credentials; or (b) the play installs a generic template once, and `agent-team create`
  instantiates it for a team (user-level runtime state, like ccy's own per-project state).
  (a) fits the IaC rule more plainly; (b) fits the issue's one-command UX. Decide in
  DESIGN.md.
- **D2. Which host address.** It must exist in the host's network namespace before the
  unit starts, or `PublishPort=<addr>:…` fails. Candidates:
  - the `lxcbr0` address: exists only after `lxc-net` (a system unit a user unit cannot
    order against) and is in the `trusted` zone (`play-lxc-install-config.yml:196-202`);
    reachable by LXC members, but `trusted` accepts everything from that bridge;
  - a dedicated dummy interface with a fixed address (for example a reserved range chosen
    in DESIGN.md) created by a system-level task, bound to its own firewalld zone that
    allows only the team ports. Nothing like it exists yet;
  - a loopback address: unreachable off-host by construction, but reachability from a
    rootless container depends on pasta's host-loopback mapping (**VERIFY**).
- **D3. Not reachable from the network.** Binding to a non-LAN address is not enough on its
  own: Linux accepts a packet for any local address on any interface (weak host model), so a
  LAN peer that routes the bind address via this machine reaches it if the LAN-facing zone
  allows the port. Fedora Workstation's default zone allows a high port range (**VERIFY**
  `firewall-cmd --get-default-zone` and `--list-ports` on the host). The acceptance check
  must send from another machine with a route added, not just inspect bind addresses.
  Note that traffic from ccy members arrives via pasta/slirp4netns as host-local
  connections, so a zone on a bridge does not filter it; the zone matters for LXC/Docker
  members arriving on their bridge.
- **D4. No outbound traffic.** A rootless container on the default network has full egress
  via pasta and no host firewall rule sees it as forwarded traffic. Options: run the
  homeserver on an `--internal` podman network (no egress, but then publishing to the host
  needs checking) or `Network=none` plus a socket-activated proxy; **VERIFY** which keeps
  `PublishPort` working. Prove it either way with a capture, as the issue requires.

## 7. Probes for `triage.bash` (read-only)

`podman --version`; `podman info --format '{{.Host.NetworkBackend}} {{.Host.Pasta.Executable}}'`;
`ls /usr/libexec/podman/quadlet`; `/usr/libexec/podman/quadlet -dryrun -user` against a
sample file; `loginctl show-user "$USER" -p Linger`; `firewall-cmd --get-default-zone`,
`--get-active-zones`, `--zone=<default> --list-all`; `ip -br addr`; `sysctl net.ipv4.ip_forward`; from a ccy container on `podman` and on a compose network, a TCP
connect to each candidate bind address.
