# S15 (X34): Element trusts the team CA through the play

Builder report, Plan 00246 Task 8.8, branch `feat/crossnet-s15` (cut from
`spike/v2-cross-network`). Not pushed, not merged.

## What is built

**The play.** `playbooks/imports/optional/common/play-agent-bus-element.yml` gets one new last
step, an import of the new `tasks/element-team-ca.yml`, for the team ids of its declared,
non-absent profiles. For each of those teams:

1. stat, then read as root, `/var/lib/teamchat-status/teams/<team>/approved.json`
   (`paths.ApprovedFile`). A team with no approved.json is not a v2 team on this host and is
   left alone. A team whose approved `url` is `https` is "an https team".
2. read as root `/var/lib/teamchat-admin/<team>/tls/ca.crt` (`paths.CACertFile`). A missing CA
   for an https team fails the play at this task, naming the file.
3. place the CA in the chosen store (below).
4. **The check**: `flatpak run --command=python3 --filesystem=<repo>/helpers/element_ca:ro im.riot.Riot -I <repo>/helpers/element_ca/nss.py check --team <t> --url <approved url>`,
   the CA PEM on stdin, as `user_login` with `XDG_RUNTIME_DIR` from a resolved uid (the
   getent-and-assert pattern of play-container-watch.yml). It runs inside Element's own
   sandbox, with the app's permissions and overrides, the runtime's python3 and NSS. It makes a
   TLS 1.3 handshake to the url's host and port, then has NSS verify the presented
   certificate, for a TLS server and for the url's host, over the stores Chromium reads:
   the user database `$HOME/.pki/nssdb` (none when none exists) plus the builtin roots module
   `libnssckbi.so`, loaded as Chromium loads it. The task is a probe (`failed_when: false`,
   annotated `FAIL-FAST-OK`), and the play's last task is an assert that fails unless every
   https team's check exited 0. Its message names, for each failed team, the CA file, the
   store and the helper's own error (an NSS error name such as `SEC_ERROR_UNKNOWN_ISSUER`, a
   wrong name, or a failed handshake).
5. On success the check writes the CA to `~/.var/app/im.riot.Riot/config/teamchat-ca/<team>.pem`
   (0600 in a 0700 directory; that is `$XDG_CONFIG_HOME` inside the sandbox). On failure it
   deletes that file. Only a passing check can leave it, so it is the proof of a placed CA
   that the Go side reads.

The check task does not change with the mechanism. The mechanism is one variable,
`agent_bus_element_ca_store` in host_vars (default `nssdb`, set in the play's vars as
`agent_bus_element_ca_default_store`), documented in `localhost.yml.dist`. An unknown value
fails the first assert of the tasks file. In `--check` mode nothing is placed, so the check
and its assert do not run.

**The helper.** `helpers/element_ca/nss.py` uses the stdlib only (ctypes over libnss3 and
libnspr4). It is one self-contained file because the sandbox runs it by path, with no
repository on its `sys.path`. It has two verbs, `place` (on the host) and `check` (in the
sandbox). Exit 0 means done, 1 means not trusted or no handshake, 2 means a bad input or an
unusable environment. It prints `CHANGED<TAB>path` marker lines on stdout and sends
diagnostics to stderr. It is Python and not Go because it must run inside the Flatpak runtime
against the runtime's own NSS, which a cgo-free Go binary cannot load. It is the kind of
Python that Ansible runs, which the rule allows.

**The Go side.** `teamchat-admin element check` and `apply`:

- **CA placed** (the play's record exists: a regular file owned by this user, writable by no
  one else, and parsed by `pki.Fingerprint` as a team CA): they say nothing about the CA.
- **CA not placed**: one stderr line per https team: `<team>: the team's url is https and Element does not trust the team's CA, <paths.CACertFile>, yet: run the Element play, which places it where Element reads it and checks a handshake from Element's sandbox`. The old
  "copy it out as root" and "has not been checked" text is gone.
- **Something else at the record's path** (a link, a file others can write, content that is
  not a CA certificate): `ErrRefused`, naming the path.
- New: `paths.ElementCAPlaced(home, team)`, with its entries in the paths tests (the
  zero-value table, the sampled-names table, the base-directory refusals and TestElementPaths).
  To keep `teamDerived` under funlen, the home-derived names moved into a helper,
  `homeDerived`.
- `docs/agent-bus-cross-network.md`, Element section: rewritten to describe the play.

## The mechanism chosen, and why

