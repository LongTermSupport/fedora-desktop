# Completeness Critique — Lightweight Agent Browser Engine Research

**Role of this document**: adversarial completeness check on the research folder as it
stood on 2026-08-12 (10 files: `integration-constraints.md`, 4 `scan-*.md`, 5
`candidate-*.md`). No verdict on which engine to adopt is given here — see the task
brief's instruction that this is a later triage step. Every claim below cites the
source file it is drawn from.

---

## 1. Claims that are UNVERIFIED or rest only on vendor marketing

This is not exhaustive — most scan files already self-tag `[VENDOR CLAIM]` — but the
following are the load-bearing ones most likely to leak into a triage decision if not
kept visibly separate from fact:

- **Every Lightpanda performance number** ("9–11x faster," "9–16x less memory," "82%
  lower server cost," "123MB vs 2GB peak," "24MB per instance vs 207MB") traces to
  Lightpanda's own benchmark, run and published by Lightpanda itself. `scan-agent-native.md`
  and `scan-js-runtimes.md` both independently found and stated the same fact: no
  third party has reproduced it, and at least one summary explicitly records
  "Independent reviewers have not independently reproduced the performance benchmark."
  This is the single most consequential unverified number in the whole folder, because
  it is the number that makes Lightpanda look categorically different from everything
  else — and it is disqualified anyway (no rendering), so it never gets tested.
- **Obscura's "~30MB vs 200MB+ for Chrome"** (`scan-agent-native.md`) — no third-party
  benchmark found, and this sits on top of an *unresolved provenance question* (repo
  proliferation, anomalous star velocity) that the scan itself flags as needing a
  legitimacy check before the performance claim is even worth chasing.
- **WPE WebKit "small... as light as possible"** (`candidate-webkitgtk-wpe-cog.md`) —
  the doc explicitly says the one page that historically had Igalia's own WPE-vs-Qt
  memory numbers now 404s, so this candidate's central lightweight claim is **currently
  unmeasurable from any source**, vendor or independent.
- **servo-fetch's "231ms vs 645ms," "51–64MB vs 300–328MB," "word-F1 0.819 vs 0.728"**
  (`scan-alt-engines.md`) — vendor's own numbers, no independent reproduction attempted
  or found.
- **Chrome's own architectural claim that chrome-headless-shell "does not require
  X11/Wayland, D-Bus"** (`candidate-chrome-headless-shell.md`, repeating Google's blog)
  is directly **contradicted by that same research folder's own later measurement**:
  `candidate-chromium-headless-shell-playwright.md` ran `ldd` on the actual binary and
  found it dynamically links `libX11`, `libwayland-server`, `libdbus-1`, and
  `libatk-bridge-2.0` — the vendor claim is about *runtime requirement* (true: no live
  X/D-Bus daemon needed) but is worded as a *link-time dependency* claim (false, per
  measurement). This is flagged inside that one doc but not reconciled back into
  `candidate-chrome-headless-shell.md`, which still states the vendor line at face value.
- **Playwright's own published WebKit size ("~180MB")** is contradicted by
  `candidate-playwright-webkit.md`'s own direct measurement (**293MB** for the current
  build) — flagged as an open, unresolved discrepancy in that same document, not
  investigated further (stale docs? different build flavor excluded from the vendor
  figure? not established).
- **Boa's ">90% Test262 pass rate"** and **Kitesurf's "215,000+ web platform tests
  passing"** (`scan-agent-native.md`) are both self-reported by the respective
  projects, not independently re-run.
- **Chrome/JavaScriptCore Speedometer/JetStream comparisons** cited in
  `candidate-webkitgtk-wpe-cog.md` are Safari-on-macOS numbers being used as a proxy
  for WPE-on-Linux — the doc itself flags this substitution as unverified for the
  actual candidate, which is good practice, but it means **no performance number
  anywhere in the folder is actually about WPE-on-Linux specifically**.
- **"Fastio Agent Browser"** (`scan-agent-native.md`) is *entirely* vendor marketing —
  every source found was fast.io's own content, including a "best headless browsers"
  roundup that ranks fast.io's own unreleased/undisclosed-engine product. Already
  correctly disqualified in the given list, but worth restating: this is the purest
  case of zero-independent-evidence in the folder.

