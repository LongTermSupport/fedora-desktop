# Plan 00185: analysis of the host triage report

Analysis of the first `triage.bash` host run (report and log kept under the plan's
`untracked/plan-runs/` run folder, never copied here). Section names below are the report's own
headings. Values are anonymised: home paths are written `~`, and no user, host or session names
are given. Times in the report are local (BST, UTC+1); #60's creation time is UTC.

## Answers to the triage questions

### Which font does fontconfig pick for the issue's code points?

From "fc-match emoji:charset=…", "fc-match monospace:charset=…" and "fc-list :charset=…":

| Code point | Meant as       | `emoji:` request | `monospace:` request    | Fonts that have it                                         |
| ---------- | -------------- | ---------------- | ----------------------- | ---------------------------------------------------------- |
| U+1F4C8    | chart up       | Noto-COLRv1.ttf  | Symbola (monochrome)    | Symbola, Noto Sans Symbols 2, Noto Emoji, Noto Color Emoji |
| U+1F4CA    | bar chart      | Noto-COLRv1.ttf  | Symbola (monochrome)    | the same four                                              |
| U+1F9F5    | thread         | Noto-COLRv1.ttf  | Noto Emoji (monochrome) | Noto Emoji, Noto Color Emoji                               |
| U+1F4E6    | package        | Noto-COLRv1.ttf  | Symbola (monochrome)    | the same four                                              |
| U+1F528    | hammer (shown) | Noto-COLRv1.ttf  | Symbola (monochrome)    | Symbola, Noto Emoji, Noto Color Emoji                      |

- "fc-list :color=true": Noto-COLRv1.ttf is the only colour font on the system.
- "fc-match -s emoji": the chain is Noto Color Emoji, then Noto Emoji, then Symbola.
- "fc-match -s monospace": Noto Color Emoji is not in the monospace chain at all; Noto Emoji
  is 14th. So a lookup that starts from the terminal's monospace font and asks only for coverage
  lands on a **monochrome** font (Symbola, or Noto Emoji for U+1F9F5), never the colour one.
- Every match is a packaged file: "rpm -V" is clean for both emoji packages, and `~/.local/share/fonts`,
  `~/.fonts` and `/usr/local/share/fonts` are absent. There is no user fontconfig directory or
  `fonts.conf`, and no fontconfig file outside the stock `conf.avail` set mentions emoji.

### Which emoji font file is each running terminal mapping?

"terminal ptyxis pid …" and "ptyxis pid …: mapped font files":

- The only terminal emulator running is Ptyxis (its `--gapplication-service` process and its
  agent). "boot time" is 10:37 today and Ptyxis started at 10:39: the machine was rebooted
  today and Ptyxis was started fresh.
- Ptyxis maps `google-noto-color-emoji-fonts/Noto-COLRv1.ttf` **and** `gdouros-symbola/Symbola.ttf`,
  neither `(deleted)`. It does **not** map `NotoEmoji-Regular.ttf`. The agent maps no font.
- No VS Code process is running, and "VS Code terminal font and GPU settings" is empty.
- "parent process chain of this run": the triage ran inside a tmux server started by the user
  systemd manager, so the chain does not reach a terminal emulator. Which terminal the tmux
  client was attached in is not recorded; Ptyxis is the only candidate running.

### Do the fontconfig cache ages predate the font update?

No. "rpm -q --last google-noto-color-emoji-fonts" and "… google-noto-emoji-fonts": both were
installed on 2026-10-02 at 17:36:30 (version 20260921, Unicode 18.0 support, per the changelog).

- "font directories": both emoji font directories have mtime 2026-10-02 17:36:30.
- "system fontconfig caches": two caches were rewritten at 2026-10-02 17:40:12, after the
  directories; they are the only system caches written that day.
