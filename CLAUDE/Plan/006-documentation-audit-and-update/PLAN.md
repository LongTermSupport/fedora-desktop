# Plan 006: Documentation Audit and Update

**Status**: 🟡 In Progress
**Created**: 2026-02-12
**Owner**: Claude Sonnet 4.5
**Priority**: High
**Type**: Documentation

---

## Overview

The fedora-desktop repository has evolved significantly with new features and improvements, particularly the speech-to-text system which recently received major enhancements (7 commits on 2026-02-12). This plan will conduct a comprehensive audit of all repository features to identify documentation gaps, then prioritize and update documentation to ensure users can discover and effectively use all available functionality.

This plan differs from Plan 004 (which remains valid for future comprehensive feature documentation) by focusing on immediate documentation needs, particularly for recently enhanced features that users need to know about NOW.

**Key Focus Areas**:
1. **Speech-to-Text** - Recently enhanced with Claude prompt integration, backup system, and improved processing
2. **Existing Features** - Inventory and assess documentation coverage
3. **Documentation Structure** - Evaluate current docs/ organization
4. **Quick Wins** - Identify features that need simple README updates vs comprehensive guides

## Goals

- Complete inventory of all user-facing features in the repository
- Assess documentation coverage for each feature (none/minimal/adequate/comprehensive)
- Prioritize documentation gaps by user impact
- Update speech-to-text documentation to reflect recent enhancements
- Create or update high-priority feature documentation
- Establish a documentation maintenance workflow

## Non-Goals

- Creating comprehensive documentation for every minor utility (focus on high-impact features)
- Rewriting existing adequate documentation (only update/augment as needed)
- Internal developer documentation (focus on user-facing features)
- Translating documentation to other languages

## Context & Background

**Recent Changes**:
- Speech-to-Text received 7 commits (2026-02-12) adding:
  - Claude prompt integration (corporate vs natural modes)
  - Backup system for user-customized prompts
  - Raw transcription logging
  - Improved XML tag handling
  - Different icons for processing modes (🤖 vs 💬)

**Current Documentation State**:
- `README.md` - High-level overview, links to docs/
- `docs/` directory with 9 markdown files
- `CLAUDE.md` - Developer/AI instructions (not user-facing)
- Various playbooks have inline comments but no dedicated guides
- Plan 004 exists but hasn't been started (comprehensive feature documentation)

**User Need**: User expressed need for documentation after completing speech-to-text work, indicating users may not be aware of all features and how to use them.

## Tasks

### Phase 1: Feature Inventory and Assessment

- [ ] ⬜ **Conduct comprehensive feature discovery**
  - [ ] ⬜ Use Explore agent to scan entire codebase for user-facing features
  - [ ] ⬜ Inventory all playbooks (core + optional) and their purposes
  - [ ] ⬜ Identify custom scripts in files/home/.local/bin/
  - [ ] ⬜ Identify custom scripts in files/var/local/
  - [ ] ⬜ Identify GNOME extensions
  - [ ] ⬜ Identify bash helper functions and aliases
  - [ ] ⬜ Check for any distrobox/container configurations

- [ ] ⬜ **Categorize features by type**
  - [ ] ⬜ Core features (automatically installed)
  - [ ] ⬜ Optional features (manual playbook execution)
  - [ ] ⬜ Advanced features (power users)
  - [ ] ⬜ Experimental features (may change)
  - [ ] ⬜ Hardware-specific features (NVIDIA, DisplayLink, etc.)

- [ ] ⬜ **Assess current documentation coverage**
  - [ ] ⬜ For each feature, determine: None / Minimal / Adequate / Comprehensive
  - [ ] ⬜ Create documentation coverage matrix (feature → coverage level)
  - [ ] ⬜ Identify quick wins (features needing only brief mention)
  - [ ] ⬜ Identify documentation gaps (features needing full guides)

### Phase 2: Prioritization and Planning

- [ ] ⬜ **Score features for documentation priority**
  - [ ] ⬜ User impact (how many users benefit?)
  - [ ] ⬜ Complexity (how hard to understand without docs?)
  - [ ] ⬜ Discoverability (how likely users find it?)
  - [ ] ⬜ Recent changes (was it updated recently?)
  - [ ] ⬜ Uniqueness (does it solve a unique problem?)

- [ ] ⬜ **Create prioritized documentation work list**
  - [ ] ⬜ Tier 1: Critical gaps (high-impact features with no/minimal docs)
  - [ ] ⬜ Tier 2: Important gaps (useful features needing better docs)
  - [ ] ⬜ Tier 3: Nice-to-have (minor features or adequate existing docs)

- [ ] ⬜ **Define documentation approach per feature**
  - [ ] ⬜ Quick mention in existing docs (low priority)
  - [ ] ⬜ Brief section in playbooks.md (medium complexity)
  - [ ] ⬜ Dedicated feature guide (high complexity/importance)

