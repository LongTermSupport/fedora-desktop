# Plan 00252: ccy egress proxy, per-request policy and credential injection

**Status**: Not Started (research; decision gate with the owner)
**Created**: 2026-11-11
**Owner**: joseph
**Priority**: Medium

## Overview

The watch-list project devvm puts a host-side proxy in front of its agent VM. The VM can reach
only the proxy; the proxy judges each request by host, method and path, and for chosen hosts
swaps a placeholder the VM holds for the real credential. So GitHub calls can be limited to named
repos under one org-wide token, per VM, and the Claude token can change under a running agent,
because the agent never held it. ccy has neither: Plan 00232 filters at L3/L4 and leaves the
public internet open, and the container holds live `GH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN`
values. The mechanism, with file paths and commit SHAs, is in
[RESEARCH-devvm-proxy.md](RESEARCH-devvm-proxy.md).

Plan 00146 already plans the Claude half: a host forwarder behind `ANTHROPIC_BASE_URL` that swaps
the Authorization header, with no TLS interception. Plan 00251 chose a seat's local change to
`rate-limited` as the trigger for that swap. This plan asks the wider question 00146 does not: is
one host-local egress proxy, with a per-container CA and per-request rules, the right home for the
Claude swap and for GitHub narrowing together, or should they stay separate?

## Goals

- Decide, with the owner, whether ccy gets a host-side L7 egress proxy, and whether the 00146
  forwarder becomes its first binding or stays standalone.
- If yes, a design that keeps real credentials out of the container for GitHub and Claude.
- A recorded answer on how containers are forced through it under rootless Podman.

## Non-Goals

- No implementation before the owner's decisions.
- No change to Plan 00146 or 00232 scope until the owner decides.
- No automatic account rotation (Plan 00146 Risks).

## Context & Background

- devvm facts: [RESEARCH-devvm-proxy.md](RESEARCH-devvm-proxy.md). In short: confinement outside
  the guest; proxy variables plus an optional transparent mode (guest dnsmasq, nftables, socat);
  most TLS tunnelled untouched, decrypted only for hosts with a bound credential or a path rule,
  under a per-VM CA; plain-text rules `host METHODS /path/**`; placeholder swap into one header;
  a watched bindings file, so a store change reaches a running proxy in 30 seconds to 5 minutes.
- Our side: Plan 00146 (forwarder, Not Started), Plan 00232 (network policy, In Progress),
  Plan 00116 (per-repo deploy key over SSH), Plan 00251 (seat status, trigger), Plan 00068
  Decision 8 (the owner dropped a launch-time `--egress` flag).
- Source neighbours, read as list rows only: KarpelesLab/teamclaude, rudderlabs/devbox,
  agentgateway/agentgateway.
- Licence: devvm is MIT, so code could be reused with notice; the design is the value, the Go is
  a large codebase and would be rewritten here.

## Tasks

### Phase 1: Owner decisions

- [ ] ⬜ **Task 1.1**: Answer the open questions below.
- [ ] ⬜ **Task 1.2**: If the answer to Q1 is no, close this plan and record the reason.

### Phase 2: Research, only after Phase 1

- [ ] ⬜ **Task 2.1**: Spike how a rootless Podman container is forced through a host proxy (pasta
  and Plan 00232's enforcement point), and whether Claude Code honours `HTTPS_PROXY` with a custom
  CA (`NODE_EXTRA_CA_CERTS`) for the Anthropic host.
- [ ] ⬜ **Task 2.2**: Read the teamclaude, devbox and agentgateway scans when they exist.
- [ ] ⬜ **Task 2.3**: Write the design and a build plan, or fold the work into 00146 and 00232.

## Open questions (the owner decides)

1. **Is this a gap worth closing?** Real credentials in the container today versus the cost of a
   credential-holding host process. The org-wide GitHub token case is the new one.
2. **Header injection or TLS interception?** 00146 swaps a header by pointing the base URL at
   a local forwarder: no CA, Anthropic only. devvm intercepts selected hosts with a per-container
   CA: covers GitHub path rules, but puts a CA key on the host and a CA in the container trust
   store, and needs every client to honour it (git, gh, node).
3. **Security of a credential-holding proxy.** It becomes the most valuable process on the host.
   devvm mitigations to weigh: per-VM placeholder salt, proxy credential, strict header shapes,
   refusal to proxy credential-minting GitHub calls, a fine-grained token as the real boundary.
   devvm's residual risks include an untrusted VM replacing the one shared login.
4. **Anthropic terms.** devvm documents none. Plan 00146 decision 3 is the same question; manual
   switching between the owner's own accounts only, or not.
5. **OAuth login capture** (token endpoint interception, host-side refresh) is only relevant if
   ccy leaves setup-tokens. Out of scope unless the owner wants it.
6. **GitHub narrowing form:** path rules on one token, or per-repo tokens (the watch-list's
   keybox and holvi rows)? devvm itself says the fine-grained token is the real boundary.
7. **Hot-swap trigger from Plan 00251:** per-seat placeholders (00146) or one shared login
   (devvm)? Research favours per-seat, since ccy runs several seats on a named-token pool.

## Dependencies

- Related: Plan 00146 (Claude forwarder), Plan 00232 (network policy), Plan 00251 (trigger),
  Plan 00116 (deploy key).

## Success Criteria

- [ ] The owner has answered Q1 to Q4.
- [ ] Either a design plan exists or this plan is closed with the reason.

## Delivery & Milestones

- Plan opened from watch-list run scan-20261111T1210Z.