---

## 2. Contradictions between documents

### 2a. The single biggest finding: what `agent-browser` actually is

The task brief (and every scan document that inherited its framing) describes the
CCY container's existing tool as **"agent-browser (npm, Playwright-based Chromium)."**
`integration-constraints.md` builds its entire "every file that must change" analysis
(§4) on that premise — new Dockerfile layer, new driving-layer/skill, new version
bumps, because the existing tool is assumed to have no extension point of its own.

`candidate-chromium-headless-shell-playwright.md` (§"Overlap and complement") checked
this directly against the real upstream project and found the opposite:

> `agent-browser`'s own README states plainly: **"No Playwright or Node.js required for
> the daemon"** — it is a **pure-Rust CLI + daemon** that speaks CDP directly... and
> already has a pluggable `--engine` flag supporting `chrome` (default) and
> **`lightpanda`** — a distinct, non-Chromium engine.

This directly contradicts:

- The task brief's own description of agent-browser.
- `integration-constraints.md` §1 ("Chromium/`agent-browser` stack"), §4 (treats a new
  engine as needing new Dockerfile + new skill + new driving code from scratch — true
  for most candidates, but potentially **false for Lightpanda specifically**, if
  `--engine lightpanda` is already wired and only needs the binary installed).
- `scan-agent-native.md`, `scan-cli-crawlers.md`, and `scan-js-runtimes.md`, which all
  evaluate Lightpanda's "CDP compatibility, drop-in for Puppeteer/Playwright" as a
  **hypothetical integration property** to be built, never checking whether the
  container's actual installed tool already exposes a flag for it.

No document in the folder re-opens this thread once it surfaces in the
chromium-headless-shell-playwright candidate file — it is a one-paragraph aside in a
candidate doc about a *different* engine, not escalated to `integration-constraints.md`
or flagged as a finding that should change how every other candidate's "integration
cost" is scored. This is the most consequential unresolved contradiction in the folder:
it means the stated "every file that must change" cost model in
`integration-constraints.md` may be **wrong for at least one candidate** (Lightpanda),
and nobody has verified it either way.

### 2b. Two deep-dives on what turns out to be nearly the same artifact, with irreconcilable numbers

`candidate-chrome-headless-shell.md` and `candidate-chromium-headless-shell-playwright.md`
were commissioned as two of the five deep-dive slots, apparently on the theory that
"chrome-headless-shell" and "Playwright's chromium-headless-shell" are meaningfully
different candidates. Reading both shows they are **the same Google-published
`chrome-headless-shell` binary**, evaluated via two different distribution channels
(Debian apt package vs. direct Google CDN download) — and the two documents used
incompatible methodologies that produce numbers that don't reconcile:

- `candidate-chrome-headless-shell.md`: desk research only; states RSS, startup
  latency, and concurrency are all **"UNKNOWN — not established"**, and gives Debian
  apt package size as **~87.8 MB download / ~277 MB installed** (headless-shell +
  chromium-common).
- `candidate-chromium-headless-shell-playwright.md`: hands-on measurement in the same
  research session; downloaded the Google CDN artifact directly and measured
  **114.8 MiB compressed / 262 MB unpacked**, plus live RSS (**355MB** for the shell,
  **1,204MB** for full Chrome) and cold-start latency (**0.581s**).

Neither document cross-references the other or notes that they are evaluating the same
underlying binary via different packaging. A reader triaging "chrome-headless-shell as
a candidate" has to manually reconcile two different size figures (87.8MB apt vs
114.8MB direct download) for what is, modulo packaging, the same thing — and would
reasonably wonder why one doc found live-measurable RSS/latency numbers trivial to
obtain in the same afternoon the other declared them unobtainable. This also means the
5-candidate deep-dive cap effectively spent **two of five slots on one engine
family** (both are stock Chromium/Blink/V8), leaving less coverage elsewhere — see §3.

### 2c. Lightpanda maturity: "fabricated-looking" data acknowledged then not corrected everywhere

