# Task 2.0c build report (Opus, 2026-10-15)

Branch `feat/bus-inject-message`. It merges `fix/pingbus-short-name-reply` and `fix/201-qa-review`. Spec: [SPEC-inject.md](../SPEC-inject.md), amended for every finding in Fable's review.

**Changes**

- **Spec amendments, by finding:**
  - H1: the attestation is cut.
  - H2: the body is capped after escaping. The 12 KiB delivery cap is absolute. H4 measures the socket's largest accepted message.
  - M1: a payload written in full counts as sent.
  - M2: a delivery that does not reach the transcript is marked missing.
  - M3: an agent sender gets 10 entries per team per 10 minutes. The rest are held back.
  - M4: each delivery has a short first line. The socket frame is explained once, at SessionStart.
  - M5 and L1-L6: done.
- **Code** (written test first):
  - new modules `helpers/pingbus/envelope.py` and `helpers/pingbus/inject.py`;
  - the watcher's socket pass in `cli.py`; `recv` and `wait` take the spool lock in every mode;
  - the hooks: a SessionStart line, the transcript record, quiet counts while the watcher delivers, and lines for a stalled or missing delivery;
  - `notify.py` no longer has the count notice.
- **Docs:**
  - the skill, with plugin 1.2.0;
  - protocol §12, §13 and §15;
  - `docs/agent-bus.md` and `docs/agent-bus-lxc-members.md`;
  - dated amendments to Plan 00161 D16 and to this plan's decision B.
- **Versions:** CCY 3.98.8, container 2.63, changelog.
- **Plan 00161:** `u20_check.py` and the M2 acceptance now read a delivery's first line instead of the notice.
- **Host runs queued in `meta-deploy.list`:** `play-claude-yolo.yml` and `play-agent-bus.yml`. After them come host checks H1-H7.

**Tests:** the pingbus suite (1269 tests, plus `test_statusline`) and Plan 00161's `test_u20_check` pass. The targeted QA (`qa-changed`) passes. `qa-all` was not run (sub-agent rule).

**Open:**

- **Transcript format.** The missing check assumes Claude Code writes a socket message into the transcript, as `u20_check.py` already parses it. If the host shows otherwise, every delivery is reported missing within about three minutes. That is loud, not silent. H2 and H7 settle it.
- **Timing.** The socket-mode watcher tests in `test_notify.py` take about 25 s, because they sleep in real time.
