# Research: desktop markdown viewers for Fedora / GNOME (Wayland)

Scope: a window that opens fast and renders a `.md` file well, reachable by double-click in Nautilus or `<tool> file.md`. Research only; nothing installed or changed.

Method and limits: web search and page fetches on 2026-10-15. Facts marked "verified" were read from the cited page. Items marked "estimate" are reasoned from the toolkit, not measured. Several project pages returned empty release sections or 404s; those gaps are stated, not filled in. No candidate was run on the host.

## 1. Comparison table

| Candidate                                          | Viewer / editor                             | View-only mode                        | Toolkit / engine                                          | Fedora packaging                                                                                               | Maintenance (verified)                                                                                       | Startup (estimate)                 | Rendering notes                                                                                          | Dark mode                                                    | Live reload                                                             | Nautilus default                                                              |
| -------------------------------------------------- | ------------------------------------------- | ------------------------------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| **Apostrophe** (org.gnome.gitlab.somas.Apostrophe) | Editor (distraction-free) with live preview | No                                    | GTK4 + libadwaita, Python, pandoc for export              | No official Fedora package found (404 on the Fedora page); a COPR `gui1ty/Apostrophe` exists                   | Flathub v3.5, page says "7 days ago" at capture; contribution policy rejects LLM-generated contributions     | ~0.3–0.6 s (estimate)              | Live preview; export to PDF/Word/LaTeX/HTML; GFM fidelity not verified                                   | Dark, light, sepia themes; follows GNOME scheme not verified | Not verified                                                            | Flatpak declares MIME; set via Settings > Default Applications (not verified) |
| **Marker** (com.github.fabiocolacio.marker)        | Editor with view and edit modes             | Has a view mode, but is not view-only | GTK3, WebKitGTK 4 (inferred from deps)                    | No Fedora package found (404)                                                                                  | Flathub listing exists, "Unverified", last commit reported 2025-08-04; last release 2023.05.02 (per listing) | ~0.5–1 s (estimate, WebKit2 GTK3)  | Mermaid, math, syntax highlight, custom CSS; SciDown syntax, not described as GFM                        | Custom CSS themes; no system dark mode mentioned             | Not mentioned                                                           | Not verified                                                                  |
| **Marka** (io.github.HemanthGangula.Marka)         | Viewer and editor, live split pane          | No (editor pane present)              | GTK4 + libadwaita, Python, WebKitGTK 6.0, GtkSourceView 5 | Flathub; pip, PPA, Snap; no Fedora dnf package found                                                           | Flathub listing exists; GitHub shows 7 commits and no release tags                                           | ~0.5–0.8 s (estimate, WebKit view) | "Full CommonMark", tables; Mermaid not mentioned; relative links not mentioned                           | Follows system light/dark via libadwaita (verified, README)  | Not mentioned (live preview updates while editing, not on-disk changes) | Flatpak declares MIME; manual default (not verified)                          |
| **Glow** (charmbracelet/glow)                      | Terminal viewer; TUI browser                | Yes (it is a reader)                  | Go, Glamour + Bubble Tea/Lip Gloss; terminal              | **Official dnf**: `glow` on Fedora 43, 44, 45, Rawhide, EPEL 10                                                | Upstream v3.0.0 released Aug 2026 (verified)                                                                 | \<0.05 s (estimate)                | GFM via Glamour: tables, task lists, code highlight; no images in terminal by default; no mermaid        | Auto dark/light from terminal background; custom JSON style  | No                                                                      | No GUI; not a Nautilus handler                                                |
| **Frogmouth** (Textualize/frogmouth)               | Terminal viewer; can open URLs              | Yes                                   | Python, Textual; terminal                                 | No (pipx / pip / Homebrew tap)                                                                                 | Release date not shown; 442 commits                                                                          | \<0.3 s (estimate, Python)         | Navigation history, bookmarks, TOC; engine not named on page                                             | Terminal theme                                               | No                                                                      | No                                                                            |
| **mdcat** (lunaryorn/mdcat)                        | Terminal renderer (cat-like)                | Yes                                   | Rust; terminal                                            | COPR only (`lunaryorn/mdcat`); not in official repos (verified via COPR result)                                | **Archived by owner 2026-06-19, read-only**; last release 2.7.1 (Dec)                                        | \<0.05 s                           | CommonMark; images via terminal protocols                                                                | Terminal theme                                               | No                                                                      | No                                                                            |
| **Okular** (KDE)                                   | Document viewer; Markdown backend           | Yes                                   | Qt/KF; Markdown backend based on Discount                 | **Official dnf**: `okular` on Fedora 43, 44, 45, Rawhide. Markdown generator not listed in package subpackages | Active (KDE, release 26.08; docs updated July 2026)                                                          | ~0.5–1 s (estimate, Qt)            | Discount rendering; SmartyPants on by default; likely weak on GFM tables/mermaid/local images (estimate) | Qt palette; not GNOME scheme                                 | Not verified                                                            | Yes via `.desktop` (KDE-owned); not verified                                  |
| **Ghostwriter** (KDE)                              | Markdown editor with live HTML preview      | No (editor)                           | Qt                                                        | **Official dnf**: Fedora 43, 44, 45, Rawhide, EPEL 10 (current 26.08.x)                                        | Active; Flathub listing, KDE-hosted                                                                          | ~0.5–1 s (estimate, Qt)            | Live preview; GFM not verified                                                                           | Qt theming                                                   | Not verified                                                            | Desktop file present (not verified)                                           |
| **ReText**                                         | Editor with optional live preview           | No                                    | Python, Qt (PyQt)                                         | **Official dnf** `retext` 8.0.0 on Fedora 41/42 stable; package **orphaned**                                   | Upstream active-ish; Fedora package orphaned                                                                 | ~0.5 s (estimate)                  | Markdown/reST; GFM not verified                                                                          | Qt                                                           | Not verified                                                            | Not verified                                                                  |
| **MarkText**                                       | Editor with realtime preview                | No                                    | Electron                                                  | Flathub listing dated 2026-05-24; AUR; no dnf                                                                  | Upstream returned to maintenance 2026-05-13 (per fork README, not confirmed upstream)                        | 1–3 s (estimate, Electron)         | CommonMark + GFM stated on Flathub                                                                       | Themes                                                       | Not verified                                                            | Not verified                                                                  |
| **Typora**                                         | Editor (WYSIWYG-like)                       | No                                    | Electron                                                  | No dnf; proprietary, paid                                                                                      | Active commercial                                                                                            | 1–3 s (estimate, Electron)         | Full GFM                                                                                                 | Themes                                                       | Not verified                                                            | Not verified                                                                  |
| **Zettlr**                                         | Editor / knowledge base                     | No                                    | Electron                                                  | Flathub                                                                                                        | Active (per listing)                                                                                         | 1–3 s (estimate)                   | Full GFM, citations                                                                                      | Themes                                                       | Not verified                                                            | Not verified                                                                  |
| **gnome-text-editor**                              | Editor                                      | No                                    | GTK4 + libadwaita                                         | Official dnf                                                                                                   | Active (GNOME)                                                                                               | \<0.5 s (estimate)                 | **No markdown preview found** in search; gedit preview plugin is gedit-only                              | Follows GNOME scheme                                         | No                                                                      | Yes (default text editor)                                                     |
| **grip-live-diff** (babs fork of go-grip)          | Browser previewer, GFM-like render offline  | Yes (view only)                       | Go binary; serves to browser at localhost:6419            | No dnf; `go install` or release binaries                                                                       | Release tag not verified                                                                                     | \<0.3 s (estimate; browser open)   | GitHub look, offline; Mermaid not checked                                                                | Browser CSS, GitHub-style                                    | **Yes**, reloads on save                                                | No (opens browser, not a GUI window)                                          |
| **gh-gfm-preview** (thiagokokada)                  | Browser previewer                           | Yes                                   | Go; uses GitHub API (online) for rendering                | No dnf; `go install`                                                                                           | Versions up to v1.0.1 on pkg.go.dev                                                                          | \<0.3 s                            | GitHub-exact via API                                                                                     | GitHub style                                                 | Yes (`-R` disables)                                                     | No                                                                            |
| **markserv** (npm; ptkdev fork)                    | Browser previewer, directory index          | Yes                                   | Node.js server                                            | No dnf; `npm i -g`                                                                                             | v1.17.4 (MIT); fork vs upstream unclear                                                                      | ~0.5 s (Node start, estimate)      | GFM-style CSS; links between .md files                                                                   | GitHub style                                                 | Yes, needs a LiveReload browser plugin                                  | No                                                                            |
| **MDV** (mvz/mdv)                                  | GNOME markdown viewer                       | Yes                                   | Ruby; WebKit2 GTK                                         | Not checked on Fedora                                                                                          | Not verified                                                                                                 | ~0.5–1 s                           | Not verified                                                                                             | Not verified                                                 | Not verified                                                            | Not verified                                                                  |
| **Marka-like "gtkmdview"** (AUR)                   | Widget library, not an app                  | n/a                                   | GTK4 widget, no WebKit                                    | No                                                                                                             | Not verified                                                                                                 | n/a                                | Native widgets                                                                                           | n/a                                                          | n/a                                                                     | n/a                                                                           |

