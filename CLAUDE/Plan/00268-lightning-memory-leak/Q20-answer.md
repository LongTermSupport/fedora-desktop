# Q20: clues from the OOM, Lightning's logs, and a debug mode

**The incident already gave the big clue.** Lightning logs to stderr, and that lands in the user
journal, where the laptop's host seat found it (RESEARCH.md section 8). Memory was flat at about
650 MB for 12,800 s. Then, at about 17:09Z, the GUI event loop stopped and the QML/JS heap grew by
about 74 GB in at most 2,760 s. This is not a slow leak. It is a runaway on a stuck GUI thread,
most likely set off by one event or one action at 17:09Z.

**Still to read** (read-only):

- systemd's CPU and peak accounting of the killed scope ([laptop-commands.md](laptop-commands.md)
  section 10).
- **The busy team's room events, 17:00-17:15Z**: their shapes only, no bodies. Only the bus
  host's homeserver admin can read them ([bus-host-commands.md](bus-host-commands.md)). The
  trigger is most likely there.

**Lightning's debug modes.** Each is an environment variable or a flag. None logs message
content or tokens. Detail and costs: RESEARCH.md 7.1.

| Mode                              | Gives                                       | Cost                       |
| --------------------------------- | ------------------------------------------- | -------------------------- |
| `LIGHTNING_SCROLL_MEMORY_TRACE=1` | RSS and timeline rows, every 5 s            | about 2 MiB per 86,400 s   |
| `LIGHTNING_SYNC_TRACE=1` + rule   | a line per event received (its id)          | one line per event         |
| `LIGHTNING_SCROLL_TRACE=1`        | why each memory trim ran or not             | a line per check, 120 s+   |
| `LIGHTNING_GUI_STALL_TRACE=1`     | GUI stalls, logged only when the stall ends | only on stalls             |
| `--log-file <path>`               | a copy of the log                           | 32 MiB, then one `.1` file |

Not for live use: `LIGHTNING_RUST_LOG` at debug level and Qt's garbage-collector statistics.
Both are heavy. There is no heap dump.

**Recommendation: yes, run the busy team's instance traced from now on.** Use memory tracing and
a line per event, logged to a file:

```text
LIGHTNING_SCROLL_MEMORY_TRACE=1 LIGHTNING_SYNC_TRACE=1
QT_LOGGING_RULES=lightning.sync.trace.debug=true
lightning-matrix --log-file <team>/cache/lightning-diagnostic.log
```

- The last event id before the log goes silent names the trigger. The last memory line dates the
  stall to within 5 s.
- The log is capped at 64 MiB on 0.10.7 and later.
- Lightning's own stall tracer would have logged nothing on 25 Nov: it reports a stall only once
  it ends. So add a small watchdog outside Lightning (Task 1.7). When the log has been silent for
  60 s while memory climbs, it records the CPU of each thread and a backtrace of every thread,
  once.

**The change, as configuration** (Task 1.4, owner decision D1):

- A per-team key `lightning_diagnostics: true` in that team's `agent_bus_element_teams` entry.
- `src-go/internal/lightning` (`execLine`) then writes the variables and `--log-file` into the
  team's launcher. Both ccy-teams and the app grid launch through that launcher.
- The key is also accepted by `helpers/agent_bus/element.py`.
- Deployed by `play-agent-bus-element.yml` (tag `agent-bus-lightning`) through
  `meta-deploy.bash`. It takes effect at the next launch.
