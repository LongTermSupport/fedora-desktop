# Synthesis: baseline-20261120-1050

Plan 00258, Task 3.2. One reading of the nine group reports in this folder: `fleets.md`,
`teams.md`, `multiplexers.md`, `vm-sandboxes.md`, `local-sandboxes.md`, `gateways.md`,
`usage-observability.md`, `tooling.md`, and `team-client.md` (Lightning, studied as a dependency
of Plan 00254, not as an idea source; its findings are in the Plan 00254 row of section 4 and in
sections 6 and 7).

Citations are `file` "heading" (and an item number where the report numbers them). Detail and
evidence stay in the reports. Every study read clones only: nothing was built or run, so
"verified" below means checked by reading source or docs (ours or theirs), never tested on a host.
Nothing here is taken from a clean-room repository except described behaviour.

## 1. Security findings (first, and separately)

Several groups found the same ways an agent in ccy can reach the host or keeps credentials. They are
merged here. "Status" says how far each claim was checked.

| #   | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | Status                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Sources                                                                                                                                                                                               |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| S1  | **The read-write project mount lets the agent plant code the host runs as the owner.** `.git/hooks`, `.git/config` (`core.fsmonitor`, `core.hooksPath`, `core.sshCommand`, `core.pager`, `filter.*`, `diff.*` drivers), `.git/info/attributes`, `.husky`, `.claude/settings*.json` hooks and `.claude/hooks/handlers/`. The owner's next host `git status` (bash-git-prompt runs it on `cd`), host commit, or host Claude Code session in that checkout runs it. In this repository `core.hooksPath` is the tracked `scripts/git-hooks`, writable from inside ccy. `docs/ccy.md` "What this means in practice" understates the worst case as damage to the tree and history. | Mount read-write: verified by reading `claude-yolo` (around line 2464). `core.hooksPath` = `scripts/git-hooks`: verified in this checkout. That host git runs drivers and fsmonitor: measured by yoloai on its own system (`copy-mode-git-rce.md`), named by sandvault, coi and devvm. That bash-git-prompt triggers it here: **reasoned from reading, not tested**. Today's launcher host git calls (`config`, `rev-parse`, `remote`) do not run hooks or fsmonitor (read). | `vm-sandboxes.md` "Takeaways, ranked" 1 and "coipond/coi" idea 1; `local-sandboxes.md` "Takeaways, ranked" 1, "webcoyote/sandvault", "kstenerud/yoloai" Y2; devvm section of `vm-sandboxes.md`        |
| S2  | **Live `CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN` in the container environment.** Any process in the container can read them; `GH_TOKEN` reaches everything the token can on GitHub. Five of eight VM tools, yoloai, nono, matchlock, intentic and substrate keep the model credential out.                                                                                                                                                                                                                                                                                                                                                                                    | Verified from our own `docs/ccy.md` ("What the container CAN reach"). Whether they also show in `podman inspect`: **unverified** (only names are passed with `-e`, values come from the environment).                                                                                                                                                                                                                                                                        | `teams.md` "agent-substrate/substrate"; `vm-sandboxes.md` "Takeaways, ranked" 2; `local-sandboxes.md` "Takeaways, ranked" 2 and Y4; `gateways.md` "Where ccy stands"; `fleets.md` "intentic/intentic" |
| S3  | **A cloned repository's tracked `.claude/ccy/mounts` is honoured after a deny list only**, so a stranger's repo can bind a home path not on that list, read-write, without the owner's yes. Untrusted project config should only tighten.                                                                                                                                                                                                                                                                                                                                                                                                                                    | From reading `docs/ccy.md` "Extra Mounts"; **not tested**.                                                                                                                                                                                                                                                                                                                                                                                                                   | `vm-sandboxes.md` "Takeaways, ranked" 3, "madarco/agentbox" idea 1, coi idea 3                                                                                                                        |
| S4  | **In-agent guards are inside the agent's reach.** The hooks daemon's config and handlers live in the writable workspace; `write_protected_paths` is not enabled here; guards judge the command asked for, not what a script then runs; `R-SECRET-READ` guards Claude's tools, not `cat` in a container shell.                                                                                                                                                                                                                                                                                                                                                                | `write_protected_paths` absent from `.claude/hooks-daemon.yaml`: verified. The rest: reasoned from the daemon's design.                                                                                                                                                                                                                                                                                                                                                      | `local-sandboxes.md` "nolabs-ai/nono" threat model; `tooling.md` "Hook-by-hook against our daemon" (config-guard row); `vm-sandboxes.md` coi idea 2                                                   |
| S5  | **Host helpers over a container-writable tree.** Any host tool that runs git or a helper by bare name inside a ccy checkout inherits S1 and PATH planting. The Go port will add more host git calls.                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Launcher calls read (defence in depth, not a live hole). The PATH audit was **not done**.                                                                                                                                                                                                                                                                                                                                                                                    | `vm-sandboxes.md` "devvmhq/devvm" idea 1; `local-sandboxes.md` N4                                                                                                                                     |
| S6  | **Refused egress is silent.** The agent sees "No route to host"; the owner sees nothing, so LAN probing goes unnoticed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | From reading `ccy-network-policy`; nothing tested.                                                                                                                                                                                                                                                                                                                                                                                                                           | `vm-sandboxes.md` "Takeaways, ranked" 4; `local-sandboxes.md` "Takeaways, ranked" 3                                                                                                                   |
| S7  | **Untrusted instruction files.** Scouts read clones that carry their own `CLAUDE.md`; nothing here checks loaded instructions or tracked files for invisible Unicode or bidi controls, and our `InstructionsLoaded` forwarder has no handler.                                                                                                                                                                                                                                                                                                                                                                                                                                | Absence of a check: verified by search (`scripts/`, `src-go/qa`, `.semgrep`). Risk: reasoned.                                                                                                                                                                                                                                                                                                                                                                                | `tooling.md` "Group takeaways (ranked)" 2                                                                                                                                                             |
| S8  | **DisplayLink RPM installed with `disable_gpg_check: true` and no checksum** (upstream releases are unsigned).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Verified by reading `play-displaylink.yml`.                                                                                                                                                                                                                                                                                                                                                                                                                                  | `tooling.md` "displaylink-rpm/displaylink-rpm" 1                                                                                                                                                      |

