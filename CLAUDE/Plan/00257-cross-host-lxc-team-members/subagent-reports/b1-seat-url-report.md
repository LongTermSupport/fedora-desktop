# Plan 00257 Task B.1: LXC seats reach the team on the bridge; the team url is free

Branch `teamchat-b1-seat-url` (from `origin/F44`). The plan folder is on
`plan-xhost-lxc-members`, not F44, so this report is here (`untracked/scratch/b1-report.md` in
the B.1 worktree) and not in the plan's `subagent-reports/`.

## What changed

X5 no longer requires the team `url` to be an address inside the LXC bridge's subnet.

- **`declare.LXCSeatURL(team, listen, subnet)`** is the base url that a seat of kind `lxc` is
  given:
  - When the url is https and its host is already an address inside the bridge subnet, it is
    returned byte for byte. So a team of LXC members alone is served exactly as before.
  - Otherwise it is `https://<first listen address inside the subnet>:<team port>`.
  - A plain-http url, or no listen address inside the subnet, is an error.
- **`declare.Input.LXCURLProblem(listen func, bridge func)`** replaces `BridgeURLProblem`. With
  a present LXC member, it refuses a plain-http url, or a team with no listen address on the
  bridge.
  - The listen set is asked for only when the url is not already on the bridge. That keeps the
    old url-on-the-bridge case free of any new read.
- **`host.AppliedListen(root)`** reads the listen set from units.json.
- **`host.AppliedLXCURL(root, bridge)`** is `LXCSeatURL` over that listen set and the live
  bridge subnet.
- **team apply** (`host/apply.go` urlHost) calls `LXCURLProblem` with the listen set it has
  just derived. X6 still holds a literal url to the listen set; X12 and URLProblem are
  unchanged.
- **lxc apply** (`host/lxc/apply.go` bridgeURL, present phase) calls `LXCURLProblem` with
  `Deps.Listen`. That is a new seam, wired to `host.AppliedListen(root)`; the refusal is a
  drift (70) before any change, as before.
- **admin**:
  - `SeatBundles.LXCURL` and `System.LXCURL` are new; `admin.LXCURL` is a new type. Both are
    wired to one `host.AppliedLXCURL(root, hostnet bridge)` in `admin/wire/wire.go`.
  - `Bundle` gives the LXC url to a handle whose type is `lxc`. The handle type is the member
    kind, since member add refuses a kind that is not the prefix type (MO3), so return and
    reissue are covered as well as add. Every other kind (`podman`, `docker`, `vm`, `host`) gets
    `team.URL`.
  - If no LXC url can be had, an LXC seat gets no bundle at all, never the team url.
  - `lxcRoster.Reach` (the stdin of guest check) now carries the LXC url, so the guest checks
    the url its bundle will carry.
- **CA pinning is unchanged.** `teamCAPEM` still takes the scheme from `team.URL`, and both urls
  are https. The same team CA PEM goes into both bundles and into the reach facts.

## TLS leaf (IP SAN for the bridge address)

The leaf already names the bridge address, so the leaf code is unchanged:

- `leafNames(url, listen)` puts every listen address in the IP SANs.
- When a present LXC member exists, the bridge subnet is an effective "own" network. `serve`
  then refuses unless an interface address inside the bridge subnet exists, and that address
  becomes a listen address.
- The LXC url is only ever built from a listen address, or from a url host that X6 already
  holds to the listen set. So it is always in the SANs.

The test `TestLXCMemberLeavesTheURLFree` proves it: with a DNS-name url, the leaf names are
IPs `[192.0.2.10, 198.51.100.1]` plus DNS `chat.example.com`.

## Every reader of team.URL on the LXC path, decided per site

