# Study: VM and sandbox runtimes for coding agents

Run baseline-20261120-1050 (Plan 00258), group "VM sandboxes". Eight repositories, read from the full clones under
`untracked/starred-repos/` (read only, nothing built or run). Paths below are relative to each
clone unless they start with `src-go/`, `docs/`, `files/` or `CLAUDE/`, which are this repository.
Commit counts and dates are from `git log` in the clones on 2026-11-20. Anything not read in source
or docs is marked **guess**.

## What we compare against

- **ccy**: rootless Podman container per session; project bind-mounted read-write at `/workspace`
  (`files/var/local/claude-yolo/claude-yolo:2464`, `-v "$PWD:/workspace..."`), no added
  capabilities, no engine socket; an nftables policy held in the container's own network namespace
  that refuses private ranges and the host but leaves the public internet open (Plan 00232,
  `src-go/cmd/ccy-network-policy`); live `CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN` passed by
  environment name, not argv (`claude-yolo:3803-3817`); SSH keys read-only or a one-key agent;
  SELinux relabel on enforcing hosts; tracked `.claude/ccy/mounts` validated against a deny list
  (`docs/ccy.md`, "Extra Mounts"). The written threat model (`docs/ccy.md`, "The Security Model")
  contains mistakes, not adversaries, and names kernel escape and credential exfiltration as out of
  scope.
- **Plan 00252** (egress proxy with credential injection, research stage, after devvm) and **Plan
  00146** (Claude token forwarder behind `ANTHROPIC_BASE_URL`).
- **LXC** full-system containers and the **VM lab** (`src-go/cmd/vmtest`, libvirt/QEMU, overlays
  on a snapshotted base), which is a test harness, not an agent sandbox.

## Summary table

| Repo              | Isolation                                                                                | Host OS                        | Model credential in sandbox?                                  | Egress control                                                              | Activity                                                        | Licence                                             |
| ----------------- | ---------------------------------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------- |
| smolvm            | libkrun microVM per workload (KVM / HVF / WHP)                                           | Linux, macOS, Windows          | No, with `--credential` (placeholder + host TLS interception) | off by default; host allow list; `block_private`; mediated per-flow decider | 2430 commits since 2026-01, 77 author names, daily              | Apache-2.0 (bundles LGPL/GPL libkrunfw in releases) |
| agentbox          | Docker container (in-box dockerd, `SYS_ADMIN`, seccomp unconfined); cloud VMs            | macOS, Linux                   | Yes (shared `~/.claude` volume)                               | none locally                                                                | 1887 commits 2026-05 to 2026-09-23, 6 authors; quiet two months | MIT                                                 |
| clawk             | VM: Virtualization.framework; Firecracker on Linux (experimental)                        | macOS first                    | Yes (`CLAUDE_CODE_OAUTH_TOKEN` via profile.d)                 | userspace net stack, host allow list, denials log                           | 33 commits (squashed drops) 2026-07 to 2026-08, 2 authors       | Apache-2.0 (+ MIT ext4 writer)                      |
| coi               | Incus system container (shared kernel)                                                   | Linux; macOS via Lima          | Yes when forwarded; not by default                            | nftables open / restricted / allowlist; threat monitor auto-pause/kill      | 1428 commits since 2026-01, 28 authors, daily                   | MIT                                                 |
| coop              | Firecracker + jailer (Linux), Lima (macOS)                                               | Linux (Ubuntu verified), macOS | No in proxy mode (capability token + host reverse proxy)      | open, or `isolated` (no NIC at all)                                         | 595 commits since 2026-04, 18 authors, daily (Trail of Bits)    | Apache-2.0                                          |
| devvm             | Lima VM per project (QEMU / VZ)                                                          | macOS, Linux                   | No (host L7 proxy, placeholder swap)                          | tiers; allowlist default; L7 host/method/path rules                         | 790 commits since 2026-10-24, 5 authors, very fast              | MIT                                                 |
| rudderlabs/devbox | Docker / Kubernetes pod per task (OpenSandbox)                                           | server                         | No (iron-proxy swaps placeholders)                            | DNS points every name at the proxy; per-repo write paths                    | 222 commits since 2026-10-27, 7 authors                         | MIT                                                 |
| deltabox-lite     | process sandbox (Landlock/seccomp via Sandlock; Seatbelt; MXC) + FUSE copy-on-write view | Linux, macOS, Windows          | n/a (not an agent launcher)                                   | deny-all / allowlist proxy, per-command grants                              | 219 commits since 2026-08, 10 authors                           | Apache-2.0                                          |

Five of the eight keep the model credential out of the sandbox, or can. ccy does not.

---

## smol-machines/smolvm

Studied: baseline at da303107e8f23a68b83a99994bca671a2d0abcea

**What and who.** A Rust runtime and embeddable SDK (Node, Python, Rust, Go) for "branchable
microVMs": each workload is a libkrun VM booted from any OCI image, no daemon. Aimed at agent
platforms, local sandboxing and portable dev environments; the open-source runtime behind a
commercial cloud (`README.md`). It is a platform, not an agent launcher, but `docs/coding-agent/`
ships a Smolfile and a SKILL.md for running Claude Code with full permissions inside.

**Architecture.**

- *Isolation:* libkrun VMM in the user's process, guest kernel from libkrunfw; KVM on Linux,
  Hypervisor.framework on macOS, WHP on Windows (`README.md`, "How It Works"). The VMM runs with
  the invoking user's rights; the docs say hostile local co-tenants need extra account separation
  (`docs/security-model.md`).
