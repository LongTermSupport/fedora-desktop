# Defence Before Fix report: fixture-plain-member-bundle

Branch `fix/acceptance-checker-a3-m3` (from F44 22e599b3d). Defence red `bd3cf6661`; fix and
green `fe0ca54d2`. The same branch carries Plan 00201's `checker-copies-cited-constant`
(its report: Plan 00201 `subagent-reports/261022-m3-dbf-report.md`).

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, from
the cached copies the refresh script listed (`/root/.cache/defence-before-fix/spec/`).

## Toolchain

No manifest declaration of DBF in this repository (none added). The toolchain is the project's
QA gates (`scripts/qa-changed.bash`, `scripts/qa-all.bash`), not graded in the register.
Project rules of this kind are stdlib `unittest` detectors under `tests/helpers/`, run by
`scripts/qa-helper-tests.bash` and documented by identifier in `CLAUDE/QA.md`. Listing: the
rule sections of CLAUDE/QA.md. Resolution: the message names `CLAUDE/QA.md "fixture-plain-member-bundle"`. Single-rule harness: `python3 -m unittest tests.helpers.plan_acceptance.test_plain_member_bundle_rule`. Calibration assumed: no
exceptions recorded.

## Defect and class

Originating instance: Plan 00211 deploy-stage-2 check A3 failed on the host with "pingbus:
team <team>: a host member must not run as a user with an Element profile". A3's throwaway
host worker was written with `agent-bus add-member --out=<home>/<team>`, a plain bundle in the
home it then ran the desktop user's pingbus with. Class: an acceptance fixture built
differently from the production path. Hazard: the check fails (or passes) for a reason that is
not the product's, and nothing after it runs. In production a member the invoking user's
pingbus loads is a seat: its own 0700 directory (`agent-bus seat take`) linked into the session
home (`seat.py` Keeper `_link`), and `config.py` `load_bundle` runs a host member beside an
Element profile only through that link and `element_accepted()`.

## Rule

`fixture-plain-member-bundle`, a reading detector over bash in active plan folders,
`scripts/`, `files/` and `tests/`: an `add-member` whose `--out=` is `<home>/<entry>` where
`<home>` is a value the same program (a plan folder, or one file elsewhere) gives
`PINGBUS_HOME` as a plain host assignment, names followed through the program's assignments.
`--out=-`, `--setenv=PINGBUS_HOME=` and `-e PINGBUS_HOME=` are not findings: a dedicated agent
user's plain bundle (systemd-run) and a container's environment are production shapes.

Next wider rule considered: any `add-member --out=DIR`. Not built: Plan 00161's dedicated
agent users and Plan 00201's never-loaded M7 bundles are plain directories by design and carry
no hazard. The wider class, any fixture built by a route production does not take, has no
mechanical form.

## Proof

Fixtures in the test: the originating A3 shape, a direct `--out` into a home, a plan folder
whose home is set in another file; near misses: a linked seat, `--setenv`/`-e` homes,
`--out=-`. At `bd3cf6661` the fixtures pass and the sweep fails with one finding,
`_acceptance-stage2.inc.bash:358`, the originating instance; that commit holds the red alone.

## Independent search

`261022-a3-seat-independent-search.md`, dispatched before the rule existed and kept out of
the rule's directory. It found one confident instance (A3) by text search and reading, six
not-instances (00201 M7's plain bundle that pingbus never loads, 00161's dedicated users, the
`seat take` users, 00209's `--list` fixture, two test suites with podman bundles, the config
tests) and two low doubts: the ccy session commands set `PINGBUS_TEAMS` by hand, and A4's
guest run omits `HOOKS_DAEMON_HOSTNAME`. Reading settled both: a ccy container's own
environment carries `PINGBUS_MEMBERSHIP` (`agent-bus-membership.bash`), which outranks
`PINGBUS_TEAMS` in `config.active_teams`, so the session's own seats are used; and
`HOOKS_DAEMON_HOSTNAME` is read only by `suggest-handle` (`cli.py` `ROLE_VAR`), not by
`report`. The rule and the search agree on one instance. Text search found the `--out` and
`PINGBUS_HOME` pairs; reading found that the home's team entry had to be a link, which no text
in A3 says.

## Sweep and count

Scope: first-party bash in active plan folders, `scripts/`, `files/` and `tests/` (completed
plans are not runnable through meta-deploy; `JOURNAL/` and `subagent-reports/` are records;
the rule's own fixtures excluded). One finding at the red, none at the green.

## Fixes

One instance, examined individually. `a3_take` makes the bundle in
`<run>/a3-seats/<team>/<seat>` (add-member makes it 0700, the user's) and links
`<run>/a3-pingbus/<team>` to it with an absolute `ln -s`. `config.py` is unchanged. A3's
failure message names the one-time acceptance at cc --teams' confirm, because a seat beside an
Element profile never accepted is refused by the product, correctly.

## Decisions referred to the owner

None.

## Permanence

A test module under `tests/helpers/`, so `qa-helper-tests.bash` runs it in every `qa-all.bash`;
it fails, never warns. `qa-changed.bash` reruns it only when the module itself changes (its
routing maps a Python file to its mirrored test), so a change to scanned bash does not rerun it
before a commit. Green through the module run and `./scripts/qa-changed.bash` (rc 0).

## Toolchain and detector gaps

No manifest declaration; no register-graded detector for bash, so the rule is a Python test
that reads bash text. No inline suppression route exists for it.

## The original defect

Fixed in `fe0ca54d2`. `acceptance.bash --stage 2 --fake` gains two legs that reproduce it:
the A3 seat, loaded by this checkout's `config.load_bundle` for a user with an Element profile
and an acceptance record, loads as a host member; the same without the record is refused with
the Element refusal. Before the fix A3's plain layout was refused in both cases. The host
rerun is queued in `CLAUDE/Plan/meta-deploy.list` (`deploy-stage-2.bash`).
