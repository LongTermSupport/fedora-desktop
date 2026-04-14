# Research: Existing Unified Compression CLIs

**Date**: 2026-04-14
**Question**: Is there a CLI that already provides a single `compress` /
`uncompress` interface over multiple backends (xz, zip, ...), so we do not
reinvent the wheel?

## Short Answer

**Yes — `ouch` (Rust).** It does everything required. Recommendation: deploy
`ouch` and add thin bash wrappers only for the two UX preferences we care
about (xz-by-default on compress, always-extract-into-a-folder on uncompress).

## Candidates Evaluated

### ouch — CHOSEN

- **Repo**: https://github.com/ouch-org/ouch (~3.5k stars, MIT)
- **Maintenance**: Active. Last release 0.6.1 in April 2025; ongoing commits.
- **Formats**: tar, zip, **xz**, gz, bz/bz2/bz3, zst, 7z, lz4, rar, br.
  Chains supported (`.tar.xz`, `.tar.zst`, etc.).
- **Interface**:
  - `ouch compress <input...> <output.ext>` — backend chosen by output extension
  - `ouch decompress <archive>` — auto-detects by input extension
  - `ouch list <archive>` — peek without extracting
  - `--dir <path>` on decompress — enforces extraction target (this is what
    solves the tarbomb problem)
- **Install on Fedora 42**: NOT in Fedora repos, NOT in any COPR.
  - Option A: `cargo install ouch` (requires rust + clang)
  - Option B (**recommended**): static musl binary from GitHub release →
    drop into `/usr/local/bin` via Ansible `get_url`. Zero runtime deps.
- **Gotchas**:
  - `.zip` has streaming limitations inherent to the zip format (not an
    `ouch` flaw)
  - No distro package means we own the update cadence

### atool — fallback only

- **In Fedora 42 repos**: yes (`atool-0.39.0-27.fc42`)
- **Maintenance**: Dormant. Last upstream release 2016. Stable but stagnant.
- **Formats**: xz + zip + many more (dispatches to system tools).
- **Interface**: `apack <out.ext> <input>`, `aunpack <archive>`.
- **Why not chosen**: Dormant upstream, Perl dependency chain, and `ouch`
  is a strictly better replacement for a new deployment.

### 7z / p7zip-plugins — not a fit

- In Fedora repos. Can compress many formats, but the interface is 7z-centric
  and does not cleanly produce `.tar.xz` in one shot. Not an abstraction
  layer, just a format implementation.

### dtrx — extract-only

- "Do The Right Extraction." Explicitly designed to solve the tarbomb
  problem on decompress. **Does not compress**, so cannot be the single
  backend for `compress`. Worth noting as prior art for the "always create
  a folder" behaviour we want `uncompress` to have — `ouch --dir` gives us
  the same guarantee.

### peazip / file-roller / xarchiver — not a fit

- GUI-first. CLI subsets are awkward for scripting and add heavy
  dependencies. Skip.

## Conclusion

Use **`ouch`** as the backend. Our value-add is:

1. An opinionated `compress` wrapper (xz by default, `--zip` flag)
2. An opinionated `uncompress` wrapper that always extracts into a dedicated
   folder, preventing tarbombs regardless of archive contents
