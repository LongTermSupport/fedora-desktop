# Plan 00202 design review (Fable, adversarial, read-only)

Reviewed: `origin/plan/ccy-claude-code-plugin` at `fe058adc`: `PLAN.md`, `RESEARCH.md`, both
subagent reports and the journal. Checked against the live docs on 2026-10-16: mods
overview, reference, api, events, interface, admin, test; plugins/loading; and
cross-session-messaging. Also read `scripts/git-hooks/pre-commit`, the Dockerfile `COPY`
lines, `play-claude-yolo.yml`, `entrypoint.sh`, `lib/common.bash`, the kit `settings.json`,
`helpers/pingbus/notify.py` and `docs/agent-bus-protocol.md`.

**Verdict: REVISE.** The core recommendation (one `ccy` plugin carrying one hooks module,
loaded in place from a directory marketplace, enforcement left to the hooks-daemon) is
right and almost every cited capability checks out. What needs revising is the section 1
picture of how pingbus delivers today, which is a release behind and distorts the T2/T3
risk and the M4 plan; the "read-only" label on commands whose reply the model reads; the
`early access` citation; the D10 pin that contradicts the image's own auto-update; and the
size of M1.

## 1. Mod or plugin: the terms are used correctly

- The docs define a mod as "a plugin that changes how Claude Code looks and behaves",
  made of JS/TS event handlers; a mod "installs as a plugin, from a marketplace"
  (overview). So "one plugin, `ccy`, with one hooks module" is exactly a mod, and
  RESEARCH §2.2 ("a mod **is** a plugin") is correct. No conflation.
- One hooks module per plugin: reference, Files table, `modules`: "an array with one
  path". Correct.
- The plan does mix the *vocabulary* once: §4.4 lists "the allowed hook set: no
  `tool.call`, `tool.check`, `prompt.submit`, `session.append`, `http.fetch` or
  `env.set`". `http.fetch` and `env.set` are **calls**, not hooks; `prompt.submit` and
  `session.append` are both a hook *and* a call. `claude plugin validate` prints
  `hooks:` and `calls:` as separate lines; the contract test must pin both lines
  separately and the text should say so.

### Claimed capabilities, checked one by one

| Claim                                                                                                | Docs                                                                                                                                                                                                                                                                                                   | Verdict                                                                                                                                                                              |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Status entry under the prompt, one per mod                                                           | api "Show something without starting a turn": `$.ui.status(text)` is "One line under the prompt that stays until you change it. It starts with `⚠` and the mod's name". interface: "one status line per mod". Reference `$.ui` lists `status`.                                                         | **Real.** The claude-code-guide report's UNVERIFIED mark is resolved in the plan's favour. In VS Code it sits above the prompt box (interface), and the panel cuts it at 200 chars.  |
| It is not the settings `statusLine`                                                                  | statusline is a settings key; `$.ui.status` is a mod call                                                                                                                                                                                                                                              | **Real.** T7 (free the single slot) is sound.                                                                                                                                        |
| Band above the prompt                                                                                | render site `AbovePrompt`, Terminal and Desktop only                                                                                                                                                                                                                                                   | **Real.** Not drawn in VS Code or `-p`; the plan already says fall back to text.                                                                                                     |
| "Wake an idle session": `$.prompt.submit`                                                            | api: "waits until the session is idle and then starts a new turn… `asUser: true`" drops the attribution sentence                                                                                                                                                                                       | **Real, but misnamed.** It starts a turn in *this* session; it does not wake another. Rename to "start a turn when idle".                                                            |
| Cross-session messages: `$.session.send`, `session.receive`/`send`                                   | api "Send and receive messages between sessions": `to` is `{sessionId}`, `{agentId}` or the reply address; origin kinds `peer`, `peer-send-message`, `task-notification`, `scheduled-trigger`; "The sender's name on a received message is whatever the sender wrote, so don't base a decision on it." | **Real.** RESEARCH §2.3 lists `coordinator` as an origin kind; it is not in the docs. The quote "never key a guard on it" is a paraphrase presented as a quotation; fix the wording. |
| Containers                                                                                           | cross-session-messaging: "A session inside a container and a session on the host can't reach each other."                                                                                                                                                                                              | **Missing from the research.** Matters for T2 and for D5 (host `cc` and a ccy session never see each other through this channel).                                                    |
| `UserMessage` rows carry `origin` and `from`                                                         | reference render-sites: `UserMessage` props `text`, `origin`, `isExpanded`, and `task` or `from` by origin                                                                                                                                                                                             | **Real.** T3 is feasible for socket-delivered messages (see §3).                                                                                                                     |
| Managed-settings guard (D4 option b)                                                                 | admin: `sec-default@builtin` loads when "The machine has managed settings" or the user is on Team/Enterprise; an org mod needs managed `enabledPlugins` + a directory marketplace by absolute path + relative-path entry; deny rules then bind user mods                                               | **Real.** The research is accurate, including "no guard loads" without managed settings or Team sign-in.                                                                             |
| `$.session.version()`                                                                                | reference `$.session` lists `version`                                                                                                                                                                                                                                                                  | Real.                                                                                                                                                                                |
| Limits (10 s, 4 MiB fs, 30 s process default, 10 min max, 1.5 s SessionEnd, 2,000-char status)       | reference Limits table                                                                                                                                                                                                                                                                                 | All real.                                                                                                                                                                            |
| A thrown hook is skipped unless `.catch` answers; three worker crashes unload every non-built-in mod | events "Handle a hook that fails"; admin                                                                                                                                                                                                                                                               | Real.                                                                                                                                                                                |
| `claude plugin test` needs no session, sign-in or network; exits 1 on failure                        | test page                                                                                                                                                                                                                                                                                              | Real.                                                                                                                                                                                |
| Directory marketplace loads in place, no bump, no install record                                     | loading: "Relative-path plugins in a marketplace you added from a local path: the plugin loads in place… you don't need to increase the version"; "A relative-path plugin needs no install record because it loads from the marketplace itself."                                                       | **Real.** D4 option (a) is viable.                                                                                                                                                   |

