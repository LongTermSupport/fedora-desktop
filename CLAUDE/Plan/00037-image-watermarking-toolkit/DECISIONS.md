# Plan 00037 — Technical Decisions

Decision records for the image watermarking toolkit. The task tree lives in
[PLAN.md](PLAN.md); the research these decisions rest on is in
[RESEARCH-watermarking.md](RESEARCH-watermarking.md); the full original prose
is preserved in [PLAN_archive.md](PLAN_archive.md).

## Decision 1: Bash vs Python

**Context**: Repo uses both. Compress/uncompress are bash; speech-to-text
helpers are Python. Watermark is a thin orchestrator over `magick` + `exiftool`.

**Options**:

1. Bash — matches `compress`/`uncompress` precedent, no runtime deps
2. Python — easier config-file parsing, would pull in `pyexiftool` or
   shell out anyway

**Decision**: **Bash**. The script is fundamentally argument parsing +
two external command invocations + a config-file load. Python adds a
runtime dep with no win. Config can be a sourceable bash file (next
decision) — simpler than INI/TOML/YAML parsing in bash.
**Date**: 2026-04-28

## Decision 2: Config file format

**Context**: Need per-user defaults and named profiles. Bash-native vs INI.

**Options**:

1. Sourceable bash (`KEY=VALUE`, profiles via `[blog]` shell-function blocks
   or per-profile files like `~/.config/watermark/profiles/blog.conf`)
2. INI (parseable by `awk`/`crudini`)
3. TOML (would require a parser binary)

**Decision**: **Per-profile sourceable bash files** at
`~/.config/watermark/profiles/<name>.conf`. The "default" profile is
`~/.config/watermark/defaults.conf`. Each file is a flat set of `KEY=VALUE`
exports, sourced by the script in a sub-shell to avoid env pollution.
Simpler than INI parsing, no external deps, and `--profile blog` maps to
`~/.config/watermark/profiles/blog.conf` with zero ambiguity. Trade-off:
no INI tooling support (e.g., editors highlighting INI sections), accepted.
**Date**: 2026-04-28

## Decision 3: Sentinel XMP namespace

**Context**: The custom sentinel tags (`Applied`, `AppliedAt`, `AppliedBy`)
need a namespace.

**Options**:

1. Reuse `XMP-x:` (generic XMP, exiftool writes it freely without config)
2. Define a custom namespace (e.g., `XMP-wm:`) — requires an
   exiftool config file deployed alongside the binary
3. Stash everything in `XMP-dc:Description` as a parseable string

**Decision**: **Option 2 — custom `XMP-wm:` namespace via deployed
exiftool config file.** Original choice (Option 1) was disproven during
Phase 1 Task 1.4 prototyping: exiftool does NOT accept arbitrary user tag
names under `XMP-x:` — it warns `Tag 'XMP-x:WatermarkApplied' is not defined`
and silently drops them. Option 2 works cleanly with a `-config FILE` arg
defining a `wm` namespace (URL `https://example.com/ns/watermark/1.0/`).
Cost is one extra deployed config file (~20 lines of Perl) and one extra
flag to every exiftool invocation. Tags written: `XMP-wm:Applied`,
`XMP-wm:AppliedAt`, `XMP-wm:AppliedBy`.

**Implementation note**: exiftool date values must use the format
`YYYY:mm:dd HH:MM:SS[.ss][+/-HH:MM|Z]` (colons in the date portion). ISO
8601 dashes (`YYYY-MM-DD`) are rejected. Use
`date -u +'%Y:%m:%d %H:%M:%SZ'` in the script.

**Date**: 2026-04-28 (revised after prototype)

## Decision 4: stdin batch mode

**Context**: User said "composable and extensible/wrappable". A
single-image CLI plus `xargs` covers most batch needs, but `--stdin0`
(read NUL-delimited paths) makes `find ... -print0 | watermark --stdin0`
trivial.

**Options**:

1. CLI-only single-image; users wrap with `xargs -0 -n1`
2. Add `--stdin0` reading NUL-delimited paths

