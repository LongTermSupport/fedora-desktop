# Research: Python CLI conventions and tests (Plan 00161, Task 1.1)

Scope: the conventions a new standard-library CLI (`pingbus`), a daemon (the warden) and
`agent-team` provisioning must follow in this repository, and a recommended file and test
layout. Every claim below names the file it was read from. Nothing was changed except this
report.

## 1. Where Python lives here

| Kind                                    | Location                                                                                                                                                           | Precedent                                                                                                                                                              |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tested logic, executors and CLIs        | `helpers/<package>/*.py`, a **namespace package with no `__init__.py`**                                                                                            | `helpers/containerwatch/`, `helpers/github443/`, `helpers/vmtest/`                                                                                                     |
| User command on `PATH`                  | `files/home/.local/bin/<name>`: a **two-line bash wrapper** that runs `env PYTHONPATH=/usr/local/lib/ccy-helpers python3 -m helpers.<pkg>.cli "$@"`                | `files/home/.local/bin/container-watch`, `files/usr/local/bin/github-ssh-443`, `files/home/.local/bin/vmtest-bridge-watcher`                                           |
| Deployed library                        | `/usr/local/lib/ccy-helpers/helpers/<pkg>/`, copied by the owning play from an **explicit file list** (no glob; a forgotten module is an ImportError on every run) | `playbooks/imports/optional/common/play-container-watch.yml`                                                                                                           |
| systemd --user units                    | `files/home/.config/systemd/user/<name>.service`/`.timer`, enabled by the play with a resolved uid (`getent` + `assert`, never `default(1000)`)                    | same play                                                                                                                                                              |
| Container-side tool run from a checkout | `python3 -m helpers.vmtest.request` behind `scripts/vmtest-request.bash`                                                                                           | Plan 00110 bridge                                                                                                                                                      |
| Standalone Python programs              | `files/home/.local/bin/wsi-*`, `clip-scan` (extensionless, shebang)                                                                                                | an outlier: their tests live in `tests/speech_to_text/` and need a bespoke runner (`scripts/test-wsi-stop-grace.bash` lists them by hand). **Do not copy this shape.** |

Rules in stone (`helpers/CLAUDE.md`, `.claude/rules/python-helpers.md`):

- **Standard library only**, helpers and tests alike. No pip, venv or pytest. A third-party
  import is a flagged decision for the owner, never an accident.
- **Split pure logic (no I/O, exhaustively unit-tested) from a thin side-effecting executor.**
- Plays call a helper with `command: argv:` and `chdir: "{{ root_dir }}"`, never a `shell:` block.
- Executors print **stable marker lines** on stdout (`PYENV-CHANGED`) for `changed_when`.
- Every `subprocess` call passes an explicit `check=`; `check=False` only when the next lines
  inspect the return code. No bare `except`, no swallowed stderr.

**Python version floor is 3.11.** The ccy image (`files/var/local/claude-yolo/Dockerfile`,
`FROM node:lts-slim`, Debian `python3`) has 3.11.2; CI uses 3.12
(`.github/workflows/qa.yml`); the Fedora 44 host is newer. So no 3.12+ syntax or modules
(`type` statements, PEP 695 generics, `itertools.batched`). `tomllib` exists (3.11) but
**reads only**: the stdlib has no TOML writer, which matters for a registry that
provisioning writes (see section 7).

## 2. How Python is tested and gated

- **Runner**: `./scripts/qa-helper-tests.bash` finds `tests/helpers/**/test_*.py` and runs them
  by **explicit module name** through `helpers.qa_environment.unittest_counts`. It cross-checks
  against `git ls-files`, so an untracked test is reported and a missed tracked one exits 2.
  **Never `python3 -m unittest discover`**: on namespace packages it reports `Ran 0 tests … OK`.
  A new test file needs no registration; it only has to be under `tests/helpers/` and tracked.
- **Mirror rule**: `helpers/<pkg>/<mod>.py` is tested by `tests/helpers/<pkg>/test_<mod>.py`.
  The test inserts the repo root (`pathlib.Path(__file__).resolve().parents[3]`) into
  `sys.path` before importing (ruff ignores `E402` under `tests/**`, `ruff.toml`).
