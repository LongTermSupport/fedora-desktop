# md-viewer plan: summary report

The plan is written and pushed to `LongTermSupport/md-viewer` `main` in two commits: `67aa1d0`
(spec, architecture, milestones, README, fixture) and `bd91e01` (ccy container and
`CLAUDE.md`).

## What is in the repository

- `docs/SPEC.md`: the numbered expectations. It covers the CLI (flags, exit codes 0, 1 and 2,
  exact messages), the typed document model, and GitHub styling with exact values from
  github-markdown-css v5.9.0 (light and dark palettes, heading sizes, spacing, code, quotes,
  lists, task boxes, tables, images). It also covers links through xdg-open, scroll, zoom
  (50 to 300%), wrap, the window, the speed rules and targets, and the non-goals.
- `docs/ARCHITECTURE.md`: the packages `cmd/md-viewer`, `internal/doc`, `internal/render`,
  `internal/app` and `internal/window` (the only importer of `gioui.org/app`). It also has
  their public types, the pinned versions, the headless render-test method (an `input.Router`
  and a recorded `Trace` of marks, asserting structure rather than pixels), the fixtures,
  the benchmarks and the cross-platform seams.
- `docs/MILESTONES.md`: **12 milestones, M0 to M11**. Each gives the expectations it covers,
  the files, the tests to write first, the verification commands and a done-when line. M0
  gives the CI workflow and both QA scripts verbatim.
- `README.md`, `CLAUDE.md`, `.claude/ccy/Dockerfile`, `testdata/elements.md`.

## Facts checked against source

- Pins: Gio v0.10.3, goldmark v1.8.6, golang.org/x/image v0.26.0, Go 1.26.9, go-qa-ci at
  commit `803b122ce5266d58c5fa296985a4e1fde206cd0f` (no tag is published).
- Gio's `layout`, `widget`, `material`, `text`, `font/gofont` and `io/input` build and run
  without a display. `app` and `gpu/headless` need the cgo headers.
- Gio v0.10.3 has no API for the system colour scheme, so dark mode is the `--dark` flag.
- Gio's wheel step is 100 px per notch on Wayland and 10 px on X11.
- The Debian 12 package names in the Dockerfile exist, and `libwayland-dev` provides
  `wayland-egl.pc`.
- go-qa-ci's consumer docs need the tools on `PATH`, not in an image. They are installed into
  `.tools/bin` by `scripts/install-qa-tools.sh` from go-qa-ci's own `tools.lock`, in both CI
  and the container.

## Decisions for the owner

The full list is at the end of `docs/MILESTONES.md`. The ones that matter most:

1. **Fonts.** Only the embedded Go fonts are used, for speed, so emoji such as ✅ ⬜ 🔄 in plan
   files show as boxes. The alternatives are system fonts (a font scan at every start) or an
   embedded emoji font.
2. **Dark mode** is `--dark` only. The desktop setting could be read through the XDG portal
   over D-Bus, at the cost of one round trip.
3. **`--time` exits after the first frame**, so it can be scripted.
4. **HTML is dropped**, with a stderr count. The alternative is to show it as text.
5. **No 980 dp column cap. Code blocks and wide tables wrap instead of scrolling sideways.
   In-page `#` links are not followed.**
6. **go-qa-ci is private**, so CI's `go-qa` job is red until it is published. The alternative
   is a read-only token secret.
7. Installing md-viewer on the desktop, and making it the `text/markdown` default, stays as
   fedora-desktop Plan 00199 Phase 2.

## Owner's host steps for the carve-out

1. Clone `LongTermSupport/md-viewer` into the projects directory on the host.
2. Run `ccy` in that checkout, which builds the image from `.claude/ccy/Dockerfile`.
3. In that session, run `/hooks-daemon install` and commit the configuration it writes. The
   first-session step in `CLAUDE.md` describes this.
4. Join the md-viewer session and the fedora-desktop session to one team with `ccy-teams`.
5. Make sure `gh` is logged in inside the md-viewer container and set up as git's credential
   helper (`gh auth setup-git`). Without that, `scripts/install-qa-tools.sh` cannot clone the
   private go-qa-ci. After that, Haiku starts at M0.
