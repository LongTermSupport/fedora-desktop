# Research: a real boundary between a `cc --teams` session and Element's session folders

Issue #101. Research only; nothing here is built. `docs/research/` does not exist, so this
sits in the Plan 00177 folder.

## The question

A `cc --teams <team>` session is desktop Claude Code running as the owner
(`files/var/local/claude-code/cc`, `cc-teams.bash`). Today the guard is instruction plus
`files/var/local/claude-code/cc-teams-settings.json`, which denies Claude's **file tools**
(`Read(~/.var/app/im.riot.Riot/**)`, `Read(~/.config/Element/**)`, ...). That stops the
Read/Edit tools and nothing else: `cat`, a Python one-liner, a child process, an MCP server
or a hook can still read the folders. What stops those, without touching project settings
(shared with ccy), without hurting network access, and without losing the owner's `cc`
login and settings?

cc already builds a per-session settings file (`$tmp/settings.json`, passed as
`claude --settings`) and launches through `pingbus seat exec ... claude`
(`cc_teams_command`). So anything at the **launch line** or in that **per-session settings
file** is cc's alone and never reaches ccy.

Status of every claim below: "doc" = read in the current official docs this session;
"repo" = read in this repository; "UNVERIFIED" = not checked on a Fedora 44 host (this
research ran in a container with no host to probe, and `/sys/kernel/security/lsm` is not
readable there).

## 1. Claude Code's sandbox (bubblewrap)

Sources: <https://code.claude.com/docs/en/sandboxing>,
<https://code.claude.com/docs/en/settings-reference>.

**What it covers (doc).** "The sandbox covers shell commands only. Claude's file tools,
MCP servers, and hooks run outside it." Bash and the processes it starts are inside, so
`cat`, interpreters and children are covered. The file tools are not: "A `denyRead` entry
doesn't stop the Read tool", they "follow permission rules instead". That is fine here: the
`permissions.deny` rules already in `cc-teams-settings.json` cover the Read/Edit tools, so
the two together cover the file tools and Bash. MCP servers, hooks, LSP servers and
`apiKeyHelper` "run with your full access", a gap for anything the owner configures (the
kit's plugin hooks run unsandboxed; they are cc's own and do not read Element).

