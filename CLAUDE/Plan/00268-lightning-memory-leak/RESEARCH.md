# Plan 00268 research: what Lightning is, what teamchat sends it, and why memory might grow

Every fact is marked **checked** (read in code, a release note or a command's output by this
plan's research) or **unchecked** (reported by someone else, or inferred). Install-specific names
are replaced: "the busy team", "a quieter team", "the laptop".

## 1. The incident, as reported

All **unchecked** here: relayed from the laptop's host seat (its findings report, read
2026-11-26; the raw report lives in the estate's own repository, not here).

| Fact                                                                                     | Source                             |
| ---------------------------------------------------------------------------------------- | ---------------------------------- |
| systemd-oomd killed the busy team's Lightning scope at 2026-11-25 18:08:56Z, usage 84.7G | `journalctl -b -t systemd-oomd`    |
| The scope was `app-flatpak-org.lightning_matrix.Lightning-<n>.scope`, in `app.slice`     | the same                           |
| app.slice memory pressure was 85.55% over 20 s when oomd acted                           | the same                           |
| The instance had run about seven hours                                                   | the seat's reading                 |
| A quieter team's instance, same build: 0.35G RSS after 21 h                              | `ps --sort=-rss`                   |
| The busy team's new instance: 0.57G RSS after 19 min, one sample, so no growth rate      | the same                           |
| Host: 93 GiB RAM, 8 GiB zram swap; no kernel oom-killer lines that boot                  | `free`, `zramctl`, `journalctl -k` |
| Firefox was killed by oomd 15 min earlier (8.5G); the pressure was the Lightning scope's | the oomd lines                     |

What the reported numbers imply (arithmetic on unchecked inputs): 84.7G over about seven hours is
about **12 GiB an hour, about 200 MiB a minute**, on average. The quieter instance grew less than
0.35G in 21 h. The busy instance's growth is at least two hundred times the quieter one's, while
teamchat's send limits (section 3.6) bound a team's agent traffic to a few hundred events a
minute at the very most. So either each event costs the client tens of MiB, or the growth is not
proportional to events at all (a loop that something in the busy team triggered). This is the
single most useful constraint on the hypotheses in section 4.

**Not known** (the laptop commands in [laptop-commands.md](laptop-commands.md) close each):
which Lightning version was running on 25 Nov (0.10.7 reached GitHub on 2026-11-21; when the
Flatpak updated on the laptop is unknown); which process in the scope held the memory; anon heap
or shared memory or file; whether the window was focused, which room was open, whether a thread
panel was open; the busy team's event rate that day.

## 2. Lightning

**Checked** (GitHub API and the source at `lightning-matrix/lightning` `origin/main` `933902d2`,
2026-11-25, read in an untracked clone):

- Repository: <https://github.com/lightning-matrix/lightning> (moved from `Mizerd/lightning`;
  the old URL redirects). GPL-3.0. Flatpak id `org.lightning_matrix.Lightning` (from
  `src-go/internal/matrixclients/clients.json` and the root `org.lightning_matrix.Lightning.yaml`).
- Stack: a native Qt 6 / QML client (C++ about 21 MB of source, QML 5 MB) over the official
  Rust Matrix SDK (`matrix-sdk` 0.18, about 9 MB of Rust in `rust/`), joined by a C FFI
  (`mx_rust_*`). Not Electron, so no V8/Chromium heap; the QML engine has its own JS heap (QV4).
- Contribution route (`CONTRIBUTING.md`): issues and pull requests on GitHub only; development
  is mirrored from the maintainer's private GitLab and GitHub branches are force-pushed, so a PR
  is applied by the maintainer, not merged as-is. A bug report wants the version, platform and a
  log from `lightning-matrix --console --log-file <path>`, never tokens or message contents.
- Our record with this maintainer (Plan 00254 Task 2.4, 2.7): issues #75, #76, #78, #79 fixed
  within a day; #88-#90, #100-#104 handled in 0.10.7.
- Existing upstream issues about memory: **#98** "High CPU/RAM Usage Windows 11" (open, 4 GB and
  lag after weeks of use). The maintainer fixed three search-indexer faults for 0.10.7 and could
  not reproduce the 4 GB; asked for a log with `LIGHTNING_GUI_STALL_TRACE=1` and `--log-file`.
  **#32** "Constantly writing to disk" (closed, 0.10.1: a crypto-store lock renewed many times a
  second, and the indexer walking every room). Nothing else matches "memory". No issue reports
  growth at our scale.
- 0.10.7's own "Memory and speed" notes (release body, checked):
  - the idle trim "never ran in a session without input, and never at all once a message had
    been clicked"; fixed in 0.10.7 (1,158 to 670 MiB in their test);
  - "History you stay scrolled up in is not trimmed (about 22 MiB per 1,000 messages)";
  - the on-disk event cache has no size limit;
  - the 4 GB in #98 was not reproduced.
- `docs/open-items.md` "2026-11-21 OPEN after the RAM and disk audit": source rows cost about
  22 MiB per 1,000; the row window bounds delegates to about 300 rows; the QML engine's
  binding-guard pool keeps its high-water mark (38.5 MB at peak, never returned).
- `docs/timeline-scrolling.md`: before the idle live-edge trim, "a room left open at the live
  edge kept every message it received as a delegate: ~0.5 MB per row ... RSS 721 -> 1510 MB in
  34 min of steady traffic". The trim (`AppController::idleLiveTrimDue`) runs only when: more
  than 400 rows are loaded, the reader follows the latest, no input for two minutes, **the window
  is not active**, no account switch, nothing blocking (viewer, popup, expanded group), and
  **no thread panel or Threads view open** (`historyTrimAllowed`). A thread timeline holds its
  own event-cache subscriber, so the SDK's shrink cannot run while one is open.
