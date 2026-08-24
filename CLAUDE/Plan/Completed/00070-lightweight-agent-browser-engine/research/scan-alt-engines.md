# Scan: Alternative rendering engines (non-Chromium)

Research date: 2026-08-12. Scope: engines usable headlessly on Linux (target: Debian 12
"bookworm", the CCY container's base OS) that execute JavaScript and perform actual DOM
rendering (CSS layout at minimum), as a second, lighter-weight engine alongside
agent-browser's existing Chromium/Playwright stack.

---

## 1. Playwright-bundled WebKit

**What it is**: Playwright ships a custom-built, patched WebKit binary (not system
WebKitGTK) that it drives via its own remote-debugging-like protocol. agent-browser
already depends on Playwright for Chromium, so adding WebKit is (in principle) `playwright install webkit` on top of an existing install rather than a new toolchain.

- [MEASURED] Independent headless memory benchmark (Python `memory_profiler`, browser
  launched + idle 5s, includes driver + child processes): **Chromium 706 MB, Firefox
  826 MB, WebKit 588 MB**. WebKit was the lightest of the three Playwright-bundled
  engines in this specific test. Source: datawookie.dev, 2025-06-06.
- [MEASURED] Playwright officially lists **Debian 12 "bookworm" (x86_64 and arm64)** as a
  supported OS for Chromium, Firefox, **and WebKit** — i.e. this is the same base OS as
  CCY's `node:lts-slim` image, so no OS-compat gap. Source: microsoft/playwright GitHub
  issue #24028 ("feature: support Debian 12 bookworm").
- [VENDOR CLAIM] "Headless Chromium comes with higher memory overhead compared to
  alternatives like WebKit or Firefox headless" and Playwright's own perf notes describe
  WebKit as adding "13% overhead" vs Chromium on speed (Firefox: 34% slower) — i.e. WebKit
  is memory-lighter but not free; it's a mild speed regression, not a mild speed win.
  Source: Zylos Research 2026 landscape piece / TestDino benchmark blog (both aggregating
  Playwright's own perf claims, not raw independent numbers).
- [MEASURED] JS engine: JavaScriptCore (WebKit's own), not V8 — full modern JS support,
  full CSS layout/paint (it is a real browser engine, headed screenshots work).
- [UNKNOWN — not established] Exact on-disk size of the Playwright WebKit browser bundle
  on Debian bookworm (not found in sources fetched); by comparison the whole set of
  Chromium + Firefox + WebKit browsers Playwright downloads is commonly cited around
  1–1.5 GB combined in various guides, but no authoritative per-browser figure was found
  and independent verification is needed before treating this as fact.

**Packaging**: `npm`/`npx playwright install webkit` + `playwright install-deps webkit`
(apt libs). No separate apt package; it's a Playwright-managed binary download, same
mechanism CCY already uses for Chromium.

**Disqualifier**: none identified. This is the most drop-in-compatible option because the
container already carries the Playwright toolchain and its Chromium apt runtime libs
almost certainly overlap heavily with what WebKit needs (nss/gtk/etc. — not independently
verified per-library).

---

## 2. WebKitGTK / WPE WebKit (+ Cog launcher)

Two related upstream WebKit ports maintained by Igalia, both packaged natively for Debian:

- **WebKitGTK** (`libwebkit2gtk-4.1-0` etc.) — the desktop/GTK-embedding WebKit port.

- **WPE WebKit** (`libwpewebkit-1.1-0`) — a WebKit port built specifically for embedded/
  low-resource devices, with **no GTK/UI-toolkit dependency at all**. Cog
  (`cog`, Igalia/cog) is a minimal single-window launcher/container for WPE WebKit,
  purpose-built for kiosk/headless use.

- [MEASURED] `libwpewebkit-1.1-0` (v2.38.6-1) on Debian bookworm: **22.6 MB download / 83.6
  MB installed** on amd64, with ~40 transitive library dependencies (GStreamer, Cairo,
  OpenSSL, libsoup3, libxml2, libwpe). Source: packages.debian.org/bookworm/libwpewebkit-1.1-0.

- [MEASURED] `wpewebkit-driver` (W3C WebDriver protocol server for WPE WebKit) on
  bookworm: **~330–590 KB download / ~0.9–1.9 MB installed**, i.e. trivial add-on cost once
  the engine itself is installed. It exists specifically to let Selenium-style
  WebDriver clients drive WPE WebKit. Source: packages.debian.org/bookworm/wpewebkit-driver.

- [VENDOR CLAIM] WPE's own FAQ and Igalia materials describe it as built for "low memory
  and storage footprint" with a "minimal set of dependencies," and note a
  `MemoryPressureSettings` mechanism (WPE ≥2.38) to cap the web process under a configured
  limit, plus cgroups-based limiting. This is a design goal statement, not an independent
  measurement.

- [UNKNOWN — not established, but with informal data points] No controlled independent
  benchmark of Cog+WPE memory was found. Anecdotal GitHub discussion/issue reports for
  older Cog/WPE versions describe web-process RSS in roughly the **180–400 MB** range for
  a running kiosk page, with one report of unbounded growth until crash on an unrelated
  older build — treat as informal, version-specific data, not a benchmark.
  Source: Igalia/cog GitHub discussion #724 and issue #693.

- [MEASURED] Headless operation is a first-class supported mode: WPE has a
  `WPE_DISPLAY=wpe-display-headless` backend, and the WebKitGTK/WPE test harness (WPT)
  documents `wpt run --headless wpewebkit_minibrowser` as a supported headless CI path
  with no Xvfb/DRM requirement mentioned. Source: web-platform-tests.org WPE MiniBrowser
  docs; wpewebkit.org FAQ.

- [MEASURED] `cog` v0.16.1-1 is packaged directly in Debian **bookworm main** (not just
  sid), so `apt install cog wpewebkit-driver libwpewebkit-1.1-0` is a plain native install
  on CCY's exact base OS with no backports needed. Source: packages.debian.org/stable/cog.

- [MEASURED] JS/rendering: WPE WebKit is a full modern WebKit fork — JavaScriptCore JS
  engine, full CSS layout and paint, full DOM. This is a real, general-purpose rendering
  engine, not a JS-only shim.

**Disqualifier**: none identified for basic capability. The main open question is how
CDP/automation-protocol driving works in practice for an agent (WebDriver via
`wpewebkit-driver` is the supported path, not CDP/Playwright-style — a different
automation protocol than agent-browser currently speaks, so integration work would be
needed, not just an apt install).

---

## 3. Firefox / Gecko headless

- [MEASURED] In the same Playwright headless benchmark above, Firefox was the **heaviest**
  of the three bundled engines at 826 MB idle — heavier than both Chromium (706 MB) and
  WebKit (588 MB) in that test. Source: datawookie.dev, 2025-06-06.
- [MEASURED] Puppeteer folded Firefox support directly into the main `puppeteer` package
  from v23 onward (the separate `puppeteer-firefox` package is deprecated/gone), and
  targets **stable-channel Firefox**, not a special headless build. Source: Puppeteer
  docs/FAQ, pptr.dev.
- [MEASURED] As of 2025, Selenium 4.29+ has **fully removed CDP support for Firefox**;
  Firefox automation is moving to WebDriver BiDi as the standard cross-browser protocol,
  not Chrome DevTools Protocol. This matters for agent-browser specifically, since it is
  CDP-based (Playwright/Puppeteer-style) — Firefox is drifting further from that protocol
  family, not closer. Source: Selenium 4.29 release notes coverage (infoq/browserstack
  summaries).
- [UNKNOWN — not established] No independent, current (2025–2026) measurement of
  standalone `firefox --headless` CLI memory/startup on Debian bookworm was found; the
  general-purpose desktop RAM comparisons found (Chrome vs Firefox with N tabs open) are
  measuring a different workload (interactive desktop browsing) and are not a reliable
  proxy for single-page headless agent fetches, so they are excluded from the numbers
  above.
- [MEASURED] JS/rendering: SpiderMonkey JS engine, Gecko layout engine — full, mature,
  general-purpose rendering.

**Packaging**: `firefox-esr` is Debian's packaged Firefox channel; also available via
Playwright's bundled download (same mechanism as WebKit above).

**Disqualifier**: none outright, but it is the heaviest of the three mainstream engines in
the one controlled headless benchmark found, and its automation-protocol trajectory (BiDi,
away from CDP) is a growing integration mismatch with agent-browser's CDP-based design.

---

## 4. Servo (via `servo-fetch` third-party wrapper, and native `servoshell`)

- [MEASURED] Servo reached its first tagged **0.1.0 release on 2026-04-13** — i.e. this is
  a very recent milestone (four months old at time of writing), not a long-established
  stable engine. Source: Wikipedia "Servo (software)" citing the release.
- [MEASURED] Servo is governed under **Linux Foundation Europe** with a Technical Steering
  Committee, and lists Futurewei, Igalia, NLnet Foundation, and Sovereign Tech Agency as
  backers — i.e. real institutional backing, not a solo/abandoned project. Source:
  servo.org.
- [MEASURED] `servoshell` (Servo's own reference browser shell) has native headless-window
  support and dedicated screenshot handling (`servoshell::desktop::headless_window`,
  PRs #35377 and #39500 moving/fixing headless screenshot behaviour) — headless is a
  first-party, actively-worked code path, not a hack. Source: doc.servo.org,
  servo/servo GitHub PRs.
- [MEASURED] `servo-fetch` (github.com/konippi/servo-fetch, MIT/Apache-2.0 dual license,
  136 stars / 369 commits at time of writing — a small, young project) wraps Servo to
  fetch+render+extract pages as Markdown/JSON/screenshots. It uses **SpiderMonkey for JS**
  and Servo's own parallel CSS layout engine — i.e. real CSS layout, not DOM-only. It
  ships CLI, Rust, Python (≥3.11), and Node.js packaging.
- [MEASURED] `servo-fetch` explicitly documents that on headless Linux systems it needs
  **`xvfb-run`** plus `libegl1`, `libfontconfig1`, `libfreetype6` — so despite being
  "headless" it still depends on an EGL/GL context, which typically means a virtual X
  display is required in a container with no GPU/display. This is a real integration cost
  for a container like CCY. Source: konippi/servo-fetch README (via GitHub fetch).
- [VENDOR CLAIM/independent build note] A separate community write-up (simonw/research)
  reports building a minimal headless-screenshot CLI directly against the servo 0.1.0
  crate on stable Rust, producing a **153 MB binary** — a rough size data point for a
  from-scratch Servo-based CLI (not `servo-fetch` itself, and not vetted further here).
- [MEASURED] `servo-fetch` explicitly states CAPTCHA-gated sites are unsupported.

**Disqualifier**: none outright, but very young (both Servo 0.1.0 itself and the
`servo-fetch` wrapper), small community (136 stars), and requires `xvfb-run` for headless
operation in a container without a real display — an added dependency CCY's Chromium path
already avoids via ozone/headless fallback.

---

## 5. Ladybird / LibWeb

- [MEASURED] Ladybird's own engine (LibWeb for layout, LibJS for JS, LibWasm, LibGfx) is
  built **entirely from scratch** — not a WebKit/Gecko/Blink derivative. Source:
  ladybird.org, Wikipedia.
- [MEASURED] Public roadmap: **Alpha targeted 2026** (Linux/macOS, developers/early
  adopters only), **Beta 2027**, **general-public stable 2028**. Source: ladybird.org,
  piunikaweb.com coverage of the June 2026 contribution-queue closure ahead of alpha.
- [MEASURED] As of the June 2026 coverage, Ladybird is explicitly described as
  **pre-alpha and not suitable for daily/production use**, "a research project" to be
  evaluated expecting "frequent breaking changes and incomplete standards coverage" until
  the 2026 alpha lands. Source: piunikaweb.com, openreplay.com blog.
- [MEASURED] A LibWeb "ref-test" headless screenshot mode exists in the codebase (used for
  the project's own layout test suite) — so a `--headless` code path exists internally —
  but an open GitHub issue (#3448) documents it has known correctness bugs (screenshots
  taken before CSS-linked resources finish loading). This is a test-harness feature, not a
  documented/stable public CLI for external automation. Source: LadybirdBrowser/ladybird
  issue #3448.
- [UNKNOWN — not established] No public, documented `--headless` CLI flag/workflow for
  driving Ladybird as an automation target (as opposed to running its own internal test
  suite) was found.

**Disqualifier**: **too immature / not yet released.** Explicitly pre-alpha, no stable
public headless-automation interface, roadmap itself says stable is 2028. Not viable to
ship to users today regardless of its architectural interest.

---

## 6. Lightpanda

Included because it's a prominent 2025–2026 "lightweight headless browser for AI agents"
project, but it fails this scan's core requirement (DOM **rendering**, not just DOM+JS) —
flagged clearly below rather than silently excluded.

- [MEASURED] Built from scratch in **Zig**, not a WebKit/Gecko/Blink fork. Uses **V8** for
  JavaScript execution (same JS engine family as Chromium, notably — so it is not a
  "non-Chromium JS engine," only a non-Chromium *renderer*, and in fact it has no renderer
  at all — see next point). AGPL-3.0 licensed. Source: lightpanda-io/browser GitHub repo
  (fetched directly).
- [MEASURED — self-disqualifying for this scan] The project's own repo states explicitly:
  **"No graphical rendering engine"** — it does DOM tree construction and JS execution,
  but has **no CSS layout engine and no paint/visual rendering** at all. It is a
  programmatic DOM+JS execution environment exposed over the Chrome DevTools Protocol, not
  a rendering engine. Source: lightpanda-io/browser GitHub repo.
- [VENDOR CLAIM] "9x faster / 1/16th the memory vs Chrome" across 933 real pages on AWS
  EC2 with 25 concurrent jobs (~215 MB vs ~2 GB for Chrome in that scenario); other vendor/
  press write-ups cite "11x faster." These are the project's own benchmark numbers as
  reported in secondary coverage, not an independently reproduced measurement — labeled
  vendor claim accordingly. Source: ScrapingBee blog, Medium (Byteiota) coverage
  (both citing Lightpanda's own published benchmark).
- [MEASURED] HN "Show HN" discussion (Jan 2025, news.ycombinator.com/item?id=42817439) was
  broadly positive on the Zig/V8 architecture choice, but independent commentary
  explicitly flags: **no pixel rendering** means it is "the wrong tool" for visual
  regression testing, screenshot-based CAPTCHA solving, or any pixel-level agent vision —
  precisely the "rendering the DOM" capability this scan is looking for. Also noted:
  Beta status, "some websites expected to fail or crash due to limited Web API support."
- [MEASURED] CDP-compatible (implements enough of Chrome DevTools Protocol that Playwright/
  Puppeteer clients can point at it by changing the endpoint) — meaning integration effort
  with an existing CDP-based stack like agent-browser would be low *if* rendering weren't
  required.
- [MEASURED] Packaging: Homebrew, AUR (`lightpanda-nightly-bin`), direct binary download,
  and official Docker images (`lightpanda/browser:nightly`) for linux/amd64 and
  linux/arm64 — no apt/Debian-native package.

**Disqualifier**: **no CSS layout or visual rendering at all** — by the project's own
description it is JS-execution-plus-DOM only. Fails the "still... rendering the DOM"
requirement as stated (it builds a DOM but never lays it out or paints it). Worth keeping
on record as the leading example of the "skip rendering entirely" alternative strategy,
but it answers a different question than this scan is asking.

---

## 7. Blitz (Dioxus Labs)

- [MEASURED] Rust engine, **alpha quality**, explicitly "not ready for production usage."
  Provides a modular headless DOM (`blitz-dom`'s `BaseDocument`) plus CSS layout (via the
  `stylo`/Servo-derived style system) and can rasterize to PNG/JPEG/SVG. Source:
  DioxusLabs/blitz GitHub repo, lib.rs crate pages.
- [MEASURED — disqualifying] The project **explicitly defers JavaScript execution** as
  out of scope for now, along with browser-grade network caching, security, and process
  isolation. Source: DioxusLabs/blitz GitHub repo description.

**Disqualifier**: **no JavaScript execution.** Fails the "still executing JavaScript"
requirement outright — it's a headless HTML/CSS renderer for static or Dioxus-driven
content, not a general web-page engine.

---

## Summary table

| Candidate              | Engine origin                   | JS engine        | CSS layout + paint?           | Headless on Debian bookworm              | Packaging                                                    | Maturity                                                                            |
| ---------------------- | ------------------------------- | ---------------- | ----------------------------- | ---------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| Playwright WebKit      | WebKit (Apple-derived)          | JavaScriptCore   | Yes                           | Yes, officially supported OS             | `playwright install webkit` (npm-managed binary)             | Mature, official                                                                    |
| WebKitGTK / WPE + Cog  | WebKit (Igalia embedded port)   | JavaScriptCore   | Yes                           | Yes (`wpe-display-headless`)             | Native apt (`cog`, `libwpewebkit-1.1-0`, `wpewebkit-driver`) | Mature, long-lived                                                                  |
| Firefox/Gecko headless | Gecko                           | SpiderMonkey     | Yes                           | Yes                                      | apt `firefox-esr` or Playwright-bundled                      | Mature; heaviest of the 3 mainstream in one benchmark; CDP support being deprecated |
| Servo (`servo-fetch`)  | Servo (from-scratch, LF Europe) | SpiderMonkey     | Yes                           | Yes, but needs `xvfb-run` in a container | cargo/pip/npm/binary, no apt                                 | Very young (0.1.0, Apr 2026); small wrapper project                                 |
| Ladybird / LibWeb      | From-scratch                    | LibJS            | Yes (internal test mode only) | No stable public headless CLI            | Build from source only                                       | Pre-alpha; disqualified                                                             |
| Lightpanda             | From-scratch (Zig)              | V8               | **No — no rendering at all**  | Yes                                      | Binary/Docker/Homebrew/AUR, no apt                           | Beta; disqualified (no rendering)                                                   |
| Blitz                  | From-scratch (Rust)             | **None — no JS** | Yes                           | N/A                                      | Rust crate only                                              | Alpha; disqualified (no JS)                                                         |

---

## Sources

- https://wpewebkit.org/about/faq.html
- https://github.com/Igalia/cog
- https://github.com/Igalia/cog/discussions/724
- https://github.com/Igalia/cog/issues/693
- https://github.com/Igalia/cog/blob/master/docs/overview.md
- https://web-platform-tests.org/running-tests/wpewebkit_minibrowser.html
- https://packages.debian.org/bookworm/libwpewebkit-1.1-0
- https://packages.debian.org/bookworm/wpewebkit-driver
- https://packages.debian.org/stable/cog
- https://packages.debian.org/source/bookworm/wpewebkit
- https://datawookie.dev/blog/2025-06-06-playwright-browser-footprint/
- https://playwright.dev/docs/browsers
- https://github.com/microsoft/playwright/issues/24028
- https://zylos.ai/research/2026-04-05-browser-automation-ai-agents-2026-landscape/
- https://testdino.com/blog/playwright-cypress-selenium-benchmarks
- https://pptr.dev/faq
- https://pptr.dev/guides/headless-modes
- https://github.com/SeleniumHQ/seleniumhq.github.io/pull/2159/files (Selenium 4.29 Firefox CDP removal coverage)
- https://servo.org/
- https://en.wikipedia.org/wiki/Servo_(software)
- https://book.servo.org/trying/getting-servoshell.html
- https://doc.servo.org/servoshell/desktop/headless_window/index.html
- https://github.com/servo/servo/pull/35377
- https://github.com/servo/servo/pull/39500
- https://github.com/konippi/servo-fetch
- https://github.com/simonw/research/tree/main/servo-crate-exploration
- https://ladybird.org/
- https://ladybirdbrowser-ladybird-72.mintlify.app/introduction
- https://piunikaweb.com/2026/06/05/ladybird-browser-closes-public-contributions/
- https://blog.openreplay.com/ladybird-non-chromium-browser/
- https://github.com/LadybirdBrowser/ladybird/issues/3448
- https://github.com/lightpanda-io/browser
- https://www.scrapingbee.com/blog/lightpanda-headless-browser/
- https://linuxiac.com/lightpanda-promises-a-faster-lightweight-alternative-to-headless-chrome/
- https://news.ycombinator.com/item?id=42817439
- https://news.ycombinator.com/item?id=42812859
- https://github.com/DioxusLabs/blitz
- https://lib.rs/crates/blitz-dom
- https://webengineshackfest.org/2024/slides/blitz_a_truly_modular_hackable_web_renderer_by_nico_burns.pdf
