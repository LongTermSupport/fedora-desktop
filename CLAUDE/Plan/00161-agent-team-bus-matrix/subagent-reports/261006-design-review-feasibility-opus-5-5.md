# Design review: feasibility and IaC fit (Plan 00161)

Lens: does each step work with the real tools as the research reports describe them, does it
follow this repo's rules, is anything in the wrong layer, and is each build unit really
testable on its own. Reviewed: `DESIGN.md`, `PROTOCOL.md`, and the six
`subagent-reports/261006-research-*.md`. Repo facts checked against the F44 tree where noted.
Claims about upstream behaviour that this container cannot run are marked "probe" and say
which probe should settle them.

Totals: 3 blocker, 13 should-fix, 7 nit.

---

## Blockers

### B1. `<ns>.role` state events use another user's ID as `state_key`, and the Matrix auth rules reject that

- **Problem.** PROTOCOL.md §2 gives `io.github.longtermsupport.agentbus.role` a `state_key` of
  "member's user ID", sent by the creating human. DESIGN.md §7 puts one per agent in
  `initial_state`, and so does the research example (`[tuwunel] §5.2`). Every room version,
  v12 included, has the auth rule "if the event has a `state_key` that starts with `@` and
  does not match the sender, reject". Relaxing it is MSC3757, which v12 does not include.
  Tuwunel's state resolution (ruma) enforces the rule.

- **Scenario.** `pingbus room create … --orchestrator X --worker Y` sends `createRoom` with
  `initial_state` containing `role` events keyed `@X:<sn>` and `@Y:<sn>`, sent by the human.
  The server rejects the room, or the room is created without those events. Then no agent
  holds a role. Every send is refused with `role`/`target`, and every worker's post-join
  check fails and it leaves the room (exit 10). This breaks the core of U08, U09 and U15.
  The fake homeserver will not enforce auth rules, so U01 to U15 all pass in the container
  and the failure first appears at U23.

- **Fix.** Change the key to one that does not start with `@`. Either:

  - use the localpart (the handle) as the `state_key`, with the user ID inside the content; or
  - use a single `<ns>.roles` state event (`state_key ""`) holding a map from user ID to
    role. This is simpler, it is atomic, and the warden reads it in one call.

  Then:

  - Update PROTOCOL.md §2 and the receive rules.
  - Teach `fake_homeserver.py` this auth rule, so the container tests catch the error class.
  - Add "room create with the role state" to probe H4's throwaway instance (U00), rather than
    leaving it to U23.
  - Fix the same example in the research report.

### B2. DNS on the dual-homed ccy container is unprobed, and either outcome breaks something

- **Problem.** The ccy session keeps `podman` (`dns_enabled: false`, `[ccy] §1`, F25) as its
  primary network and adds the team network with DNS on (DESIGN.md §2 `DNS=true`, §5).
  Podman writes the aardvark-dns address of every DNS-enabled network into the container's
  `/etc/resolv.conf`, in place of the host's resolvers. All name lookups then go to the
  team network's aardvark-dns. Whether aardvark forwards external names for an `--internal`
  network depends on the netavark/aardvark version (recent releases do not forward for
  internal networks). Either way something breaks:

  - **If it does not forward,** the ccy session cannot resolve `api.anthropic.com`,
    `github.com`, and so on. Claude Code and the forge check (send step 5) die.
    `--team` breaks the session it was meant to extend. ccy's internet preflight cannot see
    this, because it runs `alpine wget` on the primary network only (`[ccy] §1`).
  - **If it does forward,** the homeserver has a DNS path out through aardvark, which runs in
    the rootless netns with pasta egress. That is the "no route out by construction" claim
    in D4 failing. P1 also misses it: P1 captures inside the homeserver's netns and passes
    any packet to an address in the team subnet "including DNS", and the aardvark gateway
    is in the team subnet.

- **Scenario.** The first `ccy --team <team>` launch on the host. The launch succeeds, then
  Claude Code fails to connect, or nothing fails visibly and the leak goes unproven.

- **Fix.**

  - Extend H1 to cover the real launch shape. A ccy-image container on `podman` plus a test
    `--internal` DNS network must:
    - print its `resolv.conf`;
    - resolve and reach an external name (the Claude API host and `api.github.com`);
    - resolve the peer by name.
  - Add H6: from a container on the internal network alone, query an external name against
    the gateway and record whether it is answered or forwarded.
  - Choose the design from the result:
    - **If external names fail,** give members the homeserver by a fixed IP or a static
      `--add-host`, and create the team network with DNS off (`DisableDNS=true`).
    - **If aardvark forwards,** create the team network with DNS off for the homeserver's
      sake, and make P1 FAIL on any DNS query the homeserver sends.

  In both cases `member_base_url` may need an IP or `--add-host` name rather than relying on
  aardvark.