- Allocator: glibc, with `M_MMAP_THRESHOLD` pinned at 128 KiB and `M_TRIM_THRESHOLD` 2 MiB
  (`src/app/AllocatorPolicy.h`), and `malloc_trim(0)` after idle (`src/app/IdleTrim.h`: 60 s
  quiet, at most every 5 min, only when 64 MiB or more is free in the arenas).
- Diagnostic switches (environment, read by the app; checked by grep, behaviour not run):
  `LIGHTNING_SCROLL_TRACE` (logs each idle-trim check's inputs), `LIGHTNING_GUI_STALL_TRACE`,
  `LIGHTNING_SYNC_TRACE`, `LIGHTNING_EDIT_TRACE`, `LIGHTNING_RUST_LOG` (a `tracing` filter for the
  Rust side), `LIGHTNING_NO_ROLLING_LOG`; the flags `--console --log-file <path>` (32 MiB, rolls
  to `.1`). Upstream profiles with **heaptrack** (cited in both docs above). Headless runs use
  `QT_QPA_PLATFORM=offscreen` (`docs/build-and-test.md`); `--backend=mock` runs with no server.
- teamchat's rooms are unencrypted (no `m.room.encryption`, DESIGN-v2-A 4.1), and since 0.10.1
  the search indexer walks only encrypted rooms (issue #32's fix), so **the indexer faults of #98
  should not apply to team rooms** (inferred from the release notes, not from code).

## 3. What teamchat sends that a quiet human room does not

**Checked** in `src-go/internal/teamchat/` and Plan 00209's `DESIGN-v2-A-protocol.md` (the
protocol spec the code cites as "A n.n").

### 3.1 Rooms and members

- Three rooms per team, created together: main, reports, questions (A 4.1). A human is in all
  three; every active member in main and reports; every non-parked member in questions.
- Rosters allow up to 16 humans and 64 members (A 11.2). Room version 12, history `shared`, no
  encryption, admin-only state.
- One Lightning instance per team (Plan 00254 D2), so an instance holds one account in three
  rooms. A human quiet room has one or two other people; a team room has every agent seat.

### 3.2 Event shapes

- Only `m.room.message` is open to members (power levels, A 4.2). No reactions, no redactions
  (admin only, rare), no stickers, no custom state from members.
- Every agent event carries exactly one typed key in its content: `teamchat.ping`,
  `teamchat.text`, `teamchat.notice` (main), `teamchat.report`, `teamchat.receipt` (reports),
  `teamchat.question`, `teamchat.thread` (questions); also `teamchat.carried` from the admin
  (`proto/content.go`). Every typed object also carries `"kit"` since Plan 00267 batch 1
  (2026-11-26, on `v2-smoke`; not deployed at the incident).
- Bodies carry `format` / `formatted_body` HTML for rich display (Plan 00241; `richtext`).
- Sizes (A 11.2): any agent content up to 16,384 bytes; ping object 4,096; agent text 4,096;
  human body 16,384; report 1,500; question 8,000; thread post 4,000; headline 120 code points.
- `m.mentions` on agent events (pings mention their addressees; up to 32 for a ping, 16 for
  text, 17 for a notice, 64 for a `moved` notice).
- `msgtype` `m.notice` for notices, receipts, thread notes, closes and retitles; `m.text` for the
  rest.

### 3.3 Threads and replies

- An answer to a ping is a thread reply in the ping's thread (`m.thread` with
  `is_falling_back: true`, A 7.2, `proto.AnswerTo`). So **every answered ping in main is a thread
  root** with at least one reply: main is a room of many small threads, where a human room has
  few.
- Every question in the questions room is a thread root; the coordinator's and humans' posts in
  it are thread replies (A 10.3). Closes are quiet thread posts.

### 3.4 Edits

- Agents never edit, with one exception: the coordinator's **retitle** of its own question, an
  `m.replace` with `m.new_content` (A 7.1 row "edits", 10.4; `engine/questionsverbs.go`), sent on
  `close` and on `retitle`. A human's edit is allowed and delivered as a revision. So edits are
  rare: at most one or two per question.

### 3.5 Receipts, presence, typing

- **`m.read` receipts:** the link posts one per HUMAN item it delivers (`engine/proof.go`
  `receipt`, DV5; `matrix/member.go` POST `/receipt/m.read/<event>`). So each human message
  yields one receipt per agent that delivered it: with k addressed agents, k receipt EDUs reach
  the human's client per human message. Agent messages get none.
- **`teamchat.receipt` events** (A 7.8, Plan 00267, built 2026-11-26, not live at the incident):
  one `m.notice` in the reports room per addressed item per addressee, at each state change
  (delivered, held, bounced, recorded). Once live this multiplies reports-room traffic by the
  number of addressees per item; a direct new load on any client that has the reports room open.
- **Presence:** each link's long-poll `/sync` marks its member online (A 6.6); a clean release
  sets offline; a dead link goes unavailable then offline by the server's timers. So every seat
  start, restart or release is a presence change to every member and human. A link that
  restarts in a loop is a presence storm (link restarts were 0 on the seat the laptop examined;
  **unchecked** for the busy team on 25 Nov).
- **Typing:** teamchat sends none (no typing endpoint in `matrix/`; checked by grep).
- **State events:** only the admin writes state: `teamchat.roster` (main), `teamchat.room` (side
  rooms), `teamchat.colour`, names, topics, power levels. A roster change (member added, parked,
  removed, role) rewrites the roster event and may change power levels and membership: rare,
  bursty at team changes.

### 3.6 Rates

