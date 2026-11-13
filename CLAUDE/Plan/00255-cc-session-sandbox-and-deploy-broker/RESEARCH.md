# Plan 00255 research: what the seal and the broker build on

Repository facts, read from the tree on the plan's branch. Host facts are not stated here:
they are what [triage.bash](triage.bash) establishes (via [probe-host.bash](probe-host.bash)),
and its report is read before Phase 1 starts. Issue #101 is the question; the owner chose its
options 3 (seal the session) and 4 (a deploy broker instead of blanket sudo).

## 1. How cc starts a session today

`files/var/local/claude-code/cc` (installed by `playbooks/imports/play-claude-yolo.yml`) is a
bash launcher run as the desktop user, with no container:

1. `cc-project-dir` picks the checkout and `cd`s into it.
2. `ccy_tmux_insulate` re-executes cc inside a tmux session (`cc-<project>`) on ccy's tmux
   server, before anything is asked.
3. With `--teams`/`--bus`, `cc-teams.bash` takes the seats and writes a private settings file
   (kit settings merged with `/var/local/claude-code/cc-teams-settings.json`, the deny rules).
4. The token chooser runs; `claude update` runs; the chosen token is exported.
5. The session starts: `/var/local/claude-code/cc-claude "$@"`, or for teams
   `env PINGBUS_MEMBERSHIP=… pingbus seat exec … -- cc-claude --plugin-dir … --settings …`.
   Not `exec`, because an EXIT trap restores a parked Desktop credential.

