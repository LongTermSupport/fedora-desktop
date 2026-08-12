# Scan: Purpose-built headless browsers for AI agents

Scope: engines built specifically for AI-agent/automation workloads (headless-by-design,
JS execution + DOM, low memory, fast startup), as distinct from a full desktop browser or
a Chromium-wrapper product. Research date: 2026-08-12. Every claim is tagged
[MEASURED] (independently reproduced or directly observed), [VENDOR CLAIM] (asserted by
the project/company, not independently verified), or [UNKNOWN] (not established from
available sources).

---

## 1. Lightpanda

**What it is**: A headless browser written from scratch in **Zig**, purpose-built for AI
agents/automation. Not a Chromium fork. [VENDOR CLAIM/description]

- **JS engine**: V8 (embedded), same engine as Chrome, so ECMAScript coverage itself is
  strong. [MEASURED — confirmed via GitHub repo]
- **Rendering/layout**: No graphical rendering pipeline, no GPU compositor. It executes JS
  and builds the DOM but does not do visual/CSS layout or produce pixels — it is
  explicitly "DOM, JavaScript execution, network stack" only, not a layout engine.
  [VENDOR CLAIM, consistent with architecture description across multiple sources] This
  also means **no screenshots and no PDF generation** are possible today.
  [stated as a current limitation by RoundProxies write-up, corroborated independently]
- **CDP/Playwright/Puppeteer**: Full CDP server; drop-in compatible with Puppeteer and
  Playwright over the WebSocket CDP interface. \[VENDOR CLAIM, repeated consistently
  across ScrapingBee, Wavect, dev.to write-ups — not independently re-verified by this
  scan\]