### "Early access" is a stale citation

RESEARCH §2.1 says "The bundled skill's reference still calls the API 'early access', one
that 'moves between releases'". The live docs do not: the overview says "If you set
`CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` **during early access**, remove it", i.e. early access
is over, and mods are on by default from v2.1.287. The "early access… may change between
releases" wording I could find locally is in the `code-modernization` plugin's
`hooks/README.md` in the official marketplace cache, a third-party README that predates
GA. D10 should be reworded: the risk is not an early-access label but that the API is
**version-gated and growing** (reference marks fields by `v2.1.289`, `.290`, `.292`,
`.299`, `.300`) and "The copy on GitHub can be older than the Claude Code version you
have installed… trust the copy Claude Code writes for your version".

## 2. Version-gate gap: the claim is true, and slightly understated

Verified in `scripts/git-hooks/pre-commit`:

- Gate 1 (lines 123–134) matches only `files/var/local/claude-yolo/claude-yolo`,
  `…/lib/*.bash` and `helpers/ssh_agent_filter/*.py`.
- Gate 2 (lines 227–254) sets `CCY_CONTEXT="files/var/local/claude-yolo"` and matches a
  staged file only when it equals `$CCY_CONTEXT/<COPY source>`. The Dockerfile's `COPY optional/`, `COPY skills/browsing/`, `COPY docs/…` and `COPY ccy-startup-info.txt`
  sources are staged into the build context by `play-claude-yolo.yml` **from
  `files/opt/claude-yolo/`**, so no staged path under `files/opt/claude-yolo/` ever
  matches. The pingbus zipapp is built from `helpers/pingbus/` at deploy time and is not
  a repo path at all.
- Runtime: `lib/common.bash:947` and `:1030` hash the Dockerfile alone; the playbook
  passes the same `DOCKERFILE_HASH`. Nothing in the build context besides the Dockerfile
  is hashed.

So a mod under `files/opt/claude-yolo/mods/` would ship with no commit-time gate and no
runtime detection, exactly as RESEARCH §1.3 says. Two refinements:

- The fix is not "resolve COPY sources under `files/opt/claude-yolo/` too". The repo
  path and the context path differ per file (the playbook is the only map), so the gate
  needs either a second context root to try per source, or an explicit list that a test
  checks against the playbook. Task 1.3 should say which. It should also cover
  `helpers/pingbus/**` while it is there, or the plan's own §1.3 finding stays open.
