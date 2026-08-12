# Plan 00070: Lightweight Agent Browser Engine for CCY

**Status**: In Progress
**Created**: 2026-08-12
**Owner**: joseph
**Priority**: Medium

## Overview

The CCY container ships exactly one browser: `agent-browser`, a token-efficient CLI
wrapping Playwright's **Chromium**. It works well and stays. The gap is that it is the
*only* option — every browsing task, however small, pays full Chromium cost: ~17 apt
runtime libraries in the image, a Playwright browser download, hundreds of MB of RSS per
instance and a slow cold start. Many agent tasks (fetch a JS-rendered page, read a SPA,
extract text/markdown) need JavaScript execution and DOM construction but not a complete
browser.

This plan researches the alternatives, picks **one** complementary engine on evidence, and
integrates it into the CCY image and skill set so an agent can choose the cheap path first
and escalate to Chromium when it genuinely needs a full browser.

The decision is deliberately gated: research first (parallel scans + per-candidate deep
dives, all written to `research/`), then a triage pass that picks the winner and records
*why the runners-up lost*. Nothing is added to the Dockerfile before that gate closes.

## Goals

- Establish, from cited sources, what lightweight JS-executing browser engines exist that
  can run headless in a Debian 12 container — with independent measurements separated from
  vendor claims.
- Pick exactly **one** engine to adopt, with the rejected candidates and their reasons
  recorded in the plan.
- Integrate it into the CCY image: Dockerfile layer, version bumps, a `browsing`-style
  skill teaching the agent when to reach for it instead of Chromium, and startup-info /
  docs updates.
- Give the agent a clear decision rule for which browser to use, so the cheap engine is the
  default and Chromium is the escalation.

## Non-Goals

- **Not** removing or replacing `agent-browser`/Chromium. This is additive; Chromium stays
  the fallback for anything the light engine cannot render.
- Not adding more than one new engine — a second choice is useful, a third is a maintenance
  tax and a decision burden on the agent.
- Not installing anything on the HOST (host browsers are out of scope; this is the CCY
  container image only).
- Not building a new automation CLI of our own. If the chosen engine lacks an agent-facing
  interface, that is a mark against the candidate, not a licence to write a wrapper.

## Context & Background

Current state, from the repo (see `research/integration-constraints.md` for the full
checklist):

| Fact                     | Location                                                              |
| ------------------------ | --------------------------------------------------------------------- |
| Base image               | `node:lts-slim` → Debian 12 bookworm, glibc 2.36, amd64, root         |
| Chromium runtime libs    | `files/var/local/claude-yolo/Dockerfile` (~17 apt packages)           |
| agent-browser install    | same Dockerfile: `npm i -g agent-browser && agent-browser install`    |
| agent-browser config     | `/root/.agent-browser/config.json` — headed, Wayland ozone            |
| Skill teaching the agent | `files/opt/claude-yolo/skills/browsing/SKILL.md`                      |
| Startup banner           | `playbooks/imports/play-claude-yolo.yml` + `ccy-startup-info.txt`     |
| Version pins that pair   | Dockerfile `LABEL claude-yolo-version` ↔ `REQUIRED_CONTAINER_VERSION` |

Any Dockerfile change requires bumping **both** version values together (mismatch causes
infinite rebuild loops) and bumping `CCY_VERSION` if `claude-yolo` itself changes — see
`CLAUDE/ContainerRules.md`.

## Tasks

### Phase 1: Research (evidence gathering)

- [x] ✅ **Task 1.1**: Landscape scan across four independent search angles
  - [x] ✅ Agent-native headless browsers → `research/scan-agent-native.md`
  - [x] ✅ Alternative rendering engines (WebKit/Gecko/Servo/Ladybird) → `research/scan-alt-engines.md`
  - [x] ✅ DOM+JS runtimes and slim Chromium builds → `research/scan-js-runtimes.md`
  - [x] ✅ Terminal browsers and agent scraping CLIs → `research/scan-cli-crawlers.md`
- [x] ✅ **Task 1.2**: Establish integration constraints from the repo source →
  `research/integration-constraints.md`
- [ ] 🔄 **Task 1.3**: Deep dive per surviving candidate → `research/candidate-<slug>.md`
  (engine, JS fidelity, footprint, maturity, licence, Debian 12 install, agent ergonomics, risks)
- [ ] ⬜ **Task 1.4**: Completeness critique — unverified claims, contradictions, unsearched
  option classes → `research/completeness-critique.md`
- [x] ✅ **Task 1.5**: Measure the engines **in this container** rather than trusting
  published figures — `triage.bash`, reporting to `logs/browser-engine-triage.log`.
  Lightpanda 0.3.6 matched Chromium 150 on all 8 JS-capability fixtures (incl. `fetch`,
  ES modules, shadow DOM, a React 18 client render) at 379 ms / 1 process / ~25 MB peak
  RSS vs 1177 ms / 15 processes / ~1345 MB.

### Phase 2: Decision gate

- [ ] ⬜ **Task 2.1**: Triage the research and select one engine; record the decision and the
  reason each runner-up lost, in `## Technical Decisions` below
- [ ] ⬜ **Task 2.2**: Write the agent decision rule (when to use the light engine vs Chromium)
- [ ] ⬜ **Task 2.3**: Fill in Phase 3 with the concrete integration tasks for the chosen engine

### Phase 3: Integration

Deliberately left empty until Task 2.3 — the tasks depend on which engine wins.

## Dependencies

- None. Additive to the CCY image.

## Technical Decisions

To be recorded at the Phase 2 decision gate.

## Success Criteria

- [ ] Research folder contains cited, per-candidate evidence with vendor claims labelled
  as such
- [ ] Exactly one engine chosen, with rejections justified in writing
- [ ] CCY image builds with the new engine; both version pins bumped together
- [ ] The engine runs a JS-rendered page end-to-end inside the container and returns usable
  text/markdown
- [ ] A skill teaches the agent the decision rule, and startup info + docs mention the new
  option
- [ ] QA passes (`./scripts/qa-all.bash`)

## Risks & Mitigations

| Risk                                                       | Impact | Probability | Mitigation                                                      |
| ---------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------- |
| Chosen engine's JS fidelity is worse than advertised       | H      | M           | Prove it in-container on real pages before the Dockerfile lands |
| Engine is early-stage and goes unmaintained                | M      | M           | Weight maturity in triage; Chromium remains the fallback path   |
| Image size/build time grows more than the benefit is worth | M      | M           | Measure the layer delta; reject if it rivals the Chromium layer |
| Two browsers confuse the agent into picking the wrong one  | M      | H           | A single explicit decision rule in the skill, cheap-first       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00070-Journal-YY-MM-DD.md. -->

- Plan created; research workflow dispatched