### B3. The Quadlet `.network` keys are wrong, and the network name the other units bake in does not exist

- **Problem.** DESIGN.md §2 specifies `agent-team-<team>.network` with `DNS=true`. In Quadlet,
  `DNS=` sets nameserver addresses (`podman network create --dns`). It is not a switch:
  DNS is on by default, and `DisableDNS=true` is what turns it off. Second, Quadlet names
  the podman network `systemd-<unit name>` unless `NetworkName=` is set. U18 (ccy attach),
  the section 8 member contract (U21), P1's `podman network inspect` and H1 all address a
  network called `agent-team-<team>`, which will not exist.
- **Scenario.** The network unit fails to start (`true` is not an IP address), or, if that
  key is dropped, the ccy launcher's `--network agent-team-<team>` fails with "network not
  found". U16 and U18 are built in parallel against different names, and the mismatch first
  appears on the host.
- **Fix.**
  - Remove `DNS=true` (and decide `DisableDNS=` per B2).
  - Set `NetworkName=agent-team-<team>` explicitly.
  - Put the network name in `team.json` (`network`), and have `agent-team add-member` print
    it in its marker line, so ccy and the member contract read it from one place instead
    of rebuilding the name.
  - Add a container-side test that renders the Quadlet templates and checks every key
    against Quadlet's documented key list for `.network` and `.container`.
  - Add `quadlet -dryrun -user` to U00, as `[services] §7` already proposes.

---

## Should-fix

### S1. Probe H2 may fail, and the fallback quietly drops the main privacy property

- **Problem.** D4 rests on `PublishPort=127.0.0.1:<p>:8008` working for a container on an
  `--internal` network. The research marks this unknown (`[ccy] §1`, `[services] §6 D4`).
  The stated fallback is a normal network, with P1 "proven by capture alone". That gives
  the homeserver full egress, which is the opposite of D4.

- **Scenario.** H2 fails, and the play ships a homeserver with an outbound route. Privacy
  then depends on Tuwunel never connecting out, checked only by one scripted capture.

- **Fix.** Design a fallback that keeps "no route out":

  - Keep the homeserver on the internal network only, and front it with a second tiny
    container that sits on both the internal network and a publish-capable network and
    forwards one port. This is a socat-style proxy in an image pinned by digest.
  - Or use Tuwunel's `unix_socket_path` on a shared volume, with `systemd-socket-proxyd` on
    the host at `127.0.0.1:<port>`.

  Write the chosen fallback into DESIGN.md before U16, so the H2 result does not force a
  redesign on the host.

### S2. The core play cannot depend on a zipapp that only the optional play builds

- **Problem.** DESIGN.md §2: `play-claude-yolo.yml` (core, always run) "stages … the built
  zipapp into the build context". The zipapp is built by `play-agent-team-bus.yml`
  (optional) into `~/.local/share/agent-teams/dist/`. On a machine that never ran the
  optional play, the core play either fails or has to skip the copy. Skipping breaks the
  fail-fast rule, and failing breaks "nothing changes for a user who does not enable it".
  There is also no ordering between the two plays in `meta-deploy.bash`.
- **Scenario.** A user runs the main playbook without the bus play: the ccy image build
  fails on a missing source file.
- **Fix.** Have `play-claude-yolo.yml` build the zipapp itself with
  `command: argv: [python3, -m, helpers.pingbus.bundle, --out, <build-context path>]`
  (`chdir: root_dir`). The build is pure and reproducible (D11), so both plays produce
  identical bytes, and the optional play keeps its own copy for `~/.local/bin` and `dist/`.
  Say in DESIGN.md that the image always ships pingbus, inert until `--team`.

### S3. A slot is the first free container name, so a session can come back as another session's identity

- **Problem.** D8 keys the credential on the container slot. `get_next_container_name`
  (`files/var/local/claude-yolo/lib/common.bash:951-985`) picks the **first free** name:
  `_yolo` first, then `_yolo_1`, `_yolo_2`. So the slot is not stable. (The suffixes run
  from `_1`, not `_2` as DESIGN.md §5 says.)

- **Scenario.**

  - Session A runs in `_yolo` and B runs in `_yolo_1`.
  - A ends. B's container is recreated (a restart or reboot restore), or a new session C
    launches. Either one takes `_yolo`, and with it A's handle, token and inbox directory.
  - Pings addressed to A, and acks owed to A, now reach a different conversation. This is
    exactly what the issue's "never reused, so old messages are never attributed to a new
    session" forbids.
  - DESIGN.md §5's "a resumed session keeps its number" does not hold.

