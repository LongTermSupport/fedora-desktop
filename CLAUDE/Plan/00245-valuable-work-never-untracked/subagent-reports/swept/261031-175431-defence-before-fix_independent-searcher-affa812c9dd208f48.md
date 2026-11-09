Both classes turned up findings. Class A has no instance beyond the known `login_report.py` one. Class B has one clear widening, in `_add_package`.

**CLASS A**

Found by reading; text search only produced the writer list I then read through.

- No other instance carries the hazard in the writers I read.
- Per-run writers that write every run or clear the old file:
  - `helpers/host_health/status_document.py` (via `login_report.publish`)
  - `helpers/self_update/cycle.py`: `_finish` writes the result on every non-dry-run outcome. The dry-run branches skip the write, which is by design.
  - `helpers/agent_bus/health.py:415`
  - `helpers/agent_bus_lxc/applied_record.py:42`: removes the file when there is no record.
  - `helpers/containerwatch/cli.py:941`
  - `src-go/internal/wsitoken/app.go:332`: `exhausted` writes the warning file, and `app.go:247` removes it on a good run.
  - `src-go/internal/vmtest/freshness.go:294`: writes only if the whole status run succeeds.
- Failed-write caveat (unsure, mild): if `status_document.write_atomic` fails in `login_report.py`, the previous document stays. It carries `at`, so a reader can see it is old.
- `freshness.go:294` also leaves the previous `freshness-status.txt` in place when the probe or a base check fails. The file has a `written_at=` line. I am unsure whether that is enough for a reader.
- Not hazards: the `containerimages/prune.go:817` success stamp, the `consumer/app.go:852` setup marker, and the `picker/offers.go:172` never-marker are deliberate markers. `oldrange.go:260` and `stt/app.go:392` write to a path the caller names.

**CLASS B** (`helpers/self_update/affected_plays.py`)

Found by reading. The probe confirmed it.

- **`_add_package`, lines 509-526.** This is the main widening. It adds the whole `helpers/<pkg>/` prefix, then walks every `.py` in the package, not only the run module. Each file's `from helpers.x import ...` pulls in package `x`, and `_python_reads` adds its `__file__` and literal paths.
- **`_HELPER_TOKEN`, line 870.** It matches `helpers.<pkg>` anywhere in a walked line, so a mention in a comment or a name list counts.
- **`_maybe_helper_path`, line 504.** Any single file under `helpers/<pkg>/` widens to the whole package.
- Smaller widenings:
  - A Go embed or glob becomes a prefix.
  - A `_walk_bash` source or `go-tool` command adds every string literal in the Go package's non-test files that names a checkout path (`_checkout_literal`).
  - Every `.py` in the package is scanned for reads, tests included.
- **Plays pulled in by `check_panel_contract`.** `helpers/gnome/check_panel_contract.py` imports `containerwatch`, `host_health` and `play_ledger`. As a result, `play-speech-to-text`, `play-displaylink` and `play-gnome-shell-extensions` get inputs they never run. Their only run module is `helpers.gnome.apply_enabled_extensions` or `verify_extension`. The extra inputs are:
  - `containerwatch`
  - `host_health`
  - `play_ledger`
  - `play_lock`
  - `self_update`
  - `version_pins`
- Other plays with helper packages their run modules do not import (my probe's run-module detection is approximate):
  - `play-claude-code-consumer`: `agent_bus` and `agent_bus_lxc`.
  - `play-agent-bus-element`: `agent_bus_lxc`.
  - `play-fedora-desktop-panel`: `play_lock`.
  - Also several unreached sibling modules in the packages these plays get.
- **Ways one helper module reaches another that a plain import scan misses:**
  - `helpers/vmtest/bridge_watcher.py:146` runs `-m helpers.vmtest.bridge_run` as a subprocess.
  - `helpers/pingbus/cli.py:764` re-runs itself with `from helpers.pingbus.cli import main`.
  - `helpers/github_scopes/cli.py:31` reads `vars/github-required-scopes.yml` through `__file__`.
  - The `__file__` repo-root climbs in `host_health`, `play_ledger` and `version_pins` are only defaults.
  - A package `__init__` import is implicit.
  - There are no relative imports and no importlib use in `helpers`.

**Searches:** write patterns over `helpers/**/*.py` and `src-go` (`write_text`, `open(..."w"`, `os.fdopen`, `os.WriteFile`, `WriteAtomic`, `OpenFile`), `unlink` and `os.remove`, `handoff.` and `write_atomic` callers, `importlib|-m helpers|__file__|sys.path`, and relative-import patterns.

**Technique split:** text search found the writers and the dynamic-load spots. Reading found which writers skip on some outcome, and the `_add_package` widening. The runtime probe (`python3 -I`, read-only) found the per-play package sets.