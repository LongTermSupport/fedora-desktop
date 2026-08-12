# Scan: Terminal browsers and agent-oriented scraping CLIs with JS

Researched 2026-08-12. Focus: does the tool run its own JS/DOM engine that is
genuinely lighter than Chromium, or is it a Chromium/WebKit/Firefox wrapper
wearing a lightweight costume? Star counts and push dates below are pulled
live from the GitHub REST API (`api.github.com/repos/...`) on 2026-08-12 and
are [MEASURED]; anything sourced from a vendor blog/README is flagged
[VENDOR CLAIM]; anything I could not pin down is [UNKNOWN — not established].

---

## Lightpanda (lightpanda-io/browser)

**What it is:** A headless browser written from scratch in **Zig**, purpose-built
for AI agents/automation. [Source: GitHub README](https://github.com/lightpanda-io/browser)

- **JS engine:** Google **V8**, embedded directly (not via a Chromium build) —
  confirmed in the repo's own tech-stack description ("Zig v0.15.2, libcurl for
  HTTP, html5ever for HTML parsing, v8 for JS execution"). \[VENDOR CLAIM,
  self-reported repo description, but architecturally verifiable — this is not
  disputed language, it's a factual stack list\]
- **Not a Chromium/WebKit fork.** Repo states explicitly: "Not a Chromium fork.
  Not a WebKit patch. A new browser, written in Zig." [VENDOR CLAIM]
- **Rendering:** DOM construction + JS execution only — **no graphical
  rendering engine**, i.e. no CSS layout/paint pipeline. This is the actual
  mechanism behind the "lightweight" claim: it skips the layout/paint/compositor
  stages entirely rather than optimizing them. \[VENDOR CLAIM, architecturally
  consistent with a from-scratch non-Chromium build\]
- **Protocol compatibility:** Implements the Chrome DevTools Protocol (CDP), so
  it claims to be a drop-in target for existing Puppeteer/Playwright scripts.
  [VENDOR CLAIM]
- **MCP support:** Native — stdio and HTTP transports, independent per-agent
  session management. [VENDOR CLAIM, from repo docs]
- **Performance claims:** Vendor says notably lower memory use and notably
  faster execution than headless Chrome, "depending heavily on page/workload
  type," with figures in roughly the single-digit-to-teens multiple range.
  \[VENDOR CLAIM — I found no independent, methodologically-described benchmark
  reproducing these numbers; every such figure I found traces back to the
  project's own materials or blogs paraphrasing them. Treat as unverified.\]
- **Maturity/activity:** [MEASURED via GitHub API] 33,840 stars, last push
  **2026-08-12** (the same day as this research) — very actively developed.
  License: **AGPL-3.0** [MEASURED]. Still labeled **Beta** by the project
  itself; a third-party review site (roundproxies.com) independently reports
  it as buggy in places and unable to defeat sites with heavy anti-bot/
  fingerprinting checks, "since Lightpanda cannot mimic Chrome's complete
  rendering surface area." \[Source: https://roundproxies.com/blog/lightpanda-errors/
  — this reads as a somewhat promotional/SEO blog rather than a rigorous
  independent test, so treat its specific claims as low-confidence,
  but the general "beta, incomplete Web API coverage, fails on some SPAs"
  characterization matches the project's own stated Beta status.\]
- **Packaging:** prebuilt binaries (Linux/macOS, x86_64/aarch64), Homebrew, AUR,
  Docker images. No native Windows (WSL2 required per vendor docs).
- **Disqualifier:** None fatal, but note: (a) AGPL-3.0 is a copyleft license
  that has real implications for anything that bundles/ships it; (b) Beta
  status with acknowledged incomplete Web API coverage — this is the one
  candidate on this list that is a genuinely new engine rather than a wrapper,
  which is exactly the profile requested, but "new" also means less proven.

---

## Carbonyl (fathyb/carbonyl)

**What it is:** A "Chromium running inside your terminal" project — renders
to Unicode block characters + ANSI colors instead of pixels.
\[Source: https://github.com/fathyb/carbonyl]

- **Engine:** **Real Chromium's Blink engine** — described as running "a
  modified version of the Chromium headless shell" with a Rust core
  (`libcarbonyl`) that intercepts the rendering pipeline. This is **not** a
  new lightweight engine; it is full Chromium with its output path redirected
  to a terminal. [Source: project README summary via WebFetch]
- **Disqualifier: this defeats the stated purpose.** It executes JS and
  renders via the actual Blink/V8/Skia stack — so it inherits Chromium's full
  memory/CPU/startup footprint (mitigated somewhat by no GPU/compositor-to-
  screen work, but still a full browser process). Building it from source is
  reportedly a heavy, large-footprint build (tens of GB of disk space quoted
  in project notes) — evidence this is not a lightweight artifact.
  [VENDOR/community-reported build note, via WebFetch summary of README]
- **Maturity:** [MEASURED via GitHub API] 19,336 stars, but last push
  **2024-07-01** — the most recent commit predates this 2026-08-12 research by
  a wide margin. Latest tagged release is v0.0.3 (pre-1.0). License:
  BSD-3-Clause.
- **Verdict for this use case:** interesting engineering, wrong shape — it is
  Chromium, just repainted to a TTY. Disqualifier: **not lightweight; not
  recently pushed to**.

---

## Chawan (bptato/chawan, mirrored to sourcehut-mirrors/chawan on GitHub)

**What it is:** A TUI web browser with a browser engine written from scratch
in Nim, supporting HTTP(S)/FTP/Gopher/Gemini/Finger/Spartan.
\[Source: https://chawan.net/]

- **JS engine:** **QuickJS**, opt-in (JS is off by default; user enables it).
  QuickJS's own regex engine (libregexp) is reused for in-page search.
  [Source: chawan.net docs]
- **CSS/layout:** Real, if partial, CSS layout — "colors, formatting, flow
  layout (block, inline, float, etc.), table layout, flex layout" per the
  project's own feature list. \[VENDOR CLAIM, but plausible given it's a from-
  scratch engine under sustained development\]
- **Headless/agent-friendly mode confirmed:** `cha -d`/`--dump` prints a
  rendered version of all buffers to stdout and exits; this is **automatically
  enabled whenever stdout is not a TTY**, i.e. piping `cha` "just works" as a
  page→text pipeline with no special flag. `-T` sets content type when piping
  input in. A `dump`-vs-`true` distinction exists for whether it waits for all
  scripts/network to finish before dumping. \[Source: chawan.net/doc/cha/cha.html
  man page, via WebSearch summary — this is a real documented CLI behavior\]
- **Maturity/activity:** Primary repo is hosted on **sourcehut**
  (sr.ht/~bptato/chawan), not GitHub; a read-only GitHub mirror
  (`sourcehut-mirrors/chawan`) shows [MEASURED via GitHub API] last push
  **2026-08-11** (the day before this research) — actively developed. License:
  **Unlicense** (public domain) [MEASURED]. Current release is v0.4.4 —
  pre-1.0, i.e. still early/mid-stage by its own versioning. Packaged in
  Alpine, Arch, Debian, FreeBSD, Gentoo, Homebrew, NixOS, Slackware, Void
  Linux repos.
- **Note on stars:** the GitHub mirror shows only ~74 stars, but that's an
  artifact of sourcehut being the canonical home (sourcehut doesn't have a
  GitHub-style star count to compare) — star count is not a meaningful
  popularity signal here either way. \[UNKNOWN — true community size not
  established from star count alone\]
- **Disqualifier:** none fatal. This is a strong candidate: genuinely
  lightweight (Nim binary, no browser-process overhead), real JS engine, real
  (if partial) CSS box model, and a documented non-interactive dump mode built
  for exactly the "pipe page content out as text" use case. Caveats: pre-1.0,
  JS is opt-in per-site/per-run (safe default but means you must explicitly
  flip it on), and CSS/JS coverage is necessarily narrower than a full browser
  engine — some modern JS-heavy SPAs will not render correctly.

---

## Browsh (browsh-org/browsh)

**What it is:** Terminal browser that shows itself as pure text/ANSI art by
driving a **real headless Firefox** and having a WebExtension inside it stream
a serialized frame back to a Go terminal-interface client.
\[Source: https://github.com/browsh-org/browsh]

- **Engine:** **Real Gecko** (Firefox), full HTML5/CSS3/JS/WebGL/video support
  — because it *is* Firefox, just displayed as text. \[VENDOR CLAIM /
  architecturally confirmed — the project's own description is "leverages a
  real headless Firefox for rendering"\]
- **Disqualifier: this defeats the stated purpose, same failure mode as
  Carbonyl.** Prerequisite is "Firefox 57+ already installed" — you are
  paying Firefox's full memory/CPU/startup cost, not a lightweight
  alternative to it. One comparison point found: Carbonyl's own docs claim
  Browsh needs far more CPU power for the same content compared to Carbonyl
  \[VENDOR CLAIM, sourced from a competing project — a self-interested
  comparison, treat with caution, but directionally it lines up with "full
  Firefox process" vs "modified Chromium headless shell"\].
- **Maturity:** [MEASURED via GitHub API] 18,977 stars, last push
  **2025-07-11** — not abandoned but slower-moving than Chawan/Lightpanda/
  katana/jsdom. A secondary source (x-cmd.com install page) separately
  reports the last *stable release* as 1.8.3 dated 2024-01-29 — I did not
  independently re-verify this release date against the GitHub releases page
  in this pass, so treat as [UNKNOWN — not independently re-verified].
  License: LGPL-2.1.
- **Verdict:** disqualified for the same reason as Carbonyl — it's a full
  legacy-engine wrapper (Gecko here, Blink there), not a new lightweight
  engine, despite the terminal-rendering trick making it *feel* lightweight
  to the end user.

---

## eLinks (rkd77/elinks)

**What it is:** Classic text-mode browser, one of the few in that lineage with
any real JS story.

- **JS engine:** Pluggable — supports **mujs**, **QuickJS**, or
  **SpiderMonkey** as build-time options; eLinks 0.16.0rc1 added the mujs
  option as an alternative to SpiderMonkey. \[Source: WebSearch summary of
  GitHub NEWS file / ecmascript.txt doc\]
- **CSS/layout:** "Simplistic... still very much in its infancy" per its own
  documentation — basic attribute mapping (bold/italic → terminal
  attributes), explicitly **no flexbox/real layout engine**. This is
  text-mode rendering with light styling, not a box-model layout engine like
  Chawan's. [Source: WebSearch summary of ArchWiki/dataswamp.org]
- **JS coverage:** "Not complete" — described as covering common cases like
  banking-page redirects rather than full ECMAScript/DOM API surface.
  [VENDOR/community-documented limitation]
- **Maturity/activity:** [MEASURED via GitHub API] 628 stars, last push
  **2026-08-06** — actively maintained, with a stable 0.19.1 release dated
  2026-02-07 per project NEWS. License: GPL-2.0 (per long-standing project
  convention; not independently re-confirmed via API in this pass).
- **Verdict:** viable as a very old, very battle-tested fallback, but weaker
  than Chawan on both CSS and JS/DOM completeness. Include as a candidate,
  not a front-runner — its JS support is explicitly partial and it has no
  documented CSS layout model beyond text attributes.

---

## w3m

- **JS engine:** **None.** w3m itself has no JS support. `w3m-js`, an
  experimental patch that once added JS, is dead — its distribution link is
  broken and it is not available today. \[Source: WebSearch summary of
  SourceForge feature-request thread + w3m Debian wiki\]
- **Disqualifier: JS-less.** Fails the task's hard requirement ("must still
  execute JavaScript"). Included only for completeness/contrast — this is the
  classic "fast terminal browser" people reach for, and it is exactly the tool
  that cannot do what's being asked here.

---

## katana (projectdiscovery/katana)

**What it is:** A Go CLI crawler/spider from ProjectDiscovery (security recon
tooling), with an optional JS-aware "headless mode."
\[Source: https://github.com/projectdiscovery/katana]

- **JS engine (headless mode):** Uses **`chromedp`**, which drives a real
  Chrome/Chromium instance over the Chrome DevTools Protocol — typically the
  `chromedp/headless-shell` image, itself "a lightweight wrapper around
  Chromium's `//content` module" but still full Blink+V8 underneath.
  [MEASURED/confirmed via chromedp's own GitHub README]
- **Disqualifier: Chromium wrapper, same failure mode as Carbonyl/Browsh.**
  Its *default* (non-headless) mode is fast and lightweight precisely because
  it does **not** execute JS or render the DOM — it's a plain Go HTTP crawler.
  The moment you need JS execution (`-headless`), you are back to paying for a
  full Chrome/Chromium process. There is no middle ground here.
- **Maturity:** [MEASURED via GitHub API] 17,306 stars, last push
  **2026-08-11** — very actively maintained. Not disqualified for staleness,
  only for "still just Chromium when JS is needed."

---

## crawl4ai

**What it is:** Python LLM-oriented crawler/scraper framework that outputs
clean Markdown/JSON for RAG/LLM pipelines.

- **JS engine:** Launches a **real Chromium** browser via **Playwright** to
  render JS-heavy SPAs (React/Vue/etc.) before extraction.
  \[Source: WebSearch summary of ScrapingBee/Better Stack/docs.crawl4ai.com
  guides — consistent across multiple independent write-ups\]
- **Disqualifier: Chromium/Playwright wrapper.** Excellent for the
  extraction/Markdown-output side of the problem (which is a genuinely useful
  separate capability), but the JS-rendering engine underneath is exactly the
  full Chromium the user wants to get away from. Its lightness is in the
  *output* (clean Markdown, adaptive crawl-stopping), not in the *rendering
  engine*.

---

## Firecrawl (self-hosted)

**What it is:** Open-source scrape/crawl API that outputs LLM-ready Markdown;
self-hostable minus the proprietary cloud "Fire-engine" anti-bot layer.

- **JS engine:** Self-hosted stack bundles a **Playwright** service
  (`playwright-service`) alongside API server / Postgres / Redis / RabbitMQ —
  again, **real Chromium** under Playwright. \[Source: WebSearch summary
  across thunderbit.com review, webscraping.ai FAQ, firecrawl.dev blog\]
- **Disqualifier: Chromium/Playwright wrapper**, plus a **five-service Docker
  stack** (API, Playwright worker, Postgres, RabbitMQ, Redis) — the opposite
  of "lightweight" operationally, independent of the rendering engine choice.
  AGPL-3.0 license on the self-hosted code is an additional consideration
  (copyleft — same family of concern as Lightpanda's license, but Firecrawl
  also carries far more infrastructure weight).

---

## Jina Reader (jina-ai/reader, OSS)

**What it is:** Open-source engine behind `r.jina.ai` — prefix any URL to get
back clean Markdown; self-hostable Docker image (`ghcr.io/jina-ai/reader:oss`).

- **JS engine:** Uses a **headless Chrome** browser to fetch/render the page,
  then applies Mozilla's Readability algorithm to extract main content.
  [Source: WebSearch summary consistent across multiple independent reviews]
- **Disqualifier: Chromium wrapper**, and the OSS Docker image additionally
  bundles **LibreOffice** (for document conversion) and CJK fonts — a heavy,
  multi-purpose image, not a lean JS-execution primitive.
- **Maturity:** [MEASURED via GitHub API] 11,853 stars, last push
  **2026-05-22** — actively maintained, Apache-2.0 license. Not disqualified
  for staleness or license, only for engine choice.

---

## jsdom (jsdom/jsdom)

**What it is:** A pure-JS/Node.js implementation of DOM/web-platform standards
— not a "browser" in the executable-CLI sense, but a library many scraping
pipelines use as the lightweight JS-execution layer.

- **JS engine:** Runs scripts using Node's own **V8** (inline `<script>` tags
  execute automatically; external scripts can be loaded and run too).
  [Source: WebSearch summary of jsdom.org docs]
- **Rendering:** DOM-only — **no CSS layout/paint at all**. Explicitly stated
  by the project: "does not provide visual rendering." Anything that depends
  on computed layout (e.g., `getBoundingClientRect`, `offsetWidth`, actual
  visibility-based logic) will not behave like a real browser. \[VENDOR CLAIM /
  well-known, long-documented limitation\]
- **Maturity/activity:** [MEASURED via GitHub API] 21,646 stars, last push
  **2026-08-11** — extremely actively maintained. A WebSearch-summarized
  Snyk/libraries.io report cites very high weekly npm download counts;
  \[UNKNOWN — not independently re-verified against npm directly in this pass,
  treat as approximate\]. MIT-licensed (long-standing project convention; not
  re-confirmed via API this pass).
- **Verdict:** Genuinely lightweight and a real (if partial) JS/DOM engine —
  not a Chromium wrapper. Best suited to pages that don't need real layout
  geometry or advanced browser APIs (Canvas, WebGL, many `fetch`/CORS edge
  cases are weak or unimplemented). Not a "terminal browser" or agent CLI on
  its own — it's a library you'd wrap yourself — so it answers "lightweight
  JS+DOM execution" but not "agent-ready CLI/output pipeline" out of the box.

---

## HtmlUnit (HtmlUnit/htmlunit)

**What it is:** A Java "GUI-less browser" library, popular in test-automation
and Java scraping stacks for a long time.

- **JS engine:** **Rhino** (Mozilla's Java-hosted JS engine), specifically the
  project's own maintained fork (`HtmlUnit/htmlunit-rhino-fork`).
  [Source: WebSearch summary of htmlunit.sourceforge.io + rhino-fork repo]
- **Disqualifier-grade limitation:** Rhino's default language level supports
  much of ES6 syntax, but **async/await is not implemented** — a tracked,
  open feature request against Rhino itself, not something HtmlUnit can
  route around. [Source: mozilla/rhino issue #395, via WebSearch summary]
  Given async/await is ubiquitous in modern bundled JS (React/Vue/Next.js
  output, most npm packages transpiled for a modern target), this is a
  serious practical gap for "render a modern JS-heavy SPA" — not a
  theoretical one.
- **Maturity:** [MEASURED via GitHub API] 953 stars, last push
  **2026-08-12** (the same day as this research) — actively maintained; the
  maintainers have reportedly discussed eventually replacing Rhino
  \[secondary source, one of the headless-browser roundup articles — not
  independently verified against a maintainer statement in this pass, so
  treat as [UNKNOWN — not established] pending a primary source\]. No
  screenshot/visual debugging support (strictly headless, no rendering at
  all — text/DOM only, similar category to jsdom but JVM-hosted).
- **Verdict:** real from-scratch-ish engine (not a Chromium wrapper), actively
  maintained, but the async/await gap is a significant, currently-unresolved
  functional disqualifier for a large fraction of real-world modern JS pages.

---

## Ultralight (ultralight-ux/Ultralight)

**What it is:** A lightweight, GPU-accelerated HTML/CSS/JS renderer marketed
at game/app developers wanting to embed web content.

- **Engine:** Based on **WebKit** — so architecturally it *is* a WebKit fork/
  derivative, not a from-scratch engine, despite "lightweight" branding.
  [Source: WebSearch summary of project README/site]
- **License/availability — disqualifier:** **Proprietary/commercial.** Free
  tier only for non-commercial use or indie companies under a defined revenue
  threshold; full source requires a paid commercial license. The AppCore/
  WebCore modules are LGPL and usable if dynamically linked, but the core
  renderer itself is closed-source under the standard license.
  [Source: WebSearch summary of GitHub LICENSE.txt + ultralig.ht site]
- **Verdict:** Disqualified for this use case on **licensing grounds**
  (closed-source core, paid commercial tier) even setting aside that it's a
  WebKit derivative rather than a genuinely new lightweight engine.

---

## Splash (scrapinghub/splash)

**What it is:** A scriptable "browser as a service" with an HTTP API, popular
in the Scrapy ecosystem (`scrapy-splash`) for years.

- **Engine:** Originally official **QtWebKit**; migrated to a maintained
  third-party WebKit fork to escape QtWebKit's own deprecation, described as
  "similar to Safari from mid-2016." \[Source: WebSearch summary of GitHub
  issue #349/#603 discussion\] Either way — **a WebKit fork**, not a new
  lightweight engine, and one whose rendering baseline dates to roughly 2016.
- **Disqualifier: not recently maintained.** [MEASURED via GitHub API] last
  push **2024-08-02**, not archived but with no activity since, well before
  this 2026-08-12 research. 4,190 stars. Combined with the ~2016-era WebKit
  baseline, this is not viable to adopt today regardless of its engine
  lightness.

---

## MCP browser servers (Playwright MCP, Puppeteer MCP, "Browser MCP", Chrome DevTools MCP, Unbrowse)

Surveyed as a category rather than individually, since the pattern repeats:

- **Playwright MCP / Puppeteer MCP / Chrome DevTools MCP:** all drive **real
  Chromium** (or the user's installed Chrome) via CDP — same
  "defeats-the-purpose" shape as Carbonyl/katana/crawl4ai/Firecrawl above.
  \[Source: WebSearch summary of mcp.directory / webfuse.com / agentskillshub
  2026 roundups\]
- **"Browser MCP":** notable exception in mechanism (not in weight) — it
  attaches to the user's **existing** browser via an extension using
  Puppeteer's accessibility-tree data, rather than launching a fresh Chromium
  process. Lighter in the sense of "no second browser process," but it still
  requires a full real browser (with the extension installed) to be running —
  not a standalone lightweight engine, and not usable in a headless
  container with no browser at all. \[VENDOR CLAIM, from its own listing
  description\]
- **Unbrowse:** different approach entirely — discovers and calls a site's
  internal/shadow APIs directly instead of running a browser or JS engine at
  all. **Disqualifier: does not execute JavaScript** by design (that's its
  whole pitch — skip the browser). Fails the task's hard requirement, included
  for contrast only.

---

## Summary table

| Candidate                                | Real new/lightweight engine?                 | JS engine                                     | Disqualifier                                             |
| ---------------------------------------- | -------------------------------------------- | --------------------------------------------- | -------------------------------------------------------- |
| Lightpanda                               | Yes — from-scratch, V8-only, no layout/paint | V8                                            | None fatal; AGPL-3.0, Beta/incomplete API coverage       |
| Chawan                                   | Yes — from-scratch Nim engine                | QuickJS                                       | None fatal; pre-1.0, partial CSS/JS coverage             |
| eLinks                                   | Partial — text-mode only, minimal CSS        | mujs/QuickJS/SpiderMonkey (build-time choice) | Partial JS + no real layout model                        |
| jsdom                                    | Yes, as a library — no layout/paint          | V8 (via Node)                                 | Not a CLI/browser; no CSS layout at all                  |
| HtmlUnit                                 | Yes, JVM-hosted, no visual rendering         | Rhino                                         | No async/await support — breaks on much modern JS        |
| Carbonyl                                 | No — real Chromium/Blink                     | V8 (Chromium's)                               | Defeats purpose (full Chromium); no push since 2024-07   |
| Browsh                                   | No — real Firefox/Gecko                      | SpiderMonkey (Firefox's)                      | Defeats purpose (full Firefox); requires Firefox install |
| katana (headless)                        | No — chromedp → real Chrome                  | V8 (Chromium's)                               | Defeats purpose when JS is needed at all                 |
| crawl4ai                                 | No — Playwright → real Chromium              | V8 (Chromium's)                               | Defeats purpose                                          |
| Firecrawl (self-host)                    | No — Playwright → real Chromium              | V8 (Chromium's)                               | Defeats purpose; heavy 5-service stack; AGPL-3.0         |
| Jina Reader (OSS)                        | No — headless Chrome                         | V8 (Chromium's)                               | Defeats purpose; bundles LibreOffice too                 |
| Ultralight                               | No — WebKit derivative                       | WebKit's JSCore                               | Proprietary/commercial license                           |
| Splash                                   | No — WebKit fork                             | WebKit's JSCore                               | No push since 2024-08; ~2016-era WebKit                  |
| Playwright/Puppeteer/Chrome DevTools MCP | No — real Chromium/Chrome                    | V8 (Chromium's)                               | Defeats purpose                                          |
| w3m                                      | No JS at all                                 | none                                          | JS-less                                                  |
| Unbrowse                                 | No JS at all (by design)                     | none                                          | JS-less (calls internal APIs instead)                    |

**Two candidates stand out as genuinely not Chromium/WebKit/Firefox in
disguise: Lightpanda (V8-only, no layout engine, native MCP, AGPL-3.0, very
actively developed, Beta) and Chawan (QuickJS + real partial CSS layout,
from-scratch Nim engine, Unlicense, actively developed, pre-1.0, documented
headless dump-to-stdout mode).** jsdom and HtmlUnit are real non-Chromium JS
engines too, but are libraries/JVM tooling rather than agent-ready CLIs, and
each carries a specific functional gap (no layout geometry; no async/await,
respectively). This is a factual scan only — no adoption recommendation is
made here.

---

## Sources

- https://github.com/lightpanda-io/browser
- https://lightpanda.io/
- https://news.ycombinator.com/item?id=42812859
- https://news.ycombinator.com/item?id=42817439
- https://roundproxies.com/blog/lightpanda-errors/
- https://www.scrapingbee.com/blog/lightpanda-headless-browser/
- https://dev.to/stevengonsalvez/lightpanda-a-browser-engine-built-for-agents-not-humans-49o4
- https://github.com/fathyb/carbonyl
- https://www.x-cmd.com/pkg/carbonyl/
- https://github.com/browsh-org/browsh
- https://en.wikipedia.org/wiki/Browsh
- https://www.x-cmd.com/install/browsh/
- https://chawan.net/
- https://chawan.net/doc/cha/cha.html
- https://chawan.net/doc/cha/config.html
- https://git.tilde.institute/ahoang/chawan/about/
- https://sr.ht/~bptato/chawan/
- https://github.com/sourcehut-mirrors/chawan
- https://linkedlist.org/2024/11/27/chawan-text-based-browser
- https://github.com/rkd77/elinks
- https://raw.githubusercontent.com/rkd77/elinks/master/NEWS
- https://github.com/rkd77/elinks/blob/master/doc/ecmascript.txt
- https://bbs.archlinux.org/viewtopic.php?id=228455
- https://sourceforge.net/p/w3m/feature-requests/22/
- https://wiki.debian.org/w3m
- https://github.com/projectdiscovery/katana
- https://projectdiscovery.io/blog/introducing-katana-the-best-cli-web-crawler
- https://docs.projectdiscovery.io/opensource/katana/running
- https://github.com/chromedp/chromedp
- https://github.com/chromedp/docker-headless-shell
- https://docs.crawl4ai.com/core/quickstart/
- https://betterstack.com/community/guides/ai/crawl4ai-web-scraping/
- https://www.scrapingbee.com/blog/crawl4ai/
- https://thunderbit.com/blog/firecrawl-review
- https://webscraping.ai/faq/firecrawl/is-firecrawl-open-source-and-can-i-self-host-it
- https://www.firecrawl.dev/blog/best-open-source-web-scraping-libraries
- https://github.com/jina-ai/reader
- https://jina.ai/reader/
- https://jina.ai/news/reader-lm-small-language-models-for-cleaning-and-converting-html-to-markdown/
- https://github.com/jsdom/jsdom
- https://jsdom.org/
- https://security.snyk.io/package/npm/jsdom
- https://htmlunit.sourceforge.io/
- https://github.com/HtmlUnit/htmlunit-rhino-fork/blob/master/RELEASE-NOTES.md
- https://github.com/HtmlUnit/htmlunit/issues/755
- https://github.com/mozilla/rhino/issues/395
- https://github.com/ultralight-ux/Ultralight/blob/master/license/LICENSE.txt
- https://ultralig.ht/
- https://github.com/scrapinghub/splash
- https://splash.readthedocs.io/en/stable/
- https://github.com/scrapinghub/splash/issues/349
- https://github.com/scrapinghub/splash/issues/603
- https://www.unbrowse.ai/blog/best-mcp-server-web-browsing-2026
- https://mcpservers.org/servers/browser-mcp-server
- https://www.webfuse.com/blog/the-top-5-best-mcp-servers-for-ai-agent-browser-automation
- https://mcp.directory/blog/top-puppeteer-mcp-alternatives
- GitHub REST API (`api.github.com/repos/...`) queried directly on 2026-08-12 for: lightpanda-io/browser, fathyb/carbonyl, sourcehut-mirrors/chawan, browsh-org/browsh, jsdom/jsdom, jina-ai/reader, projectdiscovery/katana, scrapinghub/splash, rkd77/elinks, HtmlUnit/htmlunit
