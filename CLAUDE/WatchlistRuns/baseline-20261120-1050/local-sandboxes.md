# Study: local sandboxes for coding agents

Plan 00258, Task 2.1. Group: **local sandboxes**, five repositories that confine a coding agent on
the developer's own machine, the same ground as ccy.

| Repository               | Clone path (under `untracked/starred-repos/`) | Licence (own file)        | Platform           | Mechanism                                                  |
| ------------------------ | --------------------------------------------- | ------------------------- | ------------------ | ---------------------------------------------------------- |
| nolabs-ai/nono           | `Apache-2.0/nolabs-ai/nono`                   | Apache-2.0 (`LICENSE`)    | Linux, macOS       | Landlock + seccomp user-notify (Linux), Seatbelt (macOS)   |
| webcoyote/sandvault      | `Apache-2.0/webcoyote/sandvault`              | Apache-2.0 (`LICENSE.md`) | macOS only         | Second user account + `sandbox-exec` profile               |
| eugene1g/agent-safehouse | `Apache-2.0/eugene1g/agent-safehouse`         | Apache-2.0 (`LICENSE`)    | macOS only         | `sandbox-exec`, composable deny-first profiles             |
| kstenerud/yoloai         | `MIT/kstenerud/yoloai`                        | MIT (`LICENSE`)           | Linux, macOS       | Containers (Docker/Podman/Kata/gVisor), Tart VMs, Seatbelt |
| jingkaihe/matchlock      | `MIT/jingkaihe/matchlock`                     | MIT (`LICENSE`)           | Linux (KVM), macOS | Firecracker / Virtualization.framework microVMs            |

Method: read-only reading of each clone's README, design docs and the source files cited. Nothing
was built or run. Commit counts and dates come from `git log` in each clone. Where something is a
guess it says so.

**Licence check.** Each repository has exactly one licence file at its root and no `NOTICE` file.
A search for `SPDX-License-Identifier`, GPL/MPL text and copyright headers found no per-file
exceptions in nono, safehouse, yoloai or matchlock. nono's crates declare `license = "Apache-2.0"`
(workspace) in their `Cargo.toml`. sandvault's only differently licensed files are vendored
`helpers/tomli.py` and `helpers/tomli_w.py` (MIT, Taneli Hukkinen); none are proposed for reuse.
All five are **lift** class under [Watchlist.md](../../Watchlist.md): Apache-2.0 code keeps its
licence text and a statement of changes; MIT code keeps its copyright and permission notice.

---

## What ccy is, for comparison

From [docs/ccy.md](../../../docs/ccy.md) "The Security Model" and "Network policy", and Plans
00072, 00118, 00232, 00146 and 00252:

- **Boundary:** a rootless Podman container (asserted, Plan 00072), `container_t` with the
  per-launch relabel on an enforcing host (Plan 00118), no added capabilities, no engine socket.
- **Filesystem:** the project bind-mounted **read-write** at `/workspace`, `.git` included. The
  home directory is not mounted.
- **Network:** an nftables table in the container's own netns, applied **from the host** after
  create and read back, refusing private ranges and the host. The public internet stays open by
  the owner's decision (Plan 00232). The container holds no `NET_ADMIN`.
- **Secrets:** `CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN` are live environment variables in the
  container. SSH keys are mounted read-only, or reached through a one-key agent. Plans 00146 (a
  forwarder behind `ANTHROPIC_BASE_URL`) and 00252 (a host-side L7 egress proxy with credential
  injection) would take them out; both are Not Started.
- **In-agent guards:** the hooks daemon's PreToolUse rules (destructive git, secret file reads,
  `sed`, and so on). They run inside the session, read config from the writable workspace, and
  judge only what the model asks the tool layer to do.
- **Stated threat model:** contain the agent's own mistakes; not a defence against an
  adversarial process, a kernel escape, or exfiltration of the credentials it is given.

---

## nolabs-ai/nono

Studied: baseline at 5b283ca6de8c5a59b120db634545c556ecceb818

### What it is and who it is for

"A complete agent runtime security tool" (`README.md`): a single unprivileged Rust binary that
wraps any agent (`nono run --profile nolabs-ai/claude -- claude`) in a kernel-enforced capability
sandbox, plus a credential-injecting proxy, command policies, signed instruction files, an audit
ledger and rollback snapshots. Aimed at developers running agents on their own machine, and at
CI and Kubernetes. Built by people from the Sigstore project, with a public profile registry.

### Architecture

- **Isolation (Linux):** Landlock for filesystem (ABI v1 to v6), TCP connect/bind (ABI v4), and
  signal and abstract-socket scoping (ABI v6); a static seccomp filter that denies inet socket
  creation and `io_uring_setup` under `--block-net`, closing Landlock's UDP gap
  (`docs/cli/features/networking.mdx`, "Blocking All Network"). A seccomp **user-notification**
  trap on `openat`/`openat2` only, handled by an unsandboxed supervisor (the parent after fork),
  which can approve a path at runtime and inject the opened fd into the child
  (`docs/cli/internals/security-model.mdx`, "Syscall Scope", "Capability Expansion").
  Implementation: `crates/nono/src/sandbox/linux.rs` (7006 lines),
  `crates/nono-cli/src/exec_strategy/supervisor_linux.rs`.