- **TDD is enforced by a hook**: `tdd_enforcement` (`.claude/hooks-daemon.yaml`) blocks creating
  a source file whose test file does not exist (R-TDD-TEST-FIRST). Write the test first, then
  the module.
- **Git in tests**: pass an env with `GIT_CONFIG_GLOBAL=os.devnull`, `GIT_CONFIG_NOSYSTEM=1`,
  without `GIT_CONFIG_COUNT`/`GIT_CONFIG_PARAMETERS` (hosts sign commits by default).
- **Fake servers**: `tests/helpers/self_update/test_alerts.py` runs an `http.server` on a thread;
  `tests/helpers/vmtest/test_request.py` drives the real CLI as a subprocess against a fake
  bridge thread. Both are the precedent for a fake Matrix homeserver.
- **Lint**: `scripts/qa-python.bash` runs `py_compile` and ruff over every repo Python file
  (found by extension **or shebang**, any mode, cross-checked against git). Ruleset pinned in
  `ruff.toml` (`select = ["E4", "E7", "E9", "F"]`; E5 line length not enforced); ruff version
  pinned in `/.qa-versions` (`RUFF=0.16.8`), asserted by `scripts/qa-toolchain.bash`.
  Suppression comments (`# noqa`, `# type: ignore`) are blocked by a hook (R-QA-SUPPRESSION);
  a needed exception is a scoped `[lint.per-file-ignores]` entry in `ruff.toml`.
- **Type checking: none is gated.** pyright ships in the ccy image as a language server only.
  Type hints are used throughout `helpers/` (`from __future__ import annotations`), but nothing
  enforces them. Do not claim a type check in the plan.
- **Semgrep on Python**: `qa-ready-wait-rules.bash` (rule `ready-wait-ignores-child-exit`): a
  sleep loop in a function that starts a `subprocess.Popen` must `.poll()` the child each try.
  This applies to any provisioning step that starts something and waits for it to be ready.
  `qa-speech-to-text-rules.bash` is STT-specific.
- **Deployed drift**: `scripts/qa-deployed-drift.bash` compares repo files with deployed copies.
  `.local/bin` is covered automatically; a `ccy-helpers` package needs an `EXTRA_PAIRS` entry
  (`"helpers/vmtest/*.py|/usr/local/lib/ccy-helpers/helpers/vmtest|<play>"` is the model).
- `qa-all.bash` runs all of the above. Under the subagent rule (R-SUBAGENT-FULL-QA) a builder
  sub-agent runs targeted QA (`python3 -m unittest tests.helpers.pingbus.test_protocol`,
  `./scripts/qa-helper-tests.bash`, `ruff check <files>`) and leaves the full gate to the
  coordinator.

## 3. stdout and stderr (`CLAUDE/StderrHygiene.md`)

stdout is the return value; everything a human reads to follow along goes to stderr.

- `pingbus send`: stdout is one marker line (the sent event id) or nothing; progress, "resolving
  reference", refusals on stderr.
- `pingbus recv`/`wait`: stdout is exactly one line per **valid** ping. A dropped invalid ping is
  reported on **stderr** (and counted in the exit status, section 4), never on stdout, so a hook
  that reads stdout can never be fed an unvalidated line.
- `inbox`, `show`, `status`, `peers`, `tail`, `config check`: report commands for a human; their
  text is the payload and may stay on stdout. Still give them `--json` where a hook or script
  reads them (`status` especially, for the Stop hook).
- Warden and `agent-team`: marker lines for the play (`AGENT-TEAM-CHANGED`, `AGENT-TEAM-DONE`) on
  stdout, diagnostics on stderr; the warden as a service logs to stderr (journald).