From the roster defaults (A 11.1), per agent sender: a token bucket of `send_burst` 10 refilled
at `send_per_minute` 20; reports at most 6 an hour; receive limit 60 per sender per minute. The
admin and humans are not limited. Upper bound for a team: about 20 events a minute per active
agent, sustained; with ten agents, 200 a minute, plus receipts and presence. Real traffic is far
below that (agents spend most of the time in model turns); the busy team's real rate on 25 Nov
is **unchecked** and is the first number Phase 1 needs.

### 3.7 Sync and reconnect pattern of the links

The links are the senders, not the client, so their sync settings change the client only through
presence (long-poll timeout up to 30 s, `matrix/sync.go`). A link restart re-syncs with a filter
of its own; the human's client sees only the presence change and any notices (`back`,
`disconnected`) the link posts.

### 3.8 Compared with a human room

A quiet human room: few senders, few threads, few receipts, presence for two or three people,
no notices. A team main room: many senders, a thread per answered ping, notices on seat
lifecycle, presence changes on every seat start, `m.read` fan-in on every human message, rich
HTML bodies up to 16 KiB, mentions on every ping. The busy team differs from the quieter team
mostly in volume and in thread count, not in kind (both run the same teamchat).

## 4. Ranked hypotheses

Ordered by how well each fits the one hard constraint (section 1: about 200 MiB a minute on
average, more than two hundred times the quieter instance), then by how cheap it is to test.

### H1. A client-side loop that a teamchat pattern starts, not per-event cost