**Decision**: **Add `--stdin0`** in Phase 3. ~10 lines of bash; large
ergonomic win for batch wrappers; doesn't constrain the CLI design (each
input is still processed independently, exit codes apply per-file with a
final summary). If it adds unforeseen complexity during Phase 3
implementation, drop and document the `xargs` workaround instead.
**Date**: 2026-04-28

## Decision 5: Visible watermark — opacity, font size, tile angle

**Context**: Hardcoded constants in the script, or fully configurable?

**Options**:

1. Hardcoded "good defaults" (corner 70% opacity at 2.5% width, tile 8%
   opacity at 30°), no flags
2. Fully configurable per-flag
3. Hardcoded defaults, configurable via config file (no CLI flags)

**Decision**: **Option 3**. The defaults are good. CLI flags would bloat
the surface (8+ extra options); config-file keys
(`opacity_corner=0.7`, `opacity_tile=0.08`, `tile_angle=30`,
`size_pct=0.025`) keep the CLI focused on per-image semantics and let
profile authors tune presentation once. Phase 1 Task 1.4 locks the actual
default values via image inspection.
**Date**: 2026-04-28

## Decision 6: Output naming when `--output PATH` is passed

**Context**: Default output is `INPUT.watermarked.EXT`. If user passes
`--output foo.jpg`, do we honour it verbatim, or force the
`.watermarked.` suffix?

**Options**:

1. Honour `--output` verbatim; user is responsible for the suffix
2. Force `.watermarked.` insertion (e.g., `--output foo.jpg` becomes
   `foo.watermarked.jpg`)
3. Refuse `--output` paths that lack `.watermarked.`

**Decision**: **Option 1 — honour verbatim**. The metadata sentinel is the
authoritative idempotency channel; the filename suffix is a convenience.
Forcing the suffix into a user-supplied path is surprising. The XMP
sentinel still protects against double-watermarking. Document the
trade-off in `--help`.
**Date**: 2026-04-28

## Risks & Mitigations

| Risk                                                                                                          | Impact | Probability | Mitigation                                                                                                                                              |
| ------------------------------------------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ImageMagick `policy.xml` blocks JPEG read/write under default Fedora policy                                   | High   | Low         | Phase 1 Task 1.1 verifies; if blocked, document the fix (relax `coder` policy for JPEG/PNG only) but do not auto-modify system file                     |
| Visible watermark looks bad on edge cases (very dark images, very small images, mostly-white images)          | Med    | Med         | Phase 2 Task 2.3 inspects a varied set; add a `--invert` flag if light/dark contrast becomes an issue                                                   |
| Re-encoding JPEG for the watermark loses noticeable quality                                                   | Med    | Med         | Hardcode `-quality 95` (config override available); document that the watermarked file is for distribution, originals are preserved                     |
| ImageMagick 7 vs 6 syntax drift (`magick` vs `convert`) breaks on hosts with old IM6                          | High   | Very Low    | Preflight assert in playbook; Fedora 43 ships IM7                                                                                                       |
| User loses originals because they pointed `--output` at the input path                                        | High   | Low         | Refuse if `--output` resolves to the same realpath as input; covered in Phase 2 arg validation                                                          |
| Config file precedence becomes confusing                                                                      | Low    | Med         | Document precedence in `--help`; add `--show-config` flag (Phase 2 sub-task) that prints the resolved merged config and exits 0                         |
| `--stdin0` partial-failure behaviour ambiguous (one file fails, do remaining proceed?)                        | Med    | Med         | Define explicit policy: continue on per-file failure, print summary at end (`N succeeded, M skipped, K failed`), exit non-zero if K > 0                 |
| Custom `XMP-wm:` namespace requires `-config FILE` on every exiftool call; missing config silently drops tags | High   | Med         | Script must hardcode `-config /etc/watermark/exiftool.config`; fail fast if file missing; covered in Phase 4 (playbook deploys config alongside binary) |
