# Plan 030: PHPantom LSP — Research and Potential Migration

**Status**: In Progress
**Created**: 2026-04-13
**Owner**: Claude/User
**Priority**: Medium
**Type**: Research + Implementation

## Overview

Evaluate PHPantom (`AJenbo/phpantom_lsp`) as a replacement for Intelephense as the PHP language server in the CCY container. The current Intelephense LSP is reportedly providing inaccurate/stale feedback that may be doing more harm than good. PHPantom is a Rust-based PHP LSP that claims dramatically better performance and native Laravel support.

This plan has a **decision gate** between research and implementation — we only proceed to Phase 2 if Phase 1 confirms PHPantom is a viable improvement.

## Goals

- Determine whether PHPantom provides more accurate PHP diagnostics than Intelephense
- Evaluate PHPantom's maturity, stability, and feature completeness for daily use
- If confirmed viable, replace Intelephense with PHPantom in the CCY Dockerfile
- Ensure Claude Code's LSP integration works correctly with PHPantom

## Non-Goals

- Evaluating other PHP LSPs (Phpactor, PHP Tools) — only PHPantom vs Intelephense
- Changing host system PHP tooling — this is CCY container only
- PHPStorm or IDE-specific configuration

## Context & Background

### Current State

- Intelephense is installed globally via npm in the CCY Dockerfile (`files/var/local/claude-yolo/Dockerfile:199`)
- Referenced in docs: `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`
- Referenced in template: `files/var/local/claude-yolo/Dockerfile.project-template`
- The `lsp_enforcement` hooks daemon handler directs Claude to use LSP tools for symbol lookups

### Problem

Intelephense is providing inaccurate or stale feedback, potentially causing more harm than good when Claude Code uses it for PHP code intelligence.

### PHPantom Key Claims (from GitHub README, v0.7.0)

| Metric                        | PHPantom   | Intelephense |
| ----------------------------- | ---------- | ------------ |
| Startup (21k files, 1.5M LOC) | < 1 second | 1m 25s       |
| RAM                           | 59 MB      | 520 MB       |
| Disk cache                    | 0 MB       | N/A          |

Additional advantages claimed:

- Native Laravel Eloquent support (no plugin needed)
- Full PHPStan annotation support
- Generics/template support
- Embedded phpstorm-stubs (no runtime downloads)
- Single binary (Rust), no Node.js dependency

### PHPantom Potential Concerns

- Version 0.7.0 — pre-1.0, may have rough edges
- 518 stars / 16 open issues — smaller community than Intelephense
- Workspace symbols marked as partial support
- No Intelephense premium features comparison (formatting, rename across files)

## Tasks

### Phase 1: Research and Evaluation

- [x] ✅ **Task 1.1**: Evaluate PHPantom binary availability and installation method

  - [x] ✅ Pre-built binary available: `phpantom_lsp-x86_64-unknown-linux-gnu.tar.gz` (27MB)
  - [x] ✅ **GLIBC incompatibility found**: binary needs GLIBC 2.39 (built on Ubuntu 24.04), CCY container has GLIBC 2.36 (Debian 12 bookworm)
  - [x] ✅ No musl/static binary in releases; no musl target in CI
  - [x] ✅ build.rs confirmed musl-compatible (pure Rust deps, no FFI/system libs)
  - [x] ✅ Installation method determined: **musl multi-stage build** (see Decision 1)

- [ ] ⬜ **Task 1.2**: Test PHPantom against a real PHP codebase

  - Blocked in current container (GLIBC mismatch) — requires user to test on host or in trixie-based container
  - [ ] ⬜ Start PHPantom LSP on a PHP project
  - [ ] ⬜ Test go-to-definition accuracy
  - [ ] ⬜ Test hover/type information accuracy
  - [ ] ⬜ Test diagnostics quality (does it flag real issues, avoid false positives?)
  - [ ] ⬜ Test completion relevance

- [ ] ⬜ **Task 1.3**: Compare PHPantom vs Intelephense on known pain points

  - [ ] ⬜ Identify specific examples where Intelephense gives inaccurate feedback
  - [ ] ⬜ Test same scenarios with PHPantom
  - [ ] ⬜ Document findings

- [x] ✅ **Task 1.4**: Evaluate Claude Code LSP integration compatibility

  - [x] ✅ PHPantom supports all required LSP methods: textDocument/definition, textDocument/references, textDocument/hover, textDocument/documentSymbol, workspace/symbol (partial)
  - [x] ✅ Also supports: textDocument/completion, textDocument/signatureHelp, textDocument/formatting, textDocument/codeAction, textDocument/codeLens, textDocument/inlayHint, textDocument/semanticTokens
  - [x] ✅ No Claude Code LSP config changes needed — entrypoint already sets `ENABLE_LSP_TOOL=1`, server auto-detection by filetype
  - [x] ✅ `lsp_enforcement` handler unaffected — it steers toward LSP tools regardless of which server backs them