**`nssdb`** is the default: Chromium's own NSS database, kept in Element's Flatpak data by a
`flatpak override --user --persist=.pki im.riot.Riot` (probed first, as the keyring grant is),
so the database lives at `~/.var/app/im.riot.Riot/.pki/nssdb`. The CA goes in with trust
`C,,`, as `certutil -A -t C,,` sets it, through the helper's `place`. A new database gets the
empty password Chromium's has, and a database with a password set is refused.

Why this one:

- Electron is Chromium. On Linux, Chromium takes its trust from NSS: the user database
  `$HOME/.pki/nssdb`, plus the roots module it loads. A locally added root in that user
  database is the documented way to make Chromium trust a private CA.
- Inside the Flatpak, `$HOME` is not persisted unless the app has home access, so a database
  Chromium made there would not survive a run. `--persist=.pki` maps it into the app's own
  data directory.
- Only Element trusts the CA. The team CA has no name constraints, so a CA trusted
  system-wide could vouch for any name to every program on the host. The key is root-only on
  the homeserver host, but "every program trusts it for any name" is a far larger grant than
  Element needs.
- The helper writes the database, so the host needs no new package (no `nss-tools`), and the
  placement is covered by the same tests as the check.

**`system`**, the implemented alternative: the CA goes to
`/etc/pki/ca-trust/source/anchors/teamchat-<team>.pem` (root, 0644), then
`update-ca-trust extract` runs when the file changed. When the host has p11-kit, flatpak
starts a p11-kit server that hands the host's trust to the runtime's p11-kit trust module. In
the Freedesktop runtime that module is `libnssckbi.so`, the builtin roots Chromium loads, so
the same check covers it. The cost is that every program on the host trusts the CA, for any
name.

**What the host check must try, in this order, if `nssdb` fails:**

1. Confirm the check could run at all: `python3` in the runtime, `libnss3`/`libnspr4`
   loadable, `libnssckbi.so` loadable. An exit-2 message from the helper names which one
   failed. If python3 is missing from the runtime, the check has to change (see "Not done").
2. If the check passes but Element itself still refuses the certificate, the gap is between
   NSS's verifier and Chromium's. Look at whether this Electron uses the Chrome Root Store
   verifier and whether it honours user-database roots.
3. `system` (`agent_bus_element_ca_store: system`). Also confirm the host runs the flatpak
   p11-kit server, i.e. that `p11-kit` is installed on the host.
4. Not implemented, listed for completeness: `NODE_EXTRA_CA_CERTS` through a
   `flatpak override --env` covers only Node's own TLS in Electron's main process, not
   Chromium's network stack, which Element's matrix client uses, so it is not expected to
   work. A launcher flag (`--ignore-certificate-errors-spki-list`) would need the Go launcher
   to change and pins a key rather than trusting a CA.

The coordinator's host check has to see Element itself sign in to the https team after the
play passes, by eye. The play's check proves that the trust is in the store; it does not prove
that Element's Chromium accepts it.

## Where the CA comes from on another machine: FINDING, no channel

From the code:

- `teamchat-admin element check|apply` builds a profile only for a team with an
  `approved.json` (`declare.FileApprovals` at `/var/lib/teamchat-status/teams/<t>/approved.json`),
  which only `team approve` writes, on the homeserver host. **No Element profile exists on
  any other machine**, so there is nothing there for a CA to serve.
- The CA's only path off the homeserver host is the seat bundle's `tls_ca` (X15). It reaches
  the agent seat pool of the member machine (X22, `seat place-issued`), where the member's
  agents, not the human, hold it. Element does not use bundles.

So the design has no channel that puts the CA, or a profile, on a second machine for its
human. I did not invent one. The play reads the CA where it is, on the homeserver host.

## Findings for the coordinator

1. **Owner answer 1 and the code disagree.** The answer says Element on the homeserver host
   "uses loopback and needs no CA". `element.Env.profile` writes the team's approved `url` as
   `base_url`, and for a cross-network team that url is `https://<listen address>`, not
   loopback. Either the profile should point at `http://127.0.0.1:<port>` on the homeserver
   host, in which case X34 is not needed there and is needed nowhere today (see the previous
   section), or the answer's premise is wrong and X34 is what makes the https profile work.
   I built X34 as specified. This is a decision for the coordinator or the owner.
2. **No channel to another machine** (the previous section).
3. **CA rotation (X33) and the record.** After a rotation, the record left by the Element play
   holds the old CA, and `element check|apply`, which cannot read the root-only CA, will treat
   the CA as placed. When the play next runs, its check fails, deletes the record and fails the
   play, which is what should happen. X33's verb should also tell the owner to run the Element
   play again.
