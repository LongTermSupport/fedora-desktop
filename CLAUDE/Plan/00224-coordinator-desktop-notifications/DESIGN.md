# Plan 00224 design: coordinator desktop notifications

Placeholders only: `<session>` a ccy session's registry name, `<container>` an LXC container,
`<user>` the agent user in it, `<team>` a team, `<handle>` a member handle, `<uid>` a numeric
user id. Nothing here names an install.

## 1. What exists today

| Piece                                                                                                                      | What it gives this plan                                                                                                                                                                                                                                                                                                                                                      |
| -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CLAUDE/AgentNotes.md`, the `notify-send` standing rule                                                                    | A session on the desktop itself may run `notify-send`; a container session cannot (no session bus). Nothing wraps it: no limit, no sanitising.                                                                                                                                                                                                                               |
| `files/home/.config/systemd/user/stt-watch-failed.service`                                                                 | The repo's one deployed `notify-send` caller: a user unit, so the owner's session bus is in its environment.                                                                                                                                                                                                                                                                 |
| `.claude/hooks/notification`                                                                                               | Claude Code's `Notification` hook, forwarded to the hooks daemon. It shows nothing on the desktop and is not reused.                                                                                                                                                                                                                                                         |
| ccy membership (`lib/agent-bus-membership.bash`, `ccy-teams internal membership-init`, `src-go/internal/teams/membership`) | The precedent this design copies: a per-session directory under `$XDG_RUNTIME_DIR/ccy/membership/<session>/` that only the owner's tools write, mounted read-only into the container at `/run/ccy/membership`, relabelled per session on SELinux. Its `membership.json` (`Desired`: session, kind, checkout, seats) is the host's own record of which seats a session holds. |
| `sudo agent-bus list <team>` (`files/etc/sudoers.d/agent-bus-read-only.j2`)                                                | The owner's user may already read a team's members and roles without a password; `src-go/internal/agentbus` parses it (`ParseRoster`, `Roster.Member`, `Roster.Coordinator`). This is the host's authoritative role source (the registry under `/var/lib/agent-bus/<team>/`).                                                                                                |
| `src-go/internal/teams/lxcrecord`                                                                                          | The applied record of each team's LXC members (container, user, handle), written by `play-agent-bus.yml`.                                                                                                                                                                                                                                                                    |
| `lib/ssh-handling.bash` (the forwarded ssh agent)                                                                          | A measured fact: `container_t` may not connect to a Unix socket served by an unconfined host process, and relabelling the socket file does not help, so a forwarded socket costs the whole container its SELinux labelling (`label=disable`).                                                                                                                                |
| The member kit (`/usr/local/share/agent-bus/kit/`, `/opt/claude-yolo/optional/agent-bus/` in the image)                    | How a Go binary reaches LXC members and ccy containers: `pingbus-status-line` is built by `tasks/build-go-command.yml` into both, from `play-agent-bus.yml` and `play-claude-yolo.yml`.                                                                                                                                                                                      |
| Plan 00219 (desktop checks over D-Bus)                                                                                     | Host-agent only; its lesson reused here is the refusal pattern: the dangerous path is refused by construction, not by trusting the caller.                                                                                                                                                                                                                                   |
| Plan 00220 / Plan 00209 (remote members, bus v2 `teamchat`)                                                                | A remote member shares no filesystem with the desktop; its only path is the bus. v2's host units run with no D-Bus (00209 DESIGN-v2-C-host.md C5), so the bus side can only hand a notice to something in the owner's session.                                                                                                                                               |

## 2. Why the session bus is never passed into a container

The owner's session D-Bus is the control plane of the whole desktop session, not a
notification pipe. A process connected to it as the owner can, among other things:

- ask the user systemd manager (`org.freedesktop.systemd1`) to start a transient unit, which
  runs any command on the host as the owner: a full escape from the container;
- read every stored secret through `org.freedesktop.secrets` (the GNOME keyring), browser and
  account passwords included;
- write any GSettings/dconf key, start any activatable service, open URIs and files through
  portals, lock or unlock the screen, and drive whatever else a session service exports.

The bus authenticates by uid, and a rootless podman container already runs as the owner's
uid, so a mounted bus socket would be accepted as the owner. Passing it in would make the
container boundary decorative. A filtering proxy (`xdg-dbus-proxy`, allowing only
`org.freedesktop.Notifications`) narrows the bus but was rejected as well: the proxy is an
unconfined socket, so SELinux denies the connection (the ssh-agent measurement) unless the
container's labelling is turned off; it knows nothing of team roles, so the coordinator gate
and the rate limit would still need a relay; the Notifications interface still accepts
actions, hints and `image-path` (a host file path the shell then reads); and it reaches
neither LXC members uniformly nor remote members at all.

## 3. The design: a file spool and a host relay

```
container (ccy or LXC)                        host, the owner's session
──────────────────────                        ─────────────────────────
notify-owner "title" "body"                   notify-owner relay  (user unit)
  writes <id>.tmp, renames to <id>.req  ───►    inotify on each spool directory
  in its spool directory                        identity = which directory (never file content)
                                                validate + sanitise
  waits for <id>.result  ◄───────────────       gate: is that identity a coordinator NOW?
  exit 0 / 65 / 75 / 77 / 69                    rate limit
                                                notify-send (argv, no shell)
                                                write <id>.result, delete <id>.req