- **Fix.** Pick one of these:

  - Key the credential on something stable for a conversation. For example, the launcher
    records the claimed slot in a host-side lock file held for the container's life, and
    restart and restore reuse the recorded slot rather than the first free name.
  - Or allow one team session per checkout in v1 and refuse a second
    (`[ccy]` open question 3).

  Either way, state in DESIGN.md that a handle names a slot, not a conversation, since the
  issue says "one handle per agent session".

### S4. U18 and U19 cannot run as independent parallel branches

- **Problem.** U19 bumps the container version. That means `LABEL claude-yolo-version` in the
  Dockerfile **and** `REQUIRED_CONTAINER_VERSION` in `claude-yolo`. Any launcher change
  needs a `CCY_VERSION` bump, which the pre-commit hook enforces (`[ccy] §3`). U18 also edits
  `claude-yolo` and bumps `CCY_VERSION`. The two branches edit the same version lines and
  the same changelog head.
- **Scenario.** The wave {U11, U19} merges after U18. The result is either a conflict on
  `CCY_VERSION` or two changelog entries for one release. Worse, U18's launcher can ship
  while U19's image is not yet built, and `--team` sets `PINGBUS_CONFIG` for an entrypoint
  that ignores it.
- **Fix.**
  - Merge U18 and U19 into one ccy unit with one version bump, or put them in strict
    sequence with U19 owning both bumps.
  - Make the launcher refuse `--team` when the image's `claude-yolo-version` label is older
    than the version that added team support.

### S5. A worker is never told about an invite, so the success criterion needs an out-of-band step

- **Problem.** Joining is explicit (`pingbus room join`, `auto_accept_invites = false`), but
  `wait`, `recv` and the hooks print only `PING` and `TIMEOUT` lines (PROTOCOL.md §14). An
  invite wakes nobody. The human cannot tell the worker over the bus, because human free
  text never reaches an agent.

- **Scenario.** A human runs `room create … --worker W`. W's background `wait` sees an
  invite, prints nothing, and keeps waiting. The orchestrator's `review` ping is never
  delivered, because W is not in the room. The ack times out.

- **Fix.** Either:

  - let the syncer auto-join an invite that passes the creator check, and run the post-join
    verification it already has (this is deterministic and needs no LLM, so it is no
    weaker); or
  - add an `INVITE` output line (room ID and creator localpart only) that ends `wait` and
    appears in the Stop and prompt hooks.

  Specify the warden's own join rule too. DESIGN.md says "the warden is always invited" but
  not how it accepts.

### S6. `IPAddressDeny=` does not work under the user service manager, and H5 does not test whether it is enforced

- **Problem.** D25 confines Element with
  `systemd-run --user --scope -p IPAddressDeny=any -p IPAddressAllow=localhost`.
  - IP filtering attaches BPF cgroup programs. The per-user manager is unprivileged, so it
    logs that the unit configures an IP firewall it cannot apply, and then runs the process
    unconfined. That is a silent no-op, which goes against fail fast.
  - H5 checks only whether the Flatpak process stays in the scope's cgroup, not whether the
    filter works. Flatpak also moves apps into its own `app-flatpak-…scope`, which the
    research already suspects.
- **Scenario.** H5 passes on cgroup placement, and the launcher ships with a confinement
  that confines nothing. P4's time-window capture is then the only proof.
- **Fix.**
  - Make H5 test enforcement: inside the same `systemd-run --user --scope` with the
    properties set, `curl` an outside address must fail.
  - Expect it to fail, and drop D25 unless a privileged mechanism is chosen. That would be
    an nftables rule matching the app's cgroup, managed by Ansible. It is complex, because
    Flatpak scope names are dynamic.
  - Make P4's attribution robust (S7).

### S7. P4 cannot attribute traffic to the team profile, because the same Flatpak app talks to matrix.org

- **Problem.** P4 FAILs on unexplained packets "from the Element process's time window, with
  other network users quiet". Issue §9 keeps the user's other Matrix accounts in the same
  `im.riot.Riot` app, on the default profile, untouched. A desktop also cannot be made
  "quiet". So a time-window host capture cannot tell the team profile from the user's own
  Element, the browser, or dnf.

- **Scenario.** P4 either fails on normal noise or is waved through by a human reviewer.
  Either way it is not the "check, not an assumption" the issue requires.