- *Boot:* "\<200 ms to agent ready" claimed; measured for a coding agent machine: first start
  27-28 s with `init` installs, restart under a second (`docs/coding-agent/README.md`, "Measured").
- *Filesystem:* host directories over virtiofs (`-v HOST:GUEST[:ro][:uid=N]`, `docs/volumes.md`),
  S3/R2 buckets as volumes; the project mounted `.:/workspace`, files land owned by the host user.
  Needs `safe.directory '*'` and `IS_SANDBOX=1` for Claude's skip-permissions as root
  (`docs/coding-agent/README.md`).
- *Snapshot/branch:* the standout. Live copy-on-write branches of a running machine, checkpoints of
  RAM + CPU + disks, rewind to generation `~N`, pause/resume, checkpoints portable between hosts and
  architectures (`docs/branching.md`, `docs/incremental-checkpoints.md`, `docs/mac-to-linux.md`).
- *Egress:* networking off by default; `--allow-host`, `--allow-host-pattern`, `--allow-cidr`
  editable on a running machine; `block_private` keeps the internet and blocks private and
  link-local ranges, cloud metadata always blocked (the same default ccy chose in Plan 00232);
  *mediated egress* hands each guest TCP flow, with its first 8 KiB (enough for SNI), to a loopback
  decider that answers allow / deny / redirect (`docs/mediated-egress.md`).
- *Secrets:* `--credential NAME=ENV@HOST[:body][:set-header=H]`. The guest gets
  `SMOL_PLACEHOLDER_<NAME>_<random>`; the host terminates TLS only for bound hosts, with a
  **per-machine CA that is X.509 name-constrained to the binding's hosts**, swaps the placeholder in
  a header (or a form body on opt-in), refuses it in path, query, routing headers, or to any other
  host, and resolves the value per request so a rotated file needs no restart
  (`docs/credential-substitution.md`, `crates/smolvm-credentials/src/ca.rs:8-71`). SSH by forwarded
  agent only.
- *GPU / Docker:* Vulkan via virtio-gpu/Venus, CUDA API remoting (`docs/gpu.md`); Docker inside the
  machine and nested KVM (`docs/docker-in-machine/`, `docs/nested-virtualization.md`).
- *Host OS:* Linux x86_64/aarch64 with `/dev/kvm`, macOS arm64, Windows x86_64. Fedora install page
  exists (`docs/install-fedora.md`).

**Maturity.** Very active and broad; 2430 commits in ten months, frequent releases on npm, PyPI,
crates.io. Releases are checksummed but not signed, and the installer proceeds if the checksum file
is missing (`docs/security-model.md`), which is weak for a security tool.

**Licence.** Apache-2.0 (`LICENSE`), "lift". Release bundles carry libkrunfw (LGPL-2.1 library,
GPL-2.0 kernel) and macOS GPU libraries under their own licences (`THIRD-PARTY-NOTICES.md`); the
`libkrun`/`libkrunfw` submodule directories are empty in the clone. `vendor/` holds forks of russh,
smoltcp and tar under their own terms. No per-file SPDX headers in `crates/smolvm-credentials`.

**Threat model versus ccy.** smolvm is stronger on the kernel boundary (a guest kernel), on
credentials (the model key need never enter), and on egress (deny by default, host allow lists,
per-flow decisions). It is equal on the project folder: a mounted folder is read-write and the docs
say plainly that "every folder you mount ... becomes something the workload can use". It says
nothing about git hooks or other host-auto-executing files in the mounted project (**not found**
in its docs). ccy is stronger on integration with this desktop (display, GPU, SELinux labels,
supervisor, teams), none of which smolvm attempts.

**Ideas worth adopting.**

1. *Name-constrained per-session CA* for any TLS-terminating proxy in Plan 00252: a stolen CA key
   can then mint certificates only for the bound hosts. Go's `x509.Certificate` has
   `PermittedDNSDomains` and `PermittedDNSDomainsCritical`, so this is a design note, not a lift.
   Lands in: Plan 00252 research. **Plan (fold into 00252).**
2. *Placeholder refusal rules*: refuse the placeholder in the path, query, routing/framing headers,
   more than one placeholder per request, and to any host the binding does not name; a 502 rather
   than an unauthenticated request when the value cannot be resolved. A crisp checklist for 00252.
   **Plan (fold into 00252).**
3. *Mediated egress protocol*: a host decider that sees destination + first bytes and answers
   allow/deny/redirect, fail-closed. Worth reading if 00252 wants SNI-level decisions without
   terminating TLS for every host. **Plan (fold into 00252).**
4. *libkrun is the VMM Fedora's own `crun-krun` uses* (**from general knowledge, verify on the
   host**): Podman can run an OCI image as a libkrun microVM with `--runtime krun`. That is the
   cheapest route to a VM option for ccy (see the group verdict). **Plan (spike, low priority).**

**Code worth lifting.** None directly: it is Rust and ccy is Go. `crates/smolvm-credentials/src/`
(`policy.rs`, `intercept.rs`, `scrub.rs`, ~2800 lines, Apache-2.0) is the reference to port from if
00252 builds header substitution; keep the notice if any logic is translated closely.

**Risks / avoid.** Unsigned releases; the cloud tie-in means features may move to the hosted side.
Its `--ssh-agent` gives full signing for the VM's life, the same trade ccy names for
`--ssh-agent`.

---

## madarco/agentbox

Studied: baseline at 605ee75d63319003c188bc532d4e5b2ba36a51d6