### Phase 3: Speech-to-Text Documentation Update (Tier 1)

- [ ] ⬜ **Research current speech-to-text state**
  - [ ] ⬜ Read play-speech-to-text.yml completely
  - [ ] ⬜ Read wsi script and understand workflow
  - [ ] ⬜ Read wsi-stream and understand streaming mode
  - [ ] ⬜ Read wsi-claude-process and understand post-processing
  - [ ] ⬜ Read GNOME extension source
  - [ ] ⬜ Read recent commits to understand new features
  - [ ] ⬜ Test the feature to verify behaviour

- [ ] ⬜ **Create/update speech-to-text documentation**
  - [ ] ⬜ Decide location (docs/features/speech-to-text.md vs docs/speech-to-text.md)
  - [ ] ⬜ Write overview section (what it is, why use it)
  - [ ] ⬜ Document prerequisites (NVIDIA GPU, drivers, CUDA)
  - [ ] ⬜ Document installation (playbook command)
  - [ ] ⬜ Document configuration options (model size, language)
  - [ ] ⬜ Document usage:
    - [ ] ⬜ Keyboard shortcuts (Insert, Ctrl+Insert, Alt+Insert)
    - [ ] ⬜ Batch mode (default behaviour)
    - [ ] ⬜ Streaming mode (real-time transcription)
    - [ ] ⬜ Claude processing modes (corporate vs natural)
    - [ ] ⬜ Icon meanings (🎤 recording, 🤖 processing, 💬 natural mode)
  - [ ] ⬜ Document advanced features:
    - [ ] ⬜ Custom Claude prompts (~/.config/speech-to-text/)
    - [ ] ⬜ Prompt backup system
    - [ ] ⬜ Raw transcription logs
  - [ ] ⬜ Include architecture diagram (audio → whisper → claude → paste)
  - [ ] ⬜ Write troubleshooting section:
    - [ ] ⬜ CUDA/GPU issues
    - [ ] ⬜ ydotool permission errors
    - [ ] ⬜ Keybinding conflicts
    - [ ] ⬜ Extension not loading
    - [ ] ⬜ Slow transcription (model size)
    - [ ] ⬜ Incorrect transcription (language setting)

- [ ] ⬜ **Update related documentation**
  - [ ] ⬜ Add speech-to-text to README.md "What You Get" section
  - [ ] ⬜ Add to docs/playbooks.md optional playbooks section
  - [ ] ⬜ Update any references in existing docs

### Phase 4: High-Priority Feature Documentation (Tier 1)

These tasks will be populated after Phase 1 feature inventory is complete. Preliminary candidates:

- [ ] ⬜ **CCY (Claude Code YOLO) - If not adequately documented**
  - [ ] ⬜ Create docs/features/ccy.md or update existing
  - [ ] ⬜ Document installation, usage, custom Dockerfile support
  - [ ] ⬜ Document token management features

- [ ] ⬜ **GitHub Multi-Account - If not adequately documented**
  - [ ] ⬜ Update docs with multi-account workflow
  - [ ] ⬜ Document bash helpers (gh-switch, clone-*, etc.)
  - [ ] ⬜ Document setup and account management

- [ ] ⬜ **Nord VPN - If not adequately documented**
  - [ ] ⬜ Check if existing nordvpn-installation.md is adequate
  - [ ] ⬜ Update if needed with latest features

- [ ] ⬜ **[Additional features from Phase 1 inventory]**

### Phase 5: Medium-Priority Documentation (Tier 2)

Tasks to be populated after Phase 1 assessment.

- [ ] ⬜ **Update docs/playbooks.md**
  - [ ] ⬜ Add any missing optional playbooks
  - [ ] ⬜ Improve descriptions for existing entries
  - [ ] ⬜ Add "what you get" summaries for each

- [ ] ⬜ **[Additional Tier 2 items from inventory]**

### Phase 6: Documentation Structure Improvements

- [ ] ⬜ **Evaluate current docs/ organization**
  - [ ] ⬜ Assess if current structure serves users well
  - [ ] ⬜ Consider docs/features/ subdirectory for feature-specific guides
  - [ ] ⬜ Evaluate need for docs/README.md update

- [ ] ⬜ **Create documentation index if needed**
  - [ ] ⬜ Consider docs/FEATURES.md as central feature index
  - [ ] ⬜ Or enhance existing docs/README.md

- [ ] ⬜ **Establish documentation standards**
  - [ ] ⬜ Create CONTRIBUTING-DOCS.md template
  - [ ] ⬜ Define when features need documentation
  - [ ] ⬜ Create checklist for documentation updates

### Phase 7: Quality Assurance and Completion

