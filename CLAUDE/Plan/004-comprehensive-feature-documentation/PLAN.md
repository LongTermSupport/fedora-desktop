# Plan 004: Comprehensive Feature Documentation

**Status**: ⬜ Not Started
**Created**: 2026-02-06
**Owner**: Claude Sonnet 4.5
**Priority**: High
**Type**: Documentation

---

## ⚠️ IMPORTANT: Pre-Work Required

**Before commencing Phase 1, a thorough feature discovery must be completed:**

Use the Explore subagent to conduct a comprehensive scan of the entire codebase to identify ALL features worthy of documentation. The initial list of 7 features is based on a quick scan and may be incomplete.

**Action Required**:
1. Launch Explore subagent with "very thorough" mode
2. Search for: custom scripts, playbooks, extensions, tools, and utilities
3. Identify any features not in the current list
4. Update this plan's feature list and tasks accordingly
5. Only then proceed with Phase 1

**Why this is critical**: Documentation work is wasted if we miss features. A comprehensive scan ensures we document everything worthwhile in one go, rather than discovering features later and having to create follow-up plans.

---

## Overview

The fedora-desktop project has evolved several powerful, production-ready features that deserve comprehensive documentation. Currently, these features exist with minimal documentation, making them difficult for users to discover and use effectively. This plan will create professional-grade documentation for each feature, ensuring users can easily understand, install, configure, and troubleshoot these tools.

The features identified for documentation are:
1. **CCY (Claude Code YOLO)** - Containerised Claude Code with automatic token management
2. **CCB (Claude Code Browser)** - CCY with Playwright MCP for browser automation
3. **Nord** - NordVPN OpenVPN connection manager with interactive chooser
4. **WSI-Stream** - Real-time speech-to-text with GPU-accelerated Whisper
5. **GNOME Speech-to-Text Extension** - System-wide voice typing with Insert key
6. **GitHub CLI Multi-Account** - Multiple GitHub account management with bash helpers
7. **Distrobox Integration** - Container-based development environments

## Goals

- Create comprehensive, user-friendly documentation for 7 major features
- Establish documentation standards and templates for future features
- Ensure all documentation follows consistent structure and style
- Include installation, configuration, usage, and troubleshooting sections
- Add architecture diagrams and workflow illustrations where beneficial
- Create a central features index for easy discovery

## Non-Goals

- Writing documentation for minor utilities or one-off scripts
- API reference documentation (focus on user-facing guides)
- Video tutorials or screencasts (text-based documentation only)
- Translating documentation to other languages

## Context & Background

This project started as a simple Ansible automation for Fedora desktop setup, but has grown to include sophisticated custom tools that solve real problems:

- **CCY/CCB**: Solve OAuth token conflicts and provide isolated Claude Code environments
- **Nord**: Provides user-friendly VPN management without proprietary NordVPN app
- **Speech-to-Text**: Enables hands-free text input with GPU acceleration
- **GitHub Multi-Account**: Solves the common problem of managing work/personal accounts

These features are production-ready but under-documented. Users currently must read playbooks and source code to understand how to use them.

## Tasks

### Phase 1: Documentation Structure & Standards

- [ ] ⬜ **Create documentation directory structure**
  - [ ] ⬜ Create `docs/features/` directory
  - [ ] ⬜ Create template for feature documentation
  - [ ] ⬜ Define documentation standards (sections, style, formatting)

- [ ] ⬜ **Establish documentation conventions**
  - [ ] ⬜ Define standard sections (Overview, Installation, Configuration, Usage, Troubleshooting, Architecture)
  - [ ] ⬜ Create markdown style guide
  - [ ] ⬜ Establish screenshot/diagram naming conventions

### Phase 2: CCY Documentation

- [ ] ⬜ **Research CCY architecture and features**
  - [ ] ⬜ Read and analyse `files/var/local/claude-yolo/claude-yolo`
  - [ ] ⬜ Understand token management system
  - [ ] ⬜ Document custom Dockerfile support
  - [ ] ⬜ Document network management features
  - [ ] ⬜ Map out SSH key handling

- [ ] ⬜ **Write CCY documentation**
  - [ ] ⬜ Create `docs/features/ccy.md`
  - [ ] ⬜ Write overview and key features section
  - [ ] ⬜ Document installation via playbook
  - [ ] ⬜ Document usage patterns (basic, custom Dockerfile, network modes)
  - [ ] ⬜ Create architecture diagram showing container/token flow
  - [ ] ⬜ Write troubleshooting section
  - [ ] ⬜ Document all command-line options
  - [ ] ⬜ Include examples for common workflows

### Phase 3: CCB Documentation