Packages reported as missing on Fedora: `apostrophe`, `marker`, `frogmouth` pages returned 404 or no entry. The `mdcat` Fedora page returned 404; the COPR result is the only packaging route found.

## 2. Notes per candidate

**Apostrophe.** Editor, not a viewer. Its Flathub page lists live preview, a distraction-free mode, and dark/light/sepia themes, and it has no view-only mode. Sources: [Flathub page](https://flathub.org/apps/org.gnome.gitlab.somas.Apostrophe), [Apostrophe on linuxphoneapps](https://linuxphoneapps.org/apps/org.gnome.gitlab.somas.apostrophe/), [search summary](https://fossies.org/dox/apostrophe-v3.4). The COPR result in the search (`gui1ty/Apostrophe`) suggests the Fedora route is community packaging.

**Marker.** GTK3 with WebKitGTK 4, GPL-3.0. Feature set is view/edit, mermaid, math, custom CSS. Its Flathub listing flags it as Unverified and asks for home folder access. It is the closest "view + mermaid" candidate, but it is old (GTK3) and its upstream release history looks stale. Sources: [Flathub listing via linux-meta](https://linux-meta.duckdns.org/en/p/marker), [GitHub page](https://github.com/fabiocolacio/Marker).

**Marka.** The best modern GNOME-native candidate found. GTK4 + libadwaita + WebKitGTK 6.0, Python, follows the system light/dark scheme through libadwaita, and has tables. It is also an editor with a split pane. It has few commits and no release tags, so maintenance is thin. Sources: [GitHub README](https://github.com/HemanthGangula/marka), [PyPI](https://pypi.org/project/marka).

**Glow.** The only candidate with an official Fedora dnf package that is current (3.0.0 for Rawhide and 45; 2.1.2 for 44 and 43). Terminal output, so startup is effectively instant, and rendering uses Glamour, which handles GFM tables, task lists, and code highlighting. It has no GUI window and cannot be a Nautilus default handler without a wrapper. Sources: [Fedora glow package](https://packages.fedoraproject.org/pkgs/glow/glow/), [glow releases](https://github.com/charmbracelet/glow/releases), [glow overview](https://frica.codeberg.page/blog/glow-markdown-viewer.html).

**Frogmouth.** Textual terminal viewer that opens local files and URLs, with history, bookmarks, and TOC. Not on Fedora or Flathub; installed by pipx or pip. Release date not shown on the page. Source: [Frogmouth GitHub](https://github.com/Textualize/frogmouth).

**mdcat.** Archived by its owner on 2026-06-19 and read-only. Available only via COPR. Not a good dependency. Sources: [mdcat releases](https://github.com/lunaryorn/mdcat/releases), [COPR](https://copr.fedorainfracloud.org/coprs/lunaryorn/mdcat).

**Okular.** The Markdown backend is Discount-based with SmartyPants on by default. Official Fedora package is current. The Fedora package page does not list a markdown generator subpackage, so whether the backend is in Fedora's build is unconfirmed. Okular is a KDE/Qt document viewer and would not follow the GNOME scheme. Sources: [Okular backends docs](https://docs.kde.org/stable/en/okular/okular/configure-backends.html), [Fedora okular package](https://packages.fedoraproject.org/pkgs/okular/okular/).

**Ghostwriter.** Qt editor with live HTML preview and word counts, maintained under KDE. Official Fedora package is current in every release. It is an editor and has no view-only mode. Sources: [Fedora ghostwriter package](https://packages.fedoraproject.org/pkgs/ghostwriter/ghostwriter/), [man page](https://man.archlinux.org/man/ghostwriter.1.en).

**ReText.** Qt/PyQt editor with optional live preview. Fedora package 8.0.0 but orphaned. Source: [Fedora retext package](https://packages.fedoraproject.org/pkgs/retext/retext/).

**MarkText.** Electron editor. Flathub listing dated 2026-05-24 claims CommonMark and GFM. Upstream maintenance resumed in May 2026 per a fork's README, which is not an upstream source. Source: [Flathub listing via linux-meta](https://www.linux-meta.duckdns.org/en/p/marktext).

**Typora, Zettlr.** Electron editors. Typora is paid and proprietary. Zettlr is free, on Flathub, and aimed at knowledge management. Neither is a viewer. Source: [AlternativeTo listings](https://alternativeto.net/software/elegant-markdown-editor/).

**gnome-text-editor.** No markdown preview found in search. The gedit Markdown Preview plugin is a gedit feature; gnome-text-editor has no plugin route found. Sources: [GNOME wiki gedit markdown support](https://wiki.gnome.org/Apps/Gedit/MarkdownSupport), [OMG Ubuntu](https://www.omgubuntu.co.uk/?p=260716).

**Browser previewers (grip-live-diff, gh-gfm-preview, markserv, mdpreview).** Each runs a local server and opens a browser tab. Live reload works in grip-live-diff and gh-gfm-preview; markserv needs a browser LiveReload plugin. None is a GUI window or Nautilus handler. Sources: [grip-live-diff](https://pkg.go.dev/github.com/babs/grip-live-diff), [gh-gfm-preview](https://pkg.go.dev/github.com/thiagokokada/gh-gfm-preview@v0.9.6), [markserv npm](https://npmjs.com/package/markserv).

**Flathub search for "markdown viewer".** The live search page did not return a parseable list; the nearest results are Marker, Marka, and Ghostwriter. Source: [Flathub search](https://flathub.org/search?q=markdown%20viewer) (fetch returned no list).

## 3. Answers

### Q1: Is there a fast, view-only, GNOME-native option that renders well?

**No, not in the set examined.**

- The view-only candidates are either terminal tools (Glow, Frogmouth, mdcat), browser previewers (grip-live-diff, gh-gfm-preview, markserv), or KDE/Qt (Okular).
- The GTK4/libadwaita candidates (Marka, Apostrophe) are editors. Marka is the closest, with a system dark scheme and WebKitGTK 6 rendering, but it has an editor pane and no verified live reload.
- Glow is the fastest and most reliable option that is officially packaged for Fedora, but it is a terminal app. It answers "run `glow file.md` and see it rendered", not "double-click in Nautilus".

Practical choices, in order:

1. Use Glow from dnf for `glow file.md`, and use Marka from Flathub if a GNOME window is wanted and an editor pane is acceptable.
2. Use grip-live-diff if the GitHub-accurate browser rendering and live reload matter more than a native window.
3. Build the small viewer in §4 if a view-only, GNOME-native window is required.

### Q2: What would a small one need?

Sketch of a minimal viewer. Nothing here is built or tested.

**Stack.** Python with PyGObject (GTK4 + libadwaita + WebKitGTK 6.0), or Go with gotk4 and WebKitGTK 6.0 bindings. Python is the shorter path because Marka and Apostrophe already show the working combination on GNOME.

**Parts.**

| Part             | Choice                                                                                        | Why                                                                |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Window           | `Adw.Application` + `Adw.ApplicationWindow` + one `WebKit.WebView`                            | Native header, follows the GNOME scheme                            |
| Rendering        | `markdown-it-py` with the GFM tables plugin, or `cmark-gfm`                                   | CommonMark with GFM tables; code blocks via a vendored highlighter |
| CSS              | Vendored GitHub markdown CSS (light and dark) selected by `prefers-color-scheme`              | `Adw.StyleManager` drives the color scheme; WebKit reads it        |
| Relative links   | Rewrite `href` to `.md` files to open in the same window's history                            | Keeps the viewer view-only                                         |
| Images           | Load via `file://` base URI set to the document's directory                                   | Local images work without a server                                 |
| File watching    | `Gio.File.monitor_file` on the open path                                                      | GTK-native; no extra dependency                                    |
| Dark mode        | `Adw.StyleManager.get_dark()` → add a `dark` CSS class or call `set_color_scheme` on the view | Follows the GNOME colour scheme                                    |
| CLI              | `argparse`, `file.md` argument, or no argument for a file chooser                             | `tool file.md` works                                               |
| Nautilus default | `.desktop` with `MimeType=text/markdown;text/x-markdown;` and `xdg-mime default` in IaC       | Nautilus opens the handler on double-click                         |
| Startup          | One `WebKit.WebView` created at startup; no network; no JS framework                          | Keeps the first paint low                                          |

**Size estimate (parts, not time).**

- Python entry point and window code: about 300–400 lines.
- Renderer glue, link rewriting, file watcher: about 150–250 lines.
- CSS (light and dark) and vendored highlighter: about 2 files, the highlighter being the bulk by size.
- `.desktop`, MIME, and packaging in Ansible: 2–3 files.
- Tests: rendering fixtures (tables, code, links, images) and the MIME/`.desktop` check.

Total hand-written code: roughly 500–800 lines of Python, plus vendored CSS and JS.

**Fedora dependencies.** `gtk4`, `libadwaita`, `webkitgtk6.0`, `python3-gobject`, and a markdown library are expected in Fedora's repositories. The search did not confirm each package name; run `dnf info` before committing to the dependency list. Per the project's IaC rule, these would be installed by a playbook, not by hand.

**Risks.**

- WebKit startup is the dominant cost. A cold WebKit process typically takes a noticeable fraction of a second (estimate). A view-only window should be measured on the host before it is called fast.
- Mermaid is about 3 MB of JS. Leave it out of the first version unless it is needed.

## 4. Recommendation

1. **For the immediate need:** install `glow` from Fedora dnf (official, current, instant start) for `glow file.md`. This is the lowest-cost option and needs no build.
2. **For a GNOME window now:** test Marka from Flathub on the host. It is GTK4/libadwaita, follows the system dark mode, and has tables. Accept the editor pane, or confirm it can be hidden.
3. **If a true view-only GNOME viewer is needed:** build the small viewer in §3 Q2. Do not start from Okular or Marker; Okular is Qt and Marker is GTK3 with stale upstream.
4. **Do not use** Electron editors (MarkText, Typora, Zettlr) for a quick viewer; startup is the stated requirement.
5. **Do not depend on mdcat:** it is archived.

Decision needed from the owner: whether a GNOME window that also edits (Marka) is acceptable, or whether a view-only build should be planned.

## 5. Sources

- Apostrophe Flathub: https://flathub.org/apps/org.gnome.gitlab.somas.Apostrophe
- Apostrophe on linuxphoneapps: https://linuxphoneapps.org/apps/org.gnome.gitlab.somas.apostrophe/
- Marker GitHub: https://github.com/fabiocolacio/Marker
- Marker Flathub (via linux-meta mirror): https://linux-meta.duckdns.org/en/p/marker
- Marka GitHub: https://github.com/HemanthGangula/marka
- Marka PyPI: https://pypi.org/project/marka
- Frogmouth GitHub: https://github.com/Textualize/frogmouth
- Glow releases: https://github.com/charmbracelet/glow/releases
- Glow Fedora package: https://packages.fedoraproject.org/pkgs/glow/glow/
- Glow overview: https://frica.codeberg.page/blog/glow-markdown-viewer.html
- mdcat releases: https://github.com/lunaryorn/mdcat/releases
- mdcat COPR: https://copr.fedorainfracloud.org/coprs/lunaryorn/mdcat
- Okular backends: https://docs.kde.org/stable/en/okular/okular/configure-backends.html
- Okular Fedora package: https://packages.fedoraproject.org/pkgs/okular/okular/
- Ghostwriter Fedora package: https://packages.fedoraproject.org/pkgs/ghostwriter/ghostwriter/
- Ghostwriter man page: https://man.archlinux.org/man/ghostwriter.1.en
- ReText Fedora package: https://packages.fedoraproject.org/pkgs/retext/retext/
- MarkText Flathub (via linux-meta mirror): https://www.linux-meta.duckdns.org/en/p/marktext
- Typora / Zettlr listings: https://alternativeto.net/software/elegant-markdown-editor/
- gedit markdown support wiki: https://wiki.gnome.org/Apps/Gedit/MarkdownSupport
- gedit preview article: https://www.omgubuntu.co.uk/?p=260716
- grip-live-diff: https://pkg.go.dev/github.com/babs/grip-live-diff
- gh-gfm-preview: https://pkg.go.dev/github.com/thiagokokada/gh-gfm-preview@v0.9.6
- markserv npm: https://npmjs.com/package/markserv
- MDV (Ruby, GNOME): https://github.com/mvz/mdv
- Flathub search (no parseable list): https://flathub.org/search?q=markdown%20viewer
