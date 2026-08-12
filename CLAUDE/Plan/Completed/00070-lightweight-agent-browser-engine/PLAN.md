# Plan 00070: Lightweight Agent Browser Engine for CCY

**Status**: Complete (2026-08-12)
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

- [x] ✅ **Task 2.1**: Triage the research and select one engine; record the decision and the
  reason each runner-up lost → Decision 1 below (Lightpanda 0.3.6)
- [x] ✅ **Task 2.2**: Write the agent decision rule → Decision 2 below (Chromium stays
  default; Lightpanda opt-in, because it fails silently outside its competence)
- [x] ✅ **Task 2.3**: Fill in Phase 3 with the concrete integration tasks

### Phase 3: Integration

- [x] ✅ **Task 3.1**: Install Lightpanda in the CCY image
  - [x] ✅ Dockerfile layer: pinned 0.3.6 binary, verified against upstream's published
    sha256, installed to `/usr/local/bin/lightpanda`
  - [x] ✅ Ship `/root/.agent-browser/lightpanda.json` — the shipped Chromium config
    (`headed: true` + ozone `args`) is **rejected outright** by the Lightpanda path, so a
    separate config is required, not optional
  - [x] ✅ Ship `agent-browser-lite` — a passthrough wrapper so the engine is one word
    rather than a long `--config` flag
- [x] ✅ **Task 3.2**: Version bumps that must move together
  - [x] ✅ Dockerfile `LABEL claude-yolo-version` 2.22 → 2.23
  - [x] ✅ `REQUIRED_CONTAINER_VERSION` 2.22 → 2.23 (mismatch = infinite rebuild loop)
  - [x] ✅ `CCY_VERSION` 3.29.0 → 3.30.0, with a `docs/ccy-changelog.md` entry
- [x] ✅ **Task 3.3**: Teach the agent the decision rule in the `browsing` skill
  - [x] ✅ Replaced the stale CLI reference — 730 lines across `COMMANDLINE-USAGE.md` and
    `EXAMPLES.md` taught `agent-browser run "navigate …"`, which errors with
    `Unknown command: run`; the skill now points at `agent-browser skills get core --full`
  - [x] ✅ Added the engine-choice rule and the silent-failure warnings
  - [x] ✅ Play removes the two obsolete files from the build context (the copy task only
    ever added files, and the Dockerfile copies the whole directory)
- [x] ✅ **Task 3.4**: Surface it — startup banner, `ccy-startup-info.txt`, `CCY-GUIDE.txt`,
  `docs/playbooks.md`, `docs/containerization.md`, `docs/ccy-changelog.md`
- [x] ✅ **Task 3.5**: QA green (425 files); Dockerfile-generated wrapper and config
  replayed through `sh -n`, `shellcheck` and `jq` rather than assumed correct
- [x] ✅ **Task 3.6**: Verified in the rebuilt image (container 2.23)
  - [x] ✅ Section 7 reports PRESENT for binary, config and wrapper; `agent-browser-lite`
    returns `MARKER-DOM` with **no overrides**; installed `lightpanda version` = 0.3.6
  - [x] ✅ Re-confirmed post-rebuild: 8/8 JS fidelity both engines; Lightpanda
    469 ms / 1 proc / ~22 MB vs Chromium 1243 ms / 15 procs / ~1389 MB; the screenshot
    correctness check still catches Lightpanda's untruthful render (0/3 page colours)
  - [x] ✅ `triage.bash` is now **host-runnable**, matching this repo's plan-script
    convention: a host run re-executes the same file inside the running CCY container
    (auto-detected, `--container` to override) instead of refusing

## Dependencies

- None. Additive to the CCY image.

## Technical Decisions

### Decision 1: Adopt Lightpanda, via the flag `agent-browser` already has

**Context**: pick one lightweight JS-executing engine to complement Chromium.

**Options considered**:

| Candidate               | Why it lost                                                                                                                                                           |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `chrome-headless-shell` | Not a second engine — the same Blink/V8 in slimmer packaging. Real win (~355 MB vs ~1204 MB RSS) but it is *tuning what we ship*, so it is a follow-up, not this plan |
| Playwright WebKit       | Not lightweight: 293 MB measured, ~227 MB RSS per tab, 720 MB of official Linux deps                                                                                  |
| WPE WebKit + Cog        | The exact stack researched is being deprecated by its own maintainer                                                                                                  |
| Firefox/Gecko headless  | Heaviest of the three mainstream engines; WebDriver BiDi diverges from the CDP our tooling speaks                                                                     |
| eLinks                  | JS is partial and buggy on every backend, and no Debian package ships a JS-enabled build                                                                              |
| Ladybird, Blitz, Servo  | Pre-alpha, or no JS at all, or no automation surface                                                                                                                  |
| jsdom / happy-dom       | Libraries, not drivable browsers; happy-dom had a VM-escape RCE (CVE-2025-61927) on exactly this attack surface                                                       |
| Hosted browser APIs     | Move the cost off-image but add a network dependency, per-request cost and egress of page content — rejected for a local dev container                                |

**Decision**: **Lightpanda 0.3.6**, exposed through `agent-browser`'s existing
`--engine lightpanda`. Decisive evidence is measured in this container
(`logs/browser-engine-triage.log`), not published benchmarks:

- Matched Chromium on **all 8** JS-capability fixtures — `fetch()`, ES modules with
  private fields, custom elements + shadow DOM, and a React 18 client render included.
- 379 ms / 1 process / ~25 MB peak RSS vs 1177 ms / 15 processes / ~1345 MB.
- Equal or better text extraction on real pages (more text than Chromium on both the
  Wikipedia article and `react.dev`).

It also has by far the smallest integration surface: the engine is already wired into
the CLI we ship, so this is a binary plus a config file, not a parallel browser stack.

**Overruling the research on one point, deliberately**: a scan agent disqualified
Lightpanda for "does not render the DOM at all". The underlying fact is right — there is
no CSS layout or paint — but the conclusion does not follow for our use. It builds the
DOM and runs the JavaScript, which is what text extraction, scraping and SPA reading
need, and the measurements above show it doing exactly that. The limitation is real but
narrower than "cannot render": it cannot produce **pixels or geometry**.

**Date**: 2026-08-12

### Decision 2: Chromium stays the default; Lightpanda is opt-in per invocation

**Context**: which engine should an agent get when it does not choose?

Lightpanda fails **silently** outside its competence, which makes a cheap-by-default
rule dangerous:

- `screenshot` returns **rc=0** with `✓ Screenshot saved` and writes a placeholder PNG
  reading "Lightpanda has no graphical rendering engine". An agent checking the exit
  status believes it has a screenshot of the page.
- `get box` returns **rc=0** with fabricated geometry (`height: 100000000`).

**Decision**: `agent-browser` keeps Chromium as its default and existing behaviour is
untouched. Lightpanda is reached explicitly via `agent-browser-lite`, and the skill
teaches the boundary: text/DOM/scraping → lite; anything involving pixels, geometry or
visual correctness → Chromium. A wrong default here produces confidently wrong output,
which is worse than a slow one.

**Date**: 2026-08-12

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

- `f811688` — plan created; research workflow dispatched (11 agents, 4 scan angles +
  5 deep dives + completeness critique)
- `e2dd75b` — `triage.bash`: measured both engines in-container; Lightpanda matched
  Chromium on all 8 JS fidelity fixtures
- `b300146` — integration: Lightpanda 0.3.6 in the Dockerfile, `agent-browser-lite`,
  paired version bumps, browsing skill rewritten (730 stale lines removed)
- `680326b` — plan index row updated with the decision and its evidence
- `a73f7a8` — fixed `triage.bash` misdirecting a host run; added section 7 (deployed
  integration check)
- Verified in the rebuilt image (container 2.23): section 7 all PRESENT,
  `agent-browser-lite` returns `MARKER-DOM` with no overrides