- "user fontconfig caches": the matching user `cache-9` files (fontconfig 2.17's format) were
  written at 2026-10-02 17:36:31, after the directories, and later `cache-11`/`cache-12` files
  (other fontconfig builds, such as Flatpak runtimes) on 10-02 and 10-05.
- fontconfig also rejects a cache older than its directory, so a stale cache would be rebuilt,
  not trusted.

### What do the terminal font settings say?

"dconf /org/gnome/Ptyxis/": `use-system-font=false`, `font-name='Source Code Pro 11'` (the
mapped Source Code Pro files agree), one profile with `cell-height-scale=1.1` and no font
override. "gsettings monospace-font-name" is `Adwaita Mono 11`, unused by Ptyxis. The kitty,
Ghostty and Alacritty configs set no font (Ghostty's `font-family` line is commented out); there
is no foot config. Versions: Ptyxis 50.1 on GTK 4.22.5 and VTE 0.84.1, Pango 1.57.1, HarfBuzz
14.1.0, FreeType 2.14.3, Cairo 1.18.6. The GPU is a hybrid Intel integrated plus NVIDIA
discrete laptop pair.

### The leads, tested

| Lead                                                                                     | Verdict                                                                                                                                                                  |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| google-noto-color-emoji-fonts 20260921, installed 2026-10-02, ships only Noto-COLRv1.ttf | Confirmed by "rpm -q --last" and "rpm -ql": the package's one font file is `Noto-COLRv1.ttf`, the COLRv1 build.                                                          |
| google-noto-emoji-fonts (monochrome) is installed too                                    | Confirmed, same version and the same install second. It wins the monospace fallback only for U+1F9F5; Symbola wins the others, and Ptyxis does not map it.               |
| VTE 0.84.1, Ptyxis is the terminal                                                       | Confirmed for the version ("ptyxis --version", "VTE and terminal packages"). Ptyxis is the only running terminal, but the report cannot show it is the one with the bug. |

## Ranked diagnosis

1. **H1, the emoji font was replaced under a running terminal (most likely cause of #60 as
   reported).** Both emoji packages were upgraded at 2026-10-02 17:36 BST, and #60 was filed
   the next morning, 2026-10-03 09:43 UTC. The new build adds Unicode 18 glyphs, which shifts
   glyph IDs; a process that shaped text with the old face's glyph IDs and drew them from the
   new file (or the reverse) would draw a neighbouring glyph: a near-constant code point offset
   such as 0x60 where the glyph order follows the code points, and a jump to the skin-tone
   blocks where it does not (F5). Not confirmed directly: the process that would show it is
   gone, because the machine was rebooted today and the running Ptyxis maps the current file,
   not a `(deleted)` one ("ptyxis pid …: mapped font files", "boot time"). Earlier boots are
   not recorded, so whether the terminal of 10-03 predated the upgrade cannot be shown from the
   report.
2. **H4 in GTK form, a renderer glyph cache problem in Ptyxis.** If the bug is still visible in
   a fresh Ptyxis window today, fonts and caches are clean (sections above) and the remaining
   suspects are inside the renderer: GTK 4's GPU glyph cache, which Ptyxis/VTE 0.84 draw through,
   on a hybrid-GPU laptop. xterm.js (#6014) is out of the picture at triage time: no VS Code is
   running.
3. **H5, COLRv1 drawn wrongly.** Same condition as 2, but the bug would also follow the font
   into other COLRv1-capable GTK apps and not show in kitty, Ghostty, Alacritty or foot, all
   installed and available for the comparison.
4. **H3 weak form, monochrome fallback.** The monospace fallback for these code points is
   Symbola or Noto Emoji, not Noto Color Emoji, and Ptyxis has Symbola mapped. This explains
   black-and-white emoji, not a hammer for a chart: Symbola's own character map is correct.
   It is a real defect in the fallback order, but not the substitution in #60.

**Refuted:**

- **H2, stale fontconfig cache.** Every cache for the emoji directories is newer than the
  directories.
- **H3 strong form, a rogue second emoji font.** There is one colour emoji font, every font
  file is packaged and verified, and there are no user fonts or user fontconfig.

## Proposed IaC fix (not applied: needs the owner's go-ahead)

Two parts, matching causes 1 and 4. A cache refresh task is **not** proposed: H2 is refuted and
Fedora's font packages already rebuild the caches.

1. **Fallback order (cause 4, certain and independent of the screen check).** Deploy one
   fontconfig file to `/etc/fonts/conf.d/` from the play that already owns fontconfig
   (`play-ms-fonts.yml` installs the `fontconfig` package today; or a renamed fonts play),
   appending **Noto Color Emoji** to the `monospace`, `sans-serif` and `serif` families. It
   must be an append after the generic family, never a `prefer` ahead of it: Noto Color Emoji
   carries digits, `#` and `*` for keycaps, and a `prefer` would draw the terminal's digits from
   the emoji font. Removing `google-noto-emoji-fonts` alone is not enough (Symbola still wins
   four of the five code points) and is not needed once the append is in place.
2. **Font upgrade under running terminals (cause 1, if the screen check confirms it).** The
   defence is a restart, not a cache. A dnf5 actions hook (the `libdnf5-plugin-actions`
   package and a `post_transaction` line for the emoji and other font packages) that tells the
   desktop user, through a desktop notification, to restart terminal windows after a font
   upgrade. Whether the hook is worth carrying is the owner's call; the minimum is a line in
   the font play's docs.

## How to verify

- Part 1: `fc-match monospace:charset=1f4c8` (and the other four code points) names
  `Noto-COLRv1.ttf`; `fc-match monospace:charset=30` still names the monospace text font, not
  the emoji font; and the issue's `printf` draws colour emoji in a new Ptyxis window. These go
  in the plan's `acceptance.bash` (Task 2.2), run from meta-deploy.
- Part 2: after a forced reinstall of the emoji package, the notification appears; the issue's
  `printf` in an already-open terminal draws correctly once it is restarted.
- `triage.bash` re-run: the "fc-match monospace:charset=…" rows change to Noto Color Emoji.

## Still unknown: the owner at the screen (Task 1.2)

1. **Is the bug visible today**, after this morning's reboot, with the issue's
   `printf '\U0001F4C8 \U0001F9F5 \U0001F4E6\n'` in a **new** Ptyxis window, both inside and
   outside tmux? If it is gone, cause 1 stands and only the defences remain. If it is still
   there, cause 1 is refuted and causes 2 and 3 are next.
2. **Are the wrong glyphs in colour or black and white?** A colour hammer means the colour font
   is being drawn with the wrong glyph IDs (causes 1 to 3); a monochrome glyph means the
   fallback (cause 4) is in play.
3. **Which terminal showed it on 10-03** (Ptyxis, or VS Code's terminal), and whether that
   window had been open since before the upgrade on 10-02 17:36.
4. If it is still visible: does the same `printf` draw correctly in kitty or Ghostty (H5
   against H4)?

## Gaps in the probe (for a re-run, not blockers)

- "emoji font files …" uses `-iname '*emoji*'` on file names, so it misses `Noto-COLRv1.ttf`
  (its directory is matched); "rpm -V" being clean and the directory mtime cover it here.
- "dnf history of emoji font packages" fails (rc=1): dnf5's `history list` takes transaction
  IDs, not package names.
- The cache rows do not map each cache hash to its directory (`fc-cat -V` would). Two system
  caches for directories not identified here were rewritten on 2026-10-13 09:55, the day the
  plan was filed; what changed then is not known.