4. **The play is still v1's.** It writes profiles through `helpers.agent_bus.element` (v1)
   from `agent_bus_element_teams`. The v2 `teamchat-admin element apply` is not called by any
   play yet (Plan 00209). The new tasks key on v2's `approved.json`, so they apply as soon as a
   declared team has one, and they need no change when the profile step is ported.
5. **No CA cleanup.** When a team is undeclared, its CA stays in the store and its record
   stays. Removing them is not built; nothing asked for it.

## Proof

**Go, test first.** The new tests in `element_tls_test.go` were seen red against the old code:
the not-placed wording, the placed silence, and the three refusals. Then four planted defects,
each red and then restored (the plant script was scratch, not committed):

| planted defect                               | went red                                                              |
| -------------------------------------------- | --------------------------------------------------------------------- |
| the record ignored (always "not placed")     | `TestAnHTTPSTeamWhoseCAThePlayPlacedIsNotToldAboutIt`                 |
| the mode check removed                       | `TestARecordOfAPlacedCAThatIsNotThePlaysIsRefused/writable_by_others` |
| the content check removed                    | `.../not_a_CA_certificate`                                            |
| `Lstat` replaced by `Stat` (a link followed) | `.../a_link`                                                          |

The paths tests went red on the new name (zero-value table, sampled names) until its entries
were added. `go test -race -shuffle=on` passes for paths, element and admin.

**The check task on a store without the CA.** No harness in the repository runs a task
(`tests/fixtures/ansible-*` are static gates), so the check command itself is proven by
`tests/helpers/element_ca/test_nss.py`. It runs the helper as the play does, one process per
verb, against a real TLS 1.3 server on loopback whose leaf the test's own CA signed (made with
`openssl` at test time), and real NSS databases in a scratch HOME. The tests: no store means
exit 1 naming the store, with no record; a store holding another CA means exit 1 with
`SEC_ERROR_UNKNOWN_ISSUER`, and an old record is deleted; the placed CA is trusted and recorded
once, 0600, and both verbs are idempotent; a certificate for another name is refused; no
listener is refused; bad inputs are exit 2. Five planted defects in the helper, each red and
then restored:

| planted defect         | went red                                |
| ---------------------- | --------------------------------------- |
| verification skipped   | no-store, other-CA and other-name tests |
| name not checked       | other-name test                         |
| old record kept        | other-CA test                           |
| trust not set on place | placed-and-recorded test                |
| place always "changed" | placed-and-recorded test (idempotency)  |

What this does not prove: that the Flatpak runtime has python3 and loads `libnssckbi.so` the
way this container's Debian NSS does; that `flatpak run --command` gives the check the same
`$HOME/.pki` mapping Element gets (it should, since overrides apply to every run of the app);
and that Element's Chromium honours the store. That is the host check.

## Gate results

QA_RESULT_PLACEHOLDER

## Host run: none queued, and why

I did not add an entry to `CLAUDE/Plan/meta-deploy.list`. Nothing in the run can usefully
happen yet. No v2 team is deployed on any host: the host play for the base units and the v2
launcher are unbuilt (`docs/agent-bus-cross-network.md`, "Status"). So on the host today the
new tasks find no `approved.json`, place nothing and check nothing, and the only thing a run
would exercise is the v1 profile step, which has not changed. The list's standing rule is to
leave out what cannot usefully run.

When a v2 https team exists on the homeserver host, the entry is the line
`playbooks/imports/optional/common/play-agent-bus-element.yml`. The run reads that team's
approved.json and CA as root, adds the `--persist=.pki` override and puts the CA in
`~/.var/app/im.riot.Riot/.pki/nssdb`, runs the check inside Element's sandbox, and passes only
if the handshake verifies. The coordinator's host check then has to look at three things: the
play's last task (pass, or the failure message naming the CA file and store); the record at
`~/.var/app/im.riot.Riot/config/teamchat-ca/<team>.pem`; and Element itself signing in to the
team with no certificate warning. If that last step fails while the play passed, the next
thing to try is `agent_bus_element_ca_store: system`, as listed above.

## Not done

- The meta-deploy entry (above), and the host check itself.
- Removing a team's CA and record when the team is undeclared.
- A fallback for a runtime with no python3. If the host check shows python3 is missing, the
  check would have to run another tool inside the sandbox; that is not built.
- PLAN.md and JOURNAL are left to the coordinator.