So the one place a seal fits is step 5: around `cc-claude` (or around `pingbus seat exec`),
inside the tmux pane, after the token is chosen. Everything claude starts (Bash, the hooks
daemon's hook processes, MCP servers, sub-agents) is its child and inherits the seal.

## 2. The deny rules cc --teams passes now

`files/var/local/claude-code/cc-teams-settings.json` denies `Read`/`Edit` on the Flatpak data of
Element (`im.riot.Riot`), Nheko and Lightning, on `~/.config/Element` and `~/.config/Element-*`,
and on `~/.local/share/keyrings`; `Edit` on `~/.config/pingbus`; and Bash commands naming
`secret-tool`, `keyring`, the secrets service, the clients' app ids, `IndexedDB` and
`Local Storage`. These match text on a command line; a program that opens a file without
naming it (an interpreter, a recursive grep from a parent folder) is not stopped. That is the
gap #101 names.

## 3. The Matrix client data the seal hides

`src-go/internal/matrixclients/clients.json` is the list the Go tools read:

| Client    | Flatpak app id                   | Native (non-Flatpak) data        | Uses the keyring |
| --------- | -------------------------------- | -------------------------------- | ---------------- |
| Element   | `im.riot.Riot`                   | `~/.config/Element` (+ profiles) | yes              |
| Nheko     | `im.nheko.Nheko`                 | none listed                      | yes              |
| Lightning | `org.lightning_matrix.Lightning` | none listed                      | yes              |

A Flatpak app's data is `~/.var/app/<app id>`. Nheko's native folders (`~/.config/nheko`,
`~/.local/share/nheko`) are not in the list; triage reports whether they exist here. The seal
reads this same file, so a client added for the agent-bus guards is hidden too.

## 4. How root is reached today

- **Blanket rule.** `playbooks/imports/play-basic-configs.yml`, task "Passwordless Sudo
  (developer desktop grants it, server and consumer remove it)", writes
  `<user> ALL=(ALL) NOPASSWD: ALL` into `/etc/sudoers` inside a marker block. Present on a
  developer desktop, absent on `provisioning_profile == 'server'` or
  `install_audience == 'consumer'`. So there is already a supported state with the rule gone.
- **Scoped rules** in `files/etc/sudoers.d/`: `agent-bus-read-only.j2` (two read-only
  `agent-bus` argument shapes, pinned by regular expressions), `fedora-desktop-self-update.j2`
  (four exact argument lists) and `fedora-desktop-shutdown-with-update.j2`.
- **Go callers of sudo** (package `src-go/internal/sudo`: `Ready` obtains the credential once, by a password on
  stdin or an interactive `sudo -v`; `AsRoot` and `Run` then use `sudo -n`): agentbus,
  consumer, ftpcamera, lxcfreeze, reclaim, systemd, and the ccy-teams packages
  (`teams/board`, `teams/host`, `teams/ops`, `teams/shutdown`, `teams/winddown`,
  `teams/flow`). `agentbus` runs `agent-bus park-member` and `set-role` as root: neither is in
  the read-only drop-in, so those rely on the blanket rule today.
- **Plays.** `ansible.cfg` sets `sudo_flags=-HE`, `transport=local`; every play's shebang goes
  through `run.bash`, which runs `ansible-playbook` as the user with become.

## 5. run.bash's unattended path is the broker's play runner

`run.bash` `headless_play_preflight` (Plan 00137 T2.1) runs one play, `--changed --yes` or
`--optional-only` unattended. It refuses to run as root ("a root caller drops privileges first
(runuser -u <user> -- …)") and needs one thing: a sudo credential that works without a prompt,
either `NOPASSWD:ALL` or `RUN_BASH_SUDO_PASSWORD_FILE`, which "may name /dev/fd/N: a root caller
that opened a root-only password file hands it over as an inherited descriptor".

That is exactly the shape a broker needs, and it already has a user:

## 6. Prior art: the self-update runner (Plan 00137)

`files/usr/local/sbin/fedora-desktop-self-update` plus `helpers/self_update/cycle.py` is a
root-only runner that:

- keeps a **root-owned deploy clone** at `/var/lib/fedora-desktop/deploy`, separate from any
  checkout the user can write;
- accepts only a **signed** release tag or commit (`self-update.allowed_signers`);
- runs each play **as the user** through `runuser -u <user> -- env -i …`, handing the become
  password (root-only `/etc/fedora-desktop/self-update.become`) and the vault password over
  inherited descriptors (`RUN_BASH_SUDO_PASSWORD_FILE=/dev/fd/N`);
- uses the **root-owned system ansible-core**, refusing an ansible search path in the user's
  home (D5);
- takes the one play lock every run shares (`helpers/play_lock/lock.py`, Go side
  `src-go/internal/playlock`);
- runs only plays on an allowlist (`helpers/self_update/unattended-plays.json`);
- is reached by the user through a sudoers drop-in with exact argument lists.

`playbooks/imports/optional/common/play-self-update.yml` also sets `kernel.yama.ptrace_scope=1`
for the same reason the broker needs it: a play run as the user holds the become password, and
with scope 0 any other process of that user could read it from the play's memory.

The broker is this runner made reachable on request, for the desktop, with a different trust
check (section 8). The owner's choice of Go for new tooling (CLAUDE.md, src-go/CLAUDE.md) means
the broker is Go; the self-update cycle stays Python until it is next reworked.

## 7. meta-deploy.bash: the convention the broker turns into a rule

`CLAUDE/Plan/meta-deploy.bash` runs `meta-deploy.list` from the owner's own checkout: plan
folders (triage, deploy, acceptance), `playbooks/…yml [args]`, single plan scripts, and
`changed-plays` (`run.bash --changed --yes`). Nothing checks that what it runs is committed or
pushed; the convention (CLAUDE.md "Host runs go through meta-deploy.bash") is that it is. The
broker makes "committed, and on origin" a check rather than a convention.

## 8. What "a sha on origin" does and does not prove

An agent in a cc session can commit and push (it has the owner's git and gh logins). So "the
sha is on origin" alone means "the agent pushed it". The protected branches (F44, F43,
F44-arm) move only to a commit GitHub CI passed (CLAUDE.md, "QA Mandatory Before Commits"),
which is a quality gate, not a review. What the check buys is an **audit trail in a public
repository**: nothing runs as root that is not in the history, with the commit that asked for
it. It does not stop an agent that writes a harmful play and pushes it. Decision D3 is where the
owner chooses how much more to require.

## 9. Why the seal is not a boundary while blanket sudo stays (owner's point, checked)

- An unprivileged sandbox (bwrap, or a systemd user unit with mount settings) runs in a user
  namespace. A setuid binary owned by an unmapped root does not gain root there, so `sudo`
  inside the seal fails whatever the sudoers rules say. Triage `--active` records this on the
  host.
- But a sealed agent can still get code run **outside** the seal, by whatever unsealed process
  later runs it: the tmux server (cc runs inside tmux: `send-keys`, `new-window`), the systemd
  user manager (its private socket, or `org.freedesktop.systemd1` on the session bus), D-Bus
  services that start programs (`org.freedesktop.Flatpak` for `flatpak-spawn --host`, the
  terminal and shell services), and files the owner's own shells run (`~/.bashrc`,
  `~/.bashrc-includes/`, autostart entries, `~/.config/systemd/user/`). Each of those runs as
  the owner, unsealed, and with blanket sudo that is root.
- So the seal must close those routes (Phase 3), and blanket sudo must go (Phase 4) so that a
  route missed is not also a route to root.

## 10. xdg-dbus-proxy cannot "refuse one name and pass the rest"

xdg-dbus-proxy's filter mode is an allowlist: once `--filter` is given, only names granted with
`--see`, `--talk`, `--own` (and `--call`/`--broadcast` rules) are visible, and there is no deny
form. Without `--filter` it passes everything. So "refuse `org.freedesktop.secrets`, pass the
rest" is not expressible; the proxy must list what the session may talk to. Triage records the
installed proxy's `--help`, and the bus names present, so the list is chosen from fact (D4).
This also fits section 9: the services that start programs must be refused too.

## 11. The dedicated agent user (ruled out as the main path)

`docs/agent-bus.md`, "The dedicated agent user": a bare-host agent runs as its own user
(`sudo -iu <agent-user> agent-bus-claude`), holding no Matrix session and no passwordless sudo.
The owner ruled this out as the main path for cc: agents must keep running desktop-level plays,
which act on the owner's own account and session. It stays as it is for `host` members.

## 12. Things that will break inside a seal, to handle on purpose

- In-session `sudo` (section 4): ccy-teams operations, agent-bus park/role changes, lxcfreeze,
  reclaim. Each is routed through the broker, given a scoped path, or documented as a thing the
  owner does outside cc (Task 2.4).
- ccy-teams uses tmux throughout (`src-go/internal/teams/…`). With the tmux socket hidden
  (section 9), ccy-teams cannot manage sessions from inside a sealed cc (D5).
- gh and git may keep their logins in the keyring (secrets service). Triage records which; if
  so, refusing the secrets service breaks pushes unless the login moves to a file the seal
  allows or a credential path is passed in (D6).
- `~/.ssh`: hiding private keys still allows pushes through the ssh agent socket; the config
  and known hosts must stay readable.
