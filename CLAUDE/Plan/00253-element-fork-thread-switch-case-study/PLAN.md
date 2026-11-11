# Plan 00253: element fork thread switch case study

**Status**: Dormant (research done; parked by the owner on 2026-11-11, who lives with the bug and its close-the-panel workaround for now; the decision gate below opens when it is picked up)
**Created**: 2026-11-11
**Owner**: joseph
**Priority**: Medium

## Overview

Element Desktop, which our humans use to read the agent team bus, often shows the wrong thread:
a click on a thread flickers and the panel lands back on the thread that was open before, and only
closing the thread panel and clicking again works. Upstream has three open issues for it
(element-web 24305, 25694, 25889), none assigned, and an earlier form (21956) was fixed in 2022.

This plan holds the research and the decision. [RESEARCH-thread-switch.md](RESEARCH-thread-switch.md)
traces the code and proposes a small fix: the permalinked event id that `RoomViewStore` keeps is
replayed by `RoomView` on the next store update, and is cleared only when `ThreadView` unmounts, so
switching from one thread to another inside the right panel never clears it.
[CASE-STUDY-fork.md](CASE-STUDY-fork.md) uses the fix as a test of how hard it is to fork Element
and adapt it: licence, repository layout, build, a Flatpak of a fork, release cadence and what can
be done without any fork.

The cause is read from the code and is not yet reproduced. The first task is therefore to reproduce
it; nothing is built before the owner chooses a route.

## Goals

- Confirm the cause of the wrong-thread bug by reproduction, in a development build of element-web.
- Land a fix for it, preferably upstream, with a vitest unit test and a Playwright end-to-end test.
- Give the owner a ranked, costed choice between an upstream pull request, a small carried patch
  set, a module route and a full fork.
- If a carried patch is chosen, a build and Flatpak of it produced by our IaC, never by hand.

## Non-Goals

- A full fork of Element, or its own release channel, signing or update server.
- Any change to the existing Element Desktop install play until a route is chosen.
- Running any Ansible play from the container (edit and queue only).
- Reworking threads or the right panel beyond the one transition that leaves the stale id.

## Tasks

### Phase 1: reproduce and confirm (needs a throwaway development checkout; no install in this repo)

- [ ] ⬜ **Task 1.1**: Owner decision gate: pick the route (see Open Questions). Nothing below
  Phase 1 starts before it.
- [ ] ⬜ **Task 1.2**: In a clone under `untracked/`, reproduce with a test homeserver: two threads,
  open the room by a URL to a reply in thread A, click thread B. Record whether a permalink is
  needed or a plain click also fails (RESEARCH, "Reproduction that would confirm it").
- [ ] ⬜ **Task 1.3**: Confirm with a log at `RoomView.tsx` (`getInitialEventId()` after the click)
  that the stale id is what re-opens thread A. If not, revise the fix site in RESEARCH.

### Phase 2: the fix (upstream first)

- [ ] ⬜ **Task 2.1**: Write the failing Playwright test in
  `apps/web/playwright/e2e/threads/threads.spec.ts` and prove it red on `develop`.
- [ ] ⬜ **Task 2.2**: Add the two vitest cases to `components/structures/ThreadView.test.tsx`
  (clears on thread switch; does not clear when the new `initialEvent` is the stored id), red first.
- [ ] ⬜ **Task 2.3**: Implement the clear in `ThreadView.componentDidUpdate` (reuse the bare
  `ViewRoom` dispatch from `componentWillUnmount`); run `pnpm lint` and the tests green.
- [ ] ⬜ **Task 2.4**: Comment on or link the open issues, sign the CLA, and open the pull request
  with a human-written description and a testing strategy (contribution terms in RESEARCH).

### Phase 3: only if the owner wants the fix before upstream releases it

- [ ] ⬜ **Task 3.1**: A play, queued in `CLAUDE/Plan/meta-deploy.list`, that builds the web app at
  a pinned release tag plus our patch, repacks `webapp.asar` into the official tarball and
  installs a Flatpak from the Flathub manifest with our source entries. Decide the app id first
  (open question 3). Version, checksum and source live in variables.
- [ ] ⬜ **Task 3.2**: Make the Element plays (`tasks/element-desktop.yml`, team profile and CA
  tasks) use the chosen app id, with the keyring override repeated.
- [ ] ⬜ **Task 3.3**: Remove the carried patch and return to the Flathub app once a release that
  contains the fix is on Flathub.

### Phase 4: close

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan's final diff.
- [ ] ⬜ **Task 4.2**: Move to `Completed/` and update the plan index.

## Open Questions for the Owner

1. **Route.** Recommended order in CASE-STUDY-fork.md: (1) an upstream pull request, carrying the
   patch locally only until it ships; (2) a small patch set on a fork, built by our IaC; (3)
   `config.json` and module adaptations for anything that is not a core behaviour; (4) a full fork,
   not recommended. Which do you choose?
2. **Who does the upstream work?** Upstream asks for a CLA signed by a person and a human-written PR
   description, so the pull request has to be opened and described by a human account. Will you
   do that, or hand us a branch to describe?
3. **Carrier and app id.** A fork Flatpak cannot share `im.riot.Riot` with the Flathub one in one
   installation. Replace Flathub's, or install ours under a different id and update the team plays?
4. **Branding.** If a modified build is passed to anyone outside the organisation, the
   name and logo need confirming against Element's trademark terms, which this research did not
   read. Is any build ever shared outside the organisation?
5. **Licence option.** If we ever serve a modified web app over a network, take the GPL option
   rather than the AGPL one (no network-source clause). Are we content to record that choice in
   the fork?
6. **Wider adaptation.** The case study found that everything we currently adapt (profiles,
   locked-down config, CA trust) is configuration, not code. Is there a behaviour change we want
   beyond this bug that would need a patch? That changes the route.

## Success Criteria

- [ ] The cause is confirmed or corrected by a reproduction, recorded in the journal.
- [ ] The owner has chosen a route and the choice is recorded here.
- [ ] If a fix was written, its tests fail without it and pass with it.
- [ ] No Element version, path or machine name specific to an install appears in a tracked file.

## Delivery & Milestones

- Research written: RESEARCH-thread-switch.md and CASE-STUDY-fork.md (element-web at
  `7ec1460387a3bcd79125265e7b2e90aef0040bbe`).