- **License**: **AGPL-3.0** (confirmed by reading the repo's `LICENSE` file directly).
  [MEASURED] This is a copyleft network-service license — worth flagging explicitly for
  any redistribution/bundling decision (e.g. shipping it inside a container image), since
  AGPL's network-use clause is stricter than MIT/Apache.
- **Maturity**:
  - 33.8k GitHub stars, 8,605+ commits on `lightpanda-io/browser`. \[MEASURED, via direct
    repo fetch\]
  - **No stable/tagged versioned release — ships only `nightly` builds.** An earlier
    fetch of the GitHub Releases page returned a fabricated-looking table of "0.3.x"
    releases dated 2024; a follow-up search independently corroborated that the project
    in fact uses a single rolling `nightly` tag, last updated **2026-07-25**, with no
    numbered releases. Flagging the discrepancy: the version-table data point should be
    treated as unreliable and the nightly-only characterization as the trustworthy one.
    \[MEASURED via search corroboration, with an explicit caveat about a bad intermediate
    read\]
  - Actively shipping features through 2026: Web Bot Auth support (bot/agent
    cryptographic identity, 2026-03-20), native HTML→Markdown conversion to cut agent
    token usage (2026-03-05), an official backend slot in Nous Research's "Hermes Agent"
    (2026-05-08), a "Lightpanda Agent"/"PandaScript" LLM-automation layer (2026-06-17),
    and a from-Lightpanda benchmark comparing itself to `agent-browser` and
    `browser-use` (2026-06-03). \[VENDOR CLAIM — Lightpanda's own blog, dates as listed
    on lightpanda.io/blog\]
  - Explicitly labelled **beta** by the project itself: "Stability and coverage are
    improving and many websites now work," acknowledges crashes are possible, and states
    "hundreds of Web APIs" are still unimplemented. [VENDOR CLAIM, self-reported]
- **Platforms**: Linux (x86_64, aarch64, glibc — not musl without a source build), macOS
  (x86_64, aarch64). **No native Windows** (WSL2 only). Docker images published.
  [MEASURED via repo fetch] This matches our CCY container (Debian 12/glibc, amd64) with
  no platform blocker.
- **Performance claims**: Lightpanda's own January 2026 benchmark: 25 parallel workers
  processed 933 JS-dependent demo pages in 4.81s / 123MB peak memory, vs Chrome's 46.70s /
  ~2.0GB peak on the same workload — roughly 9–11x faster and ~9–16x less memory depending
  on which figure/write-up is cited. \[VENDOR CLAIM — explicitly **not independently
  reproduced**; multiple third-party write-ups (e.g. Wavect) state outright: "The
  benchmark was designed and run by Lightpanda. We did not independently reproduce it,"
  and caution the ratio may not hold for checkout flows, authenticated apps, visual
  tests, or a reader's own private target set.\]
- **Known real-world gaps** (from independent/practitioner write-ups, not the vendor):
  - Cannot fully mimic Chrome's rendering/fingerprint surface (no canvas/WebGL/font
    enumeration to spoof), so sites with sophisticated bot-fingerprinting can detect it
    as non-standard even with runtime "stealth patch" JS overrides. \[independent
    practitioner analysis, RoundProxies "Fix Common Lightpanda Issues" post\]
  - "Bleeding-edge" — broken pages and failed scripts are described as expected/common at
    this stage of maturity, with Chrome recommended as a fallback. [same source]
  - HN "Show HN" launch thread (2025) generated broadly positive but technically
    skeptical discussion around the Zig choice and V8-embedding economics; description of
    "hundreds of technical comments" — this scan did not fetch the raw thread text.
    \[reported by a secondary aggregator, not independently read from HN itself — treat as
    weak/UNKNOWN on specifics\]
- **Disqualifier assessment**: none outright — it is Linux/glibc-compatible, actively
  developed, has real independent-adjacent scrutiny (not just marketing), and is the
  clearest fit for "small, JS-executing, CDP-compatible, agent-focused." The two real
  caveats to weigh at triage time are (1) AGPL-3.0 licensing terms for
  redistribution/bundling, and (2) pre-1.0/nightly-only maturity with self-acknowledged
  gaps (no screenshots/PDF, missing Web APIs, anti-bot fragility).

---

## 2. Cloudflare Kitesurf

**What it is**: Announced **2026-08-06/07** — an "agent-first" browser engine written
from scratch in **Rust**, compiled to **WebAssembly**, running entirely inside Cloudflare
Workers **V8 isolates**. No Chromium binary anywhere in the stack. \[VENDOR CLAIM, per
Cloudflare's own launch blog post and corroborated by TechCrunch's independent reporting
of the announcement\]

- **Engine components**: **Blitz** (a modular rendering engine from the Dioxus Labs
  ecosystem) for rendering, **Stylo** (Firefox/Servo's CSS parser and style engine) for
  CSS, and **Boa** (a pure-Rust ECMAScript engine) for JavaScript. \[VENDOR CLAIM per
  TechCrunch's technical breakdown of the Cloudflare announcement\]
- **JS engine detail — Boa**: an independently-maintained, embeddable Rust JS engine
  (`boa-dev/boa`), reporting >90% Test262 (ECMAScript conformance suite) pass rate as of
  its own project material. \[VENDOR CLAIM/self-reported by the Boa project, not
  independently re-run by this scan\] Boa is explicitly positioned by its own maintainers
  as an embeddable engine for Rust applications, not historically as a full browser's JS
  engine — Kitesurf appears to be the most prominent "put Boa inside a real browser
  product" case found in this scan.
- **Rendering/layout**: Yes — Blitz + Stylo is a real (if non-Chromium, non-Gecko)
  layout/CSS pipeline, so this is a step beyond Lightpanda's "DOM only, no layout"
  design — Kitesurf is claimed to support real rendering for tasks like screenshots.
  [VENDOR CLAIM]
- **Web-platform conformance**: Cloudflare states Kitesurf "passes around 215,000+ web
  platform tests," with hundreds more added weekly. \[VENDOR CLAIM, unverified
  independently\]
- **CDP/Playwright/Puppeteer compatibility**: **Not established** by any source found in
  this scan. Neither the TechCrunch piece nor the Cloudflare blog post (as summarized)
  states whether Kitesurf speaks CDP or is Playwright/Puppeteer-drop-in compatible.
  [UNKNOWN]
- **Availability — the decisive disqualifier for our use case**: Kitesurf is **not
  self-hostable and not open source today**. It runs exclusively inside Cloudflare
  Workers via Cloudflare's "Browser Run" product, currently free in beta. Cloudflare has
  stated an intent to open-source it eventually so customers can self-host on their own
  accounts, but as of this scan **no repository, license, binary, or timeline exists**.
  \[VENDOR CLAIM for the "intent to open source," corroborated as still-unfulfilled by
  multiple 2026-08 write-ups explicitly noting "no download binary available for
  self-hosting outside of the Cloudflare Workers environment"\]
- **Origin note**: Cloudflare has publicly credited the open-source Rust project
  **Obscura** (see below) as direct inspiration — their initial proof of concept was
  reportedly a port of Obscura to Workers. \[VENDOR CLAIM per Cloudflare's own
  announcement, as relayed by TechCrunch\]
- **Maturity**: Days old as a public product at the time of this scan (announced
  2026-08-06/07); beta. \[MEASURED — trivially, from the announcement date vs. today's
  date of 2026-08-12\]
- **Disqualifier**: **Cloud-only, no self-hosted artifact.** A CCY container needs a
  locally installable engine; Kitesurf currently cannot run outside Cloudflare's own
  infrastructure, so it is not viable to bundle regardless of its technical merits. Worth
  re-checking if/when Cloudflare ships the promised open-source release.

---

## 3. Obscura

**What it is**: An open-source headless browser engine written in **Rust**, positioned
explicitly for "AI agents and web scraping." Runs JS via **V8** (not a from-scratch JS
engine, unlike Kitesurf/Boa), speaks CDP, and is described as a drop-in replacement for
headless Chrome under Puppeteer/Playwright. Ships its own MCP server exposing 12 browser
tools (navigate, snapshot, click, fill, evaluate, network/console inspection, etc.) for
direct use by Claude Code/Cursor/Claude Desktop-style MCP clients. \[VENDOR CLAIM per
project README, as relayed by search-result excerpts\]

- **License**: Apache-2.0 (project claims to keep the "open-source engine" permissively
  licensed). \[VENDOR CLAIM, from repo fetch of `h4ckf0r0day/obscura`\]
- **Memory claim**: ~30MB footprint vs. 200MB+ for Chrome. \[VENDOR CLAIM, unverified
  independently — no third-party benchmark found in this scan\]
- **Repo stats** (as fetched from `github.com/h4ckf0r0day/obscura`): 21.3k stars, 1.5k
  forks, 69 watchers, 36 open issues, 32 open PRs, 837 commits, multi-platform release
  archives (Linux x86_64/ARM64, macOS, Windows), Docker Hub presence, sponsor listings
  (NodeMaven, ProxyEmpire, 9Proxy, Thordata — all proxy/scraping-infrastructure vendors).
  [MEASURED via direct repo fetch, numbers as displayed on the page at fetch time]
- **AI/MCP integration**: Explicitly wired for Claude Code/Cursor/Aider via MCP
  (stdio + HTTP transports). [VENDOR CLAIM per README]
- **Build**: Requires Rust 1.75+; first build ~5 minutes because V8 compiles from source
  (cached thereafter). [VENDOR CLAIM per README/docs excerpt]
- **A caution worth recording plainly**: a plain web search for "Obscura headless browser
  Rust AI agent github" surfaced an unusually large number of **near-identical
  repositories under unrelated GitHub accounts**, all titled "obscura[-\_]..." with the
  same one-line description ("The headless browser for AI agents and web scraping"):
  `2516455367/obscura_vm`, `Timtech4u/obscura-browser`,
  `siathalysedI/obscura--h4ckf0r0day`, `RussPalms/obscura_dev`,
  `nxpatterns/obscura-headless-browser`, alongside the apparent "main" repo
  `h4ckf0r0day/obscura`. A star count in the low tens-of-thousands for a several-month-old
  niche Rust browser-engine project is also unusually high relative to comparable
  projects in this scan (e.g. Lightpanda's 33.8k took roughly two years; Obscura's 21.3k
  would be much faster growth for a narrower, newer tool). A targeted search for
  "obscura.sh scam / fake stars" returned **no direct corroborating or exonerating
  evidence either way** — only general background on GitHub fake-star fraud as a known
  phenomenon (TechXplore, HN discussions of "4.5M suspected fake stars on GitHub").
  \[MEASURED: the repo-proliferation pattern itself was directly observed in search
  results. UNKNOWN: whether this specific project's stars/forks are inflated — no
  confirming or denying independent source was found.\] **This is a flag for the triage
  stage, not a conclusion** — the account-naming pattern (`h4ckf0r0day`) and the
  repo-farm-like search footprint are unusual enough to warrant a closer legitimacy check
  (e.g. checking commit author diversity, whether the "forks" are genuine GitHub forks
  vs. independent copies, whether `obscura.sh` domain registration/company info resolves
  to a real entity) before this project is treated as equivalent in trustworthiness to
  Lightpanda or Boa/Kitesurf, both of which have clear, single, long-standing canonical
  origins (Lightpanda GmbH-style project page; Boa under the `boa-dev` GitHub org with a
  standard single-repo history).
- **Maturity**: Presents as fairly mature (release archives for 4 platforms, CI/CD,
  contributing guide, security policy, active PR queue) but this scan could not establish
  a founding/creation date or a long-run commit-history graph independently. [UNKNOWN]
- **Disqualifier**: none confirmed, but **not recommended for adoption without an
  independent legitimacy check** given the repo-proliferation and star-count anomalies
  above — a project meant to be executed inside a container that has filesystem/network
  access deserves a higher bar of provenance confidence than this scan could establish.

---

## 4. Boa (JavaScript engine — not a full browser)

**What it is**: A standalone, embeddable ECMAScript engine written in pure Rust,
maintained under the `boa-dev` GitHub organisation. **Not itself a browser** — no DOM, no
networking, no CDP. Included here because it is the JS engine inside Kitesurf and is a
building block other "agent browser" projects could embed. \[VENDOR CLAIM/project
description\]

- Reports >90% Test262 conformance. [VENDOR CLAIM, self-reported]
- Actively maintained; a Boa-specific talk was presented at JSNation 2026, and community
  work in 2025–2026 focused on inline caching and CPU-cache-friendly execution.
  \[reported via secondary aggregator (gitnation.com talk listing), not independently
  confirmed by this scan beyond the listing itself\]
- **Disqualifier for direct use**: it is a JS engine, not a browser — no DOM
  construction, no layout, no CDP surface. Not directly adoptable as an "agent browser
  engine" on its own; only relevant as the JS layer inside a larger project (Kitesurf).

---

## 5. Servo-fetch (built on Servo + SpiderMonkey)

**What it is**: A small, self-contained tool (`konippi/servo-fetch`) that fetches,
renders, and extracts web content as Markdown/JSON/screenshots, built on the **Servo**
browser engine (Rust) with **SpiderMonkey** (Firefox's JS engine) for JS execution and
Servo's own parallel CSS layout engine for real layout. Explicitly "no Chromium, no API
key, no setup." Ships an "Agent Skills" package aimed at AI coding agents, plus a CLI,
Rust library, Python SDK, Node.js SDK, MCP server, and HTTP API server. \[VENDOR CLAIM per
project README, as relayed by search excerpts\]

- **JS + layout**: Real JS execution (SpiderMonkey) plus real CSS layout — described as
  doing "layout- and visibility-aware extraction" (stripping navbars/sidebars/hidden
  content based on actual computed layout, not just DOM heuristics). This is a
  meaningfully different design point from Lightpanda (DOM-only, no layout) — servo-fetch
  actually lays the page out. [VENDOR CLAIM]
- **CDP/Playwright/Puppeteer compatibility**: **None found.** No source in this scan
  mentions a CDP server or Playwright/Puppeteer drop-in mode — it appears to be a
  fetch-and-extract tool (CLI/library/API), not a browser-automation target. \[UNKNOWN /
  apparent gap, treat as "no" unless contradicted\]
- **License**: Dual MIT/Apache-2.0. [VENDOR CLAIM per repo fetch]
- **Scale**: 136 GitHub stars, 369 commits. [MEASURED via direct repo fetch] Small,
  young, single-maintainer-scale project relative to Lightpanda/Obscura.
- **Performance claims**: 231ms for static pages vs. 645ms for Playwright; 51–64MB memory
  vs. 300–328MB for Playwright; extraction-quality metric of mean word-F1 0.819 vs. 0.728
  for Mozilla's Readability library. \[VENDOR CLAIM, all figures as stated in the
  project's own material — no independent reproduction found in this scan\]
- **Disqualifier**: No CDP/Playwright automation surface means it does not fit an
  "agent-browser"-style drop-in replacement use case the way Lightpanda/Obscura do — it
  is closer to a specialized fetch+extract tool than a general browser-automation target.
  Worth keeping in mind as an alternative if the actual need is "get me clean Markdown
  from a JS-rendered page" rather than "give an agent a browser to drive."

---

## 6. HtmlUnit

**What it is**: A long-standing (pre-dates the "AI agent" framing entirely — originally a
Java testing tool) headless "GUI-less browser for Java programs." Included because it is
a genuine, mature, still-maintained JS-executing headless browser, even though it predates
and was not designed for AI-agent use.

- **JS engine**: Mozilla **Rhino**. The maintainer has stated an intent to replace Rhino
  in "the coming months" (as of the source found), acknowledging its current limitations.
  [VENDOR CLAIM/maintainer statement, per project's own site material relayed via search]
- **Rendering/layout**: Simulates DOM + JS interaction for Chrome/Firefox/Edge-like
  behaviour profiles, but does **not** do full visual CSS layout/rendering the way a real
  browser engine does — it is fundamentally a DOM/JS simulation for testing and scraping,
  not a rendering engine. \[characterization drawn from long-standing public knowledge of
  the project plus the search excerpts gathered; treat the "no real layout" characterization as
  well-established but not freshly re-verified in this scan\]
- **Maturity**: Genuinely mature and actively maintained — version 5.3.0 published
  2026-07-15. [VENDOR CLAIM/project changelog, as relayed by search]
- **CDP/Playwright/Puppeteer compatibility**: None — HtmlUnit predates CDP and has its
  own Java API (and a Selenium `HtmlUnitDriver` integration), not a CDP/Playwright
  surface. \[established characterization, not independently re-verified fresh in this
  scan\]
- **Disqualifier**: **Not agent-native and not CDP-compatible** — it is a JVM library for
  Java test code, not a standalone process an agent CLI/MCP tool would drive over
  CDP/Playwright. Its JS engine (Rhino) is also markedly weaker than V8/SpiderMonkey/Boa
  for modern JS. Relevant mainly as a maturity baseline ("this category of tool has
  existed for a long time"), not as an actual candidate for this container.

---

## 7. Ladybird

**What it is**: A from-scratch, non-Chromium, non-Gecko browser engine (originally from
the SerenityOS project), now backed by the independent nonprofit **Ladybird Browser
Initiative** with $1M+ seed funding from a GitHub co-founder and sponsorships (Shopify,
Proton VPN, etc.). Actively developed, targeting a Linux/macOS **alpha in 2026**, beta in
2027, stable/general public release in 2028. \[VENDOR CLAIM/project material, corroborated
by Wikipedia and multiple 2026 tech-press pieces\]

- In **June 2026** the project stopped accepting external public pull requests ahead of
  its alpha, moving to maintainer-controlled development. \[reported by a secondary
  aggregator (braindetox.kr), not independently re-verified against Ladybird's own
  announcement in this scan\]
- **Agent/automation fit**: Ladybird is being built as a **general-purpose end-user
  browser** (a Chrome/Firefox alternative for humans), not a headless/automation-first
  tool. No source found in this scan mentions CDP support, a headless mode, or any
  agent/automation-specific design goal.
- **Disqualifier**: **Not headless/agent-focused, no CDP surface, and pre-alpha** (target
  alpha is later in 2026, with Linux/macOS only even then). Wrong category for this
  scan's purpose — included for completeness since it is the most talked-about
  "new browser engine" of the period, but it does not fit the "lightweight agent browser"
  brief at all today.

---

## 8. "Fastio Agent Browser" (fast.io)

**What it is**: A product mentioned by the company **fast.io** (a cloud-storage-for-AI-
agents vendor) describing "Fastio Agent Browser" as "built specifically for AI agents"
with a CLI and MCP interface, an `@ref`/snapshot element-reference system, session
persistence, and integration with the company's own storage workspace.

- **Engine**: **Not disclosed anywhere found in this scan.** No mention of Chromium,
  Playwright, a custom engine, or any JS engine. [UNKNOWN]
- **Source availability**: **No GitHub repository found.** All discoverable material is
  fast.io's own marketing/resource content, and fast.io's own "best headless browsers for
  AI agents" roundup article ranks its own product within the list — i.e., the only
  sources found are self-promotional SEO content, not independent coverage or a public
  code repository. \[MEASURED: the self-referential nature of the sourcing was directly
  observed\]
- **Disqualifier**: **Insufficient independent information to evaluate at all** — no
  confirmed engine, no confirmed license, no code repository, no independent write-up.
  Cannot be assessed as a real engineering candidate from what is publicly discoverable;
  treat as marketing-stage/closed until a primary technical source (repo, docs with
  engine specifics) surfaces.

---

## Also considered and set aside as out-of-category

- **Steel.dev / Browserbase / Anchor Browser** — all confirmed to run on **Chromium**
  under the hood (Steel.dev explicitly ships "Stealth Browser," described directly as "a
  Chromium fork," hardened/stripped down but still Chromium). \[VENDOR CLAIM per Steel's
  own blog, as relayed by search\] These are cloud/infrastructure plays around Chromium,
  not alternative lightweight engines, so they do not answer this scan's brief (a second,
  non-Chromium engine) even though they are agent-market-relevant.
- **Servo / Verso** (the general browser-shell project on top of Servo, distinct from the
  narrower `servo-fetch` tool above) — Servo itself reached a "stable 0.1.0" release on
  2026-04-13 per a secondary source; Verso is a minimal browser shell on top of it, not
  itself agent/automation-focused. Not pursued as a separate candidate beyond
  `servo-fetch`, which is the concrete agent-oriented packaging of the same engine found
  in this scan.
- **PandaScript / Lightpanda Agent** — not a separate engine, it is Lightpanda's own
  higher-level automation/scripting layer on top of the Lightpanda engine (2026-06-17
  blog post). Folded into the Lightpanda entry above rather than treated separately.

---

## Sources

- https://medium.com/@dibishks/meet-lightpanda-the-headless-browser-built-for-ai-agents-11-faster-than-chrome-89056d6d69e4
- https://lightpanda.io/blog/
- https://docs.bswen.com/blog/2026-03-19-what-is-lightpanda-browser/
- https://wavect.io/blog/lightpanda-headless-browser-ai-agents/
- https://roundproxies.com/blog/lightpanda/
- https://roundproxies.com/blog/lightpanda-errors/
- https://www.ruh.ai/blogs/lightpanda-ai-native-browser-powering-ai-employees
- https://www.scrapingbee.com/blog/lightpanda-headless-browser/
- https://dev.to/stevengonsalvez/lightpanda-a-browser-engine-built-for-agents-not-humans-49o4
- https://github.com/lightpanda-io/browser
- https://github.com/lightpanda-io/browser/releases
- https://news.ycombinator.com/item?id=42817439 (Show HN thread, referenced via secondary aggregator, not fetched directly)
- https://fast.io/resources/best-headless-browsers-ai-agents/
- https://github.com/h4ckf0r0day/obscura
- https://github.com/NousResearch/hermes-agent/issues/15445
- https://techcrunch.com/2026/08/07/cloudflare-launches-kitesurf-a-browser-built-for-ai-agents/
- https://blog.cloudflare.com/kitesurf/ (referenced via TechCrunch summary and secondary aggregators; not fetched directly)
- https://www.explainx.ai/blog/cloudflare-kitesurf-agent-browser-v8-isolates-august-2026
- https://www.compsmag.com/blogs/cloudflare-kitesurf/
- https://github.com/boa-dev/boa
- https://boajs.dev/
- https://gitnation.com/contents/building-a-javascript-engine-in-rust-lessons-from-boa
- https://github.com/konippi/servo-fetch
- https://en.wikipedia.org/wiki/Servo_(software)
- https://news.ycombinator.com/item?id=41215727 (Verso HN thread, referenced, not fetched directly)
- https://htmlunit.sourceforge.io/
- https://en.wikipedia.org/wiki/HtmlUnit
- https://ladybird.org/
- https://github.com/LadybirdBrowser/ladybird/blob/master/Documentation/FAQ.md
- https://braindetox.kr/en/posts/ladybird_browser_development_changes_2026.html
- https://en.wikipedia.org/wiki/Ladybird_(web_browser)
- https://steel.dev/blog/stealth-browser
- https://github.com/steel-dev/steel-browser
- https://techxplore.com/news/2025-09-fraudsters-fake-stars-game-github.html
- https://news.ycombinator.com/item?id=42540182 (4.5M suspected fake GitHub stars discussion)