- **Fix.** Do the P4 capture where only the profile can be the source:

  - run the scripted Element session for the check in a dedicated network namespace (for
    example `pasta` or `unshare -n`) whose only route is a forward to `127.0.0.1:<port>`; or
  - match by cgroup with nftables logging during the check.

  Separately, assert that the profile's `config.json` contains every key from
  `[clients] §1.4` (a static check that can run in the container).

### S8. The warden is a sender, so it needs a forge credential, and DESIGN.md does not give it one

- **Problem.** PROTOCOL.md §8 runs the forge check "on send (`pingbus send`, and the warden
  before it emits)". The forge check needs `PINGBUS_FORGE_TOKEN`/`GH_TOKEN` for private
  repositories, and the unauthenticated GitHub limit is 60 requests an hour. The warden
  unit (DESIGN.md §8, U15) is given a Matrix token and nothing else.
- **Scenario.** A human types `!sync <ref>` for a private repository. The warden gets 404,
  refuses it as `not-found`, and the human sees a misleading error. With public
  repositories, the 61st send in an hour fails with a 403 that maps to `forge-auth`.
- **Fix.**
  - Decide whether the warden checks the forge at all; offline validation plus the agent's
    own re-read may be enough (D18 already makes receive offline).
  - If it does, add a forge token file for the warden (0600, `LoadCredential=` in the user
    unit) and say where it comes from.
  - Map a GitHub 403 with `x-ratelimit-remaining: 0` to its own reason code (`forge-rate`)
    and exit 9, not `forge-auth`.

### S9. `room create --as-human` makes the member CLI read the host's provisioning store

- **Problem.** `pingbus room create` runs on the host with a human credential, but
  `member.json` allows only a §3 handle or `warden` as `handle` (PROTOCOL.md §11). There is
  no human config, so the zipapp would have to read
  `~/.config/agent-teams/<team>/humans/<name>.token` and `team.json`. That ties the
  portable member CLI, which also ships to section 8 members, to the host-only provisioning
  layout. It also puts a host-only command into the tool every container gets.
- **Scenario.** U09 cannot test `room create` against the documented config schema, because
  no valid config represents a human.
- **Fix.** Move room creation to `agent-team room-create <team> --as-human <name> …` in the
  host-only `agent_team` package. It can import `helpers.pingbus.matrix` and
  `helpers.pingbus.protocol`, just as the warden does. Remove `room create` from PROTOCOL.md
  §12.

### S10. A team can be added but not removed, and `server_name` can drift

- **Problem.** The play iterates `agent_teams` and creates Quadlets, directories and warden
  instances. Nothing removes them when a team leaves the list. The issue's "a team can be
  removed without touching the others" therefore has no IaC path. Tuwunel's `server_name`
  "cannot change without wiping the database" (`[tuwunel] §2`), but host_vars can change it,
  or the default can change it through a team rename.
- **Scenario.**
  - An owner deletes a team from host_vars and re-runs the play. The homeserver keeps
    running, and its warden keeps running, unmanaged.
  - Or `server_name` changes and the play re-renders `tuwunel.toml`. Tuwunel then refuses to
    start, or starts against a mismatched database.
- **Fix.**
  - Add `state: absent` support per team. A removal list, or `state:` on each item, stops
    the units and removes the Quadlets and warden instance. Data is kept unless a
    `purge: true` is given.
  - Have the play compare the `server_name` recorded in the existing `team.json` with the
    one rendered, and fail with a clear message if they differ.

### S11. Plugin-hook loading is the basis of the whole wake design, yet it is first verified at U23

- **Problem.** D13 depends on Claude Code loading `hooks/hooks.json` from a plugin copied
  into `/root/.claude/plugins/<name>` and enabled with a bare `enabledPlugins` key. That
  bare key is the phpantom-lsp pattern (`entrypoint.sh:336,357-360`), and it has only ever
  been used for an LSP plugin. The research marks hook loading as unverified (`[wake] §2`),
  and DESIGN.md §11 makes it a host-only check, run last.
- **Scenario.** At U23, the plugin's hooks turn out not to load outside a marketplace. U10,
  U17 and U19 are built on that assumption, and the user-settings `jq`-merge route has to
  be retrofitted.
- **Fix.** Make it a U00 probe. A ccy container can check it: copy a two-line plugin with a
  `SessionStart` hook that touches a file, enable it the way phpantom-lsp is enabled,
  start a child `claude -p`, and check the file. This runs in a container, which is not
  what DESIGN.md §11 implies. Decide D13 after the result.

### S12. The image pin does not fit the repo's pin manifest