| Site                                                                        | Decision                                                                                                           |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `admin/bundles.go` Bundle `BaseURL`                                         | per kind: lxc gets the LXC url, every other kind `team.URL`                                                        |
| `admin/bundles.go` teamCAPEM (scheme of `team.URL`)                         | unchanged: both urls are https, same CA                                                                            |
| `admin/lxc.go` lxcRoster.Reach `base_url` (guest reach check)               | now the LXC url                                                                                                    |
| `declare/networks.go` BridgeURLProblem (X5)                                 | replaced by LXCURLProblem / LXCSeatURL                                                                             |
| `host/apply.go` urlHost (X5 call)                                           | LXCURLProblem with the derived listen set                                                                          |
| `host/lxc/apply.go` bridgeURL (X5 call)                                     | LXCURLProblem with Deps.Listen (units.json)                                                                        |
| `host/tls.go` / `host/leaf.go` leafNames (`team.URL` host as DNS SAN)       | unchanged: listen IPs already include the bridge address                                                           |
| `element/element.go` (`g.URL`)                                              | unchanged: the human's Element/browser url, not an LXC seat                                                        |
| `ctl/ctl.go` admin and member base                                          | unchanged: it is the loopback `http://127.0.0.1:port`, not team.URL                                                |
| `health` (probes of LXC seats; `host.AppliedReach`)                         | unchanged: LXC probes read the seat lock, and the reach probe walks every listen address                           |
| `seat/homeserver.go` checkout.json identity (guest seat key and homeserver) | unchanged code; it records the bundle's base_url, which for an all-bridge team is byte for byte the same as before |
| `member/guest/check.go`, `matrix` member client                             | unchanged: they consume the given base_url and tls_ca                                                              |

## Tests (RED commit, then GREEN)

Both requested properties hold: the first case still behaves exactly as before, and the
plain-http loopback rule stays as it was.

1. **A DNS-name url with a present LXC member is accepted:**
   - by declare: `TestLXCURLProblem` "a name" and "another address";
   - by team apply: `TestLXCMemberLeavesTheURLFree`;
   - by lxc apply: `TestAURLOffTheBridgeServesTheGuests`.
2. **The LXC bundle carries the bridge url; host, podman and vm bundles carry team.URL:**
   `TestSeatBundlesGivesAnLXCSeatTheBridgeURL`. `TestSeatBundlesFailsWithoutTheLXCURL` covers
   the case with no LXC url.
3. **The guest reach check uses the bridge url:** `TestLXCRosterReachIsTheBridgeURL` (DNS url,
   bus address, bridge url) and `TestLXCRosterReachFailsOffTheBridge`.
4. **The all-bridge case is unchanged:**
   - `TestLXCSeatURL` "on the bridge" and "on the bridge at 443" (kept byte for byte);
   - `TestLXCMemberWithTheURLOnTheBridge`;
   - the existing lxc apply world, whose url is on the bridge;
   - `TestAListenSetThatCannotBeReadFails`, part 1: a url on the bridge never reads the listen
     set.
5. **Refusals:**
   - no listen address on the bridge, and plain http: declare and lxc apply
     (`TestATeamNotServedOnTheBridgeIsRefused`);
   - plain http at team apply: `TestLXCMemberNeedsAnHTTPSURL`. X6 refuses it first there,
     because loopback is never a listen address;
   - a listen set or bridge that cannot be read;
   - `TestAppliedLXCURLFailures`.
6. **Wiring:** `TestLXCURLIsTheHostsAppliedOne`. The existing nil-field checks over `System` and
   `lxc.Deps` now cover `LXCURL` and `Listen`.

## Behaviour notes (no capability lost)

- **A plain-http url is now refused while an LXC member is present.** Such a team was already
  refused before: loopback is not in the bridge subnet. The refusal text is new.
- **`TestLXCRosterReach` used to assert that Reach passes a loopback http url through.** That
  case could never serve a guest. It is now `TestLXCRosterReachFailsOffTheBridge`: Reach
  refuses it, and asks for no CA.
- **lxc apply reads `units.json` only for an LXC team whose url is off the bridge.**
  - That file is written by the team apply that precedes lxc apply in the play; lxc apply also
    requires the team's install stamp.
  - If the team's url is on the bridge but its last apply did not listen there, lxc apply
    behaves as before: it judges the declared url alone.

## Docs updated in the fix commit

- `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md`: the as-built note in 3.5.1, the X5
  paragraph.
- `CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md`: the old review-fixes line is marked
  superseded.
- `docs/agent-bus-lxc-members.md`: the url bullet.
- `playbooks/imports/play-teamchat.yml`: the header comment on `teamchat_lxc_members`, the
  `tc_reach_re` comment, and the `teamchat_address` fail_msg reason.
- No SPEC states the url rule. The X5 in SPEC-cross-network (Plan 00246) is the guest source
  check, which is untouched.

## Host run

None in this task: it is Go code that runs when `play-teamchat.yml` next deploys teamchat.
`meta-deploy.list` is untouched; the coordinator owns queueing the play.
