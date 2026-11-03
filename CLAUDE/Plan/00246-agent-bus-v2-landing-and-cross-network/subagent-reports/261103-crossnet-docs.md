# Task 8.8, S1 to S3: the docs rewrite (report)

## What was done

Merged the cross-network branch (`Merge the cross-network branch`, no conflicts), then rewrote the
current Plan 00209 documents that described `listen` and `allow_from` as declared team fields so
they say what the code does: a team declares `networks`; the listen addresses are derived at
`team apply` step 2; the unit's IP filter, the firewall rules and the LXC guest source check read
the effective networks; `team.json` and `approved.json` are `v` 2. Every statement was checked
against `declare/networks.go`, `declare/input.go`, `host/apply.go`, `host/networks.go`,
`host/render.go` and `hostnet/route.go`.

Files (all under `CLAUDE/Plan/00209-agent-bus-v2-in-go/`):

- `DESIGN-v2-C-host.md`: the `<iface>` placeholder; C2; C46; the drop-in sample; section 3.2.5
  (new bullets for the effective networks, the derivation of the listen addresses, the `url`
  host; the firewall, bridge, boundary and no-TLS bullets); DC18 text, step 2 refusals, step 9;
  the `v` 2 note; the 3.7.6 overlay passage; the host_vars sample; the LXC source check and its
  v1-to-v2 table row; the restore note; the resource, DC18 and K16 table rows; the A 6.9 item.
- `DESIGN-v2.md` (per-team cost row, DC18 row), `DESIGN-v2-A-protocol.md` (threat row),
  `DESIGN-v2-B-member.md` (the R48 mentions in the guest check and the C5 assumption),
  `DESIGN-v2-D-migration.md` (the bridge subnet sentence), `SPEC-n3.md` (the source check).
- `SPEC-n2b.md`: DR1, DR3 (the `networks` grammar, `listen` and `allow_from` refused as unknown),
  DR8 (`v` 2), AG1, AG2 (`v` 2, a `v` 1 file refused naming its version), AG6.
- `SPEC-n2d.md`: HS3 (the refusals), HS4 (the filter and the bind), HS7, HS10, n2d-H8.

Left alone on purpose: JOURNAL and subagent-reports; `RESEARCH-*` and `research-remote-seat.md`
(history of v1 or of the pre-change code); `DESIGN.md` R3 and `DESIGN-v2.md` R3 and the R48 row of
B's v1-to-v2 table (those columns state what v1 asked); `SPEC-view.md:83` (a flow `listen` list,
unrelated); Plan 00209's `PLAN.md` (over its size limit; its lines 55, 191 and 200 already read as
the direction, not as declared fields). `docs/`: the only pages that name `allow_from`
(`docs/agent-bus.md`, `docs/agent-bus-lxc-members.md`) document the deployed v1 bus, whose
`agent_bus_teams` still declares it; no `docs/` page describes v2 teams, so none changed.

## Findings for the owner or the lead

1. SPEC-cross-network X6 says a `url` IP literal that is no derived listen address is refused "at
   `declare check`"; the code refuses it at `team apply` step 2 (`host/apply.go` `urlHost`), and
   `declare check` has no such check. The design documents now say step 2. Either X6's wording or
   the code should move; I did not edit the spec.
2. A new network that adds a derived listen address changes the config's `address` line, so it is
   a restart, not a reload (`TestASecondNetworkChangesOnlyWhatItShould`); SPEC-n2d HS7 now says a
   reload only when the derived listen addresses are unchanged (the old "a changed allow_from is a
   reload" would have been false).
3. DESIGN-v2-C-host.md 3.2.5 still says "no TLS" and now names the cross-network spec's section 3
   as the cure for traffic over a declared network beyond the machine.
4. Plan 00244's own `PLAN.md` was not edited (not in my brief): the lead decides whether Task 8.8
   needs a status line for the docs half.

No source file, JOURNAL entry or subagent report other than the two new ones was touched.
`261103-crossnet-lxc.md` carries the S3 colleague's report text with its red evidence.