- `.githooks`/`core.hooksPath`: `play-git-hooks-security.yml` sets `core.hooksPath`;
  the `.git/hooks` directory holds only samples, so `scripts/git-hooks/pre-commit` is
  the live hook. Confirmed.

## 3. Security model in a bypass container

### What the research gets right

- A mod adds no *reach* inside a bypass container; it adds in-process *influence*
  (§2.5, §4.5). Correct framing. The bans on `tool.call`, `tool.check`, `prompt.submit`
  rewriting, `session.append` and `$.http.fetch` are the right bans, and `validate` can
  pin the first set statically.
- No host execution route (H3 prohibited). Correct, and consistent with the inbound
  rules on the daemon side.
- The in-container settings file is session-writable (`/root/.claude -> /workspace/.claude/ccy`, gitignored), so a session can rewrite `enabledPlugins`. The
  research concedes this adds nothing in bypass. Agreed; but it means option (a) gives
  no integrity guarantee at all, and option (b) (image-owned `managed-settings.json`)
  is the only way the mod's load is not under the session's control. D4 should say
  that plainly rather than "revisit if a session-installed mod ever needs policing".

### What the research gets wrong or misses

1. **"Read-only" commands are not read-only for the model.** api: the `{ text }` a
   `command.run` hook returns "prints in the transcript **and Claude reads it**". So
   `/team status`, `/team who` and `/ccy doctor` put their output into the model's
   context with no HUMAN/PEER framing. For `/ccy doctor` that is fine (local facts). For
   anything that carries peer-authored text (member names, last message, room topic)
   it is a prompt-injection path that bypasses the envelope Plan 00161 wraps around
   deliveries. Rule for M2: a `/team …` command returns `{}` and shows peer text in the
   band, a pane or `$.ui.log` (which "Claude doesn't read"); only operator-authored,
   fixed-format facts may go in `{ text }`.
2. **`session.receive` is a consumption route.** Returning `{ consumed: reason }` keeps a
   delivery from Claude, and a hook that throws before `next` is skipped silently. A
   display-only mod has no need to hook `session.receive` at all; T3 can style rows from
   `UserMessage` `origin`/`from` without it. Add `session.receive` and `session.send`
   to the forbidden `hooks:` list for M1–M3, not just the forbidden `calls:`.
3. **Styling rows by sender is a spoof surface.** T3 says "the trust model becomes
   visible to the owner". If the row style keys on `e.props.from.name` or on text in the
   envelope, a peer can write an envelope that *renders* as HUMAN. The docs say the
   sender's name is whatever the sender wrote. T3 may key only on `origin.kind` and on
   fields pingbus itself verifies (the watcher writes the envelope from the team record
   after its own checks), never on free text. Say so in the plan.
4. **The argv allow-list cannot be pinned by `validate`.** `calls:` shows `$.process.run`,
   not its arguments. A Python test grepping `register.ts` for literal argv arrays is
   a lint on our own source, not a contract, and a variable defeats it. Keep it, but do
   not call it a pin; the real control is that the mod ships from a root-owned image
   layer (host) and that enforcement stays outside the mod.
5. **`$.settings.read` and `$.env.get`** can expose tokens; the plan pins `env reads:`
   (good) but does not mention `$.settings.read`. Forbid it in the `calls:` set.

### The delivery picture is a release behind, and it changes T2

RESEARCH §1.4 and §4.3 say live delivery today is "the supervisor typing into the
terminal (Plan 00194)". `docs/agent-bus-protocol.md` (lines 641–680) and
`helpers/pingbus/notify.py` say otherwise: with `PINGBUS_DELIVERY` unset, which is the
default, `watch` writes each item **into the session's Claude Code inbox socket**
(`CLAUDE_CODE_MESSAGING_SOCKET`, authenticated with the session token); the supervisor
typing path is the `relay` mode set by the launch. The kit's `crossSessionInbound: accept` exists for exactly this. Consequences:

- Deliveries already arrive as cross-session **peer messages**, so Claude Code already
  gives them `origin.kind` and a `from`. T3 is cheaper than the plan thinks, and T7/T1
  can use `session.measure`/`UserMessage` facts instead of polling `pingbus status`.