- **Tokens never reach either stream** (the issue's privacy check): no token in an exception
  message, `repr`, URL query string or log line. Send the token in the `Authorization` header,
  and make the HTTP error path print status and Matrix `errcode` only.

Recommended stable line format (decide in PROTOCOL.md): tab-separated fields, fixed order,
version-prefixed, for example
`PING\t1\t<event_id>\t<sender>\t<room_id>\t<verb>\t<ref>\t<note-or-->`, plus `--json`
(one JSON object per line). Tabs cannot occur in any validated field, so no quoting is needed.

## 4. Exit codes

There is **no repo-wide table**; each tool declares named `EXIT_*` constants at module top and
documents them in its docstring. The recurring values are sysexits-style:
`64` usage (`helpers/vmtest/request.py`, `helpers/self_update/cycle.py`, several bash scripts),
`75` busy/locked (`helpers/play_lock/lock.py`, `cycle.py`), `70`/`78`-style config errors
(`cycle.py` uses 70), `130` cancelled.

Two traps found in the precedents:

1. **argparse exits 2 on a usage error.** `helpers/vmtest/request.py` declares `EXIT_USAGE = 64`
   and `EXIT_ERROR = 2`, but a bad flag still exits 2 through argparse, colliding with
   `EXIT_ERROR`. `helpers/self_update/cycle.py` does it right: it catches `SystemExit` around
   `parse_args` and returns `EXIT_USAGE` (64), or 0 for `--help`. pingbus must do the same.
2. **An uncaught Python exception exits 1.** Never assign 1 a meaning, so a crash cannot read as
   a defined outcome.

The "docs-exit-codes gate" (`scripts/test-qa-docs-exit-codes.bash`, run by `qa-all.bash`) is
specific to `qa-docs.bash`, but its principle applies: **every documented exit code must be
produced by a test** ("a crash must never read as clean"). So pingbus's tests drive the real CLI
(`subprocess.run([sys.executable, "-m", "helpers.pingbus.cli", ...])` against the fake homeserver)
to every code, and a contract test asserts PROTOCOL.md's exit-code table and the `EXIT_*`
constants agree, in both directions (precedent: `helpers/gnome/check_panel_contract.py`).

Proposed pingbus table (for PROTOCOL.md to settle):

| Code | Meaning                                                                                                         |
| ---- | --------------------------------------------------------------------------------------------------------------- |
| 0    | done: `send` accepted; `recv`/`wait` printed at least one ping                                                  |
| 1    | never assigned (uncaught exception)                                                                             |
| 2    | never assigned (argparse's default, remapped)                                                                   |
| 3    | nothing: `recv` found the inbox empty; `wait` timed out with nothing                                            |
| 4    | refused by the validator (verb, reference form, note, path outside allowlist)                                   |
| 5    | reference did not resolve at the forge                                                                          |
| 6    | invalid pings were received and dropped (valid ones, if any, were still printed)                                |
| 7    | homeserver unreachable                                                                                          |
| 8    | authentication failed (token rejected)                                                                          |
| 9    | rate limited                                                                                                    |
| 64   | usage                                                                                                           |
| 75   | busy: another syncer holds this account's lock                                                                  |
| 78   | configuration refused (missing team, token file mode not 0600, plain HTTP to a non-local URL, allowlists unset) |

## 5. Security rules the hooks enforce on Python

From the active handler table in `CLAUDE.md` and `.claude/hooks-daemon.yaml`:

- R-SEC-CMD-INJECTION: no `shell=True`, no `os.system`. Use argument lists (`podman`, `systemctl`).
- R-SEC-CODE-INJECTION: no `eval`, `exec`, `__import__`, `yaml.load`.
- R-SEC-DESERIALISATION: no `pickle`; JSON only (Matrix is JSON anyway).
- R-ERROR-HIDING: no bare `except`, no `except: pass`.
- R-QA-SUPPRESSION: no `noqa`/`type: ignore`.
- R-SEC-HARDCODED-CREDS and the pre-commit secret scanner: fixture tokens must be obviously fake
  (`"fixture-token"`), not shaped like a real token.
- Public-repo rule and the pre-commit scanner (`scripts/git-hooks/pre-commit`): **private IPs
  (`10.x`, `192.168.x`) are rejected in any staged file**, fixtures included. The "host-local
  address" rule (plain HTTP allowed only to a host-local homeserver) must therefore be tested
  with loopback (`127.0.0.1`, `::1`) and with RFC 5737 addresses declared local in a fixture
  config (`192.0.2.1`), per `CLAUDE/ExampleValues.md`. Design consequence: "host-local" should be
  loopback **or an address the team config declares** as its bind address, not a guess from
  `ipaddress.is_private`, which would also bless a LAN address the issue says must not be used.
  Handles in fixtures use placeholders (`myrepo.1+host.podman`, `server.test`).
- Secret files: write a token with `os.open(path, O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW, 0o600)`
  then rename, as `helpers/vmtest/spool.py` and `helpers/play_ledger/store.py` do; not
  write-then-`chmod` (`helpers/github443/cli.py` does that, leaving a window at the umask mode).
  pingbus refuses (78) a token file whose mode is wider than 0600.
- Comments describe current state (R-COMMENT-CHANGELOG, R-COMMENT-SIZE).

## 6. Publishing as a single file

Section 8 of the issue needs a single-file artefact for members ccy does not manage, and the
validator must be **one source** shared by `pingbus` (in containers) and the warden (on the host).
There is no bundling precedent in the repo (no zipapp, no `.pyz`).

Recommendation: keep the source as the normal namespace package and **build a zipapp** with a
small tested helper:

- `helpers/pingbus/bundle.py` writes `#!/usr/bin/env python3` then a zip of
  `helpers/pingbus/*.py` plus a `__main__.py` that calls `helpers.pingbus.cli.main`.
- **It must add `helpers/__init__.py` and `helpers/pingbus/__init__.py` inside the archive.**
  Proven in this container on Python 3.11.2: a zip without them fails with
  `ModuleNotFoundError: No module named 'helpers'` (zipimport does not find namespace packages);
  with them it runs. The source tree keeps no `__init__.py`, as `helpers/CLAUDE.md` requires.
- **Reproducible bytes**: fixed `ZipInfo.date_time=(1980,1,1,0,0,0)`, sorted entries, fixed
  `external_attr`, and `ZIP_STORED` (deflate output depends on the zlib build, so two hosts could
  differ). Proven byte-identical across two builds here. Reproducible output lets the play report
  `changed` honestly and lets `qa-deployed-drift.bash` compare it.
- Tests: `tests/helpers/pingbus/test_bundle.py` builds twice and compares bytes, then runs the
  archive with `sys.executable` to `--version` and one `validate` call; and asserts every module in
  `helpers/pingbus/` is in the archive (a missing module is the containerwatch failure mode).
- The play builds it with `command: argv: [python3, -m, helpers.pingbus.bundle, --out, <tmp>]`
  and installs the result 0755 as `~/.local/bin/pingbus` (host) and a published path the ccy
  opt-in mounts read-only into a container (ccy topic). The same artefact is the section 8
  deliverable, so ccy and non-ccy members run identical bytes.
- `pingbus --version` prints the tool version and the PROTOCOL version, so a member contract can
  be checked.

The warden and `agent-team` run only on the host, so they follow the existing ccy-helpers
precedent (explicit module list into `/usr/local/lib/ccy-helpers/helpers/...`, bash wrapper in
`.local/bin`) and import `helpers.pingbus.protocol` from the same deployed tree. Add both trees to
`EXTRA_PAIRS` in `qa-deployed-drift.bash`.

## 7. Recommended layout

Two packages: `pingbus` (everything a member needs, bundled) and `agent_team` (host-only:
provisioning and the warden). The warden imports the protocol and Matrix client from `pingbus`, so
there is one validator.

```
helpers/pingbus/
  protocol.py       PURE. PROTOCOL_VERSION, VERBS, reference grammar (path@commit, issue, PR,
                    commit), note charset/length, handle grammar, event schema; validate_outgoing()
                    and validate_incoming() return a typed result or raise a named refusal.
                    No I/O, no clock, no network.
  limits.py         PURE. Rate-limit and ack-timeout arithmetic over injected timestamps.
  config.py         Team config + token resolution (--team, per-project default); file-mode check;
                    local-URL rule. Reads files; logic split into pure functions taking text.
  matrix.py         Thin urllib client: login-free token auth, send, /sync, /messages, room create
                    with explicit power levels, join, leave. No retries hidden inside.
  inbox.py          Durable inbox and sync-token store: dedupe by event id, token persisted only
                    after the inbox write is durable, one-syncer lock (fcntl.flock, exit 75).
  forge.py          Reference resolution against the forge API (send-side check), injectable opener.
  cli.py            argparse, EXIT_* table, line format; `main(argv) -> int`; SystemExit remapped.
  bundle.py         Builds the reproducible zipapp (section 6).

helpers/agent_team/
  registry.py       PURE parse/render of the team registry; handle allocation (<n> never reused).
  provision.py      Executor: podman/Quadlet units, admin-first account creation, member creds
                    (0600, O_EXCL), rotate-token. Marker lines for the play.
  commands.py       PURE warden logic: (event content, m.mentions, room membership, roles)
                    -> Decision(ping | reply | refuse). Never reads message text for targets.
  warden.py         Executor: one /sync loop per team; posts replies; emits pings via
                    helpers.pingbus.matrix; logs to stderr.
  cli.py            `agent-team create | add-member | remove-member | list | rotate-token`.

tests/helpers/pingbus/
  test_protocol.py  Exhaustive, table-driven (subTest): every verb, every reference form, each
                    boundary of the 80-char note, every forbidden character, handle grammar,
                    oversize and wrong-type fields, m.replace edits, unknown event types.
  test_limits.py
  test_config.py    Mode 0644 token refused; plain HTTP to 192.0.2.1 refused unless declared
                    local; loopback allowed; https anywhere allowed; no allowlist -> refused.
  test_matrix.py    Against fake_homeserver: auth header present, token absent from errors.
  test_inbox.py     Dedupe, limited-sync gap fill, token-after-durable-write, second syncer 75.
  test_forge.py     Injected opener: resolves, 404, network failure -> distinct outcomes.
  test_cli.py       Real CLI as a subprocess against fake_homeserver: one test per EXIT_* code,
                    stdout holds only ping lines, dropped pings only on stderr, tokens on neither.
  test_bundle.py    Byte-identical rebuild; archive runs; every module included.
  test_protocol_doc.py  PROTOCOL.md tables (verbs, exit codes, limits) == the constants.
  fake_homeserver.py    Shared http.server fake (not test_*.py, so the runner does not run it
                        as a module; imported by the tests above).

tests/helpers/agent_team/
  test_registry.py  test_provision.py (podman/systemctl via injected runner; argv lists asserted)
  test_commands.py  Exhaustive warden table: each command, @-mention targeting, `all`, no mention
                    -> orchestrator, mention without command -> command list, non-worker mention
                    -> refusal, free text -> fixed reply and no ping, edits ignored.
  test_warden.py    Loop against fake_homeserver: human text never produces a ping.
  test_cli.py

files/home/.local/bin/agent-team            bash wrapper -> helpers.agent_team.cli
files/home/.local/bin/agent-team-warden     bash wrapper -> helpers.agent_team.warden
files/home/.config/systemd/user/agent-team-warden@.service   one instance per team
```

Notes on the layout:

- `fake_homeserver.py` is a shared test fixture module, not a test; check the runner's
  enumeration (`find tests/helpers -name 'test_*.py'`) leaves it alone, which it does.
- The registry format: `tomllib` cannot write TOML. Either make the machine-written registry
  JSON (simplest, stdlib round-trip) and keep human-edited per-member config read-only TOML, or
  write a tiny tested TOML renderer in `registry.py`. Decide in DESIGN.md; JSON is the YAGNI
  choice.
- `agent-team` should take everything as arguments. If any subcommand prompts, the wrapper falls
  under `CLAUDE/InteractiveScripts.md` (bounded re-prompt, EOF is a clean cancel).
- Anything that starts the homeserver and waits for it to answer keeps the `Popen`/unit state and
  checks it each try (`ready-wait-ignores-child-exit`).
- Where PROTOCOL.md lives: the plan names it as a plan file, but the issue requires a versioned
  spec every team's CLI enforces, so its lasting home is `docs/` (for example
  `docs/agent-team-bus-protocol.md`), linked from the plan; the contract test reads that path.

## 8. Open points for DESIGN.md

1. Registry and member config format (JSON vs TOML writer), section 7.
2. The exact stdout line format and the exit-code table, section 3 and 4.
3. Where the bundled `pingbus` is published on the host and how ccy mounts it (ccy research).
4. Whether the warden runs as host Python under `systemd --user` (recommended: stdlib only, no
   image) or as a second container in the team's Quadlet pod.
