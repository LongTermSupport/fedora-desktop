# Stack Options for Build-From-Scratch

## TL;DR

**Recommended: Rust + Ratatui.** The decisive factor is that *both* shipped Qobuz CLI players in this repo (`hifi-rs`, `qobuz-player`) are Rust, and there are three actively-maintained Rust Qobuz API crates (`qobuz-api-rust`, `hifirs-qobuz-api`, `moosicbox_qobuz`). Building in Rust means we *embed* an existing, working Qobuz client and only have to write the YXC layer — a pure HTTP+UDP problem the language handles well. Ratatui + tokio + crossterm is the canonical async TUI stack and `spotify-tui` proves the pattern (HTTP-polling music controller, ratatui rendering, async Spotify Web API). Runner-up: **Python + Textual** — only because `aiomusiccast` (Home Assistant's library) is a mature, actively-maintained YXC client (v0.15.0 released 2025-11-09) that solves Track B's other half. Go ranks third: no usable YXC library, no usable Qobuz library, no compensating advantage over Rust.

## Decision criteria

| Criterion                      | Weight | Why                                                                                                                                   |
| ------------------------------ | ------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| Existing Qobuz client to reuse | High   | Qobuz auth is reverse-engineered, fragile, and a non-trivial chunk of work. Reusing a maintained client is a force multiplier.        |
| Existing YXC client to reuse   | High   | YXC has UDP push events, a quirk most generic HTTP clients don't model. A library that already does it is gold.                       |
| Async ergonomics               | High   | We juggle YXC HTTP poll + UDP listener + Qobuz REST + key input + render. Concurrency must be ergonomic, not heroic.                  |
| Distribution                   | Medium | This repo installs single binaries from GitHub releases via Ansible (see `play-qobuz-cli.yml`). Single-binary stacks slot in cleanly. |
| TUI quality                    | Medium | Lists with virtualisation, modals, progress bar, key/mouse/paste — table stakes for the UX we want.                                   |
| Performance / footprint        | Low    | All three are fine for a desktop controller. Not a hot path.                                                                          |
| Contributor pull               | Low    | "I'd hack on this" matters but doesn't override the Qobuz library factor.                                                             |

## Stack 1: Go + Bubble Tea

### Pros

- Single static binary, trivial cross-compile.
- Bubble Tea's Elm-style Model/View/Update is genuinely pleasant to reason about; goroutines feed `tea.Msg` over a central channel via `tea.Cmd`, which maps cleanly onto "background HTTP poll" and "background UDP listener feeding events".
- Stdlib `net/http` and `net` are excellent. UDP listening is `net.ListenPacket("udp", ":<port>")`, no dependency.
- Lipgloss styling is the prettiest of the three (charm.sh aesthetic).
- Compile times fast, easy onboarding for backend devs.

### Cons

- **No usable YXC library.** `atamanroman/ymc` is the only Go option and it is dead (last commit May 2023, panics on empty speaker list — already binned by this plan).
- **No Qobuz API library in Go at all.** Reverse-engineering Qobuz auth from scratch is the largest single risk in any build path; Go is the only stack that forces us to take that risk.
- Bubbles components are good but thinner than Textual: `list` virtualises, but there's no first-class modal system (you compose it yourself), no DataTable equivalent.
- Long-running goroutines that send `tea.Msg` need careful lifecycle management — easy to leak on `Quit`.

### Existing libraries we'd use

- **YXC**: none viable — would write from scratch against the YXC PDF spec.
- **Qobuz**: none — would reverse-engineer or port from another language.
- **HTTP/UDP**: `net/http`, `net` (stdlib).
- **TUI**: `charmbracelet/bubbletea`, `charmbracelet/bubbles`, `charmbracelet/lipgloss`.

### Distribution story

Build with `go build -ldflags="-s -w"`, ship a single binary as a GitHub release tarball. The Ansible install pattern in `play-qobuz-cli.yml` (download tarball → extract → `chmod +x` → drop into `~/.local/bin/`) ports verbatim. ~10–15 MB binary.

### "Hello world" skeleton

```go
// Minimal Bubble Tea program: poll an HTTP endpoint every 5s, render JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time
type statusMsg map[string]any
type errMsg struct{ err error }

type model struct {
	endpoint string
	status   map[string]any
	err      error
}

func pollStatus(endpoint string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			return errMsg{err}
		}
		return statusMsg(out)
	}
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return tea.Batch(pollStatus(m.endpoint), tick()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tickMsg:
		return m, tea.Batch(pollStatus(m.endpoint), tick())
	case statusMsg:
		m.status = msg
		m.err = nil
	case errMsg:
		m.err = msg.err
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n(q to quit)\n", m.err)
	}
	if m.status == nil {
		return "polling...\n"
	}
	b, _ := json.MarshalIndent(m.status, "", "  ")
	return fmt.Sprintf("%s\n\n(q to quit)\n", string(b))
}

func main() {
	p := tea.NewProgram(model{endpoint: "http://192.0.2.44/YamahaExtendedControl/v1/main/getStatus"})
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
```

For the UDP push listener, you'd add a `tea.Cmd` that wraps a goroutine reading from `net.ListenPacket("udp", ":41100")` and posts each datagram as a `tea.Msg` via `Program.Send()`.

## Stack 2: Rust + Ratatui

### Pros

- **Both Qobuz CLI players already in this repo are Rust** (`iamdb/hifi.rs`, `SofusA/qobuz-player`). Lifting their API client modules is realistic. There are also three published Qobuz crates: `qobuz-api-rust` (most complete, async via `reqwest`+`tokio`), `hifirs-qobuz-api` (extracted from hifi-rs), and `moosicbox_qobuz` (full Hi-Res support).
- Ratatui + tokio + crossterm is the canonical pattern: `tokio::select!` over `crossterm::event::EventStream`, an interval ticker, and any number of mpsc channels (one per HTTP poll task, one for the UDP listener). Multiple production templates (`ratatui/async-template`, `fiadtui`, `d-holguin/async-ratatui`) document this exactly.
- `spotify-tui` is a directly comparable production app: HTTP-polled music API, ratatui rendering, key-driven UX, event-based architecture with async network operations and a route stack for navigation. We can read its source as a reference.
- Single static binary, the smallest of the three (~3–8 MB stripped). Already the distribution pattern in this repo.
- Strong types catch the "we forgot to handle this YXC variant" class of bug at compile time.
- crossterm handles paste, mouse, modifier keys, and resize uniformly across Linux terminals.

### Cons

- **No usable YXC client in Rust.** This is the gap — but it's a thin gap (HTTP GET wrappers + a `tokio::net::UdpSocket` listener, ~300 lines), not the deep Qobuz auth gap.
- Compile times noticeably slower than Go on first build; less of an issue with `cargo check` and incremental builds.
- Async Rust has a learning curve. `Pin`, `Send + 'static` bounds, and `Arc<Mutex<>>` patterns trip up newcomers. The tokio templates mitigate this but it's still the steepest entry point of the three.
- Ratatui is immediate-mode: every frame is redrawn from scratch. Very fast, but there's no built-in stateful "modal screen" abstraction — you build modals with the `Clear` widget over a centred `Rect` and manage focus yourself.

### Existing libraries we'd use

- **YXC**: none — write a small client (HTTP via `reqwest`, UDP via `tokio::net::UdpSocket`). YXC PDFs already in this plan dir.
- **Qobuz**: `qobuz-api-rust` (preferred — most complete, actively maintained) or `hifirs-qobuz-api` (battle-tested in production hifi-rs). Either gets us 80%+ of the work for free.
- **HTTP/UDP**: `reqwest` (HTTP client with tokio), `tokio::net::UdpSocket` (UDP).
- **TUI**: `ratatui`, `crossterm`, `tui-input` (text input widget), `throbber-widgets-tui` (spinners) if needed.

### Distribution story

`cargo build --release`, ship the `target/release/<name>` binary in a tarball as a GitHub release. Ansible install is *literally* the pattern already in `play-qobuz-cli.yml` for `hifi-rs` and `qobuz-player` — copy that block, change three variables, done. The most natural fit of the three.

### "Hello world" skeleton

```rust
// Cargo.toml deps:
//   tokio = { version = "1", features = ["full"] }
//   reqwest = { version = "0.12", features = ["json"] }
//   ratatui = "0.29"
//   crossterm = { version = "0.29", features = ["event-stream"] }
//   futures = "0.3"
//   serde_json = "1"

use std::io;
use std::time::Duration;

use crossterm::event::{Event, EventStream, KeyCode};
use crossterm::terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen};
use crossterm::execute;
use futures::StreamExt;
use ratatui::backend::CrosstermBackend;
use ratatui::widgets::{Block, Borders, Paragraph};
use ratatui::Terminal;
use tokio::sync::mpsc;
use tokio::time::interval;

#[derive(Debug)]
enum AppMsg {
    Status(serde_json::Value),
    Error(String),
}

async fn poll_status(endpoint: String, tx: mpsc::Sender<AppMsg>) {
    let client = reqwest::Client::new();
    let mut tick = interval(Duration::from_secs(5));
    loop {
        tick.tick().await;
        match client.get(&endpoint).send().await {
            Ok(resp) => match resp.json::<serde_json::Value>().await {
                Ok(v) => {
                    let _ = tx.send(AppMsg::Status(v)).await;
                }
                Err(e) => {
                    let _ = tx.send(AppMsg::Error(e.to_string())).await;
                }
            },
            Err(e) => {
                let _ = tx.send(AppMsg::Error(e.to_string())).await;
            }
        }
    }
}

#[tokio::main]
async fn main() -> io::Result<()> {
    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen)?;
    let mut terminal = Terminal::new(CrosstermBackend::new(stdout))?;

    let (tx, mut rx) = mpsc::channel::<AppMsg>(32);
    let endpoint = "http://192.0.2.44/YamahaExtendedControl/v1/main/getStatus".to_string();
    tokio::spawn(poll_status(endpoint, tx));

    let mut events = EventStream::new();
    let mut body = String::from("polling...");

    loop {
        terminal.draw(|f| {
            let block = Block::default().title("MusicCast").borders(Borders::ALL);
            let para = Paragraph::new(body.as_str()).block(block);
            f.render_widget(para, f.area());
        })?;

        tokio::select! {
            maybe_event = events.next() => {
                if let Some(Ok(Event::Key(k))) = maybe_event {
                    if matches!(k.code, KeyCode::Char('q') | KeyCode::Esc) { break; }
                }
            }
            Some(msg) = rx.recv() => {
                body = match msg {
                    AppMsg::Status(v) => serde_json::to_string_pretty(&v).unwrap_or_default(),
                    AppMsg::Error(e) => format!("error: {e}"),
                };
            }
        }
    }

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    Ok(())
}
```

For UDP push events, spawn a second task wrapping `tokio::net::UdpSocket::bind("0.0.0.0:41100")` that reads in a loop and forwards parsed events on the same `mpsc::Sender`.

## Stack 3: Python + Textual

### Pros

- **`aiomusiccast` exists, is mature, and is actively maintained** (v0.15.0, 2025-11-09; supports Python 3.10–3.14; backs the Home Assistant MusicCast integration). It already models the YXC HTTP API *and* the UDP unicast event dispatcher. This is the single biggest leg up of any stack — for the YXC half.
- Textual is the most feature-complete TUI of the three: built-in `DataTable` with virtualisation, `ProgressBar`, `Input`, `Modal` screens via the `Screen` stack, CSS-like theming, and 40+ widgets out of the box. Modals are first-class, not a DIY composition.
- Async-first by design: you write `async def` everywhere, `httpx` and `asyncio.DatagramProtocol` drop in cleanly, `Worker`s manage background tasks with cancellation built in. The whole event loop is one `asyncio` loop — no message-passing impedance.
- Python is the most "I'd hack on this" language for a Linux-power-user audience by a wide margin (more Linux desktop users have Python installed than Rust toolchains).
- Rapid iteration: edit-save-rerun in seconds, no compile.

### Cons

- **No usable Qobuz Python library.** `vitiko98/qobuz-dl` is inactive (no PyPI release in >12 months); `python-qobuz` (taschenb) is inactive. `qobuz-dlp` is the active fork but it's a download-focused tool, not a clean API client. We'd be reverse-engineering Qobuz auth or porting the Rust client — work the Rust stack avoids.
- **Distribution story is the worst of the three.** Pip-install-into-venv, manage Python version drift, handle missing system Python. PyInstaller / Nuitka / pyoxidizer can produce single binaries but they're 30–80 MB and add their own friction. Doesn't slot into the `play-qobuz-cli.yml` pattern; needs a separate pyenv/venv playbook (this repo has pyenv, but it's a heavier install).
- Performance is the worst of the three. Fine for our use case, but redraw cost on slow terminals / SSH is noticeably higher than Ratatui or Bubble Tea (Textual does extensive diffing, but it's still Python).
- GIL and asyncio mean a slow library call blocks the UI unless wrapped in `run_in_executor` or a `Worker(thread=True)`. Easy to footgun.
- Type hints are optional; without strict mypy, runtime errors that Rust would catch at compile time hit you at 2 a.m.

### Existing libraries we'd use

- **YXC**: `aiomusiccast` (vigonotion). The reason this stack is even competitive.
- **Qobuz**: nothing maintained — would reverse-engineer or port from `hifi-rs`.
- **HTTP/UDP**: `httpx` (HTTP, async), `asyncio.DatagramProtocol` + `loop.create_datagram_endpoint()` (UDP) — or `aiomusiccast` already wraps this.
- **TUI**: `textual` + `textual-dev`.

### Distribution story

Two realistic options. **Option A**: Ship as a Python package, install via a new `play-musiccast-controller.yml` that creates a venv (or uses pyenv from `play-pyenv.yml`), runs `pip install <our-package>`, and writes a launcher script to `~/.local/bin/`. **Option B**: PyInstaller bundle, ship as a tarball like the Rust binaries — works but 50 MB+ and slower startup. Option A is cleaner but heavier than the Rust binary path.

### "Hello world" skeleton

```python
# Requires: pip install textual httpx
import json
from typing import Any

import httpx
from textual.app import App, ComposeResult
from textual.widgets import Header, Footer, Static


ENDPOINT = "http://192.0.2.44/YamahaExtendedControl/v1/main/getStatus"


class MusicCastApp(App[None]):
    BINDINGS = [("q", "quit", "Quit")]
    CSS = "Static { padding: 1 2; }"

    def compose(self) -> ComposeResult:
        yield Header()
        yield Static("polling...", id="status")
        yield Footer()

    def on_mount(self) -> None:
        # Re-poll every 5 seconds; Textual schedules the callback on the loop.
        self.set_interval(5.0, self.refresh_status)
        # Kick off the first poll immediately as a Worker so the UI mounts fast.
        self.run_worker(self.refresh_status(), exclusive=True)

    async def refresh_status(self) -> None:
        widget = self.query_one("#status", Static)
        try:
            async with httpx.AsyncClient(timeout=3.0) as client:
                resp = await client.get(ENDPOINT)
                resp.raise_for_status()
                data: dict[str, Any] = resp.json()
            widget.update(json.dumps(data, indent=2))
        except Exception as exc:  # noqa: BLE001 — top-level UI error display
            widget.update(f"error: {exc}")


if __name__ == "__main__":
    MusicCastApp().run()
```

For the UDP push listener, override `on_mount` to call `loop.create_datagram_endpoint(MyProtocol, local_addr=("0.0.0.0", 41100))` and post received datagrams as Textual messages via `self.post_message(...)` so the UI updates reactively.

## Comparison matrix

|                                 | Go                           | Rust                                              | Python                                               |
| ------------------------------- | ---------------------------- | ------------------------------------------------- | ---------------------------------------------------- |
| Async ergonomics                | tea.Cmd / goroutines, clean  | tokio::select!, mpsc — powerful but steep         | asyncio + Workers, easiest for newcomers             |
| Distribution                    | Single static binary         | Single static binary (smallest)                   | venv or PyInstaller (heavy)                          |
| Existing YXC lib                | None viable (ymc dead)       | None                                              | **aiomusiccast (mature, active)**                    |
| Existing Qobuz lib              | None                         | **3 maintained crates + 2 in-repo Rust apps**     | None active                                          |
| Single binary                   | Yes                          | Yes                                               | No (without bundlers)                                |
| Contributor pull                | Medium                       | Medium-High (Linux power users skew Rust-curious) | High                                                 |
| Maturity of TUI lib             | High (bubbletea v2, bubbles) | High (ratatui 0.29+, large widget ecosystem)      | **Highest (Textual: DataTable, Modal screens, CSS)** |
| Reference apps for our use case | Limited                      | **spotify-tui (HTTP-polled music TUI)**           | Several Textual demos, no music-controller exemplar  |

## Recommendation

**Build in Rust on Ratatui + tokio + crossterm.** The decisive reason: Qobuz is the half of this project most likely to eat schedule, and Rust is the *only* language where mature Qobuz API clients exist and where two reference players already live in this repo. We trade a gap (writing the YXC client ourselves — a small, well-specified HTTP+UDP problem we already have PDFs for) for skipping a much bigger gap (Qobuz auth reverse-engineering). Distribution is identical to `hifi-rs` and `qobuz-player` — a copy-paste of the existing Ansible block. `spotify-tui` is the proof-of-pattern for "HTTP-polled music TUI in Rust", and it works.

If this project ends up being a thin shell that mostly exec's `qobuz-player`/`hifi-rs` and pushes URLs over YXC, Rust gets even cheaper — we link against their existing `qp` web API as the Qobuz layer and write only YXC + UI.

## Caveat

This stack analysis is conditional on Track B not finding a usable adoption candidate and Track D not landing on a path that obviates the controller (e.g. native YXC Qobuz makes a dead-simple shell wrapper viable). It also assumes Track B confirms what `aiomusiccast` and the YXC PDFs imply about UDP push events — if push turns out to be optional (i.e. polling getStatus every 1–2 s is acceptable), the UDP listener gap shrinks and all three stacks get closer on parity. Finally, if Track D recommends a Python-heavy adoption path (e.g. fork `aiomusiccast` + add UI), the Python stack moves up; in the pure build-from-scratch scenario the Rust Qobuz advantage dominates.

## Sources

- [Bubble Tea (charmbracelet/bubbletea)](https://github.com/charmbracelet/bubbletea)
- [Bubble Tea command tutorial — Charm blog](https://charm.land/blog/commands-in-bubbletea/)
- [Ratatui homepage](https://ratatui.rs/)
- [Ratatui async event stream tutorial](https://ratatui.rs/tutorials/counter-async-app/async-event-stream/)
- [Ratatui async-template](https://github.com/ratatui/async-template)
- [Textual homepage](https://textual.textualize.io/)
- [Python asyncio — Transports and Protocols (UDP)](https://docs.python.org/3/library/asyncio-protocol.html)
- [aiomusiccast (vigonotion)](https://github.com/vigonotion/aiomusiccast)
- [atamanroman/ymc — Yamaha MusicCast CLI in Go (dead)](https://github.com/atamanroman/ymc)
- [foxthefox/yamaha-yxc-nodejs (NodeJS reference impl)](https://github.com/foxthefox/yamaha-yxc-nodejs)
- [qobuz-api-rust on crates.io](https://crates.io/crates/qobuz-api-rust)
- [hifirs-qobuz-api on lib.rs](https://lib.rs/crates/hifirs-qobuz-api)
- [moosicbox_qobuz on crates.io](https://crates.io/crates/moosicbox_qobuz)
- [iamdb/hifi.rs (Rust Qobuz player — installed by play-qobuz-cli.yml)](https://github.com/iamdb/hifi.rs)
- [SofusA/qobuz-player (Rust Qobuz player — installed by play-qobuz-cli.yml)](https://github.com/SofusA/qobuz-player)
- [vitiko98/qobuz-dl (Python, inactive)](https://github.com/vitiko98/qobuz-dl)
- [spotify-tui (Rigellute/spotify-tui — reference TUI)](https://github.com/Rigellute/spotify-tui)
- [spotify-tui architecture overview (DeepWiki)](https://deepwiki.com/Rigellute/spotify-tui/1-overview)
- [d-holguin/async-ratatui (multi-event ratatui example)](https://github.com/d-holguin/async-ratatui)
- [Snyk — qobuz-dl health analysis (inactive status)](https://snyk.io/advisor/python/qobuz-dl)