- **Opt-in namespaces:** `--isolate` adds user, net, IPC and UTS namespaces. Kept opt-in on
  purpose: hosts that refuse unprivileged user namespaces must fail rather than silently
  downgrade, and user namespaces widen the kernel attack surface (`security-model.mdx`, "Opt-in
  namespace isolation").
- **Filesystem allow-lists:** JSON profiles built from named groups in
  `crates/nono-cli/data/policy.json`: `deny_credentials` (`~/.ssh`, `~/.aws`, `~/.kube`,
  `~/.docker`, `~/.git-credentials`, `~/.netrc`, `~/.npmrc`, `~/.vault-token`, and others),
  `deny_shell_history`, `deny_shell_configs`, per-runtime read groups (node, go, rust, python...),
  and per-agent groups (`claude_code_linux`). Profiles `extend` each other; inherited grants
  cannot be removed by an extending profile.
- **Command policy:** PATH shims send each invocation of a controlled command to the supervisor,
  which launches it in its own fresh sandbox with its own file, network and credential grants
  ("the `aws` CLI can use a credential, but `curl` can't"). A direct `/bin/rm` is caught as a
  direct-exec bypass of a controlled command (`docs/cli/features/dangerous-command-blocking.mdx`).
  The default `dangerous_commands` groups list `rm`, `dd`, `chmod`, `sudo`, `pip`, `npm`,
  `systemctl`, `dnf` and more.
- **Network egress:** an HTTP proxy inside the supervisor. With domain filtering on, Landlock
  limits the child's `connect()` to the proxy's loopback port only, so the proxy cannot be
  bypassed by IP. Three modes: CONNECT tunnel (host allow-list, TLS untouched), reverse proxy
  (credential injection), upstream proxy chaining. Path rules per host
  (`--allow-domain 'https://github.com/my-org/**'`), with `endpoint_policy` deny/approve/default
  per method and path, for example denying `git-receive-pack` while approving reads
  (`networking.mdx`, "Endpoint Approvals"). A 256-bit per-session proxy token, compared in
  constant time. DNS resolved by the proxy, link-local and cloud metadata always refused after
  resolution. Host groups in `crates/nono-cli/data/network-policy.json` (`llm_apis`,
  `package_registries`, `github`, `sigstore`, ...); the `claude-code` network profile is a union
  of groups. `nono proxy` runs the same proxy standalone for workloads that bring their own
  isolation (containers, microVMs) (`networking.mdx`, "Standalone Proxy").
- **Secret handling:** credentials loaded from the keyring, 1Password, Bitwarden, env or a
  command into the supervisor's memory (`Zeroizing<String>`); the child sees only
  `OPENAI_BASE_URL=http://127.0.0.1:<port>/openai`. Before running any host-side helper
  (`op`, `git`, `xdg-open`) by bare name, nono strips every PATH entry the sandbox could write,
  so the agent cannot plant a binary the unsandboxed supervisor would run (`security-model.mdx`,
  "PATH Sanitization for Host-Side Brokers").
- **Launch:** `nono run [--profile P] -- <agent>`; for Claude Code the docs advise disabling
  Claude's own sandbox so there is one boundary (`docs/cli/clients/claude.mdx`).
- **Per-project policy:** profiles under `~/.config/nono/profiles/`, plus a signed
  `trust-policy.json` in the project root that names files (`CLAUDE.md`, skills) which must carry
  a valid signature before the agent starts (`docs/cli/features/trust.mdx`).
- **Audit:** supervisor-written session events, a Merkle root per session, a global ledger,
  optional DSSE signing of the root, the agent binary's hash bound into metadata. The docs are
  candid that the ledger is local state a host attacker can rewrite, and that events from a
  crashed session may be lost (`security-model.mdx`, "Audit Integrity Limits", "Audit Durability
  Boundary"). NEP-0008 (`neps/0008-session-flight-recorder.md`, draft, unimplemented) proposes
  recording **every denial from every layer with the responsible process**, and whether each
  source was live, so "nothing happened" can be told from "nono could not see it".
- **Rollback:** `--rollback` takes content-addressed SHA-256 snapshots of tracked directories
  before and after, honouring `.gitignore`, with `nono rollback show --diff` and `restore`
  (`docs/cli/features/atomic-rollbacks.mdx`).

### Maturity and activity

1949 commits from 2026-02-09 to 2026-11-20; latest tag v0.82.0; 95 commits in the last month;
lead author 895 commits, three other regular humans, dependabot. Has a NEP process (`neps/`),
governance, security policy, OpenSSF badge. Pre-1.0: "APIs are stabilizing". About 280k lines of
Rust. The most mature project in this group by a distance.

### Threat model against ccy's

nono states its model plainly: a **capability** layer that narrows what a process can reach on a
shared kernel, not a perimeter; for hostile code it recommends nono *inside* a container or VM
(`docs/cli/internals/containers.mdx`, "Using Both Together"; `security-model.mdx`, "Composing with
an outer boundary").

- **nono is stronger** where ccy is flat inside its box: per-tool credential scoping (only `gh` may
  use the GitHub token), credentials never in the agent's address space, egress forced through a
  proxy with per-path rules, signed instruction files, and a recorded audit trail of denials.
  ccy's tokens sit in the environment where any child can read `/proc/self/environ`.
- **ccy is stronger** at the perimeter: a separate mount namespace (nothing of `$HOME` exists),
  PID isolation, a disposable root filesystem, and an SELinux MCS label. nono on the host shares
  the PID namespace and can `stat` anything; its docs list that reconnaissance gap.
- **Same idea, different place:** both hold network policy where the agent cannot change it. nono
  uses Landlock on the child; ccy uses nftables in the container's netns, applied from the host.
- **Contrast with the hooks daemon:** nono's principle "Your policies stay outside the agent's
  control" (`README.md`) is the gap in our in-agent guards. The daemon's config and handlers live
  in the workspace the agent can write, and a guard judges the command the model asked for, not
  the syscall that ran. nono's direct-exec bypass check shows the distance: our rules catch
  `rm -rf` typed in a Bash call, not a script that calls `/usr/bin/rm`.

### Ideas worth adopting

| #   | Idea                                                                                                                                                                                                 | Where it would land                                                                              | Class                                                              |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| N1  | Per-path egress rules with deny/approve/default, e.g. GitHub read allowed, `git-receive-pack` only to named repos                                                                                    | Plan 00252 design input                                                                          | plan (00252 exists)                                                |
| N2  | Proxy session token: the egress proxy refuses any client without a per-session 256-bit token, so another container on the shared bridge cannot borrow it                                             | Plans 00146 and 00252, a design requirement                                                      | plan (add to both)                                                 |
| N3  | Proxy resolves DNS itself and refuses link-local and metadata addresses after resolution (rebinding defence)                                                                                         | Plan 00252                                                                                       | plan                                                               |
| N4  | Host-side helper PATH sanitising: before the host runs a helper by bare name, drop PATH entries the container can write                                                                              | ccy launcher and any host tool that runs in a checkout; see takeaway 1                           | issue (audit which host helpers run by bare name in the workspace) |
| N5  | A record of what the policy refused. Our nftables policy answers "No route to host" and logs nothing; a counter or log of refused destinations per session would show the owner what the agent tried | `ccy-network-policy` (an nftables `log` or `counter` on the reject rule)                         | issue                                                              |
| N6  | "Fail if the host cannot enforce, never downgrade" for optional layers                                                                                                                               | Already ccy practice (Plan 00072's unknown means stop); no action                                | none                                                               |
| N7  | Signed instruction files: verify `CLAUDE.md`, hooks and skills against a signer before launch                                                                                                        | Would need a design; the workspace is the agent's to write                                       | plan, low priority                                                 |
| N8  | Snapshot before an unattended run, show the diff after                                                                                                                                               | ccy already relies on git; a pre-session `git stash create`-style ref would be the cheap version | needs discussion                                                   |

### Code worth lifting

nono is Rust; ccy's tooling is Go, so little transfers as code.

- **Data, not code:** `crates/nono-cli/data/policy.json` group `deny_credentials` (a curated list
  of credential paths) and `crates/nono-cli/data/network-policy.json` host groups (`llm_apis`,
  `package_registries`, `github`, `sigstore`). Apache-2.0: copy with the licence text and a note
  of changes. Useful as the seed list if Plan 00252 ever adds an allow-list mode.
- **Use as a binary, not a lift:** `nono proxy` is exactly the "standalone egress proxy for a
  container" Plan 00252 asks about. Worth recording as an option there: run an upstream,
  maintained proxy on the host instead of writing one. Its reverse-proxy mode for Anthropic reads
  `ANTHROPIC_API_KEY` from env (`credential-injection.mdx`, built-in routes table); whether it
  carries an OAuth bearer for `CLAUDE_CODE_OAUTH_TOKEN` was not confirmed (guess: a custom
  credential route can).

### Risks and things to avoid

- Its trusted computing base is large (the supervisor alone is many thousands of lines); the docs
  admit the seccomp-notify path has a TOCTOU race on `sockaddr` for allowed sockets.
- The profile system is complex enough that its own docs warn "profiles are security-sensitive
  code". Do not copy the complexity; copy the principles.
- Running nono **inside** ccy needs seccomp user notification and `CLONE_FILES` in the container's
  seccomp policy (`docs/cli/usage/troubleshooting.mdx`, "Running Inside Docker/Podman"). Not
  tested here.

---

## webcoyote/sandvault

Studied: baseline at 18e1249d6cc492834fa441bd05236e8cc9c511ab

### What it is and who it is for

`sv`, a 2013-line bash tool for macOS that creates a hidden second user account
(`sandvault-$USER`), gives the host user passwordless `sudo -u` to it, and runs agents there
under a root-owned `sandbox-exec` profile. Shared workspace at `/Users/Shared/sv-$USER`. Bundles
Claude Code, Codex, Gemini and others, plus Chrome and iOS Simulator automation.

### Architecture

- **Isolation:** Unix account separation first (the agent's uid cannot read the host user's home
  by POSIX permissions), Seatbelt second. The profile is `(allow default)` with targeted denies:
  all writes except the sandbox home, shared workspace, temp and `/dev`; reads of `/Users` except
  the two allowed trees; `/Volumes`; raw disks and BPF; keychains except `System.keychain`; disk
  mount services by mach-lookup (`sv`, lines 1452-1563). The profile file is root-owned and
  mode 0444 so the sandbox user cannot change it.
- **Network:** none. No egress control at all; the only network text is a macOS "Local Network"
  permission hint (`sv`, around line 1760).
- **Secrets:** agents log in inside the sandbox account; nothing is brokered.
- **Launch:** `sudo -u sandvault-$USER` (or SSH to localhost), then a per-agent wrapper in the
  repository's guest `bin/` directory (under `guest/`) that runs
  `claude --dangerously-skip-permissions` with an appended tool prompt.
- **Sudoers:** a narrow `/etc/sudoers.d` file allowing only `/bin/zsh`, `/usr/bin/env`,
  `/usr/bin/true` as the sandbox user, and `launchctl bootout` / `pkill` of that user as root
  (`sv`, lines 1416-1450), validated before install.
- **Per-repo deploy keys:** `sv-clone -k` creates a read-only deploy key for one repository and
  uploads it; `-w` grants push (`sv-clone`, lines 15-17, 208-212). The key lives in the sandbox,
  scoped to one repo.
- **Host git hardening:** `sv-clone` runs every git command against the agent-writable clone with
  `-c core.fsmonitor= -c core.sshCommand= -c core.hooksPath=/dev/null` and more, because "git
  executes several config keys as commands to run (core.fsmonitor, core.sshCommand, core.pager,
  core.hooksPath, [filter] drivers, ...), so a guest that poisons [.git/config] ..." gets code run
  as the host user (`sv-clone`, lines 60-75).
- **Audit:** none beyond verbose logging.

### Maturity and activity

558 commits from 2025-09-11 to 2026-11-12; v1.39.0; 40 commits in the last month; one main
author (501 commits) with a few contributors. Has a nightly CI and a test script. Stable,
actively maintained, small scope.

### Threat model against ccy's

macOS-only and account-based, so no mechanism transfers. On paper it is weaker than ccy: one
shared account for all projects (any project's agent can read every other project in the shared
workspace), no network control, `(allow default)` as the base. Its useful contribution is that it
**names the host-side git attack** and defends it at the one place where its host tool touches
the agent's repository.

### Ideas worth adopting

| #   | Idea                                                                                   | Where it would land                                  | Class                             |
| --- | -------------------------------------------------------------------------------------- | ---------------------------------------------------- | --------------------------------- |
| S1  | Treat an agent-writable `.git/config` as hostile when host git touches it (takeaway 1) | ccy docs and launcher, host git prompt               | issue + plan                      |
| S2  | Per-repository deploy key minted on demand, read-only unless asked                     | Plan 00116 (per-repo deploy key) already covers this | none (confirms 00116's direction) |

### Code worth lifting

None. The flag list in `sv-clone` lines 60-75 is a fact, not code worth copying, and yoloai's
analysis (below) shows it is incomplete.

### Risks and things to avoid

Passwordless sudo to a second account and `(allow default)` Seatbelt. Not a model for us.

---

## eugene1g/agent-safehouse

Studied: baseline at 534fe6467066f333a9e572b6bea704995cf3c91d

### What it is and who it is for

A macOS `sandbox-exec` wrapper (`bin/safehouse.sh`, distributed as one script) with a
**deny-first** policy assembled from numbered profile modules: `00-base.sb`,
`10-system-runtime.sb`, `20-network.sb`, `30-toolchains/*`, `40-shared/*`,
`50-integrations-core/*`, `55-integrations-optional/*` (opt-in: docker, ssh, keychain, kubectl,
clipboard...), `60-agents/*` (one per agent: claude-code, codex, aider, goose...), `65-apps/*`.
Users append a machine-local profile last to narrow or widen. Also a web "policy builder".

### Architecture

- **Isolation:** Seatbelt only, `(deny default)` base, rules rendered with the real `$HOME` and
  work directory; symlinked system paths are resolved at render time so grants stay exact
  (`README.md`, "Built-In System Path Resolution").
- **Filesystem:** home is metadata-traversable but not readable; per-integration grants (for
  git: `~/.gitconfig*`, `~/.config/git`, `~/.ssh/config`, `~/.ssh/known_hosts`, not keys;
  `profiles/50-integrations-core/git.sb`). Container engine sockets denied by default, re-opened
  only with `--enable=docker` (`50-integrations-core/container-runtime-default-deny.sb`).
  Workdir-relative denies such as `<workdir>/.env` are possible.
- **Network:** explicitly **not** a goal. `20-network.sb`: "blocking exfiltration/C2 is explicitly
  NOT a goal for this sandbox"; all outbound IP allowed.
- **Secrets:** nothing brokered; the defence is that credential files are unreadable.
- **Launch:** `safehouse claude --dangerously-skip-permissions`.
- **Audit:** none of its own; `docs/docs/debugging.md` explains streaming macOS sandbox denial
  logs and turning each denial into an allow rule.
- **Per-agent research:** `docs/docs/agent-investigations/claude-code.md` and 13 siblings are
  written analyses of what each agent reads, writes and calls, used to derive its profile.
- **Testing:** bats suites; profile chunks carry `#safehouse-test-id:<name>#` markers, and tests
  assert on the rendered policy containing or lacking them (`tests/README.md`; 16 profile files
  carry markers).

### Maturity and activity

346 commits from 2026-02-09 to 2026-11-14; v0.13.1; 17 commits in the last month; two main
contributors. macOS CI including tmux-driven end-to-end TUI tests of real agents.

### Threat model against ccy's

A hardening layer against errant agents on the host, by its own words "not a perfect security
boundary against a determined attacker". Weaker than ccy on every axis that matters here (shared
host, open network, no PID or mount isolation). It is strongest where ccy is silent: a precise,
reviewed, per-agent map of what each agent needs, and tests that pin each policy piece.

### Ideas worth adopting

| #   | Idea                                                                                                        | Where it would land                                                                                                                                 | Class          |
| --- | ----------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| A1  | Test markers embedded in policy text, asserted by tests, so a rule cannot be dropped silently               | `ccy-network-policy`: its nftables ruleset could carry rule comments that the Go tests assert on; check whether it already does before filing       | issue, small   |
| A2  | A "how to read a denial" page: what the agent sees, where the record is, how to turn it into an allow entry | docs/ccy.md "Network policy" (pairs with N5)                                                                                                        | issue, with N5 |
| A3  | Default-deny of engine sockets as a named, tested rule                                                      | ccy already mounts no socket (docs/ccy.md); a launch assertion that no `*.sock` of an engine is among the mounts would make it a check, not a habit | issue, small   |

### Code worth lifting

None: Seatbelt SBPL does not apply on Linux. The agent-investigation reports are prose about other
products and would date fast.

### Risks and things to avoid

The README's "Linux Alternatives" list (`vetto`, `sandlock`, `isolated-agent`) names projects not
studied here; they are leads, not recommendations.

---

## kstenerud/yoloai

Studied: baseline at 2d58ae138d9c14116249be1dae6e6fb9bd754633

### What it is and who it is for

The closest relative of ccy. A single Go binary that runs Claude Code, Codex, Gemini, Aider or
OpenCode with permissions off, inside a disposable sandbox, against a **copy** of the project,
then `yoloai diff` and `yoloai apply` bring chosen commits back. Backends: Docker, Podman,
containerd (Kata), Apple container, Tart, Seatbelt; isolation from runc through gVisor to Kata
VMs (`README.md`, "Features", "Isolation modes").

### Architecture

- **Isolation:** per backend. Containers run as a non-root user with the host uid. Optional
  gVisor (`container-enhanced`) or Kata (`vm`, `vm-enhanced`). A `container-privileged` mode
  exists for Docker-in-Docker, documented as escapable (`docs/GUIDE.md`, "Security").
- **Filesystem:** directories are read-only by default; `:copy` (default for the workdir) gives
  the agent a reflink or plain copy honouring `.gitignore`, so ignored credential files never
  enter the box; `:rw` is opt-in and warns on a dirty repo. Refuses to mount `$HOME`, `/` or
  system directories without `:force` (`internal/workspace/safety.go`, cited in
  `docs/contributors/design/security.md`).
- **Network:** `--network-isolated` (agent API hosts plus `--network-allow` domains) or
  `--network-none`. **Shipped** enforcement on Podman is iptables+ipset **inside** the container:
  the entrypoint holds `NET_ADMIN`, sets the rules, then drops to the agent user. The GUIDE says
  plainly that on podman "an agent that tries can flush them", and only Docker installs the rules
  from outside (`docs/GUIDE.md`, Apple container section;
  `docs/contributors/design/security.md`). A **design proposal** moves enforcement to the host
  netns (`docs/contributors/design/network-isolation.md`). It also records that shipped isolation
  is IPv4-only (finding DF104).
- **Secrets:** credential **brokering** on by default: a per-sandbox host-side reverse proxy
  (`internal/broker/injector.go`) with one fixed upstream; the agent gets
  `ANTHROPIC_BASE_URL` pointing at it and a placeholder token; the injector checks the
  placeholder in constant time (so a neighbour on the bridge cannot use it as a relay), strips
  it, injects the real key, and fails closed with a 502 if the credential cannot be resolved.
  It brokers both `ANTHROPIC_API_KEY` and `CLAUDE_CODE_OAUTH_TOKEN` (`docs/GUIDE.md`,
  "Credential Brokering"). The injector runs as a detached sidecar process that holds no secret
  on disk and is re-spawned from the environment (`internal/broker/host.go`). It binds to the
  bridge gateway, never `0.0.0.0`. Non-brokered secrets go in as read-only files under
  `/run/secrets/`, never `podman run -e`.
- **Host-side git:** the key decision for us. `yoloai diff/apply/status` originally ran git on the
  host against the agent-writable work copy; the project found that `filter.*.clean/smudge`,
  `diff.*.textconv/command` and `core.fsmonitor` in the agent-controlled `.git/config` and
  `.gitattributes` execute on the host, and that `-c core.hooksPath=/dev/null -c core.fsmonitor=false` does **not** stop filter and diff drivers (measured: "git add -A → clean
  fired; git diff HEAD → diff fired; git status → fsmonitor fired",
  `docs/contributors/archive/plans/copy-mode-git-rce.md`, lines 84-85). Their resolution: "no git
  operation touches an agent-writable `.git`/work-tree outside the agent's confinement"; git for
  the work copy runs inside the sandbox (`docs/contributors/decisions/working-notes.md`, around
  lines 1094 and 1163; `docs/contributors/backend-idiosyncrasies.md`, around line 434).
- **Audit:** JSONL logs per sandbox (`sandbox.jsonl`, `monitor.jsonl`, `agent-hooks.jsonl`); no
  record of refused network traffic was found.
- **Process:** an unusually explicit design record: `findings-*`, `critiques-*`, `questions-*`
  split by resolved, deferred, unresolved and abandoned (`docs/contributors/design/`).

### Maturity and activity

2832 commits from 2026-02-21 to 2026-10-12; latest tag v0.12.0; only 8 commits since 2026-10-01
and none in the five weeks before this study, so activity has dropped sharply after a very fast
spring and summer. One author (the counts under three names are the same person plus an
automation identity). Nightly audit CI, cosign-signed releases.

### Threat model against ccy's

yoloai frames two adversaries: the **errant** agent (primary) and the **rogue** agent driven by
prompt injection (secondary), and puts the agent's own LLM channel and DNS tunnelling out of scope
because "defending against DNS while leaving the LLM channel open would be security theater"
(`docs/contributors/design/network-isolation.md`, "Threat Model"). That is the clearest statement
of the trade ccy's owner already made in Plan 00232.

- **ccy is stronger** on network enforcement *on Podman today*: ccy's rules are applied from the
  host into the netns and the container has no `NET_ADMIN`; yoloai's shipped podman rules are
  flushable from inside. ccy also covers IPv6 private ranges (docs/ccy.md lists `fe80::/10` and
  `fc00::/7`); yoloai's do not.
- **yoloai is stronger** on credentials (the Claude token never enters the box) and on the
  project (the agent works on a copy; nothing lands until `apply`; ignored files never enter).
- **yoloai is stronger** on the host-git class: it found, measured and closed it. ccy has not
  named it (takeaway 1).

### Ideas worth adopting

| #   | Idea                                                                                                                                                             | Where it would land                                                                                                                                                | Class                             |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------- |
| Y1  | Credential broker for the Claude token: placeholder token, constant-time check, fixed upstream, fail closed, no secret on disk, bind to the bridge gateway only  | Plan 00146 (forwarder) Phase 0 spike; lift the code (below)                                                                                                        | plan (00146 exists; add as input) |
| Y2  | The host-git invariant: host tools never run git with an agent-writable `.git/config` and work tree, or else they neutralise **all** driver keys, not just hooks | Takeaway 1                                                                                                                                                         | issue (document) + plan (enforce) |
| Y3  | The errant/rogue split and "the LLM channel dominates exfiltration" argument, as the stated basis for keeping the internet open                                  | docs/ccy.md "Threat model" paragraph, citing Plan 00232's decision                                                                                                 | issue, docs                       |
| Y4  | Non-brokered secrets as read-only files, not `-e`, so they stay out of `podman inspect`                                                                          | ccy launcher: check whether `CLAUDE_CODE_OAUTH_TOKEN`/`GH_TOKEN` appear in `podman inspect` for a ccy container; if so, move to a secret file or `podman --secret` | issue (verify first)              |
| Y5  | Copy/diff/apply as an opt-in session mode for untrusted work (a repo from the internet, a PR to review)                                                          | A ccy mode; large                                                                                                                                                  | plan, needs discussion            |
| Y6  | Record design findings and critiques by state (resolved / deferred / unresolved / abandoned)                                                                     | Plan research docs                                                                                                                                                 | no action; a style note           |

### Code worth lifting

All MIT, copyright 2026 Karl Stenerud; keep the copyright line and permission notice in the
lifted files.

- `internal/broker/injector.go` (142 lines): the reverse proxy with placeholder check, header
  stripping and fail-closed transport. Directly fits Plan 00146.
- `internal/credential/binding.go`, `apply.go`, `source.go`, `errors.go` (313 lines together):
  the binding of a credential to a destination host and header.
- `internal/broker/host.go` (363 lines) and `reap*.go`: sidecar spawn, record and reaping. Lift
  only if 00146 chooses a sidecar per session; the supervisor may be the better home.
- The tests beside each (`injector_test.go`, `host_test.go`) are worth taking with the code.

### Risks and things to avoid

- The in-container iptables approach with `NET_ADMIN` that yoloai itself is replacing.
- `container-privileged` as a mode at all.
- The activity drop: if Plan 00146 lifts the broker, own it; do not expect upstream fixes.

---

## jingkaihe/matchlock

Studied: baseline at 14ad97d85bb5710880ebf93ab70368e3b490454b

### What it is and who it is for

"Run AI agents in ephemeral microVMs" (`README.md`): Firecracker on Linux (KVM),
Virtualization.framework on macOS, booting OCI images in under a second, with Go, Python and
TypeScript SDKs. Marked experimental.

### Architecture

- **Isolation:** a separate guest kernel per sandbox. **No host directory mounts at all**: files
  go in and out by `matchlock cp`, `--copy-in`, `--copy-out` or persistent block volumes; host
  mounts were removed (`docs/vfs-mounts-removal.md`).
- **Network:** host nftables (via `github.com/google/nftables`, `pkg/net/nftables.go`) redirect
  the guest's TAP traffic to a host-side transparent proxy and a DNS forwarder. With any allow
  host or secret configured, the network is sealed to the allow-list; private ranges blocked by
  default. Allow-list can be changed on a running VM (`matchlock allow-list add <vm> host`).
  Request and response hooks (block, set headers, rewrite path, replace body text, SSE-aware)
  (`docs/network-interception.md`).
- **Secrets:** the guest receives a placeholder (`SANDBOX_SECRET_<random>`) in its environment;
  the host MITM swaps in the real value in headers and the query string only, and only for the
  hosts that secret is bound to. A placeholder seen on a request to **any other host** is refused
  as `ErrSecretLeak` (`pkg/policy/engine.go`, `OnRequest`, lines 199-218). Bodies are never
  rewritten, because a server may echo a body back into the VM (comment at lines 270-273).
- **Host privilege:** `sudo matchlock setup linux` adds the user to `kvm` and `netdev`, makes
  `/dev/net/tun` group-writable, and gives the binary file capabilities
  `cap_net_admin,cap_net_raw+ep` (`cmd/matchlock/setup_linux.go`, lines 334-440). The package
  post-install runs `setcap ... || true` (`packaging/linux/scripts/postinstall.sh`, line 7).
- **Lifecycle and audit:** a SQLite state DB of VM lifecycle phases and per-step cleanup status,
  so a half-failed teardown can be resumed (`docs/lifecycle.md`). `matchlock diagnose` checks
  host setup and prints the fix.

### Maturity and activity

551 commits from 2026-02-05 to 2026-11-09; version 0.3.1; 31 commits since 2026-10-01; one main
author. Experimental by its own label.

### Threat model against ccy's

- **matchlock is stronger** at the boundary (a guest kernel, no shared filesystem) and on secrets
  (placeholder plus host binding plus leak refusal).
- **ccy is stronger** on host privilege: ccy needs no host capability at all and asserts rootless;
  matchlock gives every user who can run its binary `CAP_NET_ADMIN` in the host netns, which is a
  standing host-wide privilege grant, installed with a swallowed error.
- **Different trade:** no shared workspace means no live editing of the real checkout, so
  matchlock fits batch jobs and SDK use, not ccy's interactive session in the owner's checkout.

### Ideas worth adopting

| #   | Idea                                                                                                                  | Where it would land                                                                                                            | Class                     |
| --- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | ------------------------- |
| M1  | Bind each secret to its hosts and **refuse** a request that carries a placeholder to any other host, as a leak signal | Plans 00146 and 00252                                                                                                          | plan (add as requirement) |
| M2  | Never substitute a secret into a request body                                                                         | Plan 00252                                                                                                                     | plan                      |
| M3  | Runtime allow-list changes on a live session                                                                          | `ccy-network-policy` already refreshes live sessions on `--connect`; an owner command to add a host entry live would mirror it | needs discussion          |
| M4  | Persisted per-step teardown state, so a crashed cleanup can be resumed                                                | Only if ccy grows multi-resource teardown (proxy, CA, netns rules); note for 00252                                             | none now                  |

### Code worth lifting

MIT, copyright 2026 Jingkai He.

- `pkg/policy/engine.go` (423 lines): `OnRequest`, `isSecretAllowedForHost`,
  `requestContainsPlaceholder`, `replaceInRequest` and the Basic-auth helpers are a compact
  placeholder-swap with leak refusal, in Go. Fits Plan 00252 if it injects more than one header.
- `pkg/net/nftables.go` (610 lines): a Go nftables program over `google/nftables`. ccy already has
  `ccy-network-policy`; read it only if that tool moves from the `nft` CLI to netlink.

### Risks and things to avoid

File capabilities on a user-runnable binary; a TLS-intercepting MITM for **all** traffic when
interception is on (Plan 00252 already leans toward decrypting only hosts with a bound credential
or a path rule, which is the better default).

---

## Group section

### Takeaways, ranked

**1. ccy's workspace is a channel from the container to the host, and git config is its sharpest
edge.** The project, `.git` included, is mounted read-write, and files the container creates are
owned by the owner's uid (rootless mapping, docs/ccy.md "What it CANNOT reach"), so git's
`safe.directory` check passes. Git runs commands named in repository config: `core.fsmonitor`
(on `git status`), `core.pager`, `core.sshCommand`, `filter.*.clean/smudge`,
`diff.*.textconv/command`, and hooks under `core.hooksPath`. A misled agent that writes one of
these gets code run **as the owner, on the host, outside every ccy boundary**, the next time the
owner or a host agent runs git in that checkout. Both sandvault (`sv-clone`, lines 60-75) and
yoloai (`copy-mode-git-rce.md`, measured) found this; yoloai showed that `-c core.hooksPath=/dev/null -c core.fsmonitor=false` leaves filter and diff drivers live. Two facts here make it concrete. The
host prompt runs bash-git-prompt (`playbooks/imports/play-git-configure-and-tools.yml`), which by
its design runs `git status` in the current directory, so `cd` into the project is enough (this
is from reading, **not tested on a host**). And this repository sets `core.hooksPath` to the
tracked `scripts/git-hooks/`, which the agent can edit, so the owner's next host commit runs it.
ccy's docs do not name this residual risk; ccy's own code knows the general fact
(`files/var/local/claude-yolo/lib/ssh-handling.bash`, around line 1787: ".git/config is writable
from inside the container"), and ccy-teams hardened its own git calls (Plan 00177).

- **Clear win (issue):** name it in docs/ccy.md "What this means in practice", with what an owner
  can do now.
- **Needs discussion (plan):** options include mounting `.git/config` and `.git/hooks` read-only
  over the read-write workspace (closes the config driver and hook keys; `.gitattributes` alone
  cannot name a command), a launch-time and exit-time scan of `.git/config` for command-bearing
  keys with a refusal that names the key, and moving `core.hooksPath` out of the worktree for ccy
  projects. The class is wider than git: `.claude/settings.json` hooks and
  `.claude/hooks/handlers/` run on the host when a host Claude Code session opens the same
  checkout. A Defence Before Fix detector would be a launch check, not a lint.

**2. Take the Claude token out of the container; there is MIT Go code ready.** yoloai's broker
(`internal/broker/injector.go`, `internal/credential/*.go`) is close to what Plan 00146 plans:
placeholder in, constant-time check, fixed upstream, fail closed, `CLAUDE_CODE_OAUTH_TOKEN`
supported. matchlock adds the leak refusal (a placeholder sent to the wrong host is refused) and
the rule never to touch bodies. nono adds the per-session proxy token and DNS rebinding defence,
and `nono proxy` is a maintained standalone binary Plan 00252 could run instead of writing one.
These go into Plans 00146 and 00252 as inputs, not new plans.

**3. Record what the policy refused.** nono has a recorded audit trail and a plan (NEP-0008) to
log every denial with its process; safehouse documents how to read and convert denials; ccy's
network policy refuses silently. An nftables `log` or named `counter` on the reject rule, read by
`ccy-network-policy` or shown at session end, would show the owner what the agent tried. Small,
clear win (issue).

**4. Say ccy's threat model in yoloai's terms.** The errant/rogue split and "the LLM channel
dominates exfiltration, so DNS defence alone is theatre" is the honest basis for Plan 00232's
open-internet decision. A short paragraph in docs/ccy.md (issue). It also frames what Plans
00146 and 00252 buy: credential theft, not exfiltration.

**5. Optional copy mode for untrusted work.** yoloai's copy/diff/apply keeps `.gitignore`d
secrets out and lands nothing unreviewed. Not for everyday ccy use (live editing of the real
checkout is ccy's point), but worth a discussion for "review this stranger's repository" sessions.
Plan, low priority.

### Would a Landlock or seccomp layer inside the ccy container add real defence?

Short answer: **a little, in narrow places, and less than the items above.** Reasoning:

- **What Landlock could guard inside ccy.** The agent needs read-write `/workspace` and its own
  home; the root filesystem is disposable. The assets worth protecting from the agent inside the
  container are: the SSH key files at `/root/.ssh/key_N`, the tokens, and the host-executed parts
  of the workspace (takeaway 1). Landlock covers files only. It cannot hide an environment
  variable, so it does nothing for `CLAUDE_CODE_OAUTH_TOKEN` or `GH_TOKEN`; only taking them out
  (takeaway 2) does. For key files, the one-key ssh-agent ccy already has removes the file
  exposure more simply. For `.git/config` and hooks, a read-only bind mount does the same job as a
  Landlock rule and is enforced by the mount, outside the agent's process tree.
- **What it would need to be more than that.** nono's real value is per-**tool** grants (only
  `ssh` may read the key; only `gh` gets the token). Inside ccy that means a trusted supervisor
  process outside the agent's tree that launches each tool in its own Landlock domain. That is
  nono, and running nono inside ccy needs seccomp user notification and `CLONE_FILES` allowed by
  the container's seccomp policy (nono troubleshooting, "Running Inside Docker/Podman"). Not
  verified on rootless Podman here.
- **Network.** Landlock ABI v4 filters TCP by **port**, not address, and does not cover UDP.
  ccy's host-applied nftables in the netns is already stronger.
- **seccomp.** Podman's default profile already applies. nono's extra denials (inet sockets under
  block-net, `io_uring_setup`) matter for a process sharing the host; inside a container whose
  network is filtered in the netns, they add little.
- **Availability.** That the Fedora kernel enables Landlock and that Podman's default seccomp
  profile permits the `landlock_*` syscalls is believed but **not verified** here; a host triage
  probe would settle it if the owner wants to pursue this.

Verdict: do not plan a Landlock or seccomp layer now. Revisit only if ccy wants per-tool
credential scoping inside the container, and then evaluate running nono inside it rather than
building one.

### Repositories to consider for the watch list

- **nolabs-ai/nono**: yes. Very active, closest in ambition, candid security docs, NEPs worth
  following (0004 namespaces, 0008 flight recorder).
- **kstenerud/yoloai**: yes for its design record and broker, with a note that activity has
  dropped.
- **jingkaihe/matchlock**: optional; experimental, but its secret-binding engine is small and
  clean.
- **webcoyote/sandvault**, **eugene1g/agent-safehouse**: no; macOS-only, little transfers.

### Proposed issues and plans (for Task 3.1)

| Kind  | Title (draft)                                                                                   | Source         |
| ----- | ----------------------------------------------------------------------------------------------- | -------------- |
| issue | ccy docs: name the host-side git config and hooks channel as a residual risk                    | Takeaway 1     |
| plan  | ccy: stop agent-written repository config and hooks from running on the host                    | Takeaway 1     |
| issue | Plans 00146/00252: add the yoloai broker, matchlock leak refusal and nono proxy token as inputs | Takeaway 2     |
| issue | ccy-network-policy: count or log refused destinations per session and show them                 | Takeaway 3, N5 |
| issue | ccy docs: state the threat model as errant and rogue agent, and why the internet stays open     | Takeaway 4     |
| issue | ccy: check whether the tokens show in `podman inspect`; move them to a secret file if so        | Y4             |
| issue | ccy: audit host helpers that run by bare name from PATH inside a ccy checkout                   | N4             |
| plan  | ccy: an opt-in copy/diff/apply session mode for untrusted repositories                          | Takeaway 5     |
