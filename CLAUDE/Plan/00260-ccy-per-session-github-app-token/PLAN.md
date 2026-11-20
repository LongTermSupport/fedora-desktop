# Plan 00260: ccy per session github app token

**Status**: Not Started
**Created**: 2026-11-20
**Owner**: joseph
**Priority**: Medium

## Overview

A ccy session receives the owner's `gh` login as a live `GH_TOKEN` in its environment. Any process
in the container can read it, and it reaches everything the owner's account reaches on GitHub, not
only the project's repository (`docs/ccy.md` "What this means in practice" names this risk).

A GitHub App installation token can be minted for exactly one repository and exactly the
permissions asked, valid one hour. The host would mint one at launch for the project's own
repository, read GitHub's answer, and refuse (revoking the token) if it covers more repositories or
permissions than asked. The container then holds a token that can only touch one repository and
dies within the hour. It needs no proxy, no CA and no TLS interception, so it does not wait for
Plan 00252, and it answers that plan's question 6 ("path rules on one token, or per-repo tokens?").
A token cannot be limited to a branch, so branch protection stays the boundary for writes.

**The question to decide:** how each owner's GitHub App is set up and its private key held (IaC and
vault), which permissions a session gets by default, and how sessions that outlive the hour get a
fresh token.

## Goals

- A ccy session's GitHub token reaches only the project's own repository, with the permissions the
  owner chose, and the launch refuses a token GitHub reports as broader.
- The App's private key never enters a container.
- Sessions longer than an hour keep working, or fail with a clear message that names the cause.

## Non-Goals

- An approval flow per grant (a phone passkey, as the studied project has); possibly later for
  agent teams (Plan 00197's seat broker).
- SSH access: Plan 00116 (deploy keys) owns git over SSH.
- A credential-injecting proxy: Plan 00252.

## Tasks

### Phase 1: Facts and decision

- [ ] ⬜ **Task 1.1**: Establish, with a plan-local triage script, how an installation token is
  minted for one repository and a permission subset, and what GitHub's answer reports, so the
  breadth check reads real fields
- [ ] ⬜ **Task 1.2**: List what ccy sessions do with `gh` today (issues, PRs, runs, releases) and
  the minimum permission set that covers it
- [ ] ⬜ **Task 1.3**: Owner decision: App per owner or per organisation, key storage (vault,
  host-only file), default permissions, refresh path (host-side refresher writing a file the
  container reads, or relaunch); recorded under Technical Decisions

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: IaC for the App's private key on the host (vault variable, host-only file)
- [ ] ⬜ **Task 2.2**: Host-side minting and breadth check in the ccy launcher (Go), replacing the
  full `GH_TOKEN`; fail fast on any minting or check error
- [ ] ⬜ **Task 2.3**: Refresh for long sessions, per Task 1.3
- [ ] ⬜ **Task 2.4**: `docs/ccy.md` Security Model and "SSH and GitHub" updated; CCY version bump

## Dependencies

- Related: Plan 00252 (question 6), Plan 00116 (deploy keys), Plan 00146 (keeps the Claude token
  out of the container), issue #356

## Success Criteria

- [ ] From inside a session, `gh` can act on the project's repository and is refused on any other
- [ ] A minted token broader than asked is revoked and the launch stops with the reason
- [ ] QA passes; `qa-reviewer` run as the final step

## Sources

Watch-list run `baseline-20261120-1050` (Plan 00258), in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`:

- `SYNTHESIS.md` section 2 item 03; section 4 "New plans" 2
- `gateways.md` "IT-Hjaelp-Nu/keybox" and "The five strongest takeaways, ranked" 1

The studied project is proprietary and clean room: this plan works from the report's description
only; none of its code or text is used.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00260-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Not started