`scan-agent-native.md` explicitly flags that an earlier fetch of Lightpanda's GitHub
Releases page "returned a fabricated-looking table of '0.3.x' releases dated 2024,"
self-corrects to "nightly-only, no numbered releases," and asks the reader to treat the
version-table data as unreliable. `scan-js-runtimes.md` and `scan-cli-crawlers.md` both
independently describe Lightpanda as actively developed / beta with no numbered
release, consistent with the corrected view — so the *scan* layer is internally
consistent after the self-correction. But the self-correction itself is a documented
instance of a hallucinated-looking source appearing mid-research, and no equivalent
"did we hallucinate anything else" pass was recorded for any other candidate. Worth
naming explicitly as a process gap, not just a resolved data point.

### 2d. Disqualification-list Lightpanda rationale is applied inconsistently in strength

The task's given disqualification note for Lightpanda states flatly it "does not render
the DOM at all." `scan-alt-engines.md` agrees ("no CSS layout engine and no paint/visual
rendering at all... it builds a DOM but never lays it out or paints it"). But
`scan-agent-native.md`'s own Lightpanda entry is more hedged, calling the CDP surface
"\[VENDOR CLAIM, repeated consistently across... write-ups — not independently
re-verified by this scan\]" for the CDP-compatibility claim, while stating the no-render
fact as [MEASURED]. This isn't a true contradiction (all docs agree on the disqualifying
fact), but the *confidence framing* around the surrounding claims (CDP fidelity, MCP
support) varies between "flatly stated" and "hedged as unverified vendor claim" across
the three scan documents that each independently cover Lightpanda — a reader skimming
only one scan file gets a different confidence picture than skimming another.

---

## 3. Whole classes of option never (properly) searched for

- **Hosted/remote browser-as-a-service as a way to avoid touching the container
  image at all.** `scan-agent-native.md` "Also considered and set aside" section
  dismisses Steel.dev/Browserbase/Anchor Browser in one line each, purely on the
  grounds that their engine is Chromium under the hood — i.e. it evaluated them only as
  *engine* candidates and rejected them for not being a new engine. It never evaluated
  the **actually distinct value proposition of a hosted option**: zero container image
  bloat, zero Dockerfile/version-bump surface, no `apt`/build-stage cost at all, because
  the browser process runs on someone else's infrastructure and the container just
  speaks a network protocol to it. That is a genuinely different trade-off axis
  (network dependency + per-request cost + trust/egress concerns, vs. zero local
  footprint) from "which rendering engine is lightest," and the brief's own framing
  ("Think: remote/hosted browser APIs") explicitly anticipated this as a class worth
  checking — it was named and then dismissed without being evaluated on its own axis.
  Cloudflare Kitesurf (`scan-agent-native.md` §2) is the one candidate evaluated
  *as* a hosted service, but only because it currently has *no* self-hosted option
  at all — the broader "is a hosted API the right shape of solution even when
  self-hosting exists" question was never asked for Browserbase/Steel/Anchor.
- **Hosted/managed MCP browser servers** as a sub-case of the above —
  `scan-cli-crawlers.md`'s "MCP browser servers" section (Playwright MCP, Puppeteer
  MCP, Chrome DevTools MCP, Browser MCP, Unbrowse) evaluates only **self-hosted**
  MCP wrappers around a locally-launched Chromium. No hosted-MCP variant (e.g. a
  cloud browser exposed as an MCP endpoint with no local process at all) was
  evaluated as removing the whole "which engine to bundle" question.
- **Tuning/shrinking the browser CCY already ships**, independent of any new
  engine. No document evaluates whether the existing `agent-browser` Chromium's
  *footprint* could be cut materially by configuration alone: resource-type
  blocking (images/fonts/CSS off for text-extraction tasks), `--single-process`,
  disabling extensions/GPU/audio, or simply invoking `agent-browser`'s own
  `--engine chrome` against `chrome-headless-shell` instead of a full headed
  Chrome build (which two of the five deep-dives show is already ~3.4x lighter RSS
  for the *same* engine, per §2b above). This is the most direct "did we search
  for doing less with what we have" gap: two candidate docs independently stumbled
  onto evidence that the answer might already be "yes, tune what's there," but no
  document frames or pursues this as a distinct option in its own right.