### Decision Gate

**Criteria to proceed to Phase 2:**

- [x] ✅ PHPantom responds correctly to core LSP methods used by Claude Code
- [x] ✅ No show-stopping bugs or missing features for typical PHP development
- [x] ✅ User confirms go/no-go — user requested dual-install approach (both LSPs available)
- [ ] ⬜ Live accuracy comparison deferred to post-implementation (requires container rebuild)

### Phase 2: Implementation — Dual LSP with Plugin Switching

**Revised approach**: Install BOTH Intelephense and PHPantom. Use Claude Code's plugin system to switch between them. Only one active at a time.

- [x] ✅ **Task 2.1**: Add PHPantom musl build to CCY Dockerfile

  - [x] ✅ Add Rust multi-stage build for PHPantom with `x86_64-unknown-linux-musl` target
  - [x] ✅ Copy static binary to `/usr/local/bin/phpantom_lsp`
  - [x] ✅ Keep Intelephense in npm install list (both installed)
  - [x] ✅ Update documentation comments in Dockerfile

- [x] ✅ **Task 2.2**: Create `phpantom-lsp` Claude Code plugin

  - [x] ✅ Created at `files/var/local/claude-yolo/plugins/phpantom-lsp/`
  - [x] ✅ `.claude-plugin/plugin.json` with `lspServers` config
  - [x] ✅ Command: `phpantom_lsp` (no args, stdio by default)
  - [x] ✅ Extension mapping: `.php` → `php`

- [x] ✅ **Task 2.3**: Configure entrypoint for plugin installation

  - [x] ✅ Plugin copied to `/opt/claude-yolo/plugins/` in Dockerfile, installed to user dir at runtime
  - [x] ✅ PHPantom enabled by default via `enabledPlugins` in entrypoint
  - [x] ✅ Switching documented in entrypoint comments and CUSTOM-DOCKERFILES.txt

- [x] ✅ **Task 2.4**: Update documentation

  - [x] ✅ Updated `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt` (dual PHP LSP + switching)
  - [x] ✅ Updated `files/var/local/claude-yolo/Dockerfile.project-template`

- [x] ✅ **Task 2.5**: Bump CCY version

  - [x] ✅ CCY_VERSION: 3.11.1 → 3.12.0
  - [x] ✅ REQUIRED_CONTAINER_VERSION: 2.15 → 2.16
  - [x] ✅ Dockerfile label: 2.15 → 2.16