- **Problem.** DESIGN.md §2 says the image pin is "tracked by
  `scripts/check-pinned-versions.bash`" and so is the iamb pin. The script reads its
  population from `vars/version-pins.yml` and compares a version variable with the latest
  GitHub release (`scripts/check-pinned-versions.bash:44-48`). A bare `@sha256:` digest is
  not a version it can compare. Plan 00109's installed-vs-pinned check reads the same
  manifest.

- **Scenario.** U16 adds a digest-only variable. The checker either cannot parse it or
  reports it as permanently behind.

- **Fix.** Use two variables:

  - `tuwunel_version: v1.9.3`, with a `@see` and a row in `vars/version-pins.yml` naming
    `matrix-construct/tuwunel`;
  - `tuwunel_image_digest`, kept next to it (the `update-versions` skill already handles
    "an adjacent sha256").

  Render `Image=ghcr.io/…/tuwunel:{{ tuwunel_version }}@{{ tuwunel_image_digest }}`. Do the
  same for iamb, with its version and sha256.

### S13. The image pull happens inside the unit's start timeout

- **Problem.** With a Quadlet, the first `systemctl --user start` pulls the image inside the
  generated service's start job. The default start timeout is 90 s, and a cold pull from
  GHCR can exceed it. The bounded readiness poll then reports a failure whose cause is the
  pull, not the homeserver.
- **Scenario.** First deploy on a slow link: the unit times out, and the play fails at the
  readiness wait with a confusing error.
- **Fix.** Pre-pull the pinned reference in the play before the daemon-reload, as a
  `command: argv: [podman, pull, <ref>]` task with `changed_when` on its output, as
  `play-unifi-controller.yml` pre-pulls. Then set `Pull=never` in the `.container`, so the
  start never touches the network.

---

## Nits

- **N1. Two markdown tables are broken.** In DESIGN.md §3 and §12, `|` inside backticks
  splits the cells: the "token files" row (`O_EXCL|…`) and U10's `hook stop|prompt|session-start`.
  U10's "Needs" and "Where" columns are lost as a result, so its dependencies cannot be
  read. Write the alternatives with `/` or escape the pipe.
- **N2. Several key Tuwunel settings are not stated in DESIGN.md.** The keys the privacy
  model relies on are listed only by citation: `allow_registration = false`,
  `registration_shared_secret_file`, `new_user_displayname_suffix = ""`,
  `admin_escape_commands = false`, `federate_admin_room = false`,
  `trusted_servers = []`, `address = ["0.0.0.0"]`. Put the full rendered key list in DESIGN.md
  so U16 and its review have one source. Also add `allow_encryption = false`: a creator
  human can otherwise turn on encryption in a bus room from Element, and the warden then
  silently stops reading commands there.
- **N3. Tuwunel's config path must come from `TUWUNEL_CONFIG`.**
  `tuwunel --health-check` reads `TUWUNEL_CONFIG`, not `-c` (`[tuwunel] §4`). Pass the path
  with `Environment=TUWUNEL_CONFIG=…`, never `Exec=-c …`. Prefer Quadlet's `StopTimeout=`
  over `PodmanArgs=--stop-timeout`. Consider `Notify=healthy` instead of a hand-written
  readiness poll. The 1800 s stop timeout copied from upstream can hold up logout or
  shutdown for 30 minutes on a tiny bus database; keep it only if the migration risk is
  real.
- **N4. The warden unit has no dependency on its homeserver.** `agent-team-warden@.service`
  needs `Requires=`/`After=agent-team-%i.service` and a bounded `Restart=` policy, so a
  homeserver that is down gives a visible failed state rather than a crash loop.
- **N5. P1's in-namespace `tcpdump` needs flags.** Under `podman unshare nsenter`, the
  default privilege drop to the `tcpdump` user fails because that UID is not mapped. Use
  `-Z root`, and write the capture under `untracked/plan-runs/`. P2 should also record
  `net.ipv4.conf.all.route_localnet`, and FAIL if it is 1, because that is the one setting
  that makes a `127.0.0.1` bind reachable from outside. A vm-test-lab VM only exercises the
  libvirt bridge's zone, which belongs in owner question 2.
- **N6. `tcpdump` is installed by the service play only for the acceptance capture.** That is
  acceptable under the missing-dependency rule, but say so in a comment in the play, so a
  later YAGNI pass does not remove it.
- **N7. U07 and U13 both edit the shared fake.** U13 "fake gains admin endpoints" and U07
  owns `tests/helpers/pingbus/fake_homeserver.py`, and U08 and U09 extend it in parallel
  waves. Expect merge conflicts in one shared fixture. Either let U07 define the full fake
  surface up front (client and admin endpoints, plus the auth rule from B1), or split it
  into `fake_client_api.py` and `fake_admin_api.py`.