- **A genuine cost/baseline measurement of the problem being solved.**
  No document in the folder establishes what `agent-browser install`'s Chromium
  download actually costs today. `integration-constraints.md` §6 states this
  outright: "**UNKNOWN — not established from the repo**... the actual download
  size/count of the Chromium build it fetches is not visible in any repo file... To
  get a real number, the build would need to be run." Every subsequent candidate doc
  compares its own footprint against Chromium in the abstract (17 apt packages, or
  generic "700MB RSS" third-party benchmarks) rather than against CCY's own,
  actually-measured baseline. Without that number, "how much would a second engine
  actually save" cannot be answered even in principle — see §5.
- **Non-headless-browser JS execution sandboxes with a real layout backend other
  than Blitz/Stylo** — e.g. embeddable engines like Deno's `deno_core`/V8-only or
  Bun's JavaScriptCore-only bindings that could pair with a separate layout library.
  Not searched; likely low-value (this is essentially reinventing Blitz or Lightpanda
  by hand) but never named as a considered-and-rejected class the way Blitz/Boa were.
- **"Do nothing"** is named in the task brief as a class to think about but is not
  present anywhere as an explicit, evaluated option with its own write-up — see §5,
  where this is treated as a framing question rather than restated here as a search gap.

---

## 4. Candidates whose key decision criterion is still UNKNOWN, with an exact probe

- **Whether `agent-browser --engine lightpanda` already works in the built CCY
  image today.** This is the single highest-leverage unknown in the whole folder
  (see §2a) — if true, it could collapse most of `integration-constraints.md`'s
  cost model for this one candidate from "new Dockerfile layer + new skill + new
  driving code" down to "install the `lightpanda` binary + document the flag."
  **Exact probe** (run inside a built CCY container, or against a scratch container
  from the same base image):

  ```bash
  agent-browser --help 2>&1 | grep -A 10 -- '--engine'
  agent-browser --engine lightpanda open https://example.com 2>&1
  ```

  If the flag exists but errors because the `lightpanda` binary isn't installed, that
  confirms the wiring exists and only the binary/apt-layer work from
  `integration-constraints.md` §2/§4 is still needed — a materially smaller task than
  currently scoped.

- **chrome-headless-shell / chromium-headless-shell-playwright: the true marginal
  apt-install-size delta on top of the image CCY already builds.** Both deep-dive
  docs explicitly flag this as unresolved (§2b above) — each only measured the
  package/binary in isolation, not against an image that already has agent-browser's
  Chromium deps installed. **Exact probe**: build the actual CCY `base` image (or a
  local stage that replicates its existing `apt-get install` block for Chromium),
  then run:

  ```bash
  apt-get install --dry-run --no-install-recommends chromium-headless-shell 2>&1 \
    | awk '/^Inst /{print}'
  ```

  against that already-Chromium-provisioned image, to see exactly which packages are
  *newly* pulled vs. already satisfied — the real number `integration-constraints.md`
  §6 and both candidate docs say is missing.