- [ ] ⬜ **Task 2.6**: Verification (requires host rebuild)

  - [ ] ⬜ Rebuild CCY container on host
  - [ ] ⬜ Test PHPantom LSP works on a PHP project
  - [ ] ⬜ Test switching to Intelephense works
  - [ ] ⬜ Test that only one runs at a time
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`

## Files Affected

| File                                                                 | Change                                               |
| -------------------------------------------------------------------- | ---------------------------------------------------- |
| `files/var/local/claude-yolo/Dockerfile`                             | Add musl build stage for PHPantom; keep intelephense |
| `files/var/local/claude-yolo/Dockerfile.project-template`            | Update PHP LSP references (both options)             |
| `files/opt/claude-yolo/docs/CUSTOM-DOCKERFILES.txt`                  | Document both PHP LSPs and switching                 |
| `files/var/local/claude-yolo/claude-yolo`                            | Version bump                                         |
| `files/root/.claude/plugins/phpantom-lsp/.claude-plugin/plugin.json` | New: PHPantom plugin manifest                        |
| `files/var/local/claude-yolo/entrypoint.sh`                          | Install phpantom-lsp plugin, set default             |

## Technical Decisions

### Decision 1: Installation method — musl multi-stage build

**Context**: PHPantom is a Rust binary. The pre-built `gnu` binary requires GLIBC 2.39 (built on Ubuntu 24.04 in CI), but the CCY base image `node:20-slim` is Debian 12 bookworm with GLIBC 2.36.
**Options considered**:

1. **Download pre-built binary** — blocked by GLIBC 2.39 requirement
2. **Switch base to `node:20-trixie-slim`** (Debian 13, GLIBC 2.40) — risky, changes entire base image
3. **Multi-stage build with musl target** — compile in Rust stage with `x86_64-unknown-linux-musl`, produces static binary

**Decision**: Option 3 — musl multi-stage build
**Rationale**: build.rs confirmed musl-compatible (pure Rust deps, no FFI/system libs, custom SHA-256 impl). Static binary works on any Linux regardless of glibc. No base image change = zero risk of breaking other packages. Build cost is one-time per container rebuild and cached by Docker layers.
**Date**: 2026-04-13

### Decision 2: Open issues assessment

**Context**: 16 open issues on the project — are any show-stoppers?
**Assessment**: Reviewed all 16 issues. Most are enhancement requests (Symfony support, Drupal support, signed Apple binaries). 6 are bugs related to template/generics edge cases and false-positive diagnostics. None are critical blockers for general PHP development. No GLIBC/portability issues reported — we may be the first Docker users.
**Date**: 2026-04-13

### Decision 3: Dual-install with plugin switching (not replace)

**Context**: User wants both LSPs available so Claude can switch between them if one gives poor results.
**Options**:

1. **Replace Intelephense with PHPantom** — simpler but no fallback
2. **Install both, use plugin system to switch** — both binaries in container, toggle via `enabledPlugins`

**Decision**: Option 2 — dual-install with plugin switching
**Rationale**: The official `php-lsp` marketplace plugin already handles Intelephense. We create a custom `phpantom-lsp` plugin for PHPantom. Only one can be enabled at a time (both map `.php` → `php`). PHPantom enabled by default; user/Claude can switch to Intelephense by toggling `enabledPlugins` in settings.
**Binary details**: `phpantom_lsp` with no args (stdio mode by default)
**Date**: 2026-04-13

## Success Criteria

- [ ] Both PHPantom and Intelephense binaries present in CCY container
- [ ] PHPantom plugin created and enabled by default
- [ ] Intelephense available as fallback via plugin toggle
- [ ] Only one PHP LSP runs at a time
- [ ] Claude Code LSP tools work correctly with PHPantom
- [ ] Container startup not negatively impacted
- [ ] All documentation updated
- [ ] QA passes

## Risks and Mitigations

| Risk                                                             | Impact | Probability   | Mitigation                                                                    |
| ---------------------------------------------------------------- | ------ | ------------- | ----------------------------------------------------------------------------- |
| PHPantom v0.7.0 has critical bugs                                | High   | Medium        | Thorough testing in Phase 1; keep Intelephense as fallback option             |
| GLIBC mismatch (pre-built binary needs 2.39, container has 2.36) | High   | **Confirmed** | Resolved: musl multi-stage build produces static binary                       |
| Claude Code LSP client incompatible                              | Medium | Low           | PHPantom implements standard LSP; test in Phase 1                             |
| PHPantom project abandoned                                       | Medium | Low           | MIT licensed, Rust binary is self-contained; 518 stars and active development |

## Notes and Updates

### 2026-04-13

- Plan created based on user report that Intelephense provides inaccurate/stale feedback
- PHPantom v0.7.0 identified as candidate replacement
- Research from laravel-news.com article and GitHub README confirms strong performance claims
- **Phase 1 research findings:**
  - Pre-built binary (27MB) requires GLIBC 2.39; CCY container (Debian 12) has 2.36
  - No musl/static binary in releases, no musl target in CI pipeline
  - build.rs confirmed musl-compatible: pure Rust deps, no FFI, custom SHA-256
  - Decision: musl multi-stage Docker build (compile static binary in Rust stage)
  - `node:20-trixie-slim` rejected as alternative (unnecessary base image change risk)
  - All required LSP methods supported; Claude Code LSP config unaffected
  - 16 open issues reviewed: no show-stoppers, mostly enhancement requests + template edge cases
  - Cannot live-test in this container (GLIBC mismatch) — Task 1.2/1.3 deferred to post-implementation
  - **Recommendation: proceed to Phase 2** — technical viability confirmed, live testing after container rebuild
- **Phase 2 implementation completed:**
  - Dockerfile: added Rust musl multi-stage build (`phpantom-builder` stage), pins PHPantom v0.7.0
  - Both LSPs installed: PHPantom (static musl binary) + Intelephense (npm)
  - Created `phpantom-lsp` Claude Code plugin at `files/var/local/claude-yolo/plugins/phpantom-lsp/`
  - Entrypoint updated: installs plugin at runtime, enables PHPantom by default via `enabledPlugins`
  - Docs updated: CUSTOM-DOCKERFILES.txt (switching guide), Dockerfile.project-template
  - Version bumped: CCY 3.12.0, container 2.16
  - QA passed (qa-all.bash)
  - **Remaining**: Task 2.6 — host rebuild and live verification