**What and who.** A TypeScript CLI that "teleports" a project into a box per agent (local Docker,
remote Docker over SSH, Hetzner, Vercel, Daytona, E2B, DigitalOcean) with tmux sessions, noVNC
browser, VS Code/Cursor dev containers, a macOS tray app. Aimed at solo developers running several
agents in parallel. Despite "VM" in the README, the local backend is a Docker container
(`docs/architecture.md`, "Goal").

**Architecture.**

- *Isolation:* a Docker container started with `--cap-add=SYS_ADMIN`, `--cap-add=NET_ADMIN`,
  `--security-opt=seccomp=unconfined` (and AppArmor unconfined) so an in-box dockerd works
  (`packages/sandbox-docker/src/docker.ts:54-71`). That is materially weaker than ccy's
  no-added-capabilities rootless Podman.
- *Filesystem:* `/workspace` is a `git worktree add -b agentbox/<box>` made inside the container
  against the host repo's `.git`, which is bind-mounted **read-write at its identical host path**;
  uncommitted state is replayed from a `git stash create` SHA plus a tar of untracked files
  (`docs/architecture.md`, "Filesystem layout per box"). So the agent works on its own branch, not
  the owner's working tree, but it can still write the shared `.git` (hooks, config).
- *Snapshot:* `docker pause` for idle boxes; checkpoints are `docker commit` images, flattened after
  three layers (`docs/architecture.md`, "Checkpoints").
- *Egress:* none locally (**not found** in docs or the Docker flags).
- *Secrets:* git push and fetch go through a host relay that runs `git push` with the owner's
  credentials (`packages/relay/src/server.ts`, `/rpc git.push`). *Host tools*: one multi-call shim
  in the box forwards `gh`, `terraform`, `aws` and others to the host, which runs the real binary
  with its own auth (`docs/host-tools.md`). The Claude login lives in a shared named volume, so the
  model credential is inside.
- *Host OS:* macOS and Linux with Docker.

**Maturity.** 1887 commits in four months by six authors, then nothing after 2026-09-23 in this
clone. Many `*-plan.md` files in `docs/`: a fast solo-led project that may have paused.

**Licence.** MIT (`LICENSE`, and `packages/provider-sdk/LICENSE`), "lift". Contributors sign a CLA
(`README.md`), which does not affect reuse.

**Threat model versus ccy.** Weaker isolation (capabilities, unconfined seccomp, no egress
control, model login inside). Stronger on one axis: git and cloud credentials never enter, and the
host gates their use.

**Ideas worth adopting.**

