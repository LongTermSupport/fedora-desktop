# Plan 00055 — L3 Human-in-the-Loop Testing Checklist

**Type**: Manual visual acceptance (read alongside [`testing.md`](testing.md) §5)
**Where it runs**: HOST, in a nested GNOME Shell — no logout required
**Scope**: the one thing a machine cannot assert — *did the GNOME panel visibly
react, and was the guidance usable?* Everything else is covered by L0/L1/L2.

> **Prerequisite**: L0 (`./scripts/qa-all.bash`, ESLint, `check_extension_compat`,
> the no-kill guard), L1 (`./scripts/qa-helper-tests.bash`), and L2
> (`./scripts/acceptance-container-watch.bash`) are all green. This checklist is
> the **final** gate, run only after the automated layers pass.

> **Public-repo note**: every identifier below is a reserved placeholder
> (`project-a_yolo`, `example.com`, synthetic container ids) per
> `CLAUDE/ExampleValues.md`. Do **not** paste a real container name, workspace
> path, or captured argv into a fixture or into this file.

---

## Conventions

- Tick each box `[x]` only after **visually confirming** the stated outcome.
- The `--inject` seam drives the front-end deterministically, so **no real
  runaway is needed** for 5a — the human only watches and confirms.
- Record the run at the bottom (date, tester, GNOME Shell version, result).

---

## Setup (once per session)

- [ ] 1. The extension is deployed to
  `~/.local/share/gnome-shell/extensions/container-watch@fedora-desktop/`
  (via `play-container-watch.yml` on the HOST — never in the CCY container).

- [ ] 2. A fixture finding file exists for injection, e.g.
  `~/cw-fixture-finding.json`, containing a single synthetic finding with
  reserved placeholders. Minimal shape (engine-correct `exec_hint` that **names
  the container**):

  ```json
  [
    {
      "host_pid": 2124472,
      "container_pid": 12,
      "engine": "podman",
      "rootless": true,
      "owner_uid": 1000,
      "container_id": "0000000000000000000000000000000000000000000000000000000000000000",
      "container_name": "project-a_yolo",
      "argv0": "ugrep",
      "cmd": "ugrep -G --hidden -rl pattern /",
      "age_s": 6916,
      "cpu_pct": 1116,
      "rss_kb": 35784,
      "exec_hint": "podman exec -it project-a_yolo ps -o pid,%cpu,args -p 12"
    }
  ]
  ```

- [ ] 3. Launch a nested GNOME Shell (per `CLAUDE/GnomeShell.md`):

  ```bash
  dbus-run-session -- gnome-shell --nested --wayland
  ```

- [ ] 4. Inside the nested window, press **Alt+F1** for the nested overview, open
  the **Extensions** app, and **enable** "Container Watch". The panel indicator
  appears in the **neutral** state (no findings yet).

---

## 5a. GNOME panel + notification visual pass (injected, no real runaway)

- [ ] 5. **Inject a finding.** From a terminal *inside the nested session*:

  ```bash
  container-watch scan --inject ~/cw-fixture-finding.json
  ```

- [ ] 6. **Panel → attention state.** Confirm the panel indicator changes from
  neutral to the **attention** state (colour/icon change) within a second or two.

- [ ] 7. **Notification appears.** Confirm a desktop notification is raised
  announcing the new finding.

- [ ] 8. **Click lists the finding.** Click the panel indicator and confirm the
  popup menu lists the finding: container name (`project-a_yolo`), truncated
  `cmd`, age, and CPU%.

- [ ] 9. **Copyable, engine-correct `exec_hint`.** Confirm the menu entry exposes
  the `exec_hint` string, that it is **copyable**, that it is **engine-correct**
  (`podman exec -it …` for this fixture), and that it **names the container**
  (`project-a_yolo`).

- [ ] 10. **Return to neutral on empty.** Inject the empty set:

  ```bash
  container-watch scan --inject empty
  ```

  Confirm the indicator returns to the **neutral** state with **no stale
  finding** left in the menu.

- [ ] 11. **Notification dedupe.** Re-inject the **same** finding twice in a row:

  ```bash
  container-watch scan --inject ~/cw-fixture-finding.json   # first: notifies
  container-watch scan --inject ~/cw-fixture-finding.json   # second: identical
  ```

  Confirm the **second** identical injection does **NOT** raise a fresh
  notification (dedupe on `host_pid`+`container_id`), while the panel still shows
  the finding.

- [ ] 12. **Close the nested shell** (Ctrl+Q or close the window) to end 5a.

---

## 5b. One real guided-resolution walkthrough (once per release)

Validates the *actual goal* — guiding a human to fix the runaway **inside the
container** — which no unit test can assert. Uses a real burner, but the age
threshold is lowered so there is no 15-minute wait.

- [ ] 13. **Lower the age threshold** for this walkthrough only (a real value of
  900 s would mean a 15-minute wait — out of scope here):

  ```bash
  export CW_AGE_S=30 CW_CPU_PCT=5
  ```

- [ ] 14. **Start a real burner** (the §4 spinner, but longer / no short
  `timeout` so it persists through a timer interval):

  ```bash
  podman run -d --rm --name cw-test-burner docker.io/library/alpine:latest \
      sh -c 'while :; do :; done & while :; do :; done & wait'
  ```

- [ ] 15. **Wait one real timer interval** (default 2 min — this is the *polling*
  cadence, not the 15-min age gate) **or** trigger a scan manually:

  ```bash
  systemctl --user start container-watch.service
  # or, directly:
  container-watch scan
  ```

- [ ] 16. **Panel/CLI flags it.** Confirm the panel shows the attention state and
  that `container-watch list` shows a finding for `cw-test-burner`.

- [ ] 17. **Copy and run the `exec_hint`.** Copy the `exec_hint` from the panel
  menu (or `container-watch explain <host_pid>`) and run it in a terminal.
  Confirm it drops you to / lists the offending process **inside the container**.

- [ ] 18. **Resolve it in the container.** Following the guidance, stop the
  offending process **inside the container** (this is the human action the tool
  guides — the tool itself never kills anything).

- [ ] 19. **Confirm reporting-only.** Confirm the tool did **not** terminate the
  process on the human's behalf — the only thing that stopped it was the human's
  in-container action in step 18.

- [ ] 20. **Tear down the burner**:

  ```bash
  podman rm -f cw-test-burner
  ```

- [ ] 21. **Clears on re-scan.** Run one more scan and confirm the finding clears
  (panel returns to neutral, `container-watch list` shows no findings).

---

## Run record

Fill in on each L3 pass so the human acceptance is repeatable and auditable.

| Field             | Value                                   |
| ----------------- | --------------------------------------- |
| Date              |                                         |
| Tester            | `<name>`                                |
| GNOME Shell major | (F44 → 50; confirm in the nested shell) |
| 5a result         | PASS / FAIL                             |
| 5b result         | PASS / FAIL                             |
| Notes             |                                         |

**Acceptance for this layer**: all of 5a (boxes 5–12) and 5b (boxes 13–21)
confirmed PASS, recorded above.