- [ ] ⬜ **Research CCB-specific features**
  - [ ] ⬜ Read and analyse `files/var/local/claude-yolo/claude-browser`
  - [ ] ⬜ Understand differences from CCY
  - [ ] ⬜ Document Playwright MCP integration
  - [ ] ⬜ Document X11/GUI support

- [ ] ⬜ **Write CCB documentation**
  - [ ] ⬜ Create `docs/features/ccb.md`
  - [ ] ⬜ Write overview highlighting browser automation capabilities
  - [ ] ⬜ Document installation and prerequisites
  - [ ] ⬜ Document browser automation workflows
  - [ ] ⬜ Include practical examples (web scraping, testing, form filling)
  - [ ] ⬜ Write troubleshooting section for display/browser issues

### Phase 4: Nord VPN Manager Documentation

- [ ] ⬜ **Document Nord architecture**
  - [ ] ⬜ Read `files/home/.local/bin/nord` completely
  - [ ] ⬜ Map out interactive chooser workflow
  - [ ] ⬜ Document NetworkManager integration
  - [ ] ⬜ Understand credentials management

- [ ] ⬜ **Write Nord documentation**
  - [ ] ⬜ Create `docs/features/nord.md`
  - [ ] ⬜ Write overview and problem statement
  - [ ] ⬜ Document installation via playbook
  - [ ] ⬜ Document .ovpn file acquisition from NordVPN
  - [ ] ⬜ Document interactive mode usage
  - [ ] ⬜ Document CLI commands (list, connect, disconnect, switch, status)
  - [ ] ⬜ Include workflow diagram (download → import → connect)
  - [ ] ⬜ Write troubleshooting section (NetworkManager issues, auth failures)

### Phase 5: Speech-to-Text Documentation

- [ ] ⬜ **Research speech-to-text system**
  - [ ] ⬜ Read `files/home/.local/bin/wsi-stream` completely
  - [ ] ⬜ Read GNOME extension source
  - [ ] ⬜ Understand streaming vs batch modes
  - [ ] ⬜ Document faster-whisper integration
  - [ ] ⬜ Document GPU acceleration requirements
  - [ ] ⬜ Understand Claude Code post-processing option

- [ ] ⬜ **Write speech-to-text documentation**
  - [ ] ⬜ Create `docs/features/speech-to-text.md`
  - [ ] ⬜ Write overview of the system architecture
  - [ ] ⬜ Document prerequisites (NVIDIA GPU, drivers, CUDA)
  - [ ] ⬜ Document installation via playbook
  - [ ] ⬜ Explain Whisper model selection (tiny → large-v3)
  - [ ] ⬜ Document keyboard shortcut (Insert key)
  - [ ] ⬜ Document streaming workflow (record → transcribe → paste)
  - [ ] ⬜ Document Claude Code post-processing feature
  - [ ] ⬜ Include performance comparison table (model sizes)
  - [ ] ⬜ Write troubleshooting section (CUDA issues, ydotool, keybindings)

### Phase 6: GitHub Multi-Account Documentation

- [ ] ⬜ **Research GitHub multi-account system**
  - [ ] ⬜ Read playbook `play-github-cli-multi.yml`
  - [ ] ⬜ Understand SSH key generation per account
  - [ ] ⬜ Document bash helper functions
  - [ ] ⬜ Understand account switching mechanism

- [ ] ⬜ **Write GitHub multi-account documentation**
  - [ ] ⬜ Create `docs/features/github-multi-account.md`
  - [ ] ⬜ Write overview and use cases
  - [ ] ⬜ Document initial setup process
  - [ ] ⬜ Document adding additional accounts
  - [ ] ⬜ Document all bash helper functions (gh-switch, clone-*, remote-*, etc.)
  - [ ] ⬜ Include workflow examples (work/personal account separation)
  - [ ] ⬜ Write troubleshooting section (SSH key issues, authentication)

### Phase 7: Distrobox Integration Documentation

- [ ] ⬜ **Research distrobox setup**
  - [ ] ⬜ Read playbook `play-install-distrobox.yml`
  - [ ] ⬜ Document pre-configured containers
  - [ ] ⬜ Understand container export/import workflow

- [ ] ⬜ **Write distrobox documentation**
  - [ ] ⬜ Create `docs/features/distrobox.md`
  - [ ] ⬜ Write overview and benefits
  - [ ] ⬜ Document installation
  - [ ] ⬜ Document available pre-configured containers
  - [ ] ⬜ Document usage patterns
  - [ ] ⬜ Include examples for common development scenarios

### Phase 8: Features Index & Integration