- [ ] ⬜ **Review all documentation updates**
  - [ ] ⬜ Verify all commands are correct and tested
  - [ ] ⬜ Check all internal links work
  - [ ] ⬜ Verify formatting consistency
  - [ ] ⬜ Ensure British English throughout

- [ ] ⬜ **User review and feedback**
  - [ ] ⬜ Present documentation updates to user
  - [ ] ⬜ Make revisions based on feedback
  - [ ] ⬜ Verify documentation clarity

- [ ] ⬜ **Commit and publish**
  - [ ] ⬜ Commit all documentation updates
  - [ ] ⬜ Update CHANGELOG if applicable
  - [ ] ⬜ Push to repository

## Dependencies

- None (self-contained plan)
- Plan 004 remains valid for comprehensive feature documentation in the future

## Technical Decisions

### Decision 1: Audit-First Approach

**Context**: User requested documentation audit and update, specifically mentioning speech-to-text.

**Options Considered**:
1. Jump directly to documenting known features
2. Conduct comprehensive feature inventory first
3. Focus only on speech-to-text

**Decision**: Conduct comprehensive feature inventory first (Option 2)

**Rationale**:
- User explicitly mentioned "documentation drive" and wanted to see what features exist
- Can't assess documentation gaps without knowing all features
- Prevents wasted effort documenting wrong priorities
- Provides complete picture for prioritization
- Speech-to-text will be documented regardless (clearly Tier 1)

**Date**: 2026-02-12

### Decision 2: Separate Plan from Plan 004

**Context**: Plan 004 already exists for comprehensive feature documentation.

**Options Considered**:
1. Update Plan 004 and use it
2. Create new plan focused on immediate needs
3. Mark Plan 004 complete and replace with this

**Decision**: Create new plan focused on immediate needs (Option 2)

**Rationale**:
- Plan 004 is comprehensive but unstarted - different scope
- This plan is more focused on audit → prioritize → update workflow
- This plan specifically addresses recent speech-to-text changes
- Plan 004 can remain as future work for comprehensive guides
- Clear separation of concerns (audit/update vs create comprehensive docs)

**Date**: 2026-02-12

### Decision 3: Documentation Location Strategy

**Context**: Need to decide where to place feature-specific documentation.

**Options Considered**:
1. Everything in root-level README.md
2. Everything in docs/playbooks.md
3. Mix of docs/playbooks.md for simple, docs/features/ for complex
4. Separate docs/features/ for all features

**Decision**: Mix approach - docs/playbooks.md for simple, dedicated guides for complex (Option 3)

**Rationale**:
- Maintains existing docs/playbooks.md structure (users expect it)
- Allows complex features (CCY, speech-to-text) to have dedicated guides
- Reduces clutter in playbooks.md
- Flexible - can decide per-feature based on complexity
- Can always refactor later if needed

**Date**: 2026-02-12

## Success Criteria

- [ ] Complete inventory of all user-facing features exists
- [ ] Documentation coverage assessment complete for all features
- [ ] Speech-to-text documentation updated to reflect recent enhancements
- [ ] All Tier 1 documentation gaps addressed
- [ ] Documentation structure serves users effectively
- [ ] User review confirms documentation improvements
- [ ] All documentation committed and pushed

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Feature inventory takes too long | Medium | Low | Use Explore agent for efficiency, timebox if needed |
| Documentation becomes outdated quickly | Medium | Medium | Create maintenance guide, document update triggers |
| Scope creep (trying to document everything) | Medium | Medium | Stick to tiered approach, Tier 3 can wait |
| User expectations differ from plan priorities | Medium | Low | Get user feedback on priorities after Phase 2 |

## Timeline

This is documentation work with no time estimates per project standards. Work proceeds in phases:

- **Phase 1**: Feature inventory and assessment (foundation)
- **Phase 2**: Prioritization (enables focused work)
- **Phase 3**: Speech-to-text docs (highest priority, recent changes)
- **Phase 4-5**: Additional features by tier
- **Phase 6**: Structure improvements
- **Phase 7**: QA and completion

Target: Complete through Phase 7, with user approval before marking complete.

## Notes & Updates

### 2026-02-12 - Plan Created

Plan created after user successfully completed speech-to-text improvements and requested documentation audit. Speech-to-text received 7 commits today adding:
- Detailed Claude prompts with XML tags
- Backup system for user-customized prompts
- Raw transcription logging before Claude processing
- Force prompt updates
- Different icons for processing modes (🤖 vs 💬)

User specifically mentioned: "Let's outline the features that this repo provides and then let's check how well documented they are. Speech a text certainly isn't documented up to date because we've just added major functionality."

This plan will ensure users can discover and use all repository features, with immediate focus on documenting the enhanced speech-to-text system.
