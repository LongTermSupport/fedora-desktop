# Scan: DOM+JS runtimes and slim Chromium builds

Research date: 2026-08-12. Scope: JS-executing DOM implementations without a real
renderer (jsdom, happy-dom, linkedom, HtmlUnit) and slimmed Chromium
distributions (chrome-headless-shell / Chrome for Testing, Playwright's
chromium-headless-shell, browserless), plus one project that surfaced
repeatedly in searches and fits the brief unusually well: Lightpanda.

No verdicts here — this is a fact scan for a later triage step.

---

## 1. jsdom

**What it is**: a JS implementation of the DOM/HTML/CSSOM standards for Node.js.
No rendering, no layout engine, no visual output.

- **JS execution**: [MEASURED/vendor-docs] Executes `<script>` elements by
  default; `runScripts: "outside-only"` exposes `window.eval` to run scripts
  from Node; `resources: "usable"` fetches and runs external `<script src>`.
  Source: jsdom README / npm page.
- **No real layout**: [MEASURED] `getBoundingClientRect()` and friends return
  zero-filled `DOMRect`s — jsdom has no viewport and does not compute layout,
  a design limitation, not a bug to be fixed. Documented across many jsdom
  GitHub issues (#1504, #3178, #3002) and multiple explainer posts.
- **Security posture with untrusted content**: \[MEASURED — from jsdom's own
  SECURITY.md\] Without `runScripts: "dangerously"`, arbitrary HTML does not
  execute script. **With** `runScripts: "dangerously"` (required to actually
  run page JS from a fetched page, i.e. our use case), jsdom's own docs say
  plainly: *"you are effectively running untrusted Node.js code, and your
  machine could be compromised."* jsdom does not claim to sandbox untrusted
  page JS against the host Node process. Source: github.com/jsdom/.github
  SECURITY.md.
- **Maturity/activity**: [MEASURED] Actively maintained; repo showed a commit
  as recently as May 2026 per GitHub. 91.0M weekly npm downloads (PkgPulse,
  2026 comparison piece) — by far the most widely used of the DOM-only libs.
  \[Note: PkgPulse's download/size figures are a third-party aggregator, not an
  npm-official measurement — treat as directionally reliable, not exact.\]
- **Bundle size**: [VENDOR/AGGREGATOR CLAIM, pkgpulse.com] ~1.0 MB gzipped.
- **Debian/install friction**: `<canvas>` support requires the separate
  `canvas` npm package, which is a **native** addon (node-canvas, needs
  `libcairo`, `libpango`, `libjpeg`, `librsvg` at build time on Debian). This
  is an additional apt-layer cost if canvas rendering is wanted; jsdom works
  without it (canvas APIs just throw/no-op). \[MEASURED — well-known node-canvas
  build requirement, not independently re-verified in this scan against
  bookworm specifically.\]
- **Disqualifier for "safely execute arbitrary internet JS"**: none stated by
  the project as a hard blocker, but the project's own security doc amounts to
  "don't do this with content you don't trust" — which is the entire agent
  browsing use case. Not a fatal disqualifier (same caveat applies to a real
  browser's renderer process too, mitigated there by Chromium's own sandbox)
  but a materially different risk profile: jsdom's `vm` context is **not** a
  security boundary in the way a browser process sandbox is.

## 2. happy-dom

**What it is**: a faster, smaller alternative DOM+JS implementation, popular as
Vitest's default test environment.

- **JS execution**: [MEASURED/vendor docs] Executes scripts via a Node.js `vm`
  context, same general approach as jsdom.
- **Performance vs jsdom**: [MIXED — vendor + aggregator claims] "5–10x faster"
  per one source, "2-4x faster" per another (both via pkgpulse.com aggregation
  of Vitest benchmark discussions); the underlying benchmarks are not
  independently reproduced in this scan. Smallest gzip footprint among the
  three DOM libs compared (201.2 KB per pkgpulse, vs jsdom's ~1.0 MB).
  [AGGREGATOR CLAIM]
- **Critical security finding — CVE-2025-61927**: \[MEASURED, multiple
  independent security outlets: GitHub Advisory GHSA-37j7-fg3j-429f, Snyk
  SNYK-JS-HAPPYDOM-13535083, SentinelOne, TheCyberExpress, Endor Labs,
  Bugzilla 2403177\] A **VM context escape → Remote Code Execution**
  vulnerability affecting happy-dom versions ≤19 ("affecting 2.7 million
  users" per GBHackers, a downstream-install-count claim, not independently
  verified here). Root cause: `eval()` was enabled by default inside the VM
  context and several internal functions used `eval()` on page-supplied
  script; a page could walk the constructor chain
  (`this.constructor.constructor('return process')()`) to reach the host
  Node.js process object, `require()`, etc. **Fixed in happy-dom v20**, which
  disables JS evaluation by default and warns when it's re-enabled in
  environments that look insecure. This is directly on-point for a browser
  used to visit arbitrary internet pages: the exact attack surface (untrusted
  page JS escaping to the host process) is the one this project would expose
  by design, and it was a real, high-severity, recently-patched CVE — not a
  theoretical concern.
- **Maturity**: actively maintained (v20 shipped in direct response to the
  CVE); used as Vitest's default environment, so has a large install base.
  [MEASURED — GitHub Advisory + Vitest docs]

## 3. linkedom

**What it is**: a fast, triple-linked-list DOM implementation aimed at
server-side rendering / parsing speed, not full browser emulation.

- **JS execution**: \[MEASURED — from its own docs and third-party comparison
  pieces\] linkedom provides DOM parsing/manipulation only. No search result in
  this scan found linkedom executing arbitrary page `<script>` content itself;
  it is positioned explicitly as *not* a jsdom/happy-dom replacement for
  "simulate a full browser" use cases, and comparison writeups (PkgPulse
  "happy-dom vs jsdom vs linkedom") group it as the DOM-parsing-only member of
  the trio. Combining it with Node's `vm` module to run page JS against its
  DOM would be a DIY integration this scan found no documented, maintained
  example of.
- **Disqualifier**: **no built-in JS execution** — fails the "still executing
  JavaScript" requirement out of the box. Would need to be paired with a
  hand-rolled `vm` sandbox (inheriting the same VM-escape risk class as
  happy-dom pre-v20, with none of the vetting).

## 4. HtmlUnit

**What it is**: a Java "GUI-less browser" — a real, if partial, browser engine
with HTML parsing, CSS, and a JS engine, used from the JVM.

- **JS engine**: [MEASURED] Rhino-based. The project's own roadmap lists
  "Replace the Rhino based JavaScript engine" as a planned major item —
  i.e. even the maintainers consider Rhino a liability.
- **Maturity/activity**: [MEASURED] Actively maintained — latest release found
  was **5.4.0, dated 2026-08-07** (essentially this week), 22,470+ commits.
  This directly contradicts an older secondary claim surfaced in this scan
  ("hasn't been maintained since 2018") from a SEO/scraping-tools roundup
  (nodemaven.com) — that claim appears to be **stale/wrong** and is flagged
  here explicitly rather than repeated as fact.
- **License**: Apache 2.0.
- **Platform requirement**: JDK 17+ for the current 5.x line (4.x exists for
  JDK 8 but is sponsor-maintained-only). This means shipping HtmlUnit into the
  CCY container means shipping a **JVM**, a heavy, unrelated runtime dependency
  for a Node-based container that otherwise only needs npm/Playwright deps.
- **Modern SPA/JS support**: [MIXED] HtmlUnit's own docs claim "fairly good
  JavaScript support... constantly improving" and AJAX-library compatibility.
  Independent 2026 roundups (nodemaven.com, others) are more skeptical,
  characterizing it as having "limited JavaScript execution capabilities and
  no support for modern web applications," calling it a legacy tool relative
  to React/Vue-heavy SPAs. No independent, reproducible test-suite result
  (e.g. against a known SPA corpus) was found in this scan to adjudicate
  between these two claims — recorded as **UNKNOWN, disputed**.
- **Disqualifier candidate**: JVM footprint + disputed modern-JS/SPA fidelity
  - a JS engine (Rhino) the maintainers themselves are trying to replace.

## 5. chrome-headless-shell (Chrome for Testing)

**What it is**: the modern name for the old "headless shell" — a standalone
Chromium binary stripped of the full browser UI/shell layer, distributed
through Google's official **Chrome for Testing** infrastructure since Chrome
120 (chrome-headless-shell as its own downloadable artifact since ~Chrome 132).
Still full Chromium under the hood (`//content` module + Blink + V8) — this is
a **slimmed packaging** of Chromium, not a different engine.

- **JS/DOM fidelity**: [MEASURED — it is Chromium] Full real-world JS
  execution and rendering fidelity, because it *is* Chromium's content module.
  This is the main differentiator vs. every DOM-only library above: it will
  render/execute anything a real Chrome tab would, including canvas, WebGL,
  complex SPA frameworks.
- **What's stripped**: [MEASURED, developer.chrome.com] No X11/Wayland
  requirement, no D-Bus requirement, "substantially fewer dependencies" than
  full Chrome. Positioned by Google as better for automated
  screenshotting/scraping; the *new* headless mode (full Chrome binary in
  headless flag mode) is positioned as more feature-complete/authentic for
  high-accuracy E2E/extension testing.
- **Size**: [UNKNOWN — not established precisely in this scan]. Google's own
  posit.co writeup (2026-04-14) gives only qualitative claims ("smaller to
  download", "fewer dependencies") with **no concrete MB figures**. A full
  Chrome-for-Testing mac-arm64 build was measured elsewhere at ~150.7 MB, but
  that is the *full* browser, not chrome-headless-shell, so it is not a valid
  proxy figure. No independently measured chrome-headless-shell download/
  installed size in MB was found.
- **Memory**: [UNKNOWN, not chrome-headless-shell-specific]. General headless
  Chrome/Chromium RAM figures found in this scan (Medium/dev.to cost-modeling
  posts, not benchmarks) cite ~100–300 MB baseline + 50–200 MB per open page —
  but these are for headless Chrome broadly, not measured against
  chrome-headless-shell specifically, and are themselves secondary blog
  estimates rather than controlled benchmarks.
- **Install**: [MEASURED] `npx @puppeteer/browsers install chrome-headless-shell@stable`,
  or via Chrome for Testing's JSON API endpoints (per-channel: Stable/Beta/
  Dev/Canary). This is the same toolchain already used by
  `agent-browser`/Playwright in the CCY image, so it is low-friction to add.
- **Platform**: ships prebuilt for Linux (glibc-based), macOS, Windows via the
  Chrome for Testing dashboard/`@puppeteer/browsers`.
- **Relationship to what's already in the container**: this is essentially
  "the same Chromium the container already installs for agent-browser, minus
  the parts needed for a headed/GUI session" — it reduces the *runtime*
  footprint per browser instance but does **not** avoid the underlying
  Chromium/V8 dependency chain (~17 apt libs) already paid for headed Chromium
  support, since both need the same shared libraries.

## 6. Playwright's chromium-headless-shell

**What it is**: Playwright's own packaging/distribution of the same
chrome-headless-shell binary described above, used as Playwright's default
headless Chromium backend since v1.45.

- **Relationship to #5**: [MEASURED] Same underlying binary family; Playwright
  just manages the download/version pinning. `playwright install --only-shell`
  installs *just* the shell (skips full Chromium download); `--no-shell` does
  the opposite (skip the shell, keep full Chromium) — both flags exist
  specifically to let a CI/agent setup avoid paying for the browser variant it
  doesn't need. Source: microsoft/playwright GitHub issues (#33885, #33960,
  #33566) and Playwright's own `browsers` docs.
- **Given `agent-browser` in this container is already Playwright-based**,
  this is the lowest-friction option of the "slim Chromium" family: no new
  package manager, no new binary format, just a different install flag against
  tooling already present.
- **Size/memory**: same UNKNOWN as #5 — no independent MB or RSS figures
  found; Playwright's own docs assert it "uses fewer resources" than full
  Chrome headless without quantifying by how much in the sources checked.

## 7. browserless (browserless/browserless)

**What it is**: **not** a lighter engine — a self-hosted Docker **service**
that runs full headless Chrome/Chromium (and other browsers) behind a REST/
WebSocket API, with first-class Puppeteer/Playwright/Selenium integrations.

- **License model**: [MEASURED, docs.browserless.io] Open-source Docker image
  exists, but usable under Server Side License 1.0 terms only for OSS projects
  under a compatible license — not a permissive OSS license for general
  commercial embedding.
- **Weight**: [MEASURED — architectural fact, not a benchmark] It wraps full
  Chrome, so its resource floor is full-Chrome resource floor plus its own
  Node.js orchestration server. Docs explicitly warn to raise Docker
  `shm_size` to 2g "since Docker defaults to 64 MB... which causes Chrome to
  crash under load" — a sign of full-Chrome-class resource needs, not a
  lightweight profile.
- **Disqualifier**: fails the "super lightweight" requirement by construction
  — it is an orchestration/queueing layer around the same heavy engine already
  in the container, not an alternative engine. Relevant only as a "run many
  Chrome instances remotely" pattern, not as a second, lighter engine.

## 8. Lightpanda (lightpanda-io/browser)

**What it is**: a **from-scratch, non-Chromium, non-WebKit headless browser**
written in Zig, using **V8** as its JS engine, purpose-built for AI-agent and
automation workloads. This is the strongest fit for "super lightweight and
agent-optimized, but still executes JS and renders the DOM" found in this
scan.

- **Architecture**: [MEASURED, GitHub repo] Zig for the browser shell/DOM
  implementation, V8 embedded for JS execution, `html5ever` (a Rust HTML5
  parser) for parsing, libcurl for HTTP. No layout/paint engine — it does DOM
  - JS + network, not visual rendering. **Screenshots are explicitly
    unsupported by design**, not a bug — there is no rasterizer.
- **License**: AGPL-3.0. [MEASURED] This is a copyleft license with
  network-use provisions — a materially different legal posture than
  Chromium's BSD-style licensing or the MIT/permissive licensing of jsdom/
  happy-dom/linkedom. Worth flagging explicitly since this repo is public and
  ships to end users.
- **CDP compatibility**: \[MEASURED — project docs + third-party writeups
  (bswen.com, x-cmd.com)\] Implements 17 CDP domains; advertised as a drop-in
  replacement endpoint for Puppeteer/Playwright/chromedp — point existing
  automation at `ws://localhost:9222` instead of a real Chrome. Because
  `agent-browser` in this container is Playwright-based, this is a
  **protocol-compatible drop-in candidate**, in principle, without rewriting
  the agent-browser CLI itself. However: "Playwright dynamically detects
  browser features, and some of its assumptions about Chrome don't match
  Lightpanda yet... basic scripts work, but features that rely on specific
  Chrome internals might fail" — sourced from a third-party hands-on writeup
  (bswen.com), i.e. **partial, not full, drop-in fidelity today**. No
  independently published percentage of "real-world sites that just work" was
  found.
- **Performance claims**: \[VENDOR CLAIM, lightpanda.io/blog + repeated by many
  secondary blogs\] "11x faster than Chrome headless," "9x/16x less memory,"
  "82% lower server cost," ~24 MB per instance vs Chrome's ~207 MB; a specific
  benchmark cites 100 real pages processed in ~5s vs Chrome headless's ~46s on
  an AWS m5.large, peak memory ~123 MB vs ~2 GB. **These are first-party
  numbers.** A search for independent reproduction found none: one summary
  explicitly states *"Independent reviewers have not independently reproduced
  the performance benchmark or audited complete Web API, CDP, security or
  licence compatibility, with benchmark figures and product capability claims
  remaining first-party statements."* Treat the magnitude of the speed/memory
  advantage as **directionally plausible given the architecture (no
  layout/paint/GPU/media stack at all) but numerically unverified by a third
  party** in this scan.
- **Stability/coverage**: [MEASURED, project's own docs + GitHub issues] The
  project describes itself as **beta**: "many websites now work" but
  "stability and coverage are improving"; the team acknowledges crashes are
  still possible on certain pages. Core APIs (XHR, fetch, DOM basics, cookies,
  click, form input) are implemented; the browser does not implement the full
  breadth of "hundreds of Web APIs" a real browser has. Sites using advanced
  anti-bot/fingerprinting techniques are expected to fail because Lightpanda
  cannot mimic Chrome's full rendering/behavioral surface.
- **Debian/Linux install**: [MEASURED] Official Docker image builds
  `FROM debian:stable-slim` for the runtime stage (multi-stage build); glibc-
  linked binaries (fail on musl/Alpine without a glibc layer — explicitly not
  a fit for an Alpine-based image, but fine for our Debian bookworm base).
  Prebuilt nightly binaries for `linux-x86_64` and `linux-aarch64` are
  downloadable directly (`curl -L -o lightpanda .../lightpanda-x86_64-linux`),
  no npm package was found — it is a **standalone native binary**, not an npm
  dependency, so it wouldn't be `npm install`-able the way agent-browser's
  Chromium is; it would be fetched as a binary/Docker layer in the Ansible
  playbook instead.
- **Activity**: [MEASURED] 33.8k GitHub stars, 1.6k forks, thousands of
  commits, nightly release cadence — actively developed, not stale. First
  public traction appears to be within 2025–2026 based on the volume of
  2026-dated blog coverage found.
- **Extra features relevant to "agent-optimized"**: \[VENDOR CLAIM, project
  docs\] built-in MCP server, and an "agent mode" that can record a session as
  reusable JavaScript ("PandaScript").
- **No disqualifier found in this scan** other than: (a) beta-grade coverage/
  stability, (b) AGPL-3.0 licensing implications for a public repo shipping a
  container image, (c) no independent (non-vendor) benchmark corroboration,
  (d) no visual/screenshot capability at all (irrelevant if the use case is
  text/markdown extraction, relevant if any task ever needs a screenshot).

---

## Sources

- https://github.com/jsdom/jsdom
- https://www.npmjs.com/package/jsdom
- https://github.com/jsdom/jsdom/issues/1504
- https://github.com/jsdom/jsdom/issues/3178
- https://github.com/jsdom/jsdom/issues/3002
- https://github.com/jsdom/.github/blob/main/SECURITY.md
- https://www.pkgpulse.com/compare/happy-dom-vs-jsdom
- https://www.pkgpulse.com/guides/happy-dom-vs-jsdom-vs-linkedom-dom-simulation-2026
- https://www.pkgpulse.com/guides/happy-dom-vs-jsdom-2026
- https://blog.seancoughlin.me/jsdom-vs-happy-dom-navigating-the-nuances-of-javascript-testing
- https://gbhackers.com/happy-dom-flaw-affecting-2-7-million-users/
- https://www.miggo.io/vulnerability-database/cve/CVE-2025-61927
- https://thecyberexpress.com/critical-cve-2025-61927-vm-context-escape/
- https://www.sentinelone.com/vulnerability-database/cve-2025-61927/
- https://security.snyk.io/vuln/SNYK-JS-HAPPYDOM-13535083
- https://github.com/advisories/GHSA-37j7-fg3j-429f
- https://bugzilla.redhat.com/show_bug.cgi?id=2403177
- https://www.endorlabs.com/learn/happier-doms-the-perils-of-running-untrusted-javascript-code-outside-of-a-web-browser
- https://github.com/WebReflection/linkedom
- https://www.npmjs.com/package/linkedom
- https://github.com/HtmlUnit/htmlunit
- https://nodemaven.com/blog/headless-browser/
- https://developer.chrome.com/blog/chrome-headless-shell
- https://developer.chrome.com/docs/chromium/headless
- https://chromium.googlesource.com/chromium/src/+/lkgr/headless/README.md
- https://github.com/chromium/chromium/blob/main/headless/README.md
- https://opensource.posit.co/blog/2026-04-14_chrome-headless-shell/
- https://rstudio.github.io/chromote/articles/which-chrome.html
- https://www.remotion.dev/docs/miscellaneous/chrome-headless-shell
- https://github.com/microsoft/playwright/issues/33885
- https://github.com/microsoft/playwright/issues/33960
- https://github.com/microsoft/playwright/issues/33566
- https://playwright.dev/docs/browsers
- https://docs.browserless.io/enterprise/open-source
- https://github.com/browserless/browserless
- https://github.com/lightpanda-io/browser
- https://lightpanda.io/docs/
- https://lightpanda.io/blog/posts/from-local-to-real-world-benchmarks
- https://lightpanda.io/blog/posts/cdp-vs-playwright-vs-puppeteer-is-this-the-wrong-question
- https://www.scrapingbee.com/blog/lightpanda-headless-browser/
- https://aiforautomation.io/news/2026-03-16-lightpanda-ai-headless-browser
- https://docs.bswen.com/blog/2026-03-19-lightpanda-cdp-puppeteer-playwright/
- https://docs.bswen.com/blog/2026-03-19-how-to-use-lightpanda-browser/
- https://www.x-cmd.com/install/lightpanda/
- https://roundproxies.com/blog/lightpanda/
- https://roundproxies.com/blog/lightpanda-errors/
- https://industrialmonitordirect.com/blogs/knowledgebase/lightpanda-headless-browser-9x-faster-16x-less-memory-than-chrome
- Medium/dev.to headless-Chrome memory cost-modeling posts (secondary, non-benchmark estimates): https://medium.com/@zlata_18516/headless-chrome-at-scale-cpu-ram-and-cost-optimization-strategies-caea743245c4
