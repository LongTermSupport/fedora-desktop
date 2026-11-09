# Plan 00238 research: telling which Firefox tab is burning CPU or memory

What Mozilla's documentation and source say about each route from outside the Firefox UI to
"which tab is this", and what a test on the host would need. Every fact carries its source.
**Nothing here has been tested on the host yet**: each "Host test" line is a check Phase 1's
plan scripts own (PLAN.md Tasks 1.2 to 1.5), never a command to type by hand.

## The facts this starts from

- **F1.** One Firefox content process held 117-154% CPU and grew from 1.2G to 2.3G RSS in
  about a minute, then settled at 0.5% and 1.3G (the host agent's reading, 2026-10-29). Over
  100% means more than one thread was busy: the page's main thread plus a worker, the garbage
  collector's helpers or media threads, so a sensor that watches only a tab's main thread
  sees part of it at best.
- **F2.** The content process's command line names no site (`-isForBrowser ... tab`); only
  `about:processes` maps a pid to its tabs (host agent).
- **F3.** The host's Firefox is 158.0.1 (Fedora's RPM, installed by `play-firefox.yml`, which
  also deploys `files/etc/firefox/policies/policies.json`). Its `firefox --help` lists
  `--marionette`, `--remote-debugging-port`, `--start-debugger-server` and
  `--remote-allow-system-access` ("Enable privileged access to the application's parent
  process") (host agent).

## The one API that answers the question, and who may call it

`ChromeUtils.requestProcInfo()` returns the parent process and every child with `pid`,
`memory` (bytes), `cpuTime` (ns), its threads, and for a content process its `windows`, each
with `outerWindowId`, `documentURI`, `documentTitle` and `isProcessRoot`
([ChromeUtils.webidl](https://searchfox.org/mozilla-central/source/dom/chrome-webidl/ChromeUtils.webidl)).
That is a pid-to-pages map, which is what `about:processes` renders. `ChromeUtils` is a
`[ChromeOnly]` namespace in the same file: only privileged (browser-internal) JavaScript can
call it, never a web page or an ordinary extension. Every route below is therefore either a
way to run privileged code, or a way to avoid needing it.

## Lead 1: a control port (Marionette, or the Remote Agent with WebDriver BiDi)

**What the docs say.**

- The Remote Agent starts only when Firefox is started with `--remote-debugging-port`
  (default `localhost:9222`). It accepts loopback connections only, by default only requests
  with no `Origin` header and a `Host` that is a loopback address, and **it has no
  authentication and no encryption**. A connected client can "take control of sessions,
  intercept network traffic, and extract data such as cookies"
  ([Remote Agent security](https://firefox-source-docs.mozilla.org/remote/Security.html)).
- By default a client reaches web content only. System access (the chrome context, where
  `ChromeUtils` lives) needs the `--remote-allow-system-access` flag (the security page names
  the `MOZ_REMOTE_ALLOW_SYSTEM_ACCESS=1` environment variable too), which "grants
  unrestricted access to all available Gecko APIs" and is to be used "only when absolutely
  necessary" (same page). The old `remote.system-access-check.enabled` pref was removed in
  Firefox 141 ([WebDriver newsletter 141](https://fxdx.dev/firefox-webdriver-newsletter-141/)).
  Since Firefox 148 BiDi's `browsingContext.getTree` returns chrome contexts given
  `moz:scope: "chrome"` and that flag
  ([WebDriver newsletter 148](https://fxdx.dev/firefox-webdriver-newsletter-148/)).
- Marionette "can control both the chrome ... or the content"
  ([Marionette intro](https://firefox-source-docs.mozilla.org/remote/marionette/Intro.html)),
  and it is switched on only by the start-up flag, not at run time
  ([bug 1169290](https://bugzilla.mozilla.org/show_bug.cgi?id=1169290); old, and to be
  rechecked).
- A browser under remote control shows a striped address bar and a robot icon
  ([bug 1355890](https://bugzil.la/1355890), [bug 1709035](https://bugzilla.mozilla.org/show_bug.cgi?id=1709035),
  [bug 2069726](https://bugzilla.mozilla.org/show_bug.cgi?id=2069726)), and with Marionette
  every page sees `navigator.webdriver === true`
  ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/Navigator/webdriver)). Whether the
  Remote Agent alone sets it too is not in these sources; it needs checking.

**The security cost, weighed.** On the owner's daily, signed-in browser this would be a
listening socket with no authentication that grants every local process running as this
user, and anything else that can reach the host's loopback, full control of the browser:
every cookie and session, every page, and with system access every Gecko API, including the
file system as the user. Mozilla's own threat note is that an attacker with a shell as the
user has already won (same security page), which is true, but this would also turn a
weaker foothold (a process that can open a loopback socket but cannot read the profile, such
as a sandboxed or containerised one whose network can reach the host) into full browser
control. It would also mark every page as automated (`navigator.webdriver`), which sites'
bot checks act on, and stripe the address bar for good.

**Can it be scoped?**

- *A separate profile* removes the risk and the use together: the tabs that run away are in
  the daily profile.
- *On demand* does not work: the port is a start-up flag, so turning it on means restarting
  Firefox, and the restart ends the runaway it was meant to diagnose.
- *Prompted connections* exist for the DevTools server (`--start-debugger-server`), which can
  ask the person to accept each incoming connection; but a person present to click can open
  `about:processes` (Shift+Esc) instead.

**Recommendation: rejected for the daily browser.** Keep it only as a test harness in a
throwaway profile (Task 1.4 uses that to drive a test page).

**Host test needed:** none to reject it. Whether `navigator.webdriver` is set by the Remote
Agent alone can be read from a throwaway profile if it ever matters.

## Lead 2: a WebExtension

**What the docs say.**

- No WebExtension API reports per-tab CPU or memory, and Firefox has no `processes`
  namespace: the API index lists none
  ([MDN WebExtension APIs](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API)).
  Mozilla closed per-tab memory in `about:processes` as WONTFIX
  ([bug 1840473](https://bugzilla.mozilla.org/1840473)). Chrome's `chrome.processes` is not a
  Firefox API (and its channel availability is contested by the sources found:
  [Chromium design doc](https://www.chromium.org/developers/design-documents/extensions/proposed-changes/apis-under-development/processes-api/)).
- `runtime.onPerformanceWarning` exists, with a `tabId`, but its one category is
  `content_script`: a slow content script **of the listening extension**, not a slow page
  ([onPerformanceWarning](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/runtime/onPerformanceWarning),
  [category](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/runtime/OnPerformanceWarningCategory)).
- An ordinary extension **can** act on a tab: `tabs.discard()` unloads one, `notifications`
  raises a desktop notification, and native messaging lets it talk to a program on the host
  that Firefox itself starts, with no listening port (MDN API index above).

**2a. A privileged "experiment" extension.** An experiment API runs as privileged code, so it
could call `requestProcInfo()` and map the bad pid to its tab exactly, then badge or alert on
that tab itself. Whether one may load is decided in `AddonSettings`: experiments are allowed
when the build does not set `MOZ_REQUIRE_SIGNING`, or on Nightly, Developer Edition or
automation; otherwise `EXPERIMENTS_ENABLED` is the constant `false`
([AddonSettings.sys.mjs](https://raw.githubusercontent.com/mozilla-firefox/firefox/main/toolkit/mozapps/extensions/internal/AddonSettings.sys.mjs)).
Out-of-tree privileged extensions cannot be signed by addons.mozilla.org; Mozilla signs them
through a separate pipeline
([WebExtensions basics](https://firefox-source-docs.mozilla.org/toolkit/components/extensions/webextensions/basics.html)).
On a release build with signing required this is closed. **Whether Fedora's build sets
`MOZ_REQUIRE_SIGNING` is the deciding unknown**: not established here (the Fedora spec could
not be fetched). Cost if open: our own code with the browser's full privilege, on the
internal `ChromeUtils` shape, which may change in any Firefox release, and Fedora ships new
Firefox releases promptly.

**Host test (Task 1.2):** read `AppConstants.sys.mjs` out of the installed `omni.ja`,
read-only, and report `MOZ_REQUIRE_SIGNING`, the release channel and the unsigned add-on
scopes. If signing is not required, Task 1.4 loads a minimal experiment that calls
`requestProcInfo()` as a temporary add-on in a throwaway profile.

**2b. An ordinary extension that measures each tab from inside.** A content script in every
tab runs on that tab's main thread, so a repeating timer that fires late measures how starved
that tab's main thread is, by tab, with no privilege and no port. It identifies the tab
directly. What it misses: CPU spent off the main thread (workers, WASM threads, media, the
garbage collector's helpers, which F1 says were in play), memory growth (no per-tab memory
API), and background tabs, whose timers Firefox throttles on purpose, which a sensor must
tell apart from starvation. Its permission cost is a content script on all pages (our own,
small, reviewed code). It needs signing to install on a release build: an unlisted add-on is
signed by addons.mozilla.org through its API, which needs an account and API credentials
(kept in the vault), unless Task 1.2 finds signing not required.

**Joining 2b to the host watcher.** With native messaging the extension can receive the host
watcher's "content process P is running away" and answer with the tabs whose sensors are
starved at that moment: a correlation by time, not a pid lookup, so it can name the likely
tab, not prove it.

**Host test (Task 1.4):** a test page that burns CPU, in a throwaway profile, with the sensor
loaded as a temporary add-on (temporary add-ons need no signing); measure the sensor in a
foreground tab, a background tab, and with the burn moved into a worker.

## Lead 3: what Firefox already does

| Built-in                                         | What it catches                                                                                                                                                                                                                                                                                                   | What it misses                                                                                                                                                                                                                                                                                                      |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Slow-script / process-hang warning               | One script that runs without yielding past `dom.max_script_run_time` (10 s, also the content-process limit since [bug 1123956](https://hg.mozilla.org/mozilla-central/rev/5002e9c06365)); the bar names the page and offers to stop it ([SUMO](https://support.mozilla.org/en-US/kb/warning-unresponsive-script)) | A page that burns CPU in short tasks and keeps yielding (the usual runaway: loops of timers or animation frames, ad and tracker scripts), workers, and memory growth                                                                                                                                                |
| Tab unloading (`browser.tabs.unloadOnLowMemory`) | Low system memory: unloads tabs least recently used first, and with many tabs prefers those using most memory ([Tab unloading](https://firefox-source-docs.mozilla.org/browser/components/tabbrowser/docs/tab-unloading.html))                                                                                    | CPU entirely; it acts only once the system is short of memory. Reported off by default on Linux, with its Linux low-memory detection (PSI) disputed ([bug 1532955](https://bugzilla.mozilla.org/show_bug.cgi?id=1532955), [SUMO thread](https://support.mozilla.org/de/questions/1566403)); to be read on this host |
| `about:processes` (Shift+Esc)                    | Every process with CPU and memory, and its tabs; its kill button ends a process and every tab in it ([SUMO task manager](https://support.mozilla.org/en-US/kb/task-manager-tabs-or-extensions-are-slowing-firefox))                                                                                               | Nothing tells the person to look: it is the answer, not the alert                                                                                                                                                                                                                                                   |
| `about:unloads`                                  | The unload order; in memory-aware mode the pids of each tab's processes (Tab unloading page above)                                                                                                                                                                                                                | UI only, like `about:processes`                                                                                                                                                                                                                                                                                     |

**Host test (Task 1.3):** read `browser.tabs.unloadOnLowMemory` and the slow-script prefs as
this install has them (from the policies file the repo deploys and the profile's `prefs.js`,
read-only), and whether `firefox --new-tab about:processes` opens the task manager in the
running browser (Task 1.5, attended, because it opens a tab in front of the owner).

## Lead 4: contain Firefox instead of identifying the tab

GNOME starts each launched app in its own transient systemd user scope. gnome-session ships
a drop-in for its launched-app scopes, which is the mechanism by which defaults are set on
them ([gnome-session commit](https://lists.gnome.org/archives/commits-list/2020-May/msg03548.html)),
and systemd searches drop-in directories for each dash-truncated prefix of a unit's name
(`foo-bar-.service.d/` for `foo-bar-baz.service`)
([systemd.unit](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html)).
So a drop-in for Firefox's scope name prefix could set `CPUWeight=` (Firefox yields to the
shell and other apps under contention, at no cost when idle), `CPUQuota=` (a hard ceiling,
which slows Firefox even when nothing else wants the CPU) and `MemoryHigh=` (reclaim pressure
above a size; Firefox slows rather than the desktop).

- **Catches:** the harm. One tab cannot starve the desktop, whatever it does.
- **Misses:** the identification. The cap is on all of Firefox, so the other tabs suffer
  with the bad one; and a Firefox started any other way (from a terminal) lands in that
  terminal's scope and escapes it.
- **To check on the host (Task 1.3):** the scope name Firefox actually runs in (from the
  parent process's `/proc/<pid>/cgroup`), that a prefix drop-in reaches a transient app scope
  (`systemctl --user show` on the live scope for the property), and whether Firefox's memory
  pressure reading (system PSI) sees the scope's `MemoryHigh` pressure at all.

## Conclusion

No supported, unprivileged route names the tab from outside the UI. The exact answer needs
privileged code; of the two ways in, the control port is rejected on the daily browser, and
the privileged extension depends on one build fact Task 1.2 reads. So the plan's design
(PLAN.md Decision 1) is layered: a host watcher that names the process and opens
`about:processes` for the person, Firefox's own built-ins tuned, a containment cap so the
desktop survives, and an extension only where Task 1.2 and Task 1.4 show it earns its cost.