1. *"The project requests, the host grants."* `agentbox.yaml` (committed, untrusted) may only ask
   for a host tool; the grant lives in `~/.agentbox/projects/<hash>/tools.yaml` on the host and is
   re-read on every call (`docs/host-tools.md`, "The trust model"). ccy's tracked
   `.claude/ccy/mounts` is honoured from any clone after deny-list validation, so a cloned repo can
   bind, say, `~/Documents` read-write without the owner ever saying yes (`docs/ccy.md`, "Extra
   Mounts", lists what is refused; everything else in the home folder is allowed). A host-side grant
   keyed on the file's content (approve once, re-ask when it changes), stored beside the network
   policy file under `~/.config/claude-yolo/`, closes that. Lands in: the mounts validation in the
   ccy launcher, `docs/ccy.md`. **Issue** (the owner picks prompt-on-change versus refuse-until-granted).
2. *Host-run CLI shim for credentialed tools* (`gh` especially) as an alternative to an L7 proxy
   for GitHub. Simpler than TLS interception, but a shim that runs arbitrary `gh` argv on the host
   is a large host-execution surface; agentbox itself gates by regex allow/deny lists. **Plan (an
   option in 00252's comparison, not a separate plan).**
3. *Per-agent branch worktree as the unit of isolation* already exists in spirit in ccy teams;
   no new action.

**Code worth lifting.** None worth it (TypeScript, Docker-specific).

**Risks / avoid.** The in-box Docker recipe (`SYS_ADMIN` + seccomp unconfined) is exactly what ccy
must not copy if it ever wants Docker-in-sandbox; a VM or a system container is the honest way.

---

## clawkwork/clawk

Studied: baseline at a67d04f102152a67df4228d52da9812aa82ffe50

**What and who.** A Go CLI: `cd` into a repo, type `clawk`, and Claude Code (or Codex, pi,
opencode, a shell) runs as root in a disposable VM with the repo live-mounted. Ticket mode makes a
git worktree per repo for multi-repo tickets and opens cross-linked PRs (`README.md`,
`docs/ticket-mode.md`). macOS first.

**Architecture.**

- *Isolation:* Virtualization.framework on macOS; Firecracker on Linux, "experimental"
  (`README.md`, "Install"; `docs/linux-quickstart.md`). Own PID 1 (`clawk-init`), no systemd, no
  sshd; one vsock agent is the only way in (`README.md`, "How it works").
- *Boot:* OCI image flattened to ext4 once (no Docker daemon, `machine/internal/ext4`), then
  `clonefile`/`FICLONE` copy-on-write disks and direct kernel boot: "seconds".
- *Filesystem:* repo over virtio-fs on macOS; baked in at create on Linux. The VM disk is rebuilt
  each boot; conversations persist on the host (`README.md`, "What survives what").
- *Snapshot:* suspend-to-disk (`clawk snapshot` / `resume`), idle stop after 30 minutes, memory
  released to about 1 GiB.
- *Egress:* the VM's whole L3 is a userspace stack (a patched gvisor-tap-vsock) in the per-sandbox
  daemon; every connection and DNS answer is checked against an allow list of hosts, IPs and CIDRs,
  including UDP/QUIC and ICMP; layered named policies with external block lists; **`clawk network denials` shows what was refused, by host name** (`docs/networking.md`). Reverse forwards expose
  only named host loopback ports.
- *Secrets:* honest that the Claude token is inside (`docs/claude-auth.md`, exported via
  `/etc/profile.d/`), and that the forwarded ssh-agent can push anywhere it reaches.
- *Docker:* opt-in, needs a KVM-capable guest kernel.

**Maturity.** 33 commits, two authors, last 2026-08-13; commits are large feature drops. Pre-1.0
by its own label. Small but carefully documented (`ARCHITECTURE.md`, `DESIGN.md`).

**Licence.** Apache-2.0 with `NOTICE`; `machine/internal/ext4/` is an MIT fork of Microsoft's
hcsshim tar2ext4 with its own `LICENSE`; the gvisor-tap-vsock fork is Apache-2.0 (`NOTICE`).
"Lift" with notices.

**Threat model versus ccy.** Stronger on kernel and egress (allow list by default, with a denials
view). Same as ccy on the token and on the writable repo (it says so). Weaker on maturity and Linux
support.

**Ideas worth adopting.**

1. *A denials view.* ccy's policy refuses private ranges silently from the owner's point of view:
   the agent sees "No route to host", the owner sees nothing. A counter on the reject rule plus a
   dynamic nft set of refused destinations (address, port, first seen, count), read by
   `ccy-network-policy denials <session>` and shown in the session list, would tell the owner when
   an agent probes the LAN. Lands in: `src-go/cmd/ccy-network-policy`, `src-go/internal/netpolicy`.
   **Issue (clear win).** **Guess to verify:** kernel `log` statements in a non-initial network
   namespace are off unless `net.netfilter.nf_log_all_netns` is set, so counters and sets, not
   `log`, are the dependable route.
2. *Named, layered egress policies with external block lists* if ccy ever grows a host-name
   allow list. Fits 00252 only. **Plan (fold into 00252).**

**Code worth lifting.** `internal/netfilter/acl.go` (Apache-2.0, Go) is a host/CIDR matcher for a
userspace stack; ccy filters in the kernel, so not needed now.

**Risks / avoid.** Single-maintainer cadence; Linux path experimental.

---

## coipond/coi

Studied: baseline at 84cdb7cd84de2eb4d026555a7d99b6707018bb40

coi stands for Code on Incus.

**What and who.** A Go CLI running Claude Code and other agents in an **Incus system container**:
full OS with systemd and Docker inside, root in the container, shared host kernel (`README.md`).
For Linux users who want an agent to have "a machine, just not yours". The nearest relative of
ccy plus our LXC work.

**Architecture.**

- *Isolation:* Incus unprivileged system container; optional `reduce_kernel_surface` denies
  io_uring, bpf, userfaultfd, keyring syscalls (and `perf_event_open` in strict mode)
  (`internal/config/loader.go:405-420`).
- *Filesystem:* project mounted with idmapped ownership, so host files stay the user's.
  **Host-auto-executing paths are mounted read-only over the writable project**: `.git/hooks`,
  `.husky`, `.vscode` (directories), and `.git/config`, `.git/config.worktree`,
  `.git/info/attributes`, `.git/commondir`, `.claude/settings.json`, `.claude/settings.local.json`,
  `.codex/config.toml` (files), created as empty read-only placeholders when absent so the agent
  cannot plant them (`internal/session/security.go:430-485`). Optional `chattr +i` on the host
  copies. `SecretPaths` hide workspace files behind empty read-only mounts
  (`internal/session/setup.go:49`). Untrusted project config may add protections but never remove
  them (`sanitizeUntrustedSecurity`, `internal/config/loader.go:371-420`).
- *Snapshot:* Incus snapshots; persistent or ephemeral containers; slots for parallel agents.
- *Egress:* nftables on the host bridge in three modes (open, restricted, allowlist), DNS pinning,
  per-host port scoping (`internal/network/`). An **active monitor** reads nft logs and process
  activity, flags reverse shells, C2 patterns, private-range probing and DNS-tunnel rates, and
  auto-pauses on HIGH or kills on CRITICAL, with a JSONL audit log (`internal/monitor/`,
  `internal/nftmonitor/netdetector.go`).
- *Secrets:* nothing forwarded by default; every grant listed at startup (`README.md`).
- *Docker:* native, inside the system container. *Host OS:* Linux; macOS through Lima.
- A branch guard blocks commits and pushes to `main`/`master` from inside (`internal/config/types.go:329-333`).

**Maturity.** 1428 commits since 2026-01, 28 authors, commits today. Extensive integration tests
(`internal/network/*_integration_test.go`). A regression test forbids terminal writes from code
running while a session is attached (`internal/sinkguard/doc.go`).

**Licence.** MIT (`LICENSE`), no per-directory exceptions found, no SPDX headers in
`internal/session/security.go`. "Lift" with the MIT notice.

**Threat model versus ccy.** Same kernel-sharing class as ccy. coi is **stronger** on what the
container can leave behind for the host to run (read-only hooks, git config, agent settings), on
untrusted project config, and on detection. ccy is stronger in being rootless Podman with no
capabilities by default (coi needs Incus and its daemon, rootful by design), in SELinux labelling,
and in desktop integration.

**Ideas worth adopting.**

1. ***Host-auto-executing files read-only inside the container.*** This is the most important
   finding of the group for ccy. Today the whole project is read-write
   (`claude-yolo:2464`), so an agent (or a prompt injection) can write `.git/hooks/pre-commit`,
   `core.fsmonitor`, `core.hooksPath`, `core.sshCommand`, a `filter.*.clean` driver in
   `.git/config` or `.git/info/attributes`, or a `hooks` block in `.claude/settings.local.json`,
   and the owner's next host `git status` or host Claude Code session in that checkout runs it as
   the owner, outside every boundary ccy draws. ccy's own doc says the realistic worst case is
   "damage to this project's working tree and git history" (`docs/ccy.md`, "What this means in
   practice"); that understates it. In a rootless Podman container with no `CAP_SYS_ADMIN`, a
   read-only sub-bind cannot be unmounted from inside, so ccy can hold this more firmly than devvm
   can in a VM (devvm notes root in a VM can unmount a read-only mount,
   `internal/gitprotect/gitprotect.go:5-10`). It conflicts with real workflows here (the hooks
   daemon may install git hooks from inside; agents edit tracked `.claude/settings.json`; git
   remotes are set from inside), so it needs the owner's choices: which paths, whether tracked
   files are exempt (their changes show in review), and a per-project opt-out. Lands in: the ccy
   launcher's mount list, `docs/ccy.md` Security Model. **Plan.** A **docs correction** to the
   "realistic worst case" paragraph is a clear **issue** on its own.
2. *Mask workspace secret files* (`.env*`, `*.pem`, vault password files) behind empty read-only
   mounts, with the list owned by the host, not the project. The hooks daemon's R-SECRET-READ guards
   only Claude's tools; `cat` in the container still reads them. Same mechanism as idea 1. **Plan
   (with idea 1).**
3. *Untrusted project config may only tighten.* The rule behind `sanitizeUntrustedSecurity` is the
   right general rule for every tracked `.claude/ccy/` file ccy reads on the host. **Issue** (with
   agentbox idea 1).
4. *Threat monitor with auto-pause.* Interesting, but heuristic; ccy already refuses the private
   ranges such a monitor would flag. The cheap part is the denials view (clawk idea 1). Discard the
   rest for now.

**Code worth lifting.** `internal/session/security.go` lines 430-485 (MIT): the curated
path lists and their comments, a ready checklist for ccy's read-only set. Keep the MIT notice
(Copyright (c) 2026 Maciej Mensfeld) if copied.

**Risks / avoid.** Incus as a dependency (rootful daemon); a monitor that auto-kills can lose work
on false positives.

---

## trailofbits/coop

Studied: baseline at b1d53269067fd2dc497d61dd7f141cd5ff567385

**What and who.** A Rust CLI from a security firm that runs Claude Code, Codex and Grok Build in
disposable VMs with Docker, git and compilers inside (`README.md`). Firecracker + jailer on Linux,
Lima on macOS. For security-minded individual developers.

**Architecture.**

- *Isolation:* Firecracker microVM under the pinned, digest-verified jailer, launched as a systemd
  system service with a distinct UID/GID and chroot; setup needs `sudo` and root-owned binaries
  under `/var/lib/coop/firecracker/<version>/` (`docs/backends.md`, lines 107-128;
  `docs/trust-model.md`, "Trust zones"). Lima needs no sudo. KVM matrix: Ubuntu 24.04 and 26.04
  only; "other hosts remain unverified".
- *Filesystem:* default is **copy** (rsync into the guest), `push`/`pull` to move changes; `--mount`
  is live only on Lima, a one-time sync on Firecracker. Pull writes into a private staging tree and
  tries to drop `.git`, and the docs say to treat every pulled file as untrusted
  (`docs/workspaces.md`; `docs/trust-model.md`, "Taint sources").
- *Snapshot:* `coop commit` turns an instance into an image; reflink copies of the template rootfs.
- *Egress:* Firecracker `open` (TAP, NAT) or `isolated` (**no NIC at all**; control over SSH on
  vsock; model calls only via the host proxy over an SSH reverse forward)
  (`docs/firecracker-network-modes.md`).
- *Secrets:* optional `coop-proxy`, one per VM and provider: the guest holds a per-instance
  capability token, the proxy checks it in constant time, strips it, injects the real key and
  **default-denies every route except `POST /v1/messages` and `POST /v1/messages/count_tokens`**
  for Anthropic (`docs/credential-proxy.md`, "Operation policy"). Claude setup tokens supported as
  bearer. Secrets never on argv: environment, SSH `SendEnv` or stdin (`docs/trust-model.md`).
- *Host OS:* Linux (Ubuntu verified), macOS.

**Maturity.** 595 commits since 2026-04, 18 authors, commits today; fuzzing (`fuzz/`), `deny.toml`,
release attestation checks in `update.rs`. The most security-engineered of the group.

**Licence.** Apache-2.0 (`LICENSE`; `Cargo.toml` `license = "Apache-2.0"` for the workspace
including `coop-proxy`). "Lift".

**Threat model versus ccy.** Stronger: VM boundary, isolated mode, model key out of the guest, a
written taint analysis of every guest-to-host channel. Weaker for daily desktop use: copy-based
workspace on Linux, root-owned setup, Ubuntu-only verification. ccy already matches coop's
"never on argv" rule for tokens (`claude-yolo:3803-3817` passes `-e NAME` without a value).

**Ideas worth adopting.**

1. *Closed route allow list on the Claude forwarder.* If Plan 00146/00252 builds the forwarder,
   it should allow only the routes Claude Code needs and answer 403 to everything else, so a stolen
   capability token cannot drive account or admin APIs. **Plan (fold into 00146/00252).**
2. *A capability token instead of a placeholder* for the model API: simpler than placeholder
   substitution because the forwarder is the base URL (no TLS interception). Matches 00146's design;
   coop is evidence it works with setup tokens. **Plan (fold into 00146).**
3. *A taint inventory of container-to-host channels*, written like `docs/trust-model.md` "Taint
   sources": every file the container can write that a host process later reads or runs. For ccy
   this includes the project tree (idea 1 of coi), `.claude/ccy/` session and restore state, and
   team seat state that host tools read (**guess**: which of these the host parses was not traced
   for this study). Lands in: `docs/ccy.md` Security Model, and the plan of coi idea 1. **Plan
   (with coi idea 1).**
4. *An "isolated" network mode* (no route but the forwarder) for running untrusted repos. Only
   meaningful after 00146 exists. **Plan (later, in 00252).**

**Code worth lifting.** None directly (Rust). `coop-proxy/src/` is the reference for the forwarder's
route policy and token check (Apache-2.0).

**Risks / avoid.** Its Firecracker path needs root-owned binaries and `sudo` at setup, against this
repo's rootless preference.

---

## devvmhq/devvm

Studied: baseline at c4b4c0242478a612ca295d5ff223709e84a2d172

This builds on the 2026-11-11 watch-list scan.

The earlier scan (`CLAUDE/Plan/00200-repo-watch-list/subagent-reports/scan-20261111T1210Z.md`)
covered the host L7 proxy, placeholder swap, token hot swap, transparent mode and the per-VM CA, and
fed Plan 00252 (`RESEARCH-devvm-proxy.md`). Not repeated here. New since that scan's head
(`28d7eca`, 118 commits):

- **The proxy is split into a broker and a confined child** (`docs/content/docs/internals/design.md`,
  "The proxy's broker"). The child parses every byte the VM sends and runs under Landlock + seccomp
  on Linux (Seatbelt on macOS) with **no network of its own**: the broker resolves names and opens
  each connection after re-checking policy, then hands the descriptor over; secrets, the keychain,
  login capture and the ssh-agent relay stay in the broker. Rationale: a parser bug driven by the VM
  would otherwise run as the user with all their files.
- **Git protection** (`docs/content/docs/safety/git-protection.md`,
  `internal/gitprotect/gitprotect.go`): on macOS `chflags uchg` on `.git/hooks` and `.git/config`;
  on Linux read-only pins in the QEMU process's namespace where possible, else a watcher that
  reports changes. It also names the residual gaps: a nested repo or submodule added at run time, a
  fake bare repo anywhere in the tree (recommends `safe.bareRepository explicit`), and hooks in the
  working tree (`.husky` with `core.hooksPath`).
- **Hardened host git** (`internal/hostgit/hostgit.go`): every git command devvm itself runs on the
  host disables fsmonitor, hooks (including `hook.*` config), filters, lazy fetch, gc/maintenance,
  signing programs, replace refs, non-ssh transports, the environment's `GIT_*` variables, and
  terminal prompts.
- **`--clone` mode**: the VM gets its own clone; the host fetches its work as `devvm/<branch>` and
  reviews before merging, so nothing in the VM touches the working tree or `.git`.
- Agent store as a read-only seed with per-VM plugin folders; warnings for credentials in copied
  marketplaces; release signing with minisign.

**Licence.** MIT (`LICENSE`, Copyright (c) 2026 w3cj); docs fonts and icons carry their own
licences (`docs/static/fonts/*/LICENSE`, `docs/assets/icons/LICENSE`). "Lift".

**Ideas worth adopting.**

1. *Hardened host git for ccy's launcher.* The launcher runs host git over a container-writable
   `.git` (`files/var/local/claude-yolo/lib/ssh-handling.bash:54-64,1797-1818`,
   `lib/network-management.bash:55`). Those calls are `config`, `rev-parse` and `remote`, which do
   not run hooks or fsmonitor today, so this is defence in depth rather than a live hole; but the Go
   port will grow more host git calls. A `gitx` option that sets devvm's flag set would make every
   host git call over a project safe by construction. Lands in: `src-go/internal/gitx`. **Issue.**
   `internal/hostgit/hostgit.go` (MIT) can be lifted with its notice.
2. *Confine the parsing half of any proxy* (broker/child) for Plan 00252. **Plan (fold into 00252).**
3. *Report changes to host-auto-executing paths at session end*, devvm's Linux fallback, as a
   cheap first step before the read-only mounts of coi idea 1. **Plan (with coi idea 1).**
4. *A clone mode* (agent works on a clone, host fetches) for untrusted repos. **Plan (needs
   discussion; overlaps the worktree flows of ccy teams).**

---

## rudderlabs/devbox

Studied: baseline at 60cd61c1ac3083c7cbabf7b95dd8941740f100a8

**What and who.** Not a local sandbox: a self-hosted "autonomous engineering harness" for an
organisation. A control plane (Bun, Postgres) takes Linear/Jira/ClickUp tickets, starts one
sandbox per task through OpenSandbox (Docker Compose or Kubernetes), and a Python harness on
DeepAgents plans, codes, tests, self-reviews and opens a draft PR (`README.md`,
`docs/architecture.md`). Closest to ccy-teams in purpose, not in isolation.

**Architecture.**

- *Isolation:* an ordinary container (Compose) or pod (Kubernetes) per task, no service-account
  token; egress only to the proxy (`docs/architecture.md:160-173`).
- *Filesystem:* repos cloned into a volume; a task's sandbox gets a copy at `/workspace`, which
  outlives the sandbox for follow-ups.
- *Egress and secrets:* **iron-proxy** (an external image, `ironsh/iron-proxy`; its licence was not
  checked, it is not in this clone). The sandbox's **only DNS server is the proxy, which answers
  every name with its own address**, so all traffic reaches it without proxy variables
  (`docs/architecture.md:162`). Rules allow read-only GitHub, `git-upload-pack` for any repo,
  `git-receive-pack` and API writes only for configured repos, and `POST` only to the one LLM
  endpoint; placeholder tokens swapped in `Authorization`, `x-api-key`, `api-key`
  (`control-plane/infra/local/iron-proxy.template.yaml`). Secrets re-read from mounted files so
  rotation needs no restart.

**Maturity.** 222 commits since 2026-10-27, 7 authors; in production at the company (35 % of
internal PRs claimed, `README.md`).

**Licence.** MIT (`LICENSE`, RudderStack, Inc.). "Lift". iron-proxy is a separate project.

**Threat model versus ccy.** Weaker kernel isolation than ccy (plain Docker, docker group on
Linux), stronger credential handling (placeholders only) and per-repo write scoping.

**Ideas worth adopting.**

1. *DNS as the router.* Plan 00252 Phase 1 asks how a rootless Podman container is forced through
   the proxy. Podman's `--dns` pointed at a host-local resolver that answers every name with the
   proxy's address, combined with ccy's existing nft policy refusing everything but that address and
   the API, forces traffic through the proxy without TLS proxy variables or a transparent redirect.
   **Plan (fold into 00252).**
2. *GitHub write scoping by smart-HTTP path* (`/<owner>/<repo>.git/git-receive-pack` and
   `/repos/<owner>/<repo>/...`) is the concrete rule shape for "GitHub limited to named repos" in
   00252; SSH pushes would need the deploy-key route of Plan 00116 instead. **Plan (fold into
   00252).**
3. *Plan review before code, self-review after, learnings committed to the repo as part of the PR*:
   relevant to the teams study group, not this one; passed on.

**Code worth lifting.** `control-plane/infra/local/iron-proxy.template.yaml` (MIT) as a worked rule
example; it is configuration for someone else's proxy.

---

## delta-box/deltabox-lite

Studied: baseline at 1491775ae4df4ac5c72aa01ff71ae9a39a2feab1

**What and who.** A Rust process-level sandbox for terminal agents (plugins for DeepSeek Harness
and Pi, not Claude Code): every change is reviewed, committed or aborted, per task or per tool call
(`README.md`). "Git for agent sessions".

**Architecture.**

- *Isolation:* not a container or VM: Sandlock (Landlock + seccomp) on Linux, Seatbelt on macOS,
  MXC on Windows, built per command and torn down after (`docs/session-semantics.md`, "Process
  model").
- *Filesystem:* a FUSE copy-on-write view (`deltafs-fuse/`) in three modes: changes visible at once
  with undo records (`cbcow-through`), a private tree published only on commit (`cbcow-private`),
  or a plain copy (`copy-private`); savepoints at task start or before each mutating tool; abort
  replays undo records (`docs/session-semantics.md`).
- *Policy:* read/write rules, a protected deny set (`.env`, `*.pem`, `*.key`, `.ssh`, `.aws`)
  that a profile can never relax, scrubbed child environment, network deny-all or allowlist via a
  proxy with grants that expire when the command ends (`docs/policy.md`).
- Credential masking is on its roadmap, not built (`README.md`, "Roadmap").

**Maturity.** 219 commits since 2026-08, 10 authors, active. Young (v0.1.x).

**Licence.** Apache-2.0 (`LICENSE`, `Cargo.toml`). "Lift".

**Threat model versus ccy.** Weaker isolation (a process sandbox on the host kernel, filesystem
view only) but a stronger *undo* story than anything ccy has.

**Ideas worth adopting.**

1. *Abortable workspace for a session.* Podman can mount the project as an overlay (`:O`, with a
   persistent `upperdir`/`workdir`) so a session's changes collect in an upper layer the owner
   diffs, keeps or throws away (**guess**: rootless Podman's overlay volume with a persistent upper
   was not tested here, and it would break live sharing with a host editor and between seats).
   For git projects, git already gives most of this; the gap is untracked and ignored files.
   **Plan (needs discussion, low priority).**
2. *Protected deny set that profiles cannot relax*: same rule as coi idea 3. Covered there.

**Code worth lifting.** None; FUSE engine in Rust, wrong layer for ccy.

---

## Group section

### Takeaways, ranked

1. **Close the host-auto-execution hole in the project mount.** coi and devvm both treat
   `.git/hooks`, `.git/config` (fsmonitor, hooksPath, sshCommand, filter drivers),
   `.git/info/attributes`, `.husky` and agent settings files (`.claude/settings*.json`) as the real
   escape route from a sandbox that mounts the project read-write: no kernel bug needed, the owner's
   next host `git` or host Claude Code session runs what the agent planted. ccy mounts the whole
   project read-write and its Security Model does not mention this. Rootless Podman without
   `CAP_SYS_ADMIN` can hold read-only sub-mounts that the container cannot remove, so ccy can do
   this better than the VM tools. Actions: an **issue** to correct the "realistic worst case"
   paragraph of `docs/ccy.md` now; a **plan** for read-only protection of host-auto-executing paths,
   host-owned masking of secret files, a session-end report of changes, and a taint inventory of
   container-written files the host reads (coi `internal/session/security.go`, devvm
   `internal/gitprotect`, coop `docs/trust-model.md`). Plus an **issue** for hardened host git in
   `src-go/internal/gitx` (lift devvm `internal/hostgit/hostgit.go`, MIT).
2. **Keep the model credential out of the container.** smolvm, coop, devvm and devbox all do it,
   in two shapes: a capability token to a host forwarder that is the API base URL (coop; Plan
   00146's design), or placeholders swapped by a TLS-terminating proxy (smolvm, devvm, iron-proxy;
   Plan 00252). Concrete design inputs for those plans: coop's closed route allow list
   (`POST /v1/messages`, `count_tokens`, 403 for the rest); smolvm's name-constrained per-session CA
   and placeholder refusal rules; devvm's confined parser child; devbox's DNS-as-router to force
   traffic through the proxy under rootless Podman, and its per-repo `git-receive-pack` path rules.
   **Fold into Plans 00146 and 00252** (append to their research files; no new plan).
3. **Untrusted project configuration may only tighten.** A cloned repo's tracked
   `.claude/ccy/mounts` is honoured after a deny list; any home path not on that list can be bound
   read-write without the owner's yes. agentbox (request in the repo, grant on the host) and coi
   (untrusted config cannot remove protections) both get this right. **Issue**: host-side grant of
   a project's declared mounts, keyed on content.
4. **Show the owner what the policy refused.** clawk's `network denials`, smolvm's mediation
   events and coi's audit log all make refused egress visible. ccy refuses silently. **Issue**:
   counters and a set of refused destinations in the nft table, read by
   `ccy-network-policy denials`.
5. **Undo and fork are where VMs and FUSE tools are ahead** (smolvm live branches and checkpoints,
   agentbox `docker commit` checkpoints and pause, deltabox abortable sessions, devvm/clawk clone
   and worktree modes). ccy has `podfreeze` and git. Worth a discussion plan only if the owner wants
   per-session abort for untracked files; otherwise no action.

### Verdict: should ccy grow a VM option?

**No, not as a general option, and not now.** Plain reasons:

- The leaks that matter most for ccy are not kernel escapes. They are the live tokens (takeaway 2)
  and the writable host-auto-executing files (takeaway 1). A VM fixes neither: devvm, clawk and
  smolvm all mount the project read-write and all say the mounted folder and forwarded credentials
  stay exposed; devvm had to build git protection *on top of* its VM. ccy's own threat model already
  puts kernel escape out of scope, and rootless Podman + user namespaces + SELinux + no added
  capabilities is a reasonable bound for "mistakes, not adversaries".
- A VM costs what ccy relies on: the Wayland/X11 socket and `/dev/dri` for headed browsers, SELinux
  relabelling, the nft policy in the container's own network namespace, `podfreeze`, the supervisor,
  and teams. Every VM tool here either drops those or rebuilds them (smolvm's virtio-gpu, coop's
  SSH-over-vsock).
- The cases a VM genuinely wins (an untrusted repo whose build may try a kernel exploit;
  Docker-in-sandbox) are covered differently today: LXC full-system containers for Docker and
  systemd, the same choice coi made with Incus.

**What is worth doing instead, after takeaways 1 and 2:** a small, time-boxed spike plan to try
Podman's `--runtime krun` (crun-krun with libkrun, the VMM smolvm uses, packaged in Fedora:
**verify on the host**) as an opt-in "untrusted repository" mode. It would keep ccy's images,
launcher and Podman lifecycle and add a guest kernel. Questions the spike must answer, all
**unverified guesses** today: whether virtiofs keeps the project owned by the user and holds
read-only sub-mounts; whether the nft policy in the container's namespace still applies when the
guest's sockets are proxied by the VMM (libkrun's TSI); the SELinux type krun runs under; boot time;
and which ccy features (display, GPU, browser) must be refused in that mode. If any answer is bad,
close the spike and record why. The coop "isolated, forwarder only" network mode is the natural
companion for that mode once Plan 00146 exists.

### Watch-list candidates

- **coi**: same platform (Linux, system containers, nftables, Go, MIT), very active, and ahead on
  host-auto-execution protection; the best repository in the group to follow.
- **coop**: security-first engineering from a security firm; its trust-model document is a model
  for ccy's.
- **smolvm**: follow only for the credential-substitution crate and if the krun spike goes ahead.
- devvm is already on the list. agentbox, clawk, devbox and deltabox-lite: no need to follow.

### Code that may be lifted (all permissive; keep the notices)

| Source                                              | Licence    | What                                                    | Use here                               |
| --------------------------------------------------- | ---------- | ------------------------------------------------------- | -------------------------------------- |
| coi `internal/session/security.go` (lines ~430-485) | MIT        | curated host-auto-executing path lists with reasons     | the read-only set for ccy (takeaway 1) |
| devvm `internal/hostgit/hostgit.go`                 | MIT        | hardened host git flags and hook event list             | `src-go/internal/gitx` (takeaway 1)    |
| smolvm `crates/smolvm-credentials/src/`             | Apache-2.0 | placeholder policy, interception, scrubbing (Rust)      | reference to port for 00252, if built  |
| coop `coop-proxy/src/`                              | Apache-2.0 | capability-token forwarder with route allow list (Rust) | reference for 00146                    |
| clawk `internal/netfilter/acl.go`                   | Apache-2.0 | host/CIDR allow list matcher (Go)                       | only if ccy ever filters in userspace  |

No GPL, AGPL, BUSL or unlicensed code is in this group, so no clean-room boundary applies, except
that smolvm's release bundles include LGPL/GPL libkrunfw (not in the clone, not proposed for reuse).