Patterns the reports say never to copy: unauthenticated loopback APIs that type into panes
(agent-orchestrator, cli-agent-orchestrator, coral); root sandbox images with a shared writable
credential file (agent-of-empires); `SYS_ADMIN` plus unconfined seccomp for Docker-in-box
(agentbox); file capabilities on a user-runnable binary (matchlock); in-container iptables with
`NET_ADMIN` (yoloai on Podman); subscription request shaping and fingerprint replay (ccproxy,
claude-unlimited's refresh impersonation, teamclaude's body rewrite and residential-proxy mode).

## 2. Where others are ahead of us, ranked by consequence

01. **Credentials out of the container** (ccy). Two working shapes: a capability token to a host
    forwarder that is the base URL (coop; Plan 00146's design, with MIT Go code in yoloai's broker),
    or placeholders swapped by a host proxy (smolvm, devvm, agentgateway, matchlock, iron-proxy;
    Plan 00252). Two facts change our plans: **any `ANTHROPIC_BASE_URL` other than
    `api.anthropic.com` makes Claude Code budget 1M-context models at 200K** (claude-unlimited ADR
    0008), and **some Claude Code traffic ignores the base URL** (teamclaude's path inventory).
    Sources: `usage-observability.md` takeaway 1; `gateways.md` takeaways 2-3; `vm-sandboxes.md`
    takeaway 2; `local-sandboxes.md` takeaway 2; `fleets.md` takeaway 1.
02. **The project mount as a host-execution channel** (ccy). See S1. coi holds read-only sub-mounts
    over the project; devvm hardens every host git call; yoloai keeps git for the work copy inside
    the sandbox. Rootless Podman without `CAP_SYS_ADMIN` can hold read-only sub-mounts better than a
    VM can. Sources: as S1.
03. **Per-repository GitHub tokens** (ccy). keybox (clean room) mints a GitHub App installation
    token for one repository and minimum permissions, checks GitHub's answer, revokes if broader. No
    proxy, no CA. Answers 00252 Q6. Source: `gateways.md` "IT-Hjaelp-Nu/keybox" and takeaway 1.
04. **Seat status for Plan 00251** (agent bus, ccy-teams). Everyone has a state model we lack:
    openrig's three axes, evidence ladder and PARKED; coral's ranked, size-capped, acknowledgeable
    health check with overdue judged against each seat's own median and p90; agenttrail's evidence
    classes; OSC 7501 (Claude Code reporting its own state, per Orca) and `get_usage` (per
    agent-orchestrator), both to be probed on our pinned version; herdr's lesson that **Claude state
    hooks alone are lossy** (it removed them) and aoe's tested event map; the usage-limit stop signal
    from the transcript (hotseat). Sources: `teams.md` takeaway 1; `fleets.md` takeaway 2;
    `multiplexers.md` findings 1-2; `usage-observability.md` takeaway 5.
05. **Our screen classifier is a generation behind** (ccy-sessions, wind-down, 00256). herdr, aoe
    and cli-agent-orchestrator know "waiting on a dialog", "work still running behind an idle
    prompt", viewer freeze and the title spinner; CAO has 21 captured Claude screens to our 6. Also:
    **nothing leaves tmux copy mode before typing** into a pane (reasoned, untested). Source:
    `multiplexers.md` "Group findings, ranked" 1, 3, 5.
06. **Structured work on the bus** (agent bus). Our `done`/`blocked`/`ack` carry no owner, state or
    successor. Converging inputs: Orca's Run/Task/Dispatch and decision gates, openrig's closure
    contract, intentic's durable needs, Multica's leader-only wake, orgtree's enforced halt and
    leases (coral's `resourcequeue` is liftable Go), A2A's task state names, stageflow's verified
    hand-off, delivery honesty (written / acknowledged / done / verified kept apart). Sources:
    `teams.md` takeaways 2 and 4; `fleets.md` takeaway 4.
07. **Acting at a usage limit** (ccy, 00146). Codeman, intentic, orgtree, clarp, hotseat and
    claude-unlimited all switch account or hold and resume at reset; ccy only reports. Rotation
    semantics are worked out elsewhere (sticky until threshold, DRAINING, never rotate on a bare
    429, park per model, relapse backoff). Upstream failures need classifying (quota rejection vs
    rate-limit 429 vs refusal vs 529). Sources: `fleets.md` takeaway 3; `usage-observability.md`
    takeaway 4; `gateways.md` takeaway 4.
08. **Usage figures without billed probes** (ccy token menu). Running sessions already receive
    `rate_limits`; a host reader could refresh ccy's per-token cache; hotseat shares a cross-process
    cooldown. Source: `usage-observability.md` takeaway 2.
09. **Cost of cold returns** (hooks daemon, ccy supervisor, 00256). The hourly failsafe tick sits at
    the 1-hour cache TTL; restores and usage-pause resumes return to large histories after long
    gaps. A study says cold returns cost several times a warm or fresh start (small samples). Source:
    `usage-observability.md` takeaway 3.
10. **Guard coverage** (hooks daemon). Ours are better built but narrower: credential path list,
    test-disabling ("fake green") guard, config tamper guard, `InstructionsLoaded`/`ConfigChange`
    handlers. Source: `tooling.md` "Group takeaways (ranked)" 1-3.

Where we stay ahead, in every group: containment (rootless Podman, host-applied network policy,
no added capabilities, SELinux), survival (scope, own tmux socket, restore by exact conversation
id, freeze/thaw, cold migration), and a multi-human, multi-machine team room with forge-verified
references. No project studied is worth adopting as a component.

Rejected on the evidence: a VM option for ccy now (`vm-sandboxes.md` "Verdict"), a Landlock or
seccomp layer inside ccy (`local-sandboxes.md` "Would a Landlock or seccomp layer…"), speaking A2A
on the bus (`teams.md` "Could the agent bus speak A2A?"), shpool for tmux (`multiplexers.md`
"shell-pool/shpool").

## 3. Candidate issues (clear wins), ranked

Dedupe not yet run: before filing, the coordinator runs the dedupe scout and the `gh issue list`
search Watchlist.md requires.

| Rank | Outcome         | Title                                                                                                                                              | Lands in                                                                    | Source repository, licence, class                                                             | Report section                                                                     |
| ---- | --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| 1    | **file**        | ccy docs: name agent-planted git config, hooks and agent settings as a host-execution risk, with what an owner can do now                          | `docs/ccy.md` "The Security Model" / "What this means in practice"          | coipond/coi MIT lift; devvmhq/devvm MIT lift; kstenerud/yoloai MIT lift (ideas only, no code) | `vm-sandboxes.md` takeaway 1; `local-sandboxes.md` takeaway 1                      |
| 2    | **file**        | tmuxccy: leave copy mode before every key sent to a pane (Defence Before Fix; reproduce first)                                                     | `src-go/internal/tmuxccy` (one sending helper; a rule in `src-go/qa/rules`) | awslabs/cli-agent-orchestrator Apache-2.0 + NOTICE, lift (idea only)                          | `multiplexers.md` "Group findings, ranked" 3; CAO idea 1                           |
| 3    | **file**        | ccy-network-policy: count refused destinations per session and show them                                                                           | `src-go/cmd/ccy-network-policy` (with `internal/netpolicy`)                 | clawkwork/clawk Apache-2.0 lift; nolabs-ai/nono Apache-2.0 lift (ideas only)                  | `vm-sandboxes.md` takeaway 4; `local-sandboxes.md` takeaway 3, N5, A2              |
| 4    | not filed (cap) | Enable `write_protected_paths` for `.claude/hooks/*` forwarders, `.claude/hooks-daemon/**`, `CLAUDE/core/**`                                       | `.claude/hooks-daemon.yaml`                                                 | karanb192/claude-code-hooks MIT lift; the upstream daemon's own handler                       | `tooling.md` karanb192 idea 2                                                      |
| 5    | not filed (cap) | Broaden `secret_file_guard` protected paths (measure false positives first)                                                                        | `.claude/hooks-daemon.yaml`                                                 | karanb192/claude-code-hooks MIT (path table as data); nono `policy.json` Apache-2.0 (data)    | `tooling.md` karanb192 idea 1                                                      |
| 6    | not filed (cap) | Hardened host git: every host git call over a project disables hooks, fsmonitor, filters, drivers                                                  | `src-go/internal/gitx`                                                      | devvmhq/devvm `internal/hostgit/hostgit.go` MIT lift                                          | `vm-sandboxes.md` devvm idea 1                                                     |
| 7    | not filed (cap) | Screen classifier: a waiting-on-dialog state and "still running behind an idle prompt", with ported fixtures                                       | `src-go/internal/screen`                                                    | herdrdev/herdr Apache-2.0 lift; awslabs/cli-agent-orchestrator Apache-2.0 + NOTICE lift       | `multiplexers.md` findings 1 and 5                                                 |
| 8    | not filed (cap) | QA gate: fail on zero-width, tag and bidi-override characters in tracked text files                                                                | `src-go/qa` (a new gate)                                                    | karanb192/claude-code-hooks MIT (character sets)                                              | `tooling.md` karanb192 idea 3(b)                                                   |
| 9    | not filed (cap) | Pin a sha256 for the DisplayLink RPM and bump it with the version                                                                                  | `play-displaylink.yml` plus the update-versions pin                         | displaylink-rpm MIT (recipe only)                                                             | `tooling.md` displaylink-rpm 1                                                     |
| 10   | not filed (cap) | Check whether ccy's tokens show in `podman inspect`; move them to a secret file if they do (verify first)                                          | ccy launcher                                                                | kstenerud/yoloai MIT (idea)                                                                   | `local-sandboxes.md` Y4                                                            |
| 11   | not filed (cap) | Token usage cache: cross-process lock and persisted 429 backoff (confirm the duplicate-probe guess first)                                          | `src-go/internal/ccylaunch/tokenusage.go`                                   | Maxteabag/hotseat MIT lift (`internal/quota/`)                                                | `usage-observability.md` hotseat; takeaway 2                                       |
| 12   | not filed (cap) | ccy docs: threat model in errant/rogue terms, a status per row of what the container can reach, branch protection as the boundary for write tokens | `docs/ccy.md`                                                               | yoloai MIT; agent-substrate Apache-2.0; keybox clean room (idea only)                         | `local-sandboxes.md` takeaway 4; `teams.md` substrate idea 2; `gateways.md` keybox |
| 13   | not filed (cap) | teamchat: show agent text to a human whole or refuse it, naming control, bidi and zero-width characters                                            | `src-go/internal/teamchat`                                                  | subgentic/coral Apache-2.0 with EULA caution (owner's go-ahead before lifting)                | `teams.md` coral idea 4; takeaway 4                                                |
| 14   | not filed (cap) | Write down the convergence and liveness rules (destroy only on positive evidence; pid plus start time)                                             | `CLAUDE/AgentNotes.md`                                                      | intentic MIT; agent-orchestrator Apache-2.0; emdash Apache-2.0                                | `fleets.md` takeaway 5                                                             |
| 15   | not filed (cap) | Pace label (used % against elapsed % of window) in the token menu and `ccy --token-usage`                                                          | `src-go/internal/ccylaunch/tokenusage.go`                                   | Maciek-roboblog/Claude-Code-Usage-Monitor MIT (idea)                                          | `usage-observability.md` Usage-Monitor                                             |
| 16   | not filed (cap) | ccy-teams seat stop checks node and session id still match, and reports how far a failed stop got                                                  | `ccy-teams` seat actions                                                    | mvschwarz/openrig Apache-2.0 (idea)                                                           | `teams.md` openrig idea 5                                                          |
| 17   | not filed (cap) | pingbus and ccy-teams receipts end with the next command                                                                                           | pingbus / ccy-teams output                                                  | stablyai/orca MIT (idea)                                                                      | `fleets.md` Orca idea 4                                                            |
| 18   | not filed (cap) | ccy-sessions restore: a closed list of failure reasons, one next action each                                                                       | `src-go/internal/sessions`                                                  | multica-ai/multica, clean room (implement from the report only)                               | `fleets.md` Multica idea 3                                                         |

Why these three: 1 corrects a public security statement that is wrong today and costs one docs PR;
2 is a reliability defect in the code that closes sessions and delivers to seats, with a ready
class and detector; 3 gives the owner the only signal that an agent is probing the LAN. Items 4 and
5 are nearly as cheap and could be promoted first.

Upstream items, not ours to count against the cap (filed through `hooks-daemon issue-report` or
on the other project, owner's call): an `InstructionsLoaded` audit handler; a test-disabling
guard; broader default secret globs; corpus rows for `gh repo delete`, `gh release delete`,
`gh issue close`, filesystem wipes; a ReDoS test over guard regexes (`tooling.md` "Upstream-side
gaps"); the DisplayLink spec's `cp -a` logrotate mode (`tooling.md` displaylink-rpm 2).

## 4. Candidate plans

Prefer folding into an existing plan. Every plan named below exists (checked in `CLAUDE/Plan/`
and `CLAUDE/Plan/Completed/`).

**Fold into existing plans** (append to their research; no new plan):

| Plan                                     | What to add                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Question it raises                                                                                           | Sources                                                                                                                                                    |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **00146** token switch / forwarder       | **The 1M-context cap behind a base URL** (check: `context_window.context_window_size` in the status payload); inventory of Claude Code paths that ignore the base URL; what lands in `metadata.user_id`; upstream failure classes; closed route allow list (coop); yoloai's broker as the code base; rotation semantics; pinning by `x-claude-code-agent-id`; a per-version capture as a regression oracle; hold-and-resume at a limit                                                                                              | Rely on an internal variable for 1M context, or accept the cap? Intercept or switch off each hardcoded path? | `usage-observability.md` takeaways 1, 4; `gateways.md` teamclaude, takeaways 2, 4; `vm-sandboxes.md` coop; `local-sandboxes.md` Y1; `fleets.md` takeaway 3 |
| **00252** egress proxy                   | Gateway shape over a TLS proxy (intentic); agentgateway's behaviours as the spec (SNI passthrough by default, intercept only bound hosts, identity on CONNECT, 403 with a reason, refuse nested CONNECT and WebSocket); name-constrained per-session CA; placeholder refusal and leak refusal; never substitute into bodies; per-session proxy token; DNS rebinding defence; DNS-as-router under rootless Podman; per-repo `git-receive-pack` rules; confined parser child; monitor-then-enforce; audit without request text        | Build our own small Go proxy (the reports' consensus) or run agentgateway as the data plane?                 | `gateways.md` "Recommendation for Plan 00252"; `vm-sandboxes.md` takeaway 2; `local-sandboxes.md` N1-N3, M1-M2; `fleets.md` intentic idea 1                |
| **00251** seat status                    | openrig's state model; coral's health check and own-history overdue; evidence classes; OSC 7501 and `get_usage` probes; hooks need screen confirmation or expiry; aoe's event map; the limit-stop signal; A2A state names; rate-limited keyed on quota rejection; decision gates; a guard so automated input never answers an open dialog                                                                                                                                                                                           | Which signals are authoritative, and in what order?                                                          | `teams.md` takeaway 1; `multiplexers.md` findings 2-3; `fleets.md` takeaway 2; `usage-observability.md` takeaway 5                                         |
| **00256** idle stand-down                | "No background work" read from the screen; compact while warm or start fresh with a handoff; verify progress by a new transcript record, not a running process                                                                                                                                                                                                                                                                                                                                                                      | Compact before stand-down, or hand off?                                                                      | `multiplexers.md` aoe idea 2; `usage-observability.md` takeaway 3, clarp                                                                                   |
| **00116** deploy keys                    | An ssh agent that only signs, one socket per session; short-lived SSH certificates from a host CA                                                                                                                                                                                                                                                                                                                                                                                                                                   | Worth it over the one-key agent ccy has?                                                                     | `fleets.md` intentic idea 2; `gateways.md` keybox                                                                                                          |
| **00207** roles, **00225** liaison       | Directional audiences for communication gates; A2A's in-task authorisation chain for the liaison                                                                                                                                                                                                                                                                                                                                                                                                                                    | —                                                                                                            | `teams.md` orgtree idea 4, A2A                                                                                                                             |
| **00227** cold migrate, **00246** bus v2 | Tombstone-and-reconcile for unreachable machines; versioned install directories with an atomic `current`; event-driven wake on an issue reply                                                                                                                                                                                                                                                                                                                                                                                       | —                                                                                                            | `fleets.md` emdash ideas 2, 4; `tooling.md` claude-code-tools idea 2                                                                                       |
| **00254** Lightning client               | **The plan is out of date**: v0.10.6 (on Flathub within the hour of release) carries the thread-root fix and #76, #78, #79, so Task 7.1 gate (a) is met and Task 2.5's live check and the deletion of `lightning-thread-root.patch` can proceed; #88 and #89 are fixed on upstream `main`, so `lightning-thread-root-selectable.patch` goes at the next tag; Task 1.1 still names the old repository owner; #90 waits on a PR from us. Also: track each release's ancestry against our issues' fix commits; keep Element selectable | Ask upstream about testing on a Tuwunel server? Deliver #90's separator?                                     | `team-client.md` "Findings that change Plan 00254's state", "Strongest takeaways, ranked"                                                                  |

**New plans** (each needs discussion first):

1. **ccy: stop agent-written files from running on the host** (security, high). Read-only
   sub-mounts for host-auto-executing paths, host-owned masking of secret files, a session-end
   report of changes, a taint inventory of container-written files the host reads, project config
   that may only tighten (a host-side grant for `.claude/ccy/mounts`, keyed on content), PATH
   sanitising for host helpers. Questions: which paths; are tracked files exempt (this repository's
   `scripts/git-hooks` and `.claude/` are edited from inside on purpose); per-project opt-out.
   Sources: S1, S3, S5. Not a fit for 00255 (host `cc` sandbox) or 00232 (network).
2. **ccy: per-session GitHub App installation token for the project's own repository.** Questions:
   per-owner App setup through IaC and vault, refresh for sessions past an hour. Cross-link from
   00252 Q6 and 00116. Clean room: implement from `gateways.md` only.
3. **Bus work objects (teamchat v2 vocabulary).** Completion with outcome and the request it
   closes, a closure contract, durable needs or gates that wake the seat, leader-only wake, three
   sender outcomes for an unavailable seat, delivery honesty, enforced halt, shared-resource
   leases. Check whether 00246's scope already holds a v2 verb table; if so, add there.
   Sources: `teams.md` takeaways 2, 4; `fleets.md` takeaway 4.
4. **Defence Before Fix: declared but unenforced configuration.** A `src-go/qa/rules` detector
   that every team-record and member-config field has a consumer outside its parser. Source:
   `teams.md` takeaway 3.
5. **Measure what cold returns cost** (failsafe tick at the TTL edge, restores, usage-pause
   resumes), then set cadence, return policy and the two cache environment variables for ccy.
   Source: `usage-observability.md` takeaway 3. Partly upstream (the daemon's cron).
6. **Usage figures from running sessions** instead of the billed probe, labelled by source and
   age. May fold into 00146 decision 5 if the forwarder lands first. Source:
   `usage-observability.md` takeaway 2.
7. **Cross-branch overlap warning** with `git merge-tree` on branch tips. Questions: consumers,
   uncommitted work, the ccy image's git version. Source: `multiplexers.md` finding 4.
8. **Known-value secret scanning** in the git hooks (refuse the machine's live token values,
   fail closed). Source: `tooling.md` git-secrets 1.
9. **Untrusted-repository mode** (low priority): a `--runtime krun` spike, copy/diff/apply, and
   coop's forwarder-only network. Sources: `vm-sandboxes.md` "Verdict"; `local-sandboxes.md`
   takeaway 5.

Lower priority, mention only unless the owner asks: transcript search and lineage rollover (must
square with 00098); a blanket `git add` guard (upstream); CLAUDE.md compliance scoring; a trial of
the daemon's `long_running_command_guard` for coordinators; sleep inhibit while sessions work;
keep-warm nudges (Codeman; usage cost, and teamclaude's keep-warm has a terms angle).

## 5. Code we may lift

Permissive only; ours is Go, so most "lifts" are ports or data. Keep the copyright and licence
notice in the receiving file, state changes (Apache-2.0 §4b), carry NOTICE lines where one exists.

| Source path                                                                                                                           | Licence, notice duty                                                  | Use                                                                   |
| ------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| kstenerud/yoloai `internal/broker/injector.go`, `internal/credential/*.go`, their tests                                               | MIT, Karl Stenerud                                                    | 00146 forwarder                                                       |
| jingkaihe/matchlock `pkg/policy/engine.go`                                                                                            | MIT, Jingkai He                                                       | 00252 placeholder swap                                                |
| devvmhq/devvm `internal/hostgit/hostgit.go`                                                                                           | MIT, w3cj                                                             | `src-go/internal/gitx`                                                |
| coipond/coi `internal/session/security.go` (around lines 430-485, path lists)                                                         | MIT, Maciej Mensfeld                                                  | new plan 1                                                            |
| herdrdev/herdr `src/detect/manifests/claude.toml` (data)                                                                              | Apache-2.0, no NOTICE                                                 | `src-go/internal/screen`                                              |
| awslabs/cli-agent-orchestrator `providers/claude_code.py` 58-530 (regexes), `test/providers/fixtures/claude_code_*.txt`               | Apache-2.0, **NOTICE "Copyright Amazon.com, Inc. or its affiliates"** | screen rules and fixtures; record the Claude Code version per fixture |
| Maxteabag/hotseat `internal/quota/cache.go`, `read_lock_unix.go`, `internal/work/scan.go`                                             | MIT, Maxteabag                                                        | token usage cache, 00251                                              |
| agent-orchestrator `backend/internal/processalive/process_unix.go`                                                                    | Apache-2.0, no NOTICE                                                 | liveness rules                                                        |
| coral `coral-go/pkg/resourcequeue/`, `internal/teamhealth/deviation.go`, `internal/permreview/permreview.go`                          | Apache-2.0 **with caution** (below)                                   | leases, 00251, teamchat display                                       |
| maximhq/bifrost header deny list and Accept-Encoding rule (`core/providers/utils/utils.go`), `stripCallerQueryCredential`             | Apache-2.0, no NOTICE                                                 | 00146/00252                                                           |
| karanb192/claude-code-hooks rule tables (`protect-secrets.js` 71-105, 188-235; `protect-tests.js`; instructions-audit character sets) | MIT, Karan Bansal (data)                                              | daemon config, QA gate                                                |
| claude-unlimited `router.py`, `observation.py`, `usage_probe.py` (reference to port)                                                  | MIT, Claude Unlimited contributors                                    | 00146 rotation                                                        |
| nono `crates/nono-cli/data/policy.json`, `network-policy.json` (data)                                                                 | Apache-2.0, state changes                                             | secret paths, 00252 host groups                                       |

**Traps the reports found:**

- **agent-of-empires** `claude.toml`: its commits say the rule model and some rules were ported from
  herdr (Apache-2.0), but `THIRD_PARTY_NOTICES.md` lists only `src/tui/hyperlink.rs`. Lift Claude
  rules from herdr's file under Apache-2.0; treat aoe's as MIT plus Apache-2.0 with both notices
  (`multiplexers.md` "Licence: MIT, but its Claude rules are partly herdr's").
- **coral**: `LICENSE` is Apache-2.0, but release builds show a revocable proprietary EULA
  (`pkg/license/terms.go`). Ask the owner before lifting (`teams.md` coral "Licence and reuse").
- **wireproxy** `sni.go`: "based on" a blog post whose licence is not stated. Write our own
  (`gateways.md` "windtf/wireproxy").
- **multica**: `server/ee/` and `packages/ee/` are commercial and the root licence adds conditions
  to Apache-2.0: clean room, do not open `ee/` (`fleets.md` "multica-ai/multica").
- **agentgateway** `crates/pool` is MIT (Sean McArthur) inside an Apache-2.0 tree; its example CA
  keys must never be reused (`gateways.md` "agentgateway/agentgateway").
- Others: intentic `_extensions/onlyoffice/` is AGPL and `_sandbox/fileq` carries an Apache NOTICE;
  dmux `arnis-tile-cache/` and `.caf` sounds have no stated licence; claude-code-tools
  `plugins/writing/` holds third-party and CC-BY content; smolvm releases bundle LGPL/GPL libkrunfw;
  ThinkWatch is BUSL except one MIT directory; clarp's PolyForm Shield forbids competing use, and
  ccy-teams may count, so ideas only.

## 6. Watch-list follow

Every repository studied is in this run's manifest, so the reports' "add to the list" suggestions
are already met. What changes is attention.

- **Follow closely:** openrig, coi, nono, intentic, agentgateway, teamclaude, claude-unlimited,
  hotseat, herdr, agent-of-empires (Claude rule churn), cli-agent-orchestrator (tmux fixes), coop,
  orgtree, coral (`internal/teamhealth` and the inbox only), inevolin/claude-code-usage-analysis,
  karanb192/claude-code-hooks, smolvm (credentials crate, and only if the krun spike runs).
- **Low priority:** bifrost, A2A (watch v1.1), stageflow, substrate, matchlock, yoloai (activity
  dropped), Claude-Code-Usage-Monitor (quiet), claude-code-tools (scan in batches), agtop,
  agenttrail, clarp, Emdash ("Classic", may slow), Codeman, Orca, ThinkWatch (slowing), holvi and
  keybox (single squashed commits), wireproxy, claude-tap, devvm (already deep-read for 00252).
- **Candidates for `excluded`:** rogeriochaves/open-managed-agents (dormant since April, AGPL),
  clash-sh/clash (dormant since February), standardagents/dmux (slowing), starbaser/ccproxy
  (dormant, terms risk), shell-pool/shpool, Yu-369/VibeCurb, gyf304/dotenv, awslabs/git-secrets
  (maintenance only), webcoyote/sandvault and eugene1g/agent-safehouse (macOS only), madarco/agentbox
  and clawkwork/clawk (quiet since August/September), rudderlabs/devbox, delta-box/deltabox-lite.
  displaylink-rpm is tracked through the pinned-version checker instead.
- coral moved from `cdknorow/coral` to `subgentic/coral`; check the list row carries the new owner.
- **lightning-matrix/lightning** (team-client): follow every release, as a dependency. Weekly tags,
  one maintainer, constant churn in the thread panel we rely on, and an open upstream event-cache
  gap that can drop a message for good (the worst defect class for a bus reader; Element stays the
  fallback). Diff runs should compare each tag's ancestry with our issues' fix commits and read its
  "Known limits" and "NOT TESTED" notes (`team-client.md` "Risks to depending on it", "Ideas
  worth adopting").

## 7. Faults in our own tooling met by the reports (for the coordinator to log)

1. **watchlist sync left two clones shallow** (Orca and intentic had one commit), so their history
   could not be measured; the usage group says history "may still have been arriving". Sync should
   finish full clones, or the run should refuse to start on a shallow one (`fleets.md` method
   paragraph; `usage-observability.md` opening).
2. **Scouts did not know the list's contents**: several reports propose adding repositories that are
   already rows (`fleets.md`, `teams.md`, `gateways.md`, `usage-observability.md` watch sections).
   The scout brief should include each row's state.
3. **`src-go` tests skip when a tool is missing**, against the Missing Dependencies rule:
   `internal/ui/drain_pty_test.go:46` (gum), `internal/tmuxccy/start_test.go:173` and
   `panes_test.go:193` (tmux), `internal/transcript/transcript_test.go:309` (jq),
   `internal/teamchat/units/bus_test.go:57` (dbus-daemon). A Defence Before Fix case
   (`tooling.md` karanb192 idea 4).
4. **Wired but inert hook forwarders**: `.claude/hooks/instructions-loaded`, `config-change` and
   others have no upstream handler (`tooling.md` "Features we do not use").
5. **Backups of `localhost.yml` sit untracked and un-ignored** beside it (`git status` shows them),
   so a blanket `git add` would stage a copy of the private host vars. Whatever writes them should
   write where `.gitignore` covers, or the ignore should cover the pattern (`tooling.md`
   claude-code-tools idea 1; `fleets.md` emdash idea 3 names the ccy-teams writer).
6. **`docs/ccy.md` is wrong in two places the reports found**: the "realistic worst case" (S1), and
   the statement that usage figures are only available from rate-limit headers on a real response
   (`docs/ccy.md` around line 921; agent-orchestrator's `get_usage`, `fleets.md` AO idea 1, to
   verify on our pinned version).
7. Met while writing this synthesis: `R-SED-FILE-MODIFICATION` denies read-only `sed -n` (documented
   as deliberate; `awk` or `Read` serve), and `R-LSP-SYMBOL-LOOKUP` advised an LSP for a grep of a
   YAML key where no YAML language server exists (an advisory that cannot be followed).
8. **`team-client.md` names a GitHub account** in its "Maintainer responsiveness" section (the
   author filter of a `gh issue list` command). In a public file that ties this repository to a
   specific account; the coordinator should decide whether it stays before the report is
   committed, and the scout brief should say to write `--author <our account>`.

## 8. Filed

Plan 00258, Task 3.3. Every issue body was drafted under `untracked/watchlist-reports/` and passed
the Watchlist.md grep and the private-identifier denylist (non-empty) before sending. The
`watchlist` label did not exist and was created. Dedupe scouts were told 163 live plans; plans 3
and 4's first scouts reported fewer (Glob's 100-result cap) and were re-dispatched until they
said `Checked 163 live plans`.

**Issues (3, the cap):**

- #356 ccy docs: name agent-planted git config, hooks and agent settings as a host-execution risk,
  with what an owner can do now (section 3 rank 1). No duplicate issue; Plans 00225, 00255 and 00206
  share the hazard but none changes the docs.
- #357 tmuxccy: leave copy mode before every key sent to a pane (Defence Before Fix; reproduce
  first) (rank 2). No duplicate issue or plan.
- #358 ccy-network-policy: count refused destinations per session and show them (rank 3). Not a
  duplicate: #114 and Plan 00232 build the policy and the agent-facing refusal, not an owner-facing
  record; the issue cites both.

No comments on existing issues were needed. Ranks 4-18 stay "not filed (cap)".

**New plans:**

- 00259 ccy agent-written files never run on host (section 4 new plan 1).
- 00260 ccy per-session GitHub App token (new plan 2; clean room, cites `gateways.md` only).
- 00261 Defence Before Fix: declared but unenforced configuration (new plan 4). The scout named
  Plan 00192 as a superset by its generic goal of DBF rules for the Go ports; kept separate as one
  named class with its own design question, cross-linked to 00192, 00191 and 00207.

**Not a new plan:** new plan 3 (bus work objects). Plan 00246 holds no v2 verb table; it lives in
Plan 00209 (`DESIGN-v2-A-protocol.md` 8.1), the plan of record for v2's design, so it became a
journal entry there.

**Journal entries** (category `finding`, `JOURNAL/2026-11-20/`): 00146 (`111912Z-finding-59551d`),
00252 (`c296c6`), 00251 (`d71e81`), 00256 (`111efe`), 00116 (`833331`), 00207 (`c93a3b`), 00225
(`606756`), 00227 (`b786a6`), 00246 (`adbe4a`), 00209 (`f4c9ee`). 00254 skipped: the coordinator
already updated it.

**Not acted on here:** section 4 new plans 5-9 and the lower-priority list (not in this task's
scope; the owner may promote them); the upstream items in section 3.

**Tooling faults met while filing (for the coordinator to log):**

1. `hook_scan_text_for_private` returns 0 when the denylist file is empty or missing
   (`scripts/git-hooks/lib/secret-scan.bash`, start of the function), so a failed or empty build
   passes silently; the denylists here were checked non-empty by hand.
2. `R-PLAN-NUMBER-DISCOVERY` denies `printf '%s\n' CLAUDE/Plan/[0-9]*/ | wc -l`, a live-plan count
   Watchlist.md itself asks for (the `ls -d … | wc -l` form passed).
3. `hooks-daemon find-plan 256` returns Plan 00189 first (its title holds "sha256").
4. The dedupe scout cannot reach the full live-plan count with Glob (100-result cap, and
   three-digit plan folders are missed by `000*`/`001*`/`002*` paging); two of six scouts stopped
   short.
5. The PostToolUse plan-write advisory tells a sub-agent to create or delete the failsafe cron,
   which `R-SUBAGENT-CRON-DELETE` forbids a sub-agent to do; not acted on.
