# Claude Code Hooks

## enforce-official-plan-command.py

**Purpose**: Blocks ad-hoc plan number lookup commands and enforces the official command from CLAUDE/Plan/CLAUDE.md.

**Trigger**: PreToolUse (Bash tool only)

**Philosophy**: Plan number discovery must use a single, documented command to ensure consistency. Ad-hoc approaches are fragile and undocumented.

**BLOCKED patterns**:
- `ls -d */Plan/[0-9]*` - Directory listing approach
- `ls CLAUDE/Plan/0*` - Glob pattern approach
- `ls CLAUDE/Plan | grep` - Piped directory listing
- `find CLAUDE/Plan` (without correct flags) - find without -maxdepth 2
- `cd CLAUDE/Plan && ls` - cd then list
- Any other non-canonical plan discovery command

**ALLOWED**:
- `find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | sed 's|.*/\([0-9]\{3\}\).*|\1|' | sort -n | tail -1`
- (The exact command from CLAUDE/Plan/CLAUDE.md)

**What the official command does**:
- Finds all plan directories with 3-digit prefixes (NNN-*)
- Extracts just the plan number (NNN)
- Sorts numerically (ensuring 9 < 10 < 100)
- Returns the highest number (next plan = highest + 1)

**Benefits**:
- Ensures consistent plan discovery workflow
- Directs users to official documentation
- Prevents fragile ad-hoc approaches
- Handles all edge cases (numeric sorting, maxdepth, etc.)

---

## validate-websearch-year.py

