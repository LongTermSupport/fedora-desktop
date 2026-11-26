# Plan 00270: questions for the owner

Each question carries the recommendation it was asked with. A decided one quotes the owner
and names the tasks that apply it. [PLAN.md](PLAN.md) keeps the one-line state of each.

- **Q1. Names.** Recommendation: `ccy`, `teamchat` and `lts-go-kit`, all public, MIT like this
  repository. Not `go-kit`: that is the name of a well-known Go project.
- **Q2. History. DECIDED** (owner, 2026-11-26): "defintely no force pushign history to
  fedora-desktop, I'm not even hugely attached to the history - some initial commit is fine.
  Make sure full hooks daemon setup is done for each repo with full planning etc and theh the
  first plan can have a handover doc that descreibe the history and confirms how to get
  history from fedora-desktop if required. fedora-desktop can then have a deletion commit
  once the carve-out is completed." No history split of any kind (neither `git filter-repo`
  nor `git subtree`). Applied in PLAN.md Tasks 2.2 to 2.6.
- **Q3. Shared gates** (bash limits, private names, push gate, branch hygiene, go-deps,
  toolchain). Recommendation: upstream them into go-qa-ci, which every repository already
  pins, rather than a fourth repository or a copy per repository.
- **Q4. The v1 bus** (`agentbus`, `bus`, `declare`, `marker`, `pingbus`, and `lightning`'s v1
  reads). **DECIDED** (owner, 2026-11-26): "The new repo is defintely only for v2 the new repo
  is absolutely clean". No v1 code, v1 parity test or v1 compatibility shim leaves this
  repository (PLAN.md Tasks 1.5 and 4.1). Removing v1 from fedora-desktop is not approved
  (PLAN.md Phase 7, gated).
- **Q5. How a host installs the Go commands.** Recommendation: the plays build them from the
  pinned module (from the proxy, `-mod=readonly`, `go.sum`-checked: the same
  `tasks/build-go-command.yml` route an external module takes today). The signed release
  binaries are what an LXC or another host installs.
- **Q6. ccy's non-Go payload** (image sources, bash, plugins). Recommendation: a signed
  release tarball pinned by version and sha256 in the play, rather than a git submodule.
- **Q7. Who writes an LXC seat's network policy.** Recommendation: this repository's host-side
  LXC play, on the container's veth, because this host owns the policy. Not the other
  repository's provisioner, and never nftables inside the container. **DECIDED** (owner,
  2026-11-26): "yes new ocntainers are secure", approving the limits as proposed (PLAN.md
  Task 5.3).
- **Q8. teamchat inside ccy, or apart.** Recommendation: apart. The homeserver side has
  nothing to do with ccy, and the two release on different cadences.
- **Q9. Order of ccy and contained mode.** Recommendation: extract ccy first with no
  behaviour change (Phase 4), then build contained mode there (Phase 5). That way a fault
  points to one change.