- [ ] ⬜ **Create central features documentation**
  - [ ] ⬜ Create `docs/FEATURES.md` as central index
  - [ ] ⬜ Write overview of all documented features
  - [ ] ⬜ Create quick reference table (feature, description, installation command)
  - [ ] ⬜ Link to individual feature docs

- [ ] ⬜ **Update main documentation**
  - [ ] ⬜ Update `README.md` to reference features documentation
  - [ ] ⬜ Update `CLAUDE.md` to include features section
  - [ ] ⬜ Add features section to main playbook documentation

- [ ] ⬜ **Create documentation maintenance guide**
  - [ ] ⬜ Document process for adding new feature docs
  - [ ] ⬜ Create documentation review checklist
  - [ ] ⬜ Document when features documentation should be updated

### Phase 9: Quality Assurance

- [ ] ⬜ **Review all documentation**
  - [ ] ⬜ Check for consistency across all feature docs
  - [ ] ⬜ Verify all installation commands are correct
  - [ ] ⬜ Test all documented workflows
  - [ ] ⬜ Check all internal links work
  - [ ] ⬜ Verify formatting consistency

- [ ] ⬜ **Get user feedback**
  - [ ] ⬜ Share documentation with user for review
  - [ ] ⬜ Make revisions based on feedback
  - [ ] ⬜ Verify documentation clarity and completeness

## Dependencies

- None (this plan is self-contained)

## Technical Decisions

### Decision 1: Documentation Location

**Context**: Need to decide where feature documentation should live in the repository.

**Options Considered**:
1. Add to existing `docs/` directory as separate feature files
2. Create new `docs/features/` subdirectory for feature documentation
3. Keep in root as `FEATURES.md` with inline documentation

**Decision**: Use `docs/features/` subdirectory (Option 2)

**Rationale**:
- Keeps features documentation separate from architecture/development docs
- Allows for future expansion (one file per feature)
- Maintains clean organisation as more features are added
- Follows existing docs/ pattern but with better organisation

**Date**: 2026-02-06

### Decision 2: Documentation Format

**Context**: Need to establish standard structure for all feature documentation.

**Options Considered**:
1. Free-form documentation (no template)
2. Strict template with required sections
3. Flexible template with recommended sections

**Decision**: Flexible template with recommended sections (Option 3)

**Rationale**:
- Ensures consistency while allowing feature-specific content
- Required sections: Overview, Installation, Usage, Troubleshooting
- Optional sections: Architecture, Advanced Configuration, Performance Tuning
- Features can add sections as needed without breaking template

**Date**: 2026-02-06

### Decision 3: Diagram/Screenshot Approach

**Context**: Some features would benefit from visual aids (architecture diagrams, workflow illustrations).

**Options Considered**:
1. No diagrams (text only)
2. ASCII art diagrams in markdown
3. External diagram files (PNG/SVG) linked from docs

**Decision**: ASCII art diagrams in markdown (Option 2)

**Rationale**:
- Keeps documentation self-contained in markdown files
- ASCII diagrams are version-control friendly
- Easier to maintain (no external tools required)
- Sufficient for architectural/workflow illustrations
- Can be upgraded to proper diagrams later if needed

**Date**: 2026-02-06

## Success Criteria

- [ ] All 7 features have comprehensive documentation
- [ ] Documentation follows consistent structure and style
- [ ] All installation commands are tested and verified
- [ ] Troubleshooting sections address common issues
- [ ] Central features index exists and is easy to navigate
- [ ] User review confirms documentation is clear and helpful
- [ ] All documentation committed to repository

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Documentation becomes outdated as features evolve | Medium | High | Create maintenance guide, document update triggers |
| Documentation is too technical for average users | Medium | Medium | Include examples, use clear language, get user feedback |
| Features change during documentation process | Low | Medium | Focus on stable features first, note version numbers |
| Documentation too verbose/overwhelming | Low | Medium | Use clear sections, include TL;DR summaries, link to details |

## Timeline

This is documentation work with no time estimates per project standards. Work proceeds in phases:

- **Phase 1**: Foundation (structure, standards, templates)
- **Phase 2-7**: Feature documentation (parallel where possible)
- **Phase 8**: Integration and index creation
- **Phase 9**: Quality assurance and review

Target: Complete all phases, with user review approval before marking complete.

## Notes & Updates

### 2026-02-06 - Plan Created

Initial plan created after user identified need for feature documentation. Key features identified:
- CCY (2139 lines) - Complex container wrapper with token management
- CCB (1494 lines) - Browser automation variant
- Nord (635 lines) - VPN connection manager
- WSI-Stream (856 lines) - Speech-to-text streaming
- GNOME Extension - System-wide voice typing
- GitHub Multi-Account - Multiple account management
- Distrobox - Container integration

These are production-ready features that solve real user problems and deserve proper documentation.
