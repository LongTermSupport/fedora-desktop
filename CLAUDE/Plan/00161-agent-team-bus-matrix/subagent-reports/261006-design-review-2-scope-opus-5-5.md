# Plan 00161 design review 2: scope and plan quality

Lens: YAGNI, build order, milestone value, cross-file consistency, old-design leftovers,
leaks, and whether each owner answer is honoured. Read-only.

## Blockers

**B1. `ccy.env.local.dist` ownership is stale (answer 8 not delivered).** DESIGN §5.3 and
U23 say the dist is daemon-owned and ask on daemon issue #88. Plan 00160 Task 3.2 has since
moved it to ccy: `lib/common.bash` `ccy_env_local_dist_text` writes it, with
`CCY_ENV_LOCAL_DIST_VERSION`. #88 is withdrawn, and the `HOOKS_DAEMON_HOSTNAME` line is
already there. Fix: U21 adds a commented `#export PINGBUS_TEAMS=<team>[,<team>]` block to
`ccy_env_local_dist_text`, raises `CCY_ENV_LOCAL_DIST_VERSION`, and extends
`scripts/test-ccy-env-local-dist.bash`. Remove the #88 text from §5.3, U23 and the revision
report's follow-ups. Add Plan 00160 as a dependency, because the dist must exist first.

**B2. PLAN.md contradicts the design.** Phase 2 still lists U00–U30 and "M3 warden and
control room". Success criterion 2 says "no agent can send free text", but D13 says the
server cannot block it. Fix: copy the DESIGN §12 milestone table into Phase 2. Reword the
criterion to "no agent's free text reaches another agent". Mark Task 1.2's "separate control
room" as superseded by Task 1.3/1.4.

**B3. The U24 docker leg quietly skips ("only when docker is installed").** Owner answer 2
makes docker first-class, and a silent skip breaks fail-fast. Fix: report
SKIPPED-NEEDS-OWNER and block plan close, as P2's VM leg already does.

## Other findings

1. **The first real ping comes late (§12 Milestones).** M1 needs 16 units. U18 depends on
   U17 (the play), but M1 needs only the installer. Fix: make U18 depend on U16 and move
   U17 to M3, so the first ping does not wait on playbook work.
2. **U24 is too big for one unit.** It covers LXC, docker, a VM, the standalone installer in
   a VM, and a WireGuard link that the script builds and tears down. Split it: U24a for
   same-host bridges (LXC, docker, VM), U24b for the installer in a VM plus the WireGuard leg.
   A link that acceptance creates on the host also needs an IaC note (how it is created and
   how it is removed if the run fails).
3. **U25's conditional TLS has no fixed scope.** Make "TLS with a team-private CA" its own
   unit, created only if H7 fails, so U25 stays bounded.
4. **YAGNI.** `rotate-admin`, `tail`, `show` and `peers` serve no success criterion. U19
   sits in M2, but U22 does not need it. Fix: keep `status` (wake path) in U12 and defer the
   other report commands and `rotate-admin`.
5. **Answer 5 is applied more strictly than the owner stated it.** §3.3/D4 refuses LAN and
   Wi-Fi addresses. The owner said "an address members can route to". The restriction is
   defensible without TLS, but it should be an owner question, not a silent narrowing.
6. **Reusing the wave-1 U02 branch.** That branch replaced PROTOCOL.md with a stub and wrote
   `docs/agent-team-bus-protocol.md`. Fix: take only `helpers/`, `tests/` and `link_check.py`
   from it. Do not merge its PROTOCOL.md. Rename the doc to `docs/agent-bus-protocol.md`.
7. **`registry.json` has no defined place.** §3.7 backs it up, but §3.4 step 2 does not list
   it. Add it to the layout.
8. **§11 container column.** It lists U23, which is a docs unit, as tested "against fakes".
   Remove it.
9. **U17 has no test.** Add a container check, `ansible-playbook --syntax-check`, via the
   repo's existing QA (Ansible 2.19 parsing gotchas).

## Owner answers

1, 2, 3, 4, 6 and 7 are honoured. The warden question is answered plainly (D14: gone).
Answer 5 is honoured but narrowed (finding 5). Answer 8 is blocked by B1.

## Leaks and jargon

No install-specific names. The examples use `workstation`, `example-org`, `alice` and
placeholders. "Seat", "kit" and "bundle" are defined where they are first used.

## The owner's paste request (not in this plan's scope)

The infra agent's role override needs no Plan 00161 work. It needs CCY 3.83.0 deployed
(Plan 00160, queued in `meta-deploy.bash`). Then that project's IaC places an untracked
`.claude/ccy/ccy.env.local` whose content is the dist with
`# based on ccy.env.local.dist version 1` and `export HOOKS_DAEMON_HOSTNAME=<role>`. The
role-scoped entries in its `.claude/hooks-daemon.yaml` (`persistent_crons` `hosts:`, the
top-level `hosts:`) then name `<role>`.