**Mechanism:** some pattern puts the client into a retry, re-fetch or re-render loop that
allocates on every pass and never frees (a pagination or backfill loop on a room with a gap, a
thread list or Activity re-fetch that never settles, a model reset loop). Candidates named in
upstream's own open items: `mx_rust_fetch_unread_events` has "no per-room in-flight guard"
(`docs/open-items.md`, 2026-11-20 review, L6); the highlight-gap tracker and limited-sync gap
fetches; a thread list re-sort that replaces its whole `QVariantList` model on each change.
**Why unbounded:** a loop allocates at its own rate, independent of how much the team posts, and
the trims never run while it is busy (input or not, the idle trim needs quiet).
**Fits:** the rate (no realistic event rate gives 200 MiB a minute at upstream's measured costs).
**Test:** on the laptop, CPU time of the instance (high and steady), its request rate to the
homeserver (the homeserver's access log or the scope's network counters), and growth while the
team is quiet. In the reproduction: replay a captured busy-team history, then stop all traffic;
growth that continues with no traffic confirms; growth that stops with traffic refutes. Run with
`LIGHTNING_SYNC_TRACE=1` and `LIGHTNING_RUST_LOG=matrix_sdk=debug` to see the repeated request.

### H2. Per-event work that grows with history (superlinear), whose garbage is not returned

**Mechanism:** a model rebuilt from all events on every new event (thread list, Activity rows,
receipt piles, the questions room's many threads), each rebuild allocating in proportion to the
history held, into the QML JS heap or the binding-guard pool, whose high-water mark is never
returned (upstream's own finding for the guard pool).
**Why unbounded:** cost per event rises with events seen, so memory grows as n squared; a busy
room crosses from harmless to explosive in hours, while a quiet one stays flat for days. This
fits the busy/quiet divergence without needing a loop.
**Test:** in the reproduction, drive N events at a fixed rate and plot RSS against N: linear
refutes, quadratic confirms. Then run each event family alone (threads only, receipts only,
mentions only) to find which. Qt's QML garbage-collector statistics (section 7.1) separate JS
heap growth from C++ heap growth; heaptrack names the allocation site.

### H3. The trims never run for a watched team, so every event is kept

**Mechanism:** the idle live-edge trim needs the window inactive and **no thread panel or Threads
view open**. A team's human tends to leave the questions room's thread panel or main's thread
open. Before 0.10.7 the trim also never ran without input or once a message was clicked. Each
received event then stays as an SDK timeline item and a source row (about 22 MiB per 1,000) and
the thread panel's replies may not be windowed (unchecked: the row window is the main timeline's).
**Why unbounded:** nothing drops rows while the blocking condition holds.
**Fits poorly alone:** at 22 MiB per 1,000, 84.7G needs millions of events. Even at the
pre-window 0.5 MB a row it needs about 170,000 rows in seven hours. So H3 is an amplifier of H1
or H2, or a cause only if a per-row cost is far larger for teamchat events (large HTML bodies,
mention pills, receipt piles).
**Test:** `LIGHTNING_SCROLL_TRACE=1` logs every trim check's inputs (`idle live-edge trim check ...`); in the reproduction, the same load with and without a thread panel open, focused and not.
Confirm: growth only with the panel open or the window focused.

### H4. Read-receipt fan-in

**Mechanism:** each human message gets one `m.read` per delivering agent; the client keeps
per-event receipt sets and renders a receipt pile per row; if superseded receipts are kept
rather than replaced, they grow with messages times agents.
**Test:** reproduction with receipts only (a human posts, k fake agents send `m.read`), k from 1
to 20; growth proportional to k times messages confirms.

### H5. Presence churn

**Mechanism:** every seat start, restart or release is a presence change for every member;
a seat in a restart loop sends a stream of them. A client that appends presence history, or
rebuilds the member list per presence event, grows with it.
**Test:** reproduction with presence only (links that go online and offline every few seconds,
no messages). Laptop: count `back`/`disconnected` notices and link restarts in the busy team on
25 Nov.

### H6. Thread-heavy rooms

**Mechanism:** every answered ping is a thread, so main holds hundreds of threads where a human
room holds a few; each thread may hold its own timeline subscriber (an SDK event cache that never
shrinks while subscribed), its own list entry and its root preview.
**Test:** reproduction with many short threads against the same count of plain messages; also
whether thread timelines are dropped after the thread panel closes (heaptrack, or RSS after
opening and closing the panel many times).

### H7. Not message memory at all: graphics or shared memory

**Mechanism:** a scene-graph, texture or dmabuf leak in a window that redraws for every event
(the busy team's window is the one on screen, and it redraws far more often). This is shared or
GPU memory, not anon heap.
**Test:** the cgroup's `memory.stat` (`anon` against `shmem`/`file`), smaps (`/memfd:`,
`/dev/dri`), and `drm-*` lines in `/proc/<pid>/fdinfo`. Mostly `shmem` or drm confirms; mostly
`anon` refutes.

### Ruled unlikely

- **The search indexer** (#98's faults): only encrypted rooms are indexed since 0.10.1 and team
  rooms are unencrypted (inferred, section 2).
- **Edits:** agents send at most one or two retitles per question (section 3.4); too rare to
  explain the rate, unless a single edit starts a loop (then it is H1).

## 5. Reproduction away from the laptop

### 5.1 What exists

- `src-go/internal/teamchat/matrix/tuwunelhs`: starts a **real Tuwunel** for tests built with
  `-tags tuwunel`, from the binary `FAKEHS_TUWUNEL_BIN` names (checked). The homeserver the teams
  really run.
- `src-go/internal/teamchat/matrix/fakehs`: an in-process fake homeserver (rooms, sync,
  receipts, presence, threads; checked). Fine for the generator's unit tests; not for Lightning,
  which needs a real server.
- `src-go/internal/teamchat/member/wiring` e2e tests and `proto.Composer`: compose
  byte-identical teamchat events (pings, answers, notices, reports, questions, thread posts,
  retitles).
- Plan 00254's `Containerfile.build-test` / `build-test.bash`: builds Lightning's non-Rust tree
  in podman on Fedora 44 (QML tests only, `-DLIGHTNING_ENABLE_WEBRTC=OFF`).
- The VM lab (a Fedora desktop VM) for a real Flatpak in a real session.

### 5.2 What must be built

1. **A load generator** (Go, `src-go/`, as all new tooling): signs in as N agent accounts and
   one or two human accounts on a test team (rooms made by the real rooms engine, so state and
   power levels match), and replays teamchat's patterns through `proto.Composer`: pings with
   mentions, thread answers, notices, reports, questions with thread posts and retitles,
   `m.read` receipts on human posts, presence flaps, big HTML bodies. Each pattern is a switch
   with a rate, so one variable changes at a time. A replay mode takes a captured shape (counts
   and timing by type, no content) of the busy team's day.
2. **A test team on Tuwunel**, created by the same admin code as production, with a human
   account for Lightning.
3. **Lightning in a session it can run unattended:** the Flathub Flatpak (the build in use) in
   the VM lab, or headless in a nested compositor (`weston --backend=headless` or a headless
   sway), with a D-Bus session and a Secret Service (gnome-keyring) for the token. Sign-in once
   per instance (the GUI; whether `LIGHTNING_TEST_HOMESERVER/USER/PASSWORD` sign the GUI in is
   unchecked: they are documented for `--rust-sdk-smoke-test`). `QT_QPA_PLATFORM=offscreen`
   would change the "window active" condition of H3, so it is used only as one arm.
4. **A sampler:** every 30 s, the instance's RSS, PSS (`smaps_rollup`), `RssAnon`, the scope's
   `memory.current` and `memory.stat`, CPU time, and the generator's cumulative event counts by
   type, into one CSV per run, so RSS can be plotted against events (H2) and against time (H1).
5. **Arms:** baseline (signed in, no traffic); main pings with answers only; questions threads
   only; receipts only; presence only; big bodies only; many rooms (several teams on one
   account is not our layout; several rooms per team is); each with the window focused, not
   focused, and with a thread panel open.

### 5.3 Profiling Lightning's stack

- **Which allocator site:** heaptrack (upstream's own tool) on a Lightning built from source with
  debug info; inside the Flatpak, `flatpak run --devel --command=sh` gives the SDK runtime where a
  debugger exists (whether heaptrack is in `org.kde.Sdk` is unchecked; the fallback is a native
  build in a podman container from Plan 00254's Containerfile with Rust added).
- **JS heap or C++ heap:** Qt's QML garbage-collector statistics (section 7.1; `QV4_MM_STATS` is
  the Qt 5 spelling).
- **Rust side:** `LIGHTNING_RUST_LOG=matrix_sdk=debug,matrix_sdk_ui=debug` for repeated requests;
  per-thread glibc arenas (many anon mappings of 64 MiB) in smaps point at Rust/tokio threads
  rather than the GUI thread's main heap (`[heap]`).
- **Trim decisions:** `LIGHTNING_SCROLL_TRACE=1`.
- **Stalls and loops:** `LIGHTNING_GUI_STALL_TRACE=1`, `LIGHTNING_SYNC_TRACE=1`, with
  `--log-file <path>` (`--console` only opens a console window on Windows).
- **Without a rebuild:** smaps by mapping, `memory.stat`, `/proc/<pid>/task/*/stat` CPU per
  thread (which thread is busy names the loop's owner: the GUI thread, a QML render thread, a
  tokio worker), and `gcore` of a test instance (never the owner's) for offline inspection.

## 6. What a fix could look like

- **Upstream (most likely):** an issue on `lightning-matrix/lightning` with the measured growth,
  the memory class (anon/shmem), the arm of the reproduction that triggers it, and the generator
  so the maintainer can run it; a pull request where the cause is local (a missing in-flight
  guard, a model rebuild, a trim condition). Upstream's PR route is documented and they have
  taken ours before.
- **teamchat's side, if it is the trigger:** fewer threads in main (an answer as a reply, not a
  thread), batching or dropping `m.read` receipts, not flapping presence, keeping
  `teamchat.receipt` events out of rooms a human opens. Only after the cause is shown; teamchat's
  protocol is not changed on a guess.
- **The stopgap cap** (a systemd scope MemoryHigh 6G / MemoryMax 8G) is in another batch; it
  bounds the damage, not the cause. Whether it stays is decided once the fix is verified.

## 7. Logs and debug modes (owner question Q20)

Read in Lightning's source at `origin/main` `933902d2` and the tags `v0.10.6` and `v0.10.7`
(checked by reading code; none of it run).

### 7.1 Every logging, debug and memory mode Lightning has

**Where output goes by default.** Every Qt message (`qInfo` and above; release builds drop
`qDebug`) is written to **stderr**, through a repeat limiter that folds repeated lines into a
summary (`src/main.cpp`, `installRepeatLimiter`). Where stderr ends up depends on who launched it
(**unchecked**, from how GLib and GNOME Shell launch apps): an instance opened by ccy-teams
inherits the stderr of `gio launch`, which `systemd.LaunchClient` points at
`<ccy-teams state dir>/lightning-<team>.log`, appended for ever with no cap; one opened from the
GNOME app grid writes to the user journal. So **some log of the killed instance may already
exist**, holding Lightning's ordinary `qInfo` lines, among them the `idle trim:` line each time
the allocator trim runs (`resident <before> -> <after> MB`), which is a memory reading.

| Mode                                                                 | What it writes                                                                                                                                                                                                                                | Where                                                                                                            | Cost                                                                                                       | Privacy                                                                                                                       |
| -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| default                                                              | `qInfo` and warnings, repeat-limited; `idle trim:` lines with RSS before and after                                                                                                                                                            | stderr                                                                                                           | none extra                                                                                                 | user ids and paths; the code's own rule is no message content, keys or tokens                                                 |
| `--log-file <path>`                                                  | a mirror of the Qt messages; Rust tracing is NOT mirrored (it writes to stderr directly)                                                                                                                                                      | the path; 0.10.7+ rolls once to `<path>.1` at 32 MiB (`CappedLogFile`), so 64 MiB at most; **0.10.6 has no cap** | one async write per line                                                                                   | file header: "Matrix user ids and local file paths; never message content, keys or tokens"; created owner-only, refuses links |
| "Keep a diagnostic log" (Settings, About; key `diagnostics/keepLog`) | a rolling copy of the Qt messages                                                                                                                                                                                                             | `<cache>/logs/`, files of 1 MiB, 8 MiB and 16 files at most                                                      | small                                                                                                      | as `--log-file`; the export to share is sanitised again                                                                       |
| `LIGHTNING_SCROLL_MEMORY_TRACE=1`                                    | every 5 s: `scroll-memory rssKiB=… bridgeBytes=… qtPixmapLimitKiB=… rows=… sourceRows=…` (RSS, media cache bytes, delegates, timeline rows held)                                                                                              | stderr and `--log-file`                                                                                          | one `/proc/self/status` read every 5 s; about 120 bytes a line, 17,280 lines a day, about 2 MiB a day      | numbers only                                                                                                                  |
| `LIGHTNING_SCROLL_TRACE=1`                                           | each idle-trim check's inputs (`idle live-edge trim check following= idleMs= windowActive= paneBlocked= popup= switching= rows=`), plus scroll events                                                                                         | stderr and `--log-file`                                                                                          | a line per trim check (120 s or more apart) and per scroll step while the human scrolls (unchecked volume) | numbers and widget class names                                                                                                |
| `LIGHTNING_GUI_STALL_TRACE=1` (or a threshold in ms)                 | `GUI stall <ms> ms category=…` when the GUI thread misses its watchdog beat                                                                                                                                                                   | stderr and `--log-file`                                                                                          | a watchdog thread; a line only on a stall                                                                  | none                                                                                                                          |
| `LIGHTNING_SYNC_TRACE=1` (or a threshold in ms)                      | `SLOW event` lines (an event whose `sdk->bridge->model->ui` total reaches the threshold: 2,000 ms for `=1`, 100 ms at least) and `SLOW sync gap=` lines; the line for every event is a `qCDebug`, compiled out of the Flathub (Release) build | stderr and `--log-file`                                                                                          | a stamp per event; a line per slow event                                                                   | event ids and a room key, no content                                                                                          |
| `LIGHTNING_EDIT_TRACE=1`                                             | edit handling                                                                                                                                                                                                                                 | stderr                                                                                                           | per edit                                                                                                   | event ids (unchecked for content)                                                                                             |
| `LIGHTNING_RUST_LOG=1` or a `tracing` filter                         | the Rust SDK's tracing (`1` = crypto debug, base and sdk info, ui warn); HTTP-layer targets are forced off                                                                                                                                    | **stderr only**                                                                                                  | at `debug` a heavy, unbounded stream                                                                       | ids, and whatever the SDK logs at that level (unchecked); the forced-off HTTP targets keep URLs and tokens out                |
| `QT_LOGGING_RULES` (Qt)                                              | turns on any Qt or Lightning category, e.g. `lightning.e2ee.debug=true`, and Qt's QML GC statistics categories `qt.qml.gc.statistics` / `qt.qml.gc.allocatorStats`                                                                            | stderr and `--log-file`                                                                                          | GC statistics print on every collection: heavy for a day-long run                                          | none                                                                                                                          |

There is **no heap-dump route in Lightning**: it reads `mallinfo2()` and `/proc/self/statm`
only to decide whether to trim, and reports nothing else. A heap profile needs heaptrack or
`gcore` from outside (section 5.3), on a test instance, never the owner's.

### 7.2 What the incident can still tell us

All read-only; the commands are in [laptop-commands.md](laptop-commands.md) section 10.

- **systemd's own accounting of the killed scope.** When a scope stops, the user manager logs a
  line for it, normally "Consumed <cpu> CPU time, <peak> memory peak" (the form varies with the
  systemd version). The CPU time over the instance's life is the best single clue: a loop (H1)
  burns CPU steadily for its whole run; a slow accumulation (H2, H3) does not.
- **systemd-oomd's lines** already read: usage 84.7G, pressure 85.55%. oomd logs no breakdown.
- **Lightning's stderr** for that instance: the ccy-teams log for the busy team, or the user
  journal, from its start to 18:08:56Z. `idle trim:` lines give RSS over time (a growth curve
  for free); `GUI stall` or repeat-limiter summaries point at a loop.
- **Lightning's own files:** a rolling log only if "Keep a diagnostic log" was on; the instance's
  event-cache database and its write-ahead log size and mtime (growth with events kept).
- **Gone for good:** the dead scope's `memory.stat` and smaps. Only the next growth can show
  where the memory is, which is why 7.3 matters.

### 7.3 Recommendation: run the busy team's instance with memory tracing from now on

Yes. As built (Task 1.4, after 7.5 and the lead's decision on the sync threshold):

```text
LIGHTNING_SCROLL_MEMORY_TRACE=1 LIGHTNING_SYNC_TRACE=100
lightning-matrix --log-file <team>/cache/lightning-diagnostic.log
```

This section's first draft named `LIGHTNING_SCROLL_TRACE=1 LIGHTNING_GUI_STALL_TRACE=1` instead.
The stall tracer cannot see a stall that never ends (7.5), so it was dropped; the trim-check
trace was dropped with it, since section 8 moved H3 down. The table below still reads the
memory line.

How it separates them, with laptop-commands.md steps 3 and 6 (the cgroup's `memory.stat`)
sampled beside it:

| Reading                                                                                    | Points at   |
| ------------------------------------------------------------------------------------------ | ----------- |
| `rssKiB` climbs while `sourceRows` stays flat; stall lines; high CPU time                  | H1 (a loop) |
| `rssKiB` climbs faster than `sourceRows`, the cost per row rising with rows                | H2          |
| `sourceRows` climbs and the trim check reads `windowActive= true` or `paneBlocked= true`   | H3          |
| growth in step with the human-message or seat-start counts of the traffic probe (Task 1.3) | H4, H5      |
| growth when a thread panel is open, none when closed                                       | H6          |
| `memory.stat` grows in `shmem` or drm memory while `rssKiB`'s anon part is flat            | H7          |

Cost: about 2 MiB of memory lines per 86,400 s, and a line only for an event slower than 100 ms.
Cap: `--log-file` rolls at 32 MiB to one `.1` file, so 64 MiB at most, **on 0.10.7 or later
only** (0.10.6 has no cap: confirm the version first, laptop-commands.md step 1). The same lines
also go to stderr, so the ccy-teams log, which has no cap, grows by the same 2 MiB per 86,400 s; that
log's lack of rotation is logged as Task 1.6. Not recommended live: `LIGHTNING_RUST_LOG` at
debug (heavy, stderr only, uncapped) and the QML GC statistics (a block per collection); both
belong in the reproduction.

### 7.4 The launch change, as configuration

The launcher is the one place every launch goes through: ccy-teams runs `gio launch` on it, and
the app grid uses it. It is written by `src-go/internal/lightning/lightning.go`
(`tree.execLine`), from the team list `play-agent-bus-element.yml` passes on stdin
(`agent_bus_element_profiles`, built from host vars `agent_bus_element_teams`). Today:

```text
Exec=flatpak run --command=env org.lightning_matrix.Lightning XDG_DATA_HOME=<team>/data XDG_CONFIG_HOME=<team>/config XDG_CACHE_HOME=<team>/cache SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=<app>/teamchat-ca lightning-matrix
```

The change: a new optional per-team key `lightning_diagnostics` (boolean, default false) in an
`agent_bus_element_teams` entry. When true, the Exec line becomes:

```diff
-... SSL_CERT_DIR=<app>/teamchat-ca lightning-matrix
+... SSL_CERT_DIR=<app>/teamchat-ca LIGHTNING_SCROLL_MEMORY_TRACE=1 LIGHTNING_SYNC_TRACE=100 lightning-matrix --log-file <team>/cache/lightning-diagnostic.log
```

Files it touches, which is why it is a task and not done here:

- `src-go/internal/lightning/entries.go`: the key in `entryKeys`, a boolean check (L3);
- `src-go/internal/lightning/lightning.go`: `execLine` adds the variables and `--log-file`
  (L8: the launcher is rewritten when its content differs, so turning it on or off rewrites it);
  tests first in `lightning_test.go`;
- `helpers/agent_bus/element.py`: `PROFILE_KEYS` and its check, since the same list is checked
  there and an unknown key is refused; its tests;
- `environment/localhost/host_vars/localhost.yml.dist` and `docs/agent-bus.md`: the key;
- the play passes each whole entry, so the key needs no play change; the watchdog of 7.5 does
  (its user service). Deployed by the play's `agent-bus-lightning` tag through
  `meta-deploy.bash`; it takes effect at the instance's next launch.

### 7.5 Catching the trigger next time (after the new evidence in section 8)

The stall watchdog does **not** catch a stall that never ends: `watchdogLoop`
(`src/app/GuiStallTracer.cpp`, checked) logs "GUI stall" only when the GUI thread beats again,
so the 25 Nov stall would have logged nothing. Two additions to 7.3:

- **A line per slow event:** `LIGHTNING_SYNC_TRACE=100`. The line for every event cannot be
  had from the Flathub build (checked by reading Lightning's source at v0.10.7, not run): the
  manifest builds `-DCMAKE_BUILD_TYPE=Release`, CMakeLists.txt defines `QT_NO_DEBUG_OUTPUT` for
  Release, and the per-event line is a `qCDebug(lcSyncTrace)` (`SyncLatencyTracer.cpp`), so a
  `QT_LOGGING_RULES` rule turning it on does nothing. Only its `qCWarning` lines print: `SLOW event` (the event's id, room key and times, when its total reaches the threshold) and `SLOW sync gap`. `=1` means a 2,000 ms threshold; 100 ms is the lowest it takes (`kMinSlowMs`). An
  event that sets off a GUI-thread runaway is likely to be slow itself, so `=100` names it if it
  reaches the UI at all; the watchdog below covers the stall itself. The memory line
  (`scroll-memory`, a `qInfo` in `src/main.cpp`) survives the Release build, which the
  watchdog's silence test rests on.
- **The log's cap holds only from 0.10.7** (checked by reading source, not run):
  `CappedLogFile` (`src/app/LogExport.cpp`, 32 MiB) rolls once to `<path>.1`; 0.10.6 opens a
  plain file. A non-regular file at the path is written uncapped. Whether the laptop runs
  0.10.7 is **unchecked**: laptop-commands.md step 1 reads it.
- **A watchdog outside the process** (fedora-desktop side, Task 1.7): a user timer that every
  30 s compares the instance's RSS with its last log line; when the log has been silent for
  60 s while RSS rises past a threshold (say 4 GiB), it records once, read-only, the per-thread
  CPU (`/proc/<pid>/task/*/stat`), `wchan`, and a native backtrace of all threads
  (`gdb -batch -p <pid> -ex 'thread apply all bt'`, which needs ptrace for the same user and
  pauses the process briefly; the instance is already lost by then). With the runtime's debug
  extension the GUI thread's frames name the QML engine call. A core dump would show the JS
  objects being allocated but holds message content: only with the owner's consent, kept
  untracked.
  - **As built (Task 1.7):** `agent-bus-lightning watch` (`src-go/internal/lightning/watch`,
    W1-W12), run by the user service `agent-bus-lightning-watch` only while a team has
    `lightning_diagnostics: true`. Every 15 s it reads `/proc`; a Lightning process of this user
    logging under the teams folder whose log has been silent 60 s while its RSS rose 256 MiB
    since the log last moved is recorded once, in `lightning-watchdog.log` beside the log
    (mode 0600, 1 MiB, rolled once): per-thread state, CPU ticks and wchan, then gdb's
    `thread apply all bt` with frame arguments off and no debuginfod (else `eu-stack`). It
    never opens the log or any other file of the instance, and never signals Lightning.
  - **The ptrace restriction (checked in this repository):** `play-self-update.yml` sets
    `kernel.yama.ptrace_scope=1` where self-update is on, so a process may attach only to its
    own descendants; it guards the become password in the running controller. The watchdog is
    a separate user service, not Lightning's parent, so there gdb and eu-stack are refused. The
    record names the scope (W12) and keeps the per-thread state, CPU and wchan lines, which
    need no attach. Lowering the scope while tracing is owner decision D3 (recommended: no).
    The play installs gdb and elfutils while a team is traced.
  - **Unchecked:** that gdb outside the Flatpak sandbox can attach to the sandboxed process and
    resolve its frames where the scope is 0. A failed attach is written into the record (W5),
    so the first record says.

## 8. New evidence (laptop host seat, 2026-11-26 09:17Z) and the re-ranking

Read by the seat from `journalctl --user _PID=<killed instance>` and its 17:56Z capture; relayed,
so **unchecked** here except where marked.

- Lightning's stderr **is** in the user journal (the seat read it by pid), so 7.2's journal route
  is the right one.
- **Not a slow leak.** The `idle trim:` line (every 300 s) shows RSS flat at 607-672 MB from
  13:37Z to 17:09:12Z (last: "resident 664 -> 661 MB"). `ps` read 75 GB at 17:55Z: about 74 GB
  in at most 2,760 s.
- **The event loop stopped at about 17:09-17:10Z.** The search index sweep (every 300 s) last ran
  at 17:05:00Z; the 17:10:00Z run never logged. Nothing was logged from 17:09:12Z until the oomd
  kill at 18:08:56Z. Last lines before: 17:05:00Z sweep (rooms=3 held_events=414), 17:06:16Z
  "rows marked seen by room read state rows=1", 17:09:12Z idle trim.
- **The memory:** 80 GB anonymous private dirty; 153,465 `/memfd:unknown-usage:QtQml` mappings
  and 3,795 JSGCHeap mappings. That is the QML/JS engine's heap, not graphics and not the Rust
  SDK. The seat reads it as one runaway allocation loop on a stuck thread, likely QML/JS
  (its own interpretation, unverified).
- Repeats 11:00-18:10Z (949 lines): 196 `qt.qpa.wayland.textinput disableSurface`; 61
  `TypeError MessageDragSelection.qml: Property 'applyDragSelection' ... is not a function`;
  60/59/54 thread subscription start/stop/close; 43 notification sounds. Nothing at warning
  priority or above.
- One process per instance, no WebView child; 28 threads. Build of the instance running now:
  app-commit `51e71f77662d`, org.kde.Platform 6.11 (Qt 6.11.3), and the seat reads the version as
  0.10.7. Whether the killed process ran the same commit is unchecked.

### 8.1 Hypotheses re-ranked

| Rank | Hypothesis                                                                                                    | Against the new evidence                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ---- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | **H8 (new): a synchronous runaway on the GUI thread, in QML/JS, started by one event shape or one UI action** | fits all of it: both GUI-thread timers stopped at once (event loop blocked), the growth is JS heap (QtQml memfd, JSGCHeap), sudden after 12,800 s flat. Forms: a JS loop whose exit waits on something that cannot change while it runs, collecting into an array; or a handler that re-enters itself through a model or property change (a re-entrant model reset or signal cycle, which Qt's binding-loop check does not catch). Unchecked which. |
| 2    | H1, narrowed to the GUI thread (a teamchat pattern starts the loop)                                           | H1's "allocates at its own rate, independent of traffic" fits; a Rust-side or async loop does not, since it would not stop the Qt timers and the memory is QtQml's                                                                                                                                                                                                                                                                                  |
| 3    | H6 as a trigger: thread panel activity                                                                        | the thread subscription start/stop/close lines (about 60 each) show threads opened and closed all day; a thread event or a thread panel change is a candidate trigger shape                                                                                                                                                                                                                                                                         |
| 4    | Edits as a trigger (was "ruled unlikely")                                                                     | 0.10.7 is new in exactly this area: the thread root and thread list follow edits (#101), edit history shows a word diff. The coordinator's retitle is an `m.replace` of a thread root. One such event is a candidate trigger. Unchecked                                                                                                                                                                                                             |
| 5    | Drag selection                                                                                                | 61 `applyDragSelection is not a function` TypeErrors show the 0.10.7 multi-message selection code meeting a row that lacks the function; a UI action, so it needs the human at the laptop at 17:09Z (unchecked)                                                                                                                                                                                                                                     |
| -    | H2, H3 (slow accumulation)                                                                                    | **down**: RSS was flat for 12,800 s before the stall                                                                                                                                                                                                                                                                                                                                                                                                |
| -    | H4, H5 (receipts, presence)                                                                                   | down as causes; one burst could still be the trigger, but `/messages` cannot show it (bus-host-commands.md)                                                                                                                                                                                                                                                                                                                                         |
| -    | H7 (graphics)                                                                                                 | **refuted** by the seat's capture: anonymous private memory in QtQml mappings, not drm or shared buffers                                                                                                                                                                                                                                                                                                                                            |

Checked in source for the two `while` loops in 0.10.7's `TimelinePane.qml` and `ThreadPanel.qml`:
both are bounded (a binary search; a loop guarded by `guard++ < 64`). Not the culprit; a full
audit of loops and re-entrant handlers in the files the trigger names is Task 3.3.

### 8.2 The window to read

17:05:00Z-17:09:12Z in all three rooms of the busy team. No member can read it (`teamchat history` is capped at 100 recent items). The homeserver admin can, read-only:
[bus-host-commands.md](bus-host-commands.md) (event type, time, typed key, relation, thread root,
size, sender role, redacted and edit flags; no bodies, the token never in argv). Receipts,
typing and presence are ephemeral and not in that history.

The reproduction (section 5) now starts with that window: replay its exact event shapes, in
order and with the same spacing, against a Lightning with a thread panel open, and watch for the
event loop to stop (the 300 s timers go silent) and the JS heap to climb.

### 8.3 The window, read (bus host, 2026-11-26)

The bus host's admin seat read the event shapes of 25 Nov 17:00:00Z-17:15:00Z in the busy
team's three rooms, read-only and with no bodies (the infrastructure repository's report;
relayed, **checked** as that seat's read):

| Room      | Events | Shapes                                                      |
| --------- | ------ | ----------------------------------------------------------- |
| main      | 7      | 5 `teamchat.text`, 2 replies                                |
| questions | 1      | a `teamchat.thread`, with an `m.thread` relation, 17:06:16Z |
| reports   | 0      | none                                                        |

- No edits (`m.replace`), redactions or state events in the window (**checked**, same read).
  Edits as a trigger (8.1 rank 4) find no event here.
- The densest minute is 17:06Z: a worker reply of 7,672 bytes, then within 15 s a
  question-thread post, a notice and a reply (**checked**, same read). The last event is at
  17:07:57Z.
- The laptop's log line "rows marked seen by room read state rows=1" is at 17:06:16Z, the same
  second as the `m.thread` event (**checked**: the laptop log line, section 8, and the bus
  host's read). That one caused the other is **unchecked**.
- Timing (**checked** arithmetic on the times above): the last log line, an idle trim, is at
  17:09:12Z, 176 s after the thread event and 75 s after the last event. Nothing in the window
  arrived at 17:09Z.

**Re-ranked (unchecked; an inference from the read):** H8 stays first, now with a candidate
trigger: a thread event in the questions room together with a read-state change. H6 (thread
activity) rises. Receipts are ephemeral and not in this history, so a receipt burst at 17:09Z
can be neither shown nor ruled out (8.2).

**A sub-hypothesis, H8a (unchecked): deferred work.** No event arrived when the loop stopped,
and the last thing the GUI thread logged is the idle trim at 17:09:12Z. So the trigger may be
work deferred until idle (the trim, or what runs beside it), meeting state the 17:06Z events
left behind, rather than an event's arrival. Test: replay the window, then (a) set off an idle
trim at once, against (b) trims suppressed; the arm that stops the loop names it.

**First replay candidates for Task 2.3**, in this order:

1. the questions-room thread post (`m.thread`) at 17:06:16Z;
2. its read-state marking (the "rows marked seen" path);
3. the 7,672-byte worker reply just before it;
4. then H8a's two arms on the replayed window.
