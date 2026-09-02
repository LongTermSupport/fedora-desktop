# Plan 00079 — Facts, Findings and Risks

Supporting document for [PLAN.md](PLAN.md). Confirmed facts (per
`CLAUDE/PlanTriage.md`), each traceable to its source. Decisions that rest on
these facts are in [DECISIONS.md](DECISIONS.md).

## Phase 0 research and host triage (F1–F14)

Settled and compacted here; the verbatim originals with all their sources are
in `JOURNAL/00079-Journal-26-08-20.md` (section "Facts F1-F14 relocated from
PLAN.md"). The claims are unchanged.

- **F1/F2/F10** — `pause`/`unpause` (cgroup freezer, takes `--all` and
  `--filter network=|label=|name=|status=|ancestor=`) is the **only**
  rootless-capable mechanism for the user's four verbs; CRIU
  `checkpoint`/`restore` is root-only
- **F3/F11** — rootless Podman 5.8.4, cgroups v2, `systemd`/`crun`: **rootless
  pause is viable, Task 0.3's gate passes** (H1)
- **F4** — CCY containers are named `<project>_<suffix>[_N]`, suffix `yolo` or
  `browser` (`common.bash:598-631`) — the fallback selector, see F19
- **F5/F12** — the `run` invocation passed **no `--label`**; only the *image*
  carried `claude-yolo-version`, which matched the running set at the time and
  so looked sufficient. F16 is why it was not
- **F6** — CCY containers run with `--rm` (`claude-yolo:2944`)
- **F7/F8/F9/F14** — `fzf` is already deployed with a numbered-menu fallback; no
  container UI tool is deployed by any play; podman-tui is packaged and its
  socket already active (D3's dependency question)
- **F13** — `pause` accepts `-f, --filter`, and `ps --filter network=`
  partitions every network correctly, reporting empty sets rather than errors
- **F15** — **a CCY container shares a user-defined network with a
  ten-container compose stack**, and **seven CCY containers share the default
  `podman` network**

**F15 is the safety case, and no hypothesis anticipated it** — a network-scoped
freeze's blast radius is not guessable from the network's name. D7 records where
that protection ended up.

H5 (checkpoint refuses `--rm` without `--export`) was not probed — checkpoint is
a non-goal per F2 + F3, so it matters only if that is reopened.

## From the first host acceptance run (Phase 3)

- **F16** — **the inherited `claude-yolo-version` label over-matches**: it marks
  anything BUILT FROM the CCY image, session or not. Demonstrated — `--ccy`
  selected the gate's own throwaway, built from `localhost/claude-yolo:*`.
  Fixed by the run-time `ccy=true` label (Phase 1); the image label is no longer
  consulted (D6)
- **F17** — **`podman` is ONE shared bridge network, not a per-container
  default.** `podman network ls` lists it with a single NETWORK ID and the
  `bridge` driver, and seven CCY sessions were attached to it simultaneously.
  So a session launched without `--network` joins the same L2 domain as every
  other one. That is podman's default rather than a CCY choice, but it makes
  "network: podman" in the menu approximately "every session that did not join
  a project network" — and it is why F15's hazard runs the other way too: an
  eighth session sat on the app network `mkt` instead
- **F18** — **the host was running a `podfreeze` older than the repo's**, and
  `acceptance.bash` refused rather than verify it. The D9 drill-down (`a678e31`,
  10:42) landed **10 minutes after** run 2's deploy (10:32), so run 2's "16
  passed" never exercised it. Plan 00099's defect class, caught by the check
  written for it. **`deploy.bash`, not `acceptance.bash`, is the entry point
  whenever the tool itself changed**
- **F19** — **check 9 verified only the labelled path.** It built its expected
  set from `label=ccy=true` alone, so the name-pattern fallback was never
  asserted — and **4 of the 6 live sessions are unlabelled**, so the majority of
  the real population reaches `--ccy` through the one path the gate was silent
  about. Had the pattern broken, check 9 would still have reported OK. Identical
  in shape to Plan 00080's P4. Closed by **9b**, which then **proved** the
  fallback works — all four named, all four resolved
- **F20** — **the identity axes silently under-match unlabelled sessions.** An
  unlabelled session is in `--ccy` but in **no** identity group, because its
  account cannot be inferred. `select_identity` refused the case where *nothing*
  is labelled and said nothing about the case where only *some* is — the state
  the machine is in until every session is relaunched. So
  `podfreeze freeze --github X` answered "every session for X" with a strict
  subset — the exact ask the axis was added for. Fixed by disclosing on
  **stderr**: the identity is genuinely unknowable, so blocking would be wrong;
  being quiet was the defect

## From the `qa-reviewer` pass (Task 3.3)

Three more of the same shape, found in the diff that documents the shape:

- **F21** — **the identity menu row was gated on cardinality, not coverage.**
  `${#values[@]} -lt 2` is only a proxy for "this value covers every session",
  and it is wrong in exactly the partial-rollout state the axis is for: one
  account + four unlabelled sessions is one distinct value covering 2 of 6. The
  row vanished from the menu while `--github X` still worked on the CLI, leaving
  the *wider* "all CCY containers" as the only menu route — steering toward
  freezing sessions the user had not asked for — and making F20's disclosure
  unreachable, since `select_identity` was never called
- **F22** — **`none` and "unlabelled" were collapsed.** The inventory comment
  claimed they stayed distinct; the code dropped `none` into the same empty-map
  state as an absent label, so a 3.40.0 session with no identity on any axis was
  reported as pre-3.40.0 and its owner told to relaunch it — advice that cannot
  work. Fixed with `INV_HAS_LABELS`, keyed on the `ccy=true` query result
- **F23** — **acceptance check 13 could pass having asserted nothing.** `gh_one`
  comes from a query including paused sessions; the expectation narrowed to
  running. The only labelled session being paused — normal, since pausing is
  this tool's job — left an empty expected set, an unexecuted loop body, and a
  printed pass over a population of zero

## Risks & Mitigations

| Risk                                                                               | Impact | Probability | Mitigation                                                                                         |
| ---------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------- |
| Freezing a live CCY session mid-write (agent stalls, SSH/API connections time out) | M      | M           | Menu row names the verb and count before the choice; CCY rows marked; reversible by repeating (D7) |
| H1 wrong — rootless pause blocked on this host                                     | H      | L           | Phase 0 decision gate before any build                                                             |
| Name-pattern CCY match catches an unrelated `*_yolo` container                     | L      | L           | Explicit labels (D4) make the pattern a transition fallback only                                   |
| User forgets containers are frozen (a paused container looks hung)                 | M      | M           | `podfreeze list` surfaces paused set; freeze prints the exact thaw command                         |
| `--rm` interaction: `podman stop` on a paused `--rm` container removes it          | M      | L           | Tool never stops; docs warn to thaw before stopping                                                |