**Keys (doc).** `sandbox.enabled`; `sandbox.filesystem.denyRead` (paths use `~/` and
absolute `/`, unlike permission rules); `sandbox.filesystem.allowRead`;
`sandbox.filesystem.allowWrite`; `sandbox.allowUnsandboxedCommands`;
`sandbox.failIfUnavailable`; `sandbox.excludedCommands`; `sandbox.network.allowedDomains`,
`deniedDomains`, `strictAllowlist`. "To change the sandbox for one session without writing
to a settings file, start Claude Code with `--settings`", with the example
`claude --settings '{"sandbox": {"enabled": true, "allowUnsandboxedCommands": false}}'`.
So the whole thing can live in cc's per-session settings. Two safety keys are needed or
the boundary leaks: `allowUnsandboxedCommands: false` ("Claude Code ignores the
`dangerouslyDisableSandbox` parameter" then), and `failIfUnavailable: true` ("By default,
if the sandbox can't start because a dependency is missing ... Claude Code runs commands
without sandboxing").

**Network: can it be opened fully while the filesystem part stays? Not cleanly.**

- The network layer cannot be turned off while the filesystem layer stays. The only switch
  is the other way round: `sandbox.filesystem.disabled: true` "skip[s] filesystem
  isolation while keeping network isolation", and then `denyRead` is "Not enforced".
- With the sandbox on, on Linux "the command runs in a separate network namespace that has
  no connection to your network"; connections go "through a proxy ... that checks each
  host against your allowed domains, which start empty".
- Opening it for HTTP(S): a bare `*` in a `WebFetch(domain:*)` allow rule is honoured for
  sandboxed commands (Claude Code v2.1.186 or later; doc: "the sandbox honors two wildcard
  forms: a leading `*.` ... and a bare `*`"). Separately, in `bypassPermissions` mode a
  host outside the allowlist is "Allowed without a prompt" unless `strictAllowlist` is on.
  I found no documented `allowedDomains: ["*"]`; the `WebFetch(domain:*)` route is the
  documented one. Note it would also let WebFetch fetch anything.
- What stays broken however open the list is (doc): "Tools that ignore the proxy variables:
  plain `ssh`, most database drivers, and similar tools can't connect, even to an allowed
  host"; "Anything that isn't TCP: UDP, HTTP/3 over QUIC, and ICMP tools such as `ping`
  can't leave the sandbox". The fix the docs give for such tools is `excludedCommands`,
  which runs them **unsandboxed** with full access (matched on the command text; a script
  that calls the tool inside does not match).
- Hostnames resolving to this machine are refused (doc: "refuses the connection when the
  name resolves only to local addresses ... addresses assigned to your own machine"); an IP
  must be listed, e.g. `"127.0.0.1:8080"`. The agent bus homeserver listens on 127.0.0.1
  and the machine's bus address (`docs/agent-bus.md`, `listen`), so a pingbus call from a
  sandboxed Bash would need its address listed and pingbus to honour the proxy variables.
  Whether pingbus's HTTP client honours `HTTP_PROXY` is UNVERIFIED; if it does not, it
  needs an `excludedCommands` entry. The kit's hooks run unsandboxed and are unaffected.

**Default write limits (doc).** Sandboxed commands write only to the working directory,
`--add-dir` directories and the per-user temp directory; everything else needs
`sandbox.filesystem.allowWrite` (`~/.cache`, `~/.npm`, `~/.cargo`, ...). That is a real
change to normal work in a session that today can write anywhere.

**Linux requirements (doc).** `bubblewrap` and `socat` ("`sudo dnf install bubblewrap socat`"); an optional seccomp filter (`npm install -g @anthropic-ai/sandbox-runtime`) "adds
Unix domain socket blocking". Not in this repository's playbooks today (grep for
`bubblewrap`/`socat` finds nothing; `flatpak` Requires bubblewrap on Fedora, UNVERIFIED
here, and the Element play installs the Element flatpak). Fedora 44 and Wayland: bubblewrap
is the same tool flatpak uses on this desktop, so unprivileged user namespaces are expected
to work (UNVERIFIED on the host); the sandbox is headless and does not touch the display.

**Residual hole (UNVERIFIED).** Without the seccomp socket filter, a sandboxed command may
still reach the owner's session D-Bus socket. Through it, `systemd-run --user` starts a
process in the user manager, outside the sandbox, with the owner's full access. Whether the
sandbox hides that socket is not stated in the docs read; it has to be probed.

**Verdict.** Strong for Bash, children and interpreters, plus the file tools via the
existing deny rules. Cost: a network namespace for every command (non-HTTP tools break,
pingbus and local services need care) and a write allow-list. Doc-supported, cc-local, but
the heaviest change to normal work of the options that actually work.

## 2. `systemd-run --user` with `InaccessiblePaths=` / `TemporaryFileSystem=`

Source: `systemd.exec(5)` (man.archlinux.org, checked 26-10-13). It confirms the central
claim: in a per-user service, the sandboxing options such as `InaccessiblePaths=` are
available because they imply `PrivateUsers=`, which needs unprivileged user namespaces.
That this desktop's user manager actually applies them is still UNVERIFIED until it is
probed on the host. Repo precedent:
`files/var/local/claude-yolo/lib/tmux-session.bash` already starts ccy's tmux with
`systemd-run --user --scope --quiet --collect --unit ...` (repo), so `systemd-run --user`
is known to work on the owner's desktop.

- A **scope** cannot carry the mount settings: scopes take resource-control properties
  only, so it has to be a **service**: `systemd-run --user --pty --wait --collect --same-dir -p InaccessiblePaths=-%h/.var/app/im.riot.Riot -p InaccessiblePaths=-%h/.config/Element -p InaccessiblePaths=-%h/.config/Element-... -- <the launch line>`. The leading `-` makes a missing path fine; globs are not accepted, so
  the `~/.config/Element-*` profiles that `cc-teams-settings.json` lists must be expanded
  by cc at launch.
- Unprivileged: a user manager can apply mount-namespace options because systemd sets up an
  unprivileged user namespace for them (and implies `NoNewPrivileges`). Needs a kernel with
  unprivileged user namespaces, which is the Fedora default (`user.max_user_namespaces` is
  large in the container probed; the host is UNVERIFIED), and a systemd new enough
  (Fedora 44's is). No root, no playbook-installed daemon.
- `InaccessiblePaths=` overmounts the path with an inaccessible node, in the namespace of
  that service and every process in it. It therefore covers file tools, Bash, children,
  interpreters, MCP servers and hooks alike. `flatpak run` from inside also sees the
  inaccessible node, not the data.
- Network, display, `~/.claude`, `~/.claude.json`, settings, project settings: unchanged.
  Wayland/GNOME: unaffected (the sockets are not hidden).
- Costs: `NoNewPrivileges` means no `sudo`/`su`/setuid helpers inside the session (the
  `sudo agent-bus add-member` happens in cc **before** the launch, so it is unaffected;
  the session itself should not need sudo). The service runs from the user manager, so the
  environment is the manager's, not the shell's: cc must pass what claude needs with
  `--setenv`/`-E` (`CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CONFIG_DIR`, `PATH`, `PINGBUS_*`,
  `TERM`), and `--pty` plus `--wait` keep the TUI and the exit status. cc's EXIT trap
  (credential restore) still runs because cc stays the parent.
- The same residual hole as section 1: the session's own `systemd-run --user` over the
  session bus starts an unrestricted service. Closing it means also hiding the bus
  (`InaccessiblePaths=-%t/bus`), which costs D-Bus clients inside the session (desktop
  notifications, `flatpak-spawn`, keyring); Claude Code on Linux keeps its login in
  `~/.claude/.credentials.json` (repo: `cc`), so it does not need the bus. Whether to hide
  it is a trade-off to put to the owner (below).

**Verdict.** The strongest boundary per unit of cost: process-level, covers everything the
session starts, no network change, no new login. UNVERIFIED parts are small and testable
(see the acceptance probe).

## 3. Landlock

- Kernel feature, unprivileged, no-new-privs; Fedora's kernel is expected to build it in
  (UNVERIFIED: no `/boot/config-*` or readable LSM list in the container; the host check is
  `cat /sys/kernel/security/lsm` showing `landlock`).
- Landlock is **allow-list only**: access is granted to a hierarchy and a child of a
  granted tree cannot be carved out again. Denying two folders inside `~` means granting
  every other entry of `~`, `~/.config` and `~/.var/app` individually, enumerated at
  launch. A directory created after launch (`~/.config/newtool`) is then denied, which
  gets in the way of normal work, the opposite of the brief.
- `landrun` is not in this repository's playbooks, and I could not confirm a Fedora 44
  package (UNVERIFIED); a Python wrapper over `landlock_*` syscalls would be new, security
  sensitive code in this repo.
- Network rules exist (TCP bind/connect by port, newer ABIs) but are optional; leaving them
  unset keeps the network untouched. Connecting to a path Unix socket (the D-Bus hole)
  depends on the ABI (a newer filesystem right for it is UNVERIFIED), so it has no clear
  advantage over section 2 there.

**Verdict.** Same strength as option 2 for these two folders, with worse ergonomics and
new code to own. Not recommended.

## 4. A dedicated agent user

Repo: `docs/agent-bus.md` "The dedicated agent user" and the cc comment in `cc-teams.bash`
("It runs as the owner, with no dedicated agent user, by the owner's decision").

- Strongest boundary there is (a different uid: the owner's home and Element are simply
  unreadable) and it is the model the docs already prescribe for every other bare-host
  agent.
- Cost: not the owner's session any more. A separate `~/.claude` login or OAuth token, its
  own settings, plugins and hooks daemon, its own `gh`/git/ssh identity, the checkout
  needing group or ACL write access for a user that is not its owner (and git
  `safe.directory`), and `sudo -iu` for every start. That removes what a cc session is for
  (the owner's login, settings and checkout), and the owner already decided against it.
- The docs state "Creating the user is the install's own IaC; this repository does not
  build it", so this would also be new IaC.

**Verdict.** Right when the owner wants a hard wall and will accept a second identity;
wrong as the answer to "cc as me, but not Element".

## 5. Moving Element's profile out of reach

- A different `HOME`, a symlink, or a different directory does not help: it is the same
  uid, so any path it can name it can read. This is concealment, not a boundary.
- A flatpak override changes what Element can reach, not what other owner processes can.
  Flatpak's data directory is `~/.var/app/<app-id>`; moving it is not a supported
  setting (UNVERIFIED beyond that), and `agent-bus-claude` and pingbus look at exactly
  these paths (repo: `ELEMENT_FLATPAK_DIR`, `ELEMENT_NATIVE_*`).
- The only version that is a boundary is to keep the profile where the session's mount
  namespace never sees it: a mount namespace for **Element's** launcher exposing the
  profile, and the rest of the desktop (including cc) never mounting it. That is option 2
  inverted, needs a launcher wrapper for a GNOME-launched flatpak (a `.desktop` override),
  and breaks as soon as anything else starts Element.

**Verdict.** Not feasible as a standalone boundary.

## Comparison

| Option               | File tools | Bash / children | Interpreters / MCP / hooks      | Network cost                                       | Login/settings            | IaC weight               | F44 / Wayland                          |
| -------------------- | ---------- | --------------- | ------------------------------- | -------------------------------------------------- | ------------------------- | ------------------------ | -------------------------------------- |
| 1 Claude sandbox     | via deny   | yes             | Bash-launched yes; MCP/hooks no | netns + proxy: non-HTTP, local IPs, UDP/ICMP break | unchanged                 | packages + settings JSON | bwrap; UNVERIFIED                      |
| 2 systemd-run --user | yes        | yes             | all                             | none                                               | unchanged (env passed on) | launch line + 0 packages | systemd-run exists; options UNVERIFIED |
| 3 Landlock           | yes        | yes             | all                             | none (optional rules)                              | unchanged                 | new wrapper code         | UNVERIFIED                             |
| 4 Agent user         | yes        | yes             | all                             | none                                               | lost                      | user, ACLs, tokens       | yes                                    |
| 5 Move profile       | no         | no              | no                              | none                                               | unchanged                 | n/a                      | n/a                                    |

## Recommendation

**Option 2: launch the cc `--teams` session as a transient user service with
`InaccessiblePaths=` for Element's folders**, plus the file-tool deny rules that exist today.

Why: it is a real boundary at the mount-namespace level, so file tools, Bash, children,
interpreters, MCP servers and hooks are all covered by one mechanism; it does not touch the
network, project settings, or the owner's login and checkout; it needs no new packages and
reuses a pattern the repo already ships (`systemd-run --user` in the ccy tmux launcher); it
is entirely inside cc's launch line, so ccy is untouched.

Trade-offs to put to the owner:

1. **`NoNewPrivileges`**: no `sudo` inside the session. Acceptable (seat creation is done
   by cc before launch) but a change.
2. **The session bus escape**: a session that runs `systemd-run --user` itself leaves the
   restriction. Close it by also hiding `%t/bus` (costs D-Bus clients in the session), or
   accept it as the residual of "instruction plus wall". Option 1 has the same hole unless
   the optional seccomp filter is installed.
3. **Environment handoff**: cc must pass the right environment to the service; a missing
   variable is a visible failure, not a silent one (it should fail fast).
4. **Unverified until probed**: that a user manager applies `InaccessiblePaths=` on this
   desktop unprivileged, and that `flatpak run` then sees an inaccessible node.

Fall back to option 1 only if the probe fails, and then accept its network costs knowingly
(`WebFetch(domain:*)` for HTTP, `excludedCommands` for pingbus, an `allowWrite` list).

## Sketch of the IaC change (not built)

1. `files/var/local/claude-code/cc-teams.bash`: a `cc_teams_confine` step that wraps
   `CC_TEAMS_CMD` as
   `systemd-run --user --pty --wait --collect --quiet --same-dir --unit cc-teams-$$ \ -p InaccessiblePaths=-<each existing Element path> --setenv=... -- <CC_TEAMS_CMD>`.
   The paths are the same list `cc-teams-settings.json` carries; derive both from one
   place (DRY), expanding `~/.config/Element-*` at launch. Fail fast (status 78, message to
   stderr) when `systemd-run` is missing, or when a probe that runs `test ! -r` on an
   existing Element folder inside the same property set fails: a launch that cannot confine
   must not start unconfined. Keep stdout empty (StderrHygiene).
2. `files/var/local/claude-code/cc`: the launch at the end already does not `exec`, so
   the EXIT trap stays; only `"${CC_TEAMS_CMD[@]}"` becomes the wrapped array. The
   `cc_teams_command` environment (`PATH` with the kit first, `PINGBUS_SEATS`) goes in
   with `--setenv`.
3. `playbooks/imports/play-claude-yolo.yml`: no package task is needed for option 2; only
   the existing copy of `cc` and `cc-teams.bash` (already there, `cc-teams.bash` at the
   `claude-code` copy loop) changes. No `vars/` change.
4. Version bump: `cc-teams.bash` is under `files/var/local/claude-code/`, not
   `claude-yolo/`, so the CCY version bump rule does not apply; check the cc file's own
   version convention before editing.
5. Docs: `docs/agent-bus.md` "A cc session" changes from "guidance, not a wall" to the
   wall it is, and drops the "Claude Code's sandbox is not used" sentence, pointing at this
   result.
6. Acceptance (a host run through `CLAUDE/Plan/meta-deploy.bash`, per CLAUDE.md, never a
   hand-given command): a probe that starts the confined launch against a throwaway
   directory standing in for an Element folder and asserts, from a child of the session,
   that `cat`, `python3 -c open(...)`, and `flatpak run --command=ls` all fail, and that
   `curl` to an external host, `ssh -V`, and pingbus still work. The repo's vmtest
   machinery (`docs/vm-acceptance-testing.md`) is the place for the Fedora 44 check.

If option 1 is chosen instead: add `bubblewrap` and `socat` to the package list, add
`sandbox.{enabled,failIfUnavailable,allowUnsandboxedCommands,filesystem.denyRead, filesystem.allowWrite,excludedCommands}` and a `WebFetch(domain:*)` allow to
`cc-teams-settings.json`, and keep the file-tool deny rules.