- **WebKitGTK/WPE + Cog: whether the container's existing Wayland-socket-only mount
  is sufficient for the `fdo`/EGL backend, or whether the `headless` SHM backend is
  the only viable path in a GPU-less container.** `candidate-webkitgtk-wpe-cog.md`
  states this is "untested, not established" and cites independently-filed issues of
  the `fdo` backend failing outright in GPU-less Docker setups. **Exact probe**
  (inside the CCY container, or a scratch container with the same Wayland-socket bind
  mount CCY uses):

  ```bash
  apt-get install -y cog wpewebkit-driver libwpewebkit-1.1-0
  cog --platform=headless https://example.com 2>&1   # or the fdo default, to compare
  ```

  and check for `EGLDisplay Initialization failed: EGL_NOT_INITIALIZED` (the exact
  error string from the cited WPEWebKit issue #625) vs. a clean render.

- **eLinks: whether `elinks -dump` executes JavaScript to completion before
  producing its dump**, which `candidate-elinks.md` calls "a load-bearing unknown for
  any agent workflow that needs JS-rendered dump output specifically." **Exact probe**
  (reusing the exact test methodology `candidate-chromium-headless-shell-playwright.md`
  already used against chrome-headless-shell, for direct comparability): serve a local
  page whose `<div id="root">` starts as `LOADING` and is rewritten by a `setTimeout`
  callback to a computed value, then:

  ```bash
  elinks -dump -no-numbering http://127.0.0.1:<port>/test.html
  ```

  and check whether the dumped text shows the post-JS value or the pre-JS placeholder.

- **Obscura: whether the star count / repo-proliferation pattern indicates
  inflated/fake popularity**, which `scan-agent-native.md` explicitly declines to
  resolve either way. **Exact probe** (no container needed, pure GitHub API/web):

  ```bash
  gh api repos/h4ckf0r0day/obscura/forks --paginate | jq '.[].full_name' | wc -l
  gh api repos/h4ckf0r0day/obscura --jq '.forks_count, .stargazers_count'
  gh api repos/h4ckf0r0day/obscura/commits --paginate | jq -r '.[].commit.author.email' | sort -u | wc -l
  ```

  compare the API-reported fork count against the actual list of independent
  "obscura[-\_]..." repos already found in the scan, and check commit-author diversity
  (a single-author repo with 21.3k stars in a few months is a different risk profile
  than a multi-contributor one).

---

## 5. Is "add a second engine" even the right shape of answer? (open question, not a recommendation)

Several pieces of evidence gathered across this folder point at the same underlying
tension, without any document stating it directly:

- **The research folder itself demonstrates that "second engine" is not one crisp
  category.** Two of the five deep-dive slots (`candidate-chrome-headless-shell.md`,
  `candidate-chromium-headless-shell-playwright.md`) turned out to investigate a
  *slimmer packaging of the exact engine already in the container*, not an
  architecturally distinct one (§2b). If a "lighter mode of what's already there"
  candidate consumed 40% of the deep-dive budget and came out looking like the
  strongest-evidenced, best-measured, lowest-risk option in the folder (real hands-on
  RSS numbers, no new license, no new automation protocol, prebuilt binaries, actively
  shipped by Google on every Chrome release) — that is itself evidence the answer
  might be "use less of what you have" rather than "add something new."
- **The cost side of the ledger was only worked out for the "new engine" branch.**
  `integration-constraints.md` catalogs, in detail, every file/version-bump/QA gate a
  *new* engine touches (§2–§5: Dockerfile, `claude-yolo` script, a new skill directory,
  the Ansible play, the startup banner, the human docs, potentially `CLAUDE/ContainerRules.md`).
  No equivalent cost was ever computed for the "tune existing Chromium usage" branch
  (§3 above) — so the two branches were never actually compared on cost, only one of
  them was scoped.
- **The benefit side of the ledger is also unmeasured.** As noted in §3/§4,
  `agent-browser install`'s actual Chromium download size, and thus the current real
  pain point, is explicitly "UNKNOWN — not established" per `integration-constraints.md`
  §6. A plan to add a second engine to solve a footprint/startup-time problem whose
  magnitude has never been measured against this specific container is optimizing
  against an assumed problem, not a demonstrated one.
- **The strongest non-Chromium-family candidate (Lightpanda) is disqualified for
  the stated task** (no DOM rendering at all), and the second-strongest
  (Chawan, per `scan-cli-crawlers.md`'s own conclusion — "no CDP/Playwright automation
  surface... pre-1.0... partial CSS/JS coverage") was **not one of the five deep-dive
  candidates**, despite that scan document's own summary table naming it as one of only
  two "genuinely not Chromium/WebKit/Firefox in disguise" options found in the entire
  research pass. That is a visible gap between what the scan layer surfaced as
  interesting and what the deep-dive layer actually investigated.
- **A materially cheaper, zero-new-integration-surface finding surfaced almost by
  accident** (`agent-browser`'s existing `--engine lightpanda` flag, §2a) and was never
  escalated into the framing question at all — if it pans out, it reframes the whole
  plan from "should we integrate a new engine" to "should we flip on integration
  the tool already has and decide whether the Beta-grade, no-rendering Lightpanda
  engine is worth shipping through it," which is a different and smaller decision.

None of this is a recommendation — it is an observation that the evidence gathered so
far has a shape (two "slim the existing engine" deep-dives outperforming on rigor,
an unmeasured baseline problem, an unresolved existing-integration-point discovery,
and the strongest genuinely-new engine being disqualified outright) that the framing
question in the task brief anticipated and that this research pass has not yet
answered either way.