- T2 as written, "a ping arrives through `session.receive`, or is appended as a meta row
  or a `$.prompt.submit` when idle, instead of the supervisor typing", is solving a
  problem the socket already solved. The one in-process route exists; the mod must not
  add `$.prompt.submit` or `$.session.append` as a second. M4 should be cut to a
  question ("does the mod need any delivery role at all?"), and the default answer is no.
- The docs' container caveat ("a session inside a container and a session on the host
  can't reach each other") does not bite, because the watcher runs inside the same
  container and posts to the local socket. The research should state this rather than
  leave `session.receive` unexplained.
- In a container "where Claude Code runs as process ID 1 it has no process evidence" for
  own-child verification and falls back to the token. ccy runs under the supervisor
  wrapper, so PID 1 is not `claude`, but the point belongs in `/ccy doctor` (is the
  socket bound, is the token exported).

## 4. Architecture

- **One source for container and host.** `files/opt/claude-yolo/` is already the build
  context and becomes `/opt/claude-yolo/` on the host, so one tree, two consumers, is
  real. One caveat: `agent-bus-claude` members (LXC, VM, other hosts; the kit READMEs)
  get the kit from `agent-bus-install`, not from `/opt/claude-yolo`. If M3 moves the
  pingbus hooks and skill into `ccy`, those members need the `ccy` plugin too, which
  means the kit must carry it (a second copy, which §4.2 "one host copy" denies) or
  M3's "pingbus stub for one release" is permanent for them. Decide this before M3.
- **First-launch loading.** loading page: at session start plugins load "from
  `installed_plugins.json` and the cache"; a marketplace that settings declare but
  `known_marketplaces.json` lacks is registered **in the background after the session
  starts**, then plugins reload. Because `~/.claude` is per-project in ccy, every new
  project's first session may start without the mod until `/reload-plugins`. The
  entrypoint should run `claude plugin marketplace add /opt/claude-yolo/mods`
  (directory source, no network, idempotent) before `exec claude`, or Task 1.7 must
  test the first-session case specifically. Note that `known_marketplaces.json` is
  "one per user", which in ccy means one per project; on the host it is truly per user.
- **Ownership split with the hooks-daemon** is clean and the right one: daemon owns
  enforcement and the settings `statusLine`; mod owns UI and commands; pingbus owns
  protocol. Two additions: the `classic.<Event>` hooks let a mod sit *in front of* the
  daemon's settings hooks; forbid `classic.*` in the contract test too. And a mod that
  answers `command.run` for a name the daemon's skills use would shadow them; pick
  names under `/ccy` and `/team` only and keep a test that the names are not built-ins
  (`$.command.register` throws for a taken name and the whole `session.start` hook is
  then skipped; register last or wrap in try/catch, per the api page).
- **Migration without breaking running teams.** A running container keeps its image, so
  nothing changes under a live team; a relaunch gets both plugins until M3. Sound. The
  gap is the `--settings` file: once T7 drops `statusLine` from the kit settings, `cc --teams` and `agent-bus-claude` lose the bus segment unless they also load `ccy`.
  Sequence T7 after D5 lands on the host, and keep `statusLine` for `agent-bus-claude`
  until its members have `ccy`.
- **D10 contradicts the image.** `docs/ccy.md` §"Claude Code auto-update": once per 24 h
  the image compares the bundled Claude Code against npm and updates it in place; `cc`
  runs `claude update` every launch; Plan 00169 is "image keeps its tools current".
  "Pin the image's Claude Code version" is not current policy and would need its own
  decision. Without a pin, `tsc` in CI type-checks against whatever version CI has, not
  the version a session runs. The honest guard is: `session.start` checks
  `$.session.version()` against the floor the types were generated for, and degrades to
  a toast (already in §4.2); `tsc` catches drift at the next bump, not before a session.
- **Two plugins are already enabled with no marketplace.** The explore report counts the
  mechanisms right; the plan could retire the phpantom `jq` merge onto the same
  directory marketplace in M2, which is the "collapse the hand-wiring" the overview
  promises.

## 5. Is M1 small and shippable?