**Purpose**: Blocks WebSearch queries containing outdated year references (like "2024" when it's 2025).

**How it works**:
1. Intercepts WebSearch tool calls before execution
2. Scans query for 4-digit year patterns (1900-2099)
3. Compares found years against current year
4. Blocks if any year is not the current year
5. Provides clear error message with current year

**Trigger**: PreToolUse (WebSearch tool only)

**Philosophy**: Search results stay relevant when queries use current years. Outdated year references return stale information.

**BLOCKED patterns**:
- `"latest React features 2024"` when it's 2025
- `"PHP 8.3 2024 release"` when it's 2025
- Any query with years that don't match current year

**ALLOWED patterns**:
- `"latest React features 2025"` (current year)
- `"latest React features"` (no year, search engine handles recency)
- Historical queries are still blocked (needs refinement for legitimate historical research)

**Example blocked query**:
```
Query: "TypeScript best practices 2024"
Status: BLOCKED
Message: It's not 2024, it's 2025!
Suggestion: Remove year or use 2025
```

**Benefits**:
- Prevents searching with outdated year references
- Ensures Claude searches for current information
- Catches copy-paste queries from old conversations
- Automatic awareness of current year

**Note**: This is a STRICT BLOCKING hook - queries with wrong years cannot proceed

## enforce-llm-npm-commands.py

**Purpose**: Enforces use of LLM-optimized npm commands. Blocks ALL npm commands except llm: prefixed ones and a tiny whitelist.

**How it works**:
1. Intercepts Bash tool calls containing `npm run` commands
2. BLOCKS ALL commands by default (strict deny-by-default policy)
3. Only allows llm: prefixed commands or tiny whitelist (currently just `clean`)
4. Blocks piping npm commands to grep/awk/sed (llm: commands already create cached files)
5. Provides clear error message with suggested llm: command or instruction to ask user

**Trigger**: PreToolUse (Bash tool only)

**Philosophy**: Claude should ALWAYS use llm: prefixed commands which provide:
- Minimal stdout (summary only)
- Verbose JSON logging to files (./var/qa/)
- Machine-readable output for parsing
- Caching system for performance

**BLOCKED (nearly everything)**:
- `npm run build` → Must use `npm run llm:build`
- `npm run lint` → Must use `npm run llm:lint`
- `npm run type-check` → Must use `npm run llm:type-check`
- `npm run format:check` → Must use `npm run llm:format:check`
- `npm run spell-check` → Must use `npm run llm:spell-check`
- `npm run test` → Must use `npm run llm:test`
- `npm run test:smoke` → Must use `npm run llm:browser-test` or `npm run llm:test:smoke`
- `npm run test:smoke:quick` → Must use `npm run llm:browser-test:quick` or `npm run llm:test:smoke`
- `npm run test:smoke-pages` → Must use `npm run llm:test:smoke`
- `npm run qa` → Must use `npm run llm:qa`
- `npm run screenshots` → Must use `npm run llm:screenshots`
- `npm run <any> | grep` → Piping is blocked (llm: commands create files)
- `npx eslint` → Must use `npm run llm:lint`
- `npx tsc` → Must use `npm run llm:type-check`
- `npx prettier` → Must use `npm run llm:format:check`
- `npx cspell` → Must use `npm run llm:spell-check`
- `npx playwright` → Must use `npm run llm:test`

**ALLOWED (tiny whitelist)**:
- All `npm run llm:*` commands (the whole point!)
- `npm run clean` (cleanup operation, no llm: version needed)

**Why llm: commands**:
- Minimal stdout (summary only, not verbose output)
- Verbose JSON logging to ./var/qa/ files
- Machine-readable output (no parsing needed)
- Caching system for performance
- Read cache files directly with Read tool
- No need for grep/awk/sed post-processing

**Benefits**:
- Forces use of optimized LLM workflows
- Prevents brittle text-parsing with grep/awk/sed
- Ensures consistent machine-readable output
- Leverages caching for faster repeated runs
- Dramatically reduces token usage (summary only, not full output)

**Example blocked command**:
```bash
npm run build
Status: BLOCKED
Suggestion: npm run llm:build
```

**Example allowed commands**:
```bash
npm run llm:build
Status: ALLOWED
Creates: ./var/qa/build/build.{timestamp}.json (verbose)
Stdout: Summary only (minimal)

npm run llm:lint
Status: ALLOWED
Creates: ./var/qa/eslint-{timestamp}.json (verbose)
Stdout: Summary only (minimal)

npm run llm:screenshots
Status: ALLOWED
Creates: ./var/qa/screenshots/screenshots.{timestamp}.json (verbose)
Stdout: Summary only (minimal)
```

**Note**: This is a STRICT BLOCKING hook - nearly all non-llm: commands are blocked by design

## prevent-adhoc-scripts (AdHocScriptHandler)

**Purpose**: Prevents ad-hoc script execution (via `npx tsx scripts/...`) when npm command wrappers exist.

**How it works**:
1. Intercepts Bash tool calls that invoke scripts via `npx tsx`, `tsx`, or `node`
2. Checks if the script has a corresponding npm command wrapper
3. BLOCKS ad-hoc execution if npm command exists
4. ALLOWS ad-hoc execution for scripts without npm wrappers
5. Provides clear error message with the correct npm command to use

**Trigger**: PreToolUse (Bash tool only)

**Philosophy**: All scripts with npm command wrappers must be run via npm to ensure:
- Standardised execution environment
- Proper error handling and logging
- Consistent caching behaviour
- Integration with build pipeline

**BLOCKED patterns** (scripts with npm commands):
- `npx tsx scripts/llm-lint.ts` → Must use `npm run llm:lint`
- `tsx scripts/qa.ts` → Must use `npm run qa`
- `node scripts/generate-screenshots.ts` → Must use `npm run screenshots`
- `npx tsx scripts/llm-type-check.ts` → Must use `npm run llm:type-check`
- `tsx ./scripts/verify-build.ts` → Must use `npm run build:verify`
- `npx tsx /workspace/scripts/find-unused-exports.ts` → Must use `npm run llm:unused-exports`

**Protected scripts** (27 scripts with npm wrappers):
- All `llm-*.ts` scripts (llm-lint, llm-type-check, llm-format, etc.)
- Build pipeline scripts (extract-seo-metadata, prerender-pages, verify-build, etc.)
- QA scripts (qa.ts, generate-screenshots.ts, lighthouse-audit.ts, etc.)
- Utility scripts with wrappers (find-unused-exports, sort-lazy-imports, etc.)

**ALLOWED ad-hoc patterns** (scripts WITHOUT npm commands):
- `npx tsx scripts/debug-page.ts` ✅ No npm wrapper
- `tsx scripts/eslint-wrapper.ts` ✅ No npm wrapper
- `npx tsx scripts/generate-page-folders.ts` ✅ No npm wrapper
- `tsx scripts/test-playwright-403.ts` ✅ No npm wrapper

**Benefits**:
- Enforces use of npm commands for consistency
- Prevents confusion about which execution method to use
- Ensures proper logging and caching
- Makes scripts discoverable via `npm run` list
- Clear separation between production scripts (npm) and ad-hoc utilities

**Example blocked command**:
```bash
npx tsx scripts/llm-lint.ts
Status: BLOCKED
Suggestion: npm run llm:lint
Reason: This script has an npm wrapper - use it for standardised execution
```

**Example allowed commands**:
```bash
npm run llm:lint
Status: ALLOWED (correct way to run script)

npx tsx scripts/debug-page.ts
Status: ALLOWED (no npm wrapper exists)
```

**Note**: This is a STRICT BLOCKING hook - scripts with npm wrappers cannot be run ad-hoc

## auto-continue.py

**Purpose**: Enables automatic continuation of multi-step tasks without confirmation prompts.

**How it works**:
1. Detects when Claude/agents ask confirmation questions like "Would you like me to continue?"
2. When user responds with minimal answers ("yes", "y", "continue", "proceed", "go ahead"), enhances the prompt with STRONG auto-continue instructions
3. Adds explicit direction to proceed through ALL remaining batches/phases/steps without further approval
4. Works in conjunction with customInstructions in settings.local.json that tell Claude to avoid confirmation prompts

**Trigger**: UserPromptSubmit (fires when user submits a prompt)

**Configuration**: Registered in `.claude/settings.local.json`

**Common patterns detected**:
- "would you like me to continue/proceed/start"
- "should I continue/proceed"
- "shall I continue"
- "do you want me to..."
- "ready to implement/execute/run"
- "continue with batch/phase/step X"
- "shall I proceed with batch/phase/step X"
- Questions ending with confirmation words

**Enhancement applied**:
When minimal response detected, the hook adds:
```
[AUTO-CONTINUE MODE: YES, continue with ALL remaining work.
Do NOT ask for confirmation again.
Proceed through all batches/phases/steps automatically.
Only stop if you encounter an error or need critical information.
Execute the full plan without further approval requests.]
```

**Minimal responses recognized**:
- yes, y, yep, yeah
- ok, okay
- continue, proceed
- go ahead, go, sure
- do it, yes please

**Benefits**:
- Eliminates need to repeatedly approve continuation
- Ensures agents complete full multi-batch workflows
- Particularly useful for orchestration agents (page-orchestration, qa, eslint, sitemap)
- User can still say "stop" or "pause" to halt execution

**Usage**: Automatic - just respond with "yes" or "continue" when asked

**Note**: This hook ENHANCES user prompts, it doesn't block anything. The customInstructions field provides the first line of defense against confirmation prompts.

## enforce-british-english.py

**Purpose**: Warns when American English spellings are detected in content files.

**How it works**:
1. Checks `.md`, `.ejs`, `.html`, and `.txt` files
2. Scans for American spellings (color, organize, center, etc.)
3. Suggests British alternatives (colour, organise, centre, etc.)
4. Non-blocking - shows warnings but doesn't prevent commits

**Trigger**: File write operations

**Directories checked**: `private_html`, `docs`, `CLAUDE`

## enforce-no-eslint-disable.py

**Purpose**: Blocks all ESLint suppression comments in TypeScript/JavaScript files.

**How it works**:
1. Checks `.ts`, `.tsx`, `.js`, `.jsx` files
2. Scans for suppression patterns: `eslint-disable`, `@ts-ignore`, `@ts-expect-error`
3. Blocks the write operation if any suppression is detected
4. Forces developers to fix violations instead of hiding them

**Trigger**: File write operations (PreToolUse)

**Philosophy**: "Fix the problem, don't hide it"

**Blocked patterns**:
- `eslint-disable`
- `eslint-disable-line`
- `eslint-disable-next-line`
- `@ts-ignore`
- `@ts-expect-error`

**Note**: This is a BLOCKING hook - it will prevent file writes containing suppressions

## validate-eslint-on-write.py

**Purpose**: Runs ESLint validation on all TypeScript/TSX file writes to ensure code quality.

**How it works**:
1. Intercepts Write and Edit operations on `.ts` and `.tsx` files
2. Runs ESLint validation on the file before allowing the write
3. For Write operations: creates temp file, validates, then allows write if passed
4. For Edit operations: validates the file after changes applied
5. Blocks the operation if ESLint reports any errors or warnings

**Trigger**: PreToolUse (Write and Edit tools)

**What it catches**:
- Ad-hoc HTML/JSX without proper components
- Invalid Tailwind classes (text-create, text-support, etc.)
- Card-like divs that should use ServiceGrid
- Import order violations
- Unused variables and imports
- All ESLint rule violations

**Benefits**:
- Prevents bad code from being written in the first place
- Catches issues immediately, not at commit time
- Forces agents to write clean, compliant code
- No broken builds from ESLint failures

**Note**: This is a BLOCKING hook - writes will fail if ESLint validation fails

## validate-claude-readme-content.py

**Purpose**: Validates that CLAUDE.md and README.md files contain only useful instructions, NOT logs, research, or LLM output.

**How it works**:
1. Intercepts Write and Edit operations on CLAUDE.md and README.md files
2. Scans for blocked content patterns (logs, research, summaries, status indicators)
3. Skips detection of patterns inside markdown code blocks (between ``` markers)
4. Blocks the operation if non-instructional content is detected
5. Provides clear list of detected issues with line numbers

**Trigger**: PostToolUse (Write and Edit tools on CLAUDE.md and README.md files)

**Philosophy**: These files should contain actionable instructions for humans or LLMs, not documentation of what was already done.

**ALLOWED content**:
- Clear, actionable instructions for LLMs/humans
- Context about directory/module purpose
- Guidelines, conventions, patterns to follow
- "How to use this directory" explanations
- Configuration notes, gotchas, important warnings
- Examples showing proper usage

**BLOCKED patterns** (all case variations):
- Implementation logs ("Created X", "Modified Y", "Added Z", "Step 1", "Phase 2")
- Research findings, analysis output
- Test results, QA output
- Timestamps and dates (YYYY-MM-DD, "created: 2025-12-01")
- LLM-style summaries ("## Summary", "## Key Points", "here's what...")
- Status updates with emojis (✅ Complete, 🟢 Working, 🔴 Failed)
- File listings ("created files:", "modified: src/file.ts")
- Change summaries ("changes made:", "what was changed:")
- Completion indicators ("all done", "that should fix it")

**Exceptions** (not flagged):
- Code examples containing blocked patterns
- Markdown code blocks with ``` delimiters
- Quoted examples explaining what NOT to do

**Benefits**:
- Ensures CLAUDE.md/README.md stay focused on instructions
- Prevents bloat from implementation notes
- Keeps documentation useful long-term
- Distinguishes permanent docs from temporary working notes
- Encourages use of untracked/ for ad-hoc summaries

**Examples**:

Blocked - implementation log:
```
CLAUDE.md contains: "Created hook for validating content"
Status: BLOCKED
Suggestion: Move to untracked/implementation-notes.md
```

Blocked - LLM summary:
```
README.md contains: "## Summary
Here's what I did..."
Status: BLOCKED
Suggestion: Remove summary, focus on instructions for using the directory
```

Allowed - instructional content:
```
CLAUDE.md contains:
"## How to use hooks
1. Create hook script in .claude/hooks/
2. Register in settings.local.json
3. Test with sample input"
Status: ALLOWED
```

**Note**: This is a BLOCKING hook (PostToolUse) - writes will fail with exit code 1 if non-instructional content is detected

## discourage-git-stash.py

**Purpose**: BLOCKS git stash usage (dangerous workflow pattern) with escape hatch for confirmed necessary cases.

**How it works**:
1. Detects when Claude attempts to run `git stash` commands
2. Distinguishes between safe operations (viewing/recovering stashes) and dangerous ones (creating stashes)
3. BLOCKS dangerous stash operations by default (exit code 2)
4. Provides escape hatch phrase for truly necessary cases
5. Suggests better alternatives (commits, branches, worktrees)

**Trigger**: PreToolUse (Bash tool only)

**Why git stash is dangerous**:
- Stashes can be forgotten and lost
- `git stash drop` and `git stash clear` permanently destroy work
- Stashes are not part of the git graph (can become orphaned)
- Especially problematic in worktree-based workflows
- Usually a bodge to avoid solving the real git issue

**What it BLOCKS** (dangerous stash operations):
- `git stash` (creating stash)
- `git stash push`
- `git stash save "message"`
- `git stash -u` (stash untracked files)

**What it ALLOWS without warning** (safe read/recovery operations):
- `git stash list` (viewing stashes)
- `git stash show` (viewing stash contents)
- `git stash apply` (recovering stashed work)
- `git stash branch` (creating branch from stash)

**Escape Hatch**:
Include this EXACT phrase in your command to bypass the block:
```
"I HAVE ABSOLUTELY CONFIRMED THAT STASH IS THE ONLY OPTION"
```

Example usage:
```bash
git stash  # I HAVE ABSOLUTELY CONFIRMED THAT STASH IS THE ONLY OPTION
```

**Suggested alternatives** (use these instead of stash):
- `git commit -m "WIP: description"` - Proper version control
- `git checkout -b experiment/feature-name` - New branch for experimental work
- `git worktree add ../worktree-name` - Use worktrees for parallel work
- `git add -p` - Stage specific changes

**Note**: This is a BLOCKING hook - it prevents git stash operations unless the escape hatch phrase is present

## block-plan-time-estimates.py

**Purpose**: Prevents time estimates and completion dates from being written to CLAUDE/Plan/*.md files.

**How it works**:
1. Intercepts Write and Edit operations on files in CLAUDE/Plan/ directories
2. Scans content for time estimate patterns (hours, days, weeks, target completion dates)
3. Blocks the operation if any time estimates are detected
4. Provides clear error message with matched patterns and suggestions

**Trigger**: PreToolUse (Write and Edit tools)

**Philosophy**: Time estimates are context bloat. Plans should focus on WHAT needs to be done and HOW, not WHEN or HOW LONG.

**What it BLOCKS** (all case variations):
- `Estimated Effort: X hours/minutes/days/weeks`
- `Time estimated: ...` or `Estimated time: ...`
- `Total Estimated Time: ...`
- `Target Completion: YYYY-MM-DD`
- `Completion: YYYY-MM-DD`
- Phase headings with durations (e.g., `Phase 1: 2 hours`)
- Timeline sections containing time durations

**Plans should focus on**:
- What needs to be done
- Why it's needed
- How to implement it

**Plans should NEVER include**:
- When work will be completed
- How long tasks will take
- Timeline estimates

**Escape Hatch**:
If a pattern match is a false positive (genuinely not a time estimate), add this comment:
```html
<!-- {regex-pattern} match is a false positive for time estimate blocking hook -->
```

Example:
```markdown
The reference documentation talks about "2 hours" of battery life.
<!-- \*\*Estimated [^:]*\*\*: .*?(?:hours?|minutes?|days?|weeks?) match is a false positive for time estimate blocking hook -->
```

**Note**: This is a BLOCKING hook - writes containing time estimates will fail until they are removed

## enforce-markdown-organization.py

**Purpose**: Enforces strict organization rules for markdown documentation files. Prevents markdown files from littering the filesystem.

**How it works**:
1. Intercepts Write and Edit operations on `*.md` files
2. Validates file path against allowed location patterns
3. Auto-detects current plan being worked on
4. For .claude/agents/, checks for magic string from agent-creator agent (blocks all operations without it)
5. Provides context-aware suggestions for correct location
6. Blocks writes to forbidden locations with clear error message

**Trigger**: PreToolUse (Write and Edit tools on `*.md` files)

**Philosophy**: Documentation chaos leads to lost context and confusion. Enforce strict organization from the start.

**ALLOWED markdown locations**:

1. **`CLAUDE/Plan/{plan-number}-*/`** - Plan-specific documentation
   - Documentation for the current plan being worked on
   - Examples: `PLAN.md`, `test-results.md`, `implementation-notes.md`, `architecture.md`
   - Hook auto-detects current plan and suggests correct path

2. **`CLAUDE/` (root level only)** - Generic, persistently useful LLM-focused documentation
   - Only for documentation that applies across the entire project
   - Must be genuinely useful long-term
   - Examples: `CLAUDE.md`, `PlanWorkflow.md`, `Worktree.md`, `Audience.md`
   - Use sparingly - prefer plan-specific docs

3. **`docs/`** - Human-facing documentation
   - User guides, tutorials, API documentation
   - Documentation for humans, not for LLM consumption

4. **`untracked/`** - Ad-hoc temporary documentation
   - Implementation summaries, test outputs, scratch notes
   - Anything useful now but not long-term
   - NOT tracked in git
   - Perfect for temporary working notes

5. **`.claude/agents/*.md`** - Agent definition files (MAGIC STRING REQUIRED)
   - ONLY the agent-creator agent can write agent files
   - Agent-creator must include: "I AM CLAUDE CODE AGENT CREATOR, LET ME IN" in description parameter
   - ALL other attempts blocked with "YOU SHALL NOT PASS" message
   - Enforces proper agent design workflow

6. **`.claude/skills/*/SKILL.md`** - Skill definition files (auto-allowed)
   - Skill specification files only

7. **`README.md`** (root only) - Standard repository README

**BLOCKED markdown locations**:
- `.claude/hooks/` - Hooks are code only, no documentation
- `.claude/agents/` (WITHOUT magic string) - Use agent-creator agent
- `.claude/skills/*/` (except SKILL.md) - Skill definitions only
- Root directory `*.md` - Except README.md (prevents clutter)
- Any other arbitrary locations - Enforces organization

**Magic String Authentication**:
The agent-creator agent includes "I AM CLAUDE CODE AGENT CREATOR, LET ME IN" in its tool descriptions. This proves the operation originates from the authorized agent-creator and bypasses normal blocking for .claude/agents/ files.

**Error message provides**:
- Clear explanation of why location is blocked
- For agent files: Special "YOU SHALL NOT PASS" message with instructions to use agent-creator
- Suggested correct location based on context
- Auto-detected current plan if applicable
- List of all allowed locations with examples
- Quick reference for choosing the right location

**Detection strategy**:
- Regex patterns for each allowed location type
- Case-insensitive matching
- Path normalization (handles leading/trailing slashes)
- For .claude/agents/, checks hook input for magic string in description field
- Special case handling for skill definitions

**Benefits**:
- Prevents documentation from cluttering filesystem
- Keeps plan-specific docs organized together
- Clear distinction between temporary and permanent docs
- Enforces proper agent creation workflow via agent-creator
- Prevents accidental corruption of agent definitions
- Maintains agent quality and consistency
- Helps future developers find documentation quickly
- Encourages re-use and cross-referencing of docs

**Examples**:

Blocked - attempting to edit agent without magic string:
```
Write to: .claude/agents/my-agent.md
Status: BLOCKED
Message: 🧙 YOU SHALL NOT PASS! 🧙
Solution: Use Task tool with subagent_type='agent-creator'
```

Allowed - agent-creator with magic string:
```
Write to: .claude/agents/my-agent.md
Description: "Create new agent - I AM CLAUDE CODE AGENT CREATOR, LET ME IN"
Status: ALLOWED
```

Blocked - docs in wrong location (.claude/hooks/):
```
Write to: .claude/hooks/implementation-notes.md
Status: BLOCKED
Suggestion: CLAUDE/Plan/024-eslint-worktree-workflow-enhancement/implementation-notes.md
```

Allowed - plan-specific docs:
```
Write to: CLAUDE/Plan/024-eslint-worktree-workflow-enhancement/test-results.md
Status: ALLOWED
```

Allowed - temporary notes:
```
Write to: untracked/quick-test-notes.md
Status: ALLOWED
```

**Note**: This is a BLOCKING hook - markdown files in wrong locations will be blocked until moved to correct location. Agent files require agent-creator with magic string.

## prevent-destructive-git.py

**Purpose**: Blocks destructive git commands that permanently destroy data without recovery options.

**How it works**:
1. Intercepts Bash tool calls
2. Detects dangerous git patterns
3. Blocks with clear error message and alternatives
4. Prevents accidental data destruction

**Blocked commands**:
- `git reset --hard` - Destroys all uncommitted changes
- `git clean -f` - Permanently deletes untracked files
- `git push --force` / `git push -f` - Force-overwrites remote history
- `git reflog expire` - Destroys recovery options

**Note**: This is a BLOCKING hook - prevented for safety

## prevent-absolute-workspace-paths (AbsolutePathHandler)

**Purpose**: Blocks Write/Edit operations using `/workspace/` absolute paths. Enforces use of relative paths from repository root.

**How it works**:
1. Intercepts Write and Edit tool calls
2. Checks if file_path starts with `/workspace/`
3. Blocks the operation with clear explanation
4. Suggests the correct relative path (strips `/workspace/` prefix)

**Trigger**: PreToolUse (Write and Edit tools only)

**Philosophy**: Container-specific absolute paths are environment-dependent and break portability. All file paths should be relative to repo root.

**Why `/workspace/` paths are problematic**:
- `/workspace/` only exists in certain Docker/container environments
- Absolute paths break portability across different setups (local dev, CI, different containers)
- Relative paths work in all environments without modification
- Hard-coded absolute paths couple code to specific deployment environments

**BLOCKED patterns**:
- `Write` to `/workspace/src/test.ts` → Must use `src/test.ts`
- `Edit` to `/workspace/CLAUDE/Plan/001/PLAN.md` → Must use `CLAUDE/Plan/001/PLAN.md`
- Any file path starting with `/workspace/`

**ALLOWED patterns**:
- `Write` to `src/test.ts` (relative path)
- `Edit` to `./src/components/Button.tsx` (dot-relative path)
- `Write` to `CLAUDE/documentation.md` (relative path)
- Other absolute paths like `/tmp/project/file.ts` (not environment-specific)

**Benefits**:
- Ensures code portability across all environments
- Prevents container-specific path coupling
- Makes file operations work in local dev, CI, and production
- Clear error messages with suggested relative paths

**Note**: This is a BLOCKING hook - operations with `/workspace/` paths are blocked

## prevent-worktree-file-copying.py

**Purpose**: Prevents copying files between worktrees (dangerous cross-worktree workflow pattern).

**How it works**:
1. Detects cp/copy commands that copy FROM one worktree TO another
2. Blocks the operation
3. Suggests proper git-based alternatives

**Why this is dangerous**:
- Worktrees should be independent branches
- Copying files breaks git history tracking
- Can cause merge conflicts and data loss
- Violates worktree isolation principles

**Safe alternatives**:
- Use git commits/branches
- Use shared configuration files
- Use git merge (proper workflow)

**Note**: This is a BLOCKING hook - cross-worktree copies are prevented for safety

## validate-sitemap-on-edit.py

**Purpose**: Reminds to validate sitemap files after editing them.

**How it works**:
1. Triggers on Edit/Write operations to `CLAUDE/Sitemap/**/*.md` files
2. Allows the write operation (non-blocking)
3. Adds reminder context to run sitemap-validator agent after editing
4. Enforces build→check workflow pattern

**Trigger**: PostToolUse (Edit/Write tools on sitemap files)

**Philosophy**: Allow modifications but remind to validate afterward. Validation ensures:
- No content (statistics, prose, descriptions)
- No hallucinated components (must exist in src/components/CLAUDE.md)
- No implementation details (props, code, styling)
- Correct notation (CSI enums, arrow syntax)

**What it checks**:
- Files in CLAUDE/Sitemap/ directory
- Markdown files only (.md extension)
- Ignores CLAUDE/Sitemap/CLAUDE.md (the rules file itself)

**Reminder includes**:
- How to run sitemap-validator agent
- What the validator checks
- Note that sitemap skill runs validation automatically

**Note**: This is a NON-BLOCKING hook - writes are allowed with reminder context

## remind-validate-after-builder.py

**Purpose**: Generic hook to remind running validator agents after builder/modifier agents complete.

**How it works**:
1. Triggers on SubagentStop event (when any subagent completes)
2. Parses conversation transcript to identify which agent completed
3. Checks if completed agent has a corresponding validator in the mapping
4. If match found, adds reminder context to run the validator agent
5. Enforces build→check workflow loop for all mapped agents

**Trigger**: SubagentStop (fires when any subagent completes)

**Philosophy**: Automate reminders for validation after modifications. Builder agents should always be followed by validator agents to complete the build→check loop.

**Currently mapped workflows**:
- `sitemap-modifier` → `sitemap-validator` - Validate sitemap notation and components
- `page-implementer` → `page-technical-reviewer` - Review page implementation for code quality
- `page-content-updater` → `page-humanizer` - Humanize content and remove LLM tells
- `eslint-fixer` → `eslint-assessor` - Verify ESLint fixes and assess quality
- `typescript-refactor` → `qa-runner` - Run ESLint + TypeScript checks on refactored code
- `typescript-react-component-builder` → `qa-runner` - Run ESLint + TypeScript checks on new component
- `typescript-specialist` → `qa-runner` - Run full QA suite (ESLint + TypeScript + tests) on new code

**Extensible**: Add more builder→validator mappings in `BUILDER_TO_VALIDATOR` dict:
```python
BUILDER_TO_VALIDATOR = {
    "sitemap-modifier": {
        "validator": "sitemap-validator",
        "description": "sitemap modifications",
        "validation_target": "CLAUDE/Sitemap/ files",
        "validation_command": "..."
    },
    # Add more here as needed
}
```

**How it detects agent**:
1. Reads conversation transcript (JSONL format)
2. Parses in reverse to find most recent Task tool call
3. Extracts `subagent_type` parameter
4. Looks up validator in mapping

**Reminder includes**:
- Confirmation that builder agent completed
- Which validator to run
- How to invoke the validator
- Note that orchestration skills run validation automatically

**Note**: This is a NON-BLOCKING hook - provides reminder context only

## validate-plan-number.py

**Purpose**: Validates plan folder numbers when creating new plans in CLAUDE/Plan/. Ensures sequential numbering by checking BOTH Plan/ and Plan/Completed/ directories.

**How it works**:
1. Detects when a new plan folder is about to be created (pattern: CLAUDE/Plan/NNN-*)
2. Finds highest existing plan number from both Plan/ and Plan/Completed/
3. Validates new plan number is highest + 1
4. If incorrect, warns agent to use correct number
5. Non-blocking but adds strong reminder to conversation context

**Trigger**: PreToolUse (BEFORE Bash mkdir/Write operations)

**Why PreToolUse (not PostToolUse)**:
The handler was moved from PostToolUse to PreToolUse to fix a timing bug:
- PostToolUse ran AFTER directory creation
- Handler saw the just-created directory as "existing"
- Creating 061 when highest was 060 incorrectly warned "use 062"
- PreToolUse validates BEFORE creation, so 060 is correctly seen as highest

**Why check both directories**:
- Active plans in CLAUDE/Plan/
- Completed plans in CLAUDE/Plan/Completed/
- Numbering must be sequential across both
- Prevents number reuse and collisions

**Command to find correct number**:
```bash
find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | sed 's|.*/\([0-9]\{3\}\).*|\1|' | sort -n | tail -1
```

**Philosophy**: Plans must be numbered sequentially for easy reference and tracking.

**What happens on incorrect number**:
- Hook allows operation (non-blocking)
- Provides exact `mv` command to fix
- Reminds to update references in plan files
- Points to CLAUDE/Plan/CLAUDE.md for instructions

**Example**:
```
Highest existing: 028 (in Completed/)
Agent creates: CLAUDE/Plan/028-new-feature/
Hook detects: Number collision
Instructs: mv CLAUDE/Plan/028-new-feature CLAUDE/Plan/029-new-feature
```

**Note**: This is a NON-BLOCKING hook - provides correction instructions but allows the operation