```

A spool of files, not a socket, because files cross the SELinux boundary with the per-session
relabel the membership mount already uses, and a socket does not (section 1). The relay reads
container-labelled files as the unconfined owner; the container never connects to anything on
the host.

### 3.1 One command everywhere: `notify-owner`

`notify-owner [--urgency low|normal] [--team <team>] <title> [<body>]` is the same Go binary on
the host, in the ccy image and in the LXC kit. It picks its route itself:

1. `NOTIFY_OWNER_SPOOL` set (the ccy launcher and the LXC env file set it): the spool route. The
   directory must exist and be a directory, or exit 69 naming it.
2. Unset, on the host (no `/run/.containerenv`, no `/run/ccy`), with `DBUS_SESSION_BUS_ADDRESS`
   set: the direct route. The same validation, sanitising and the host limiter (section 3.5,
   identity `host`), then `notify-send`. No coordinator gate: the owner's standing rule already
   lets a desktop session notify (decision D2).
3. Anything else: exit 69, "no route to the owner's desktop from here", with the reason.

Exit codes: 0 shown; 64 usage; 65 invalid content (named); 69 no route, or no answer from the
relay within 10 s (the relay is not running, or the owner is not logged in); 75 rate limited
(with when the next one is allowed); 77 refused, not the coordinator (naming the team's
coordinator, or that it has none).

### 3.2 Spool directories and identity

| Member kind | Host directory                              | Seen inside as                              | Identity the relay derives                                                                                              |
| ----------- | ------------------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| podman ccy  | `$XDG_RUNTIME_DIR/ccy/notify/<session>/`    | `/run/ccy/notify` (rw, per-session relabel) | `<session>` → `$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json` → its seats → handles                         |
| LXC         | `/run/agent-notify/lxc/<container>/<user>/` | `/run/agent-notify/<user>`                  | (`<container>`, `<user>`) → the team's `lxcrecord` item → handle; the request file's owner must be the member's `<uid>` |
| remote      | none (section 5)                            | -                                           | -                                                                                                                       |

The directory is created by host tooling, never by the container, and its path is the
identity. Nothing the container writes into a request (no `from`, no handle, no team other
than a choice among teams it coordinates) can change who the relay thinks is asking. The
membership file is on a read-only mount inside the container and is written only by the
owner's tools, so the container cannot change its seats either.

A ccy session gets its spool directory whenever it carries the bus kit, seated or not, because
a role can be given while the session runs (hot control, Plan 00203); a request from a session
with no coordinator seat is simply refused (decision D6).

**LXC.** The containers this repository admits are privileged (an `lxc.idmap` container is
refused, `docs/agent-bus-lxc-members.md`), so the agent user's numeric uid is the same on the
host. Each member directory is `0700 <uid>` with a POSIX ACL granting the owner's user `rwx`,
created at boot by a `tmpfiles.d` file the play renders (`/run` is a tmpfs), and bind-mounted
by one `lxc.mount.entry` per container (`/run/agent-notify/lxc/<container>` at
`run/agent-notify`), which takes effect at the container's next start (decision D7). Another
user in the container cannot write a member's directory. The container's root can, but root in
a privileged container is root on the host already; this plan does not pretend otherwise.

### 3.3 The request and its checks

The request is one JSON object, at most 4 KiB:
`{"v":1,"id":"<16 hex>","title":"…","body":"…","urgency":"normal","team":"<team>"}` (`body`
and `team` optional). The client and the relay run the same checks from one Go package, so a
refusal names the field in both places:

- valid UTF-8; title 1-80 runes, body at most 400 runes and 6 lines (decision D5);
- no control characters (C0 and C1, except newline in the body), no bidirectional overrides
  (U+202A-U+202E, U+2066-U+2069) and no zero-width joiners used as spoofing (refused, not
  stripped: fail fast);
- urgency `low` or `normal` (decision D4);
- unknown keys refused.

Before display the relay escapes `&`, `<` and `>` (GNOME Shell renders a small markup subset in
the body) and prefixes the summary with the team, so a coordinator cannot pose as another team
or as the system: summary `<team>: <title>`, app name `Agent team coordinator` (decision D9). No
icon, actions, hints, sound or expiry come from the agent. `notify-send` runs by argument list
with `--` before the positional arguments.

**File safety.** The relay opens each `*.req` with `openat(dirfd, O_RDONLY|O_NOFOLLOW|O_NONBLOCK)`,
checks with `fstat` that it is a regular file with one link and within the size cap, and
refuses symlinks, FIFOs, devices and hard links. It writes `<id>.result` with
`openat(O_CREAT|O_EXCL|O_NOFOLLOW)`; an existing name is a refusal, never an overwrite. It keeps
at most 16 entries per directory: anything beyond, and any entry older than 60 s, is deleted.
Writing the directory full only fills a tmpfs the container could fill through its own mounts
anyway.

### 3.4 The gate: the coordinator, from the host's records

For each request, after the content checks and the pre-gate counter (section 3.5):

1. Identity from the directory (3.2).
2. The identity's handles, one per team: for ccy, the seats in its `membership.json` resolved to
   handles the way `ccy-teams describe-seats` resolves them; for LXC, the `lxcrecord` item.
3. For each of those teams, `sudo -n agent-bus list <team>` (the existing passwordless rule),
   parsed by `agentbus.ParseRoster`; the handle's role must be `coordinator` and the member not
   parked.
4. The team: the only one the identity coordinates, or the request's `team`, which must be one
   it coordinates. Coordinating none: refused 77, naming each team's coordinator or that it has
   none.

The roster is read per request, never cached across requests, so a role taken away is honoured
at once. The container's claim to be a coordinator is never consulted.

### 3.5 Rate limiting

Two layers, both in the relay, persisted to `$XDG_STATE_HOME/notify-owner/limits.json` so a
relay restart (or a crash loop) does not refill anything:

- **pre-gate**, per directory: at most 30 requests an hour are looked at; beyond that a request
  is deleted with a 75 result and no roster read, so a looping worker cannot make the relay run
  `sudo` in a loop;
- **display**, per identity: a token bucket, 3 at once, one more every 10 minutes; across all
  identities at most 12 shown an hour; the same summary and body from the same identity within
  30 minutes is refused as a duplicate (decision D3).

When an identity is first limited in an hour, the relay shows one notice of its own, `<team>: coordinator notifications paused` with how many were dropped, and no more that hour. Every
decision is logged to the journal (identity, team, outcome, reason); the title and body are not
logged (decision D11).

### 3.6 The relay

`notify-owner relay`, run by `notify-owner-relay.service`, a user unit `PartOf` and `WantedBy`
`graphical-session.target`, so it runs only while the owner has a desktop to show things on and
has the session bus in its environment. It watches `$XDG_RUNTIME_DIR/ccy/notify/` and
`/run/agent-notify/lxc/` with inotify (new directories included), and scans every directory at
start for requests left waiting. It holds no secret and opens no socket.

## 4. Where the code goes

- `src-go/internal/notifyowner/`: request (parse, validate, sanitise), limiter, gate (with the
  roster and membership readers behind interfaces), spool (safe open, result write, pruning),
  client (route choice, wait); unit tests through fakes (`execx` for `sudo` and `notify-send`).
- `src-go/cmd/notify-owner/`: the binary (`notify-owner …` and `notify-owner relay`).
- `play-agent-bus.yml`: build and install `notify-owner` to `/usr/local/bin` and into the kit;
  the user unit; `/run/agent-notify` and the LXC member directories by `tmpfiles.d`; the
  `lxc.mount.entry` and the env file's `NOTIFY_OWNER_SPOOL` for each LXC member. The relay is a
  team feature and its gate reads team state, so it lives with the team play.
- `play-claude-yolo.yml`: build `notify-owner` into the image's `/opt/claude-yolo/optional/agent-bus/`.
- ccy: the launcher passes the spool mount and `NOTIFY_OWNER_SPOOL`; the entrypoint puts
  `notify-owner` on `PATH` with the kit. Both bump versions (CCY and container).

## 5. Remote members (future, Plan 00220 / Plan 00209)

A remote member has no shared directory, so its path is the bus. The future shape, not built
here: v2's host program, which already authenticates every event by its Matrix sender, accepts
a `notify` from a member and writes it into a host-only spool,
`/run/agent-notify/bus/<team>/`, writable only by the v2 host user. The relay treats a request
there as carrying an attested handle (attested by the directory's owner, as 3.2 attests by
path) and applies the same role check, against v2's roster, and the same limits. The relay
needs a role-source interface from the start so v2 is an added implementation, not a rewrite.
The design note goes to Plan 00209's coordinator; this plan builds nothing for it.

## 6. Decisions (coordinator defaults; the owner may change any)

| ID  | Question                       | Default                                                                                                                                   |
| --- | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | The command's name             | `notify-owner` everywhere. Not a `notify-send` shim in containers: half its flags would be refused, which surprises more than a new name. |
| D2  | Host `cc` sessions             | No coordinator gate (the standing rule allows them); the same checks and the host limiter apply.                                          |
| D3  | Limits                         | Burst 3, one per 10 min per member; 12 an hour in all; duplicates within 30 min refused; pre-gate 30 requests an hour per directory.      |
| D4  | Urgency                        | `low` and `normal`; `critical` refused (it stays on screen until dismissed).                                                              |
| D5  | Sizes                          | Title 80 runes; body 400 runes, 6 lines; request file 4 KiB.                                                                              |
| D6  | Which ccy sessions get a spool | Every session carrying the bus kit, so a role given while it runs works without a relaunch.                                               |
| D7  | LXC mount activation           | Takes effect at the container's next start; the play never restarts a container; `ccy-teams lxc-add` and the play say so.                 |
| D8  | No desktop session             | The relay runs only in the graphical session; a request then gets no answer and the client exits 69, saying so.                           |
| D9  | Presentation                   | Summary `<team>: <title>`; app name `Agent team coordinator`; no agent-supplied icon or actions.                                          |
| D10 | Remote members                 | Not built; section 5 handed to Plan 00209.                                                                                                |
| D11 | Logging                        | Identity, team, outcome and reason to the journal; never the title or body.                                                               |