No. Tasks 1.1–1.8 contain five pieces of infrastructure before the first feature: a
new contract test framework; `claude` (and a Node/TS toolchain for `tsc`) provisioned
into CI through IaC; a rewrite of the pre-commit COPY gate (Plan 00093's whole scope);
a marketplace plus plugin skeleton; and four features, two of which (C2 update-needed,
H2 deploy-pending) need data the container does not have today. C2 "reads a host-written
file, mounted read-only": no such file or mount exists; the launcher never passes
`CCY_VERSION` into the container (`entrypoint.sh` has no `CCY_VERSION`; the launcher
only validates before launch). That is a launcher change, a `CCY_VERSION` bump and a new
mount in a milestone meant to prove the mod loads.

A shippable M1: Task 1.3 (gate) first and alone, as its own commit, since it is a
prerequisite for anything under `files/opt/claude-yolo/`; then marketplace + manifest +
`register.ts` with exactly C1 (status entry from image-baked facts) and D1 (`/ccy doctor`, local checks only); `claude plugin validate --strict` and `claude plugin test`
wired into QA; entrypoint enable; container bump; Task 1.7 verification including the
first-session case. Move C2, H1/H2 and `tsc` to M2. `claude plugin test` runs TS
itself, so `tsc` is not needed for the tests to run; it is the API-drift check and can
wait for the version-floor decision above.

## 6. The owner decisions

| Decision                      | Recommendation in plan                                | Review                                                                                                                                                                                                                           |
| ----------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1 mod                        | mod                                                   | Right. It is what the owner asked for and the only way to get a band, a status entry and turn-free commands.                                                                                                                     |
| D2 one                        | one                                                   | Right; one status line per mod and one hooks module per plugin make several mods strictly worse.                                                                                                                                 |
| D3 path                       | `files/opt/claude-yolo/mods/ccy`                      | Right, provided Task 1.3 lands first. Note the directory must be root-owned on the host ("Anyone who can write there can rewrite your mod", admin).                                                                              |
| D4 (a)                        | entrypoint merge                                      | Acceptable for M1, but say what it does not give: the session controls its own load. (b) is the only integrity option and also brings `sec-default`, which is harmless for a mod that never approves tool calls. Lean (b) by M2. |
| D5 host                       | yes from M2                                           | Right, but the mod on the host runs as the owner with no bypass; re-check the argv list against host paths (`pingbus` lives in the host kit, not `/opt/claude-yolo`).                                                            |
| D6 detect                     | `/run/.containerenv` + marker, `userConfig` overrides | Right. Prefer an image-baked marker the Dockerfile writes over `.containerenv`, which podman writes and docker does not.                                                                                                         |
| D7 delivery                   | defer to M4                                           | Reframe: delivery already goes in-process through the inbox socket; the mod should never add a route. Keep M4 only as a question, default no.                                                                                    |
| D8 absorb pingbus at M3       | M3                                                    | Fine, with the `agent-bus-claude` question (§4) answered first.                                                                                                                                                                  |
| D9 TypeScript, `claude` in QA | yes                                                   | Right that it is not bash tooling. Add: the mod's own tests are `claude plugin test` fixtures, not pytest; the Python contract test only reads `validate --json`.                                                                |
| D10 early access              | accept with `tsc`                                     | Reword per §1 and §4: GA, version-gated API; guard is the runtime version floor plus `tsc` at bump time, not a pin the image does not keep.                                                                                      |

## 7. Smaller corrections

- RESEARCH §1.1 row "pingbus plugin": "`--plugin-dir` plus `--settings` added to the
  `claude` argv by the entrypoint": confirmed at `entrypoint.sh:595`.
- RESEARCH §2.3 "the first message's context" is `prompt.context`; fine. "Timers… do not
  start one": api says a timer callback "doesn't start one". Fine.
- Explore report §6 says `feat/teams-shutdown` has no commits beyond HEAD; T6 and Task
  3.3 therefore depend on work that does not exist yet. Mark T6 as blocked on that
  branch, not aligned with it.
- PLAN "Delivery & Milestones" ends mid-sentence ("the branch
  `plan/ccy-claude-code-plugin`"); the branch name still says plugin, the plan says mod.
  Harmless, but the plan-QA sweep may not like the dangling list.
- No install-specific names found in any of the five files.
