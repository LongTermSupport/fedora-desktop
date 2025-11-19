#!/bin/bash
# Custom Dockerfile Management Library
# Shared custom Dockerfile workflow for claude-yolo and claude-browser
#
# Version: 1.2.0

# Function to create/update custom Dockerfile for project
# Args: $1 = script_path ($0), $2 = project_subdir (".claude/ccy" or ".claude/ccb"), $3 = tool_name (for display)
custom_dockerfile() {
    local script_path="$1"
    local project_subdir="$2"
    local tool_name="$3"
    local custom_dir="/opt/claude-yolo/custom-dockerfiles"
    local project_dockerfile="$project_subdir/Dockerfile"

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Custom Dockerfile Setup for $(basename "$PWD")"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""

    # Check if project already has a Dockerfile
    if [ -f "$project_dockerfile" ]; then
        echo "Found existing Dockerfile: $project_dockerfile"
        echo ""
        echo "Options:"
        echo "  1) Use Claude to customize it (launches $tool_name)"
        echo "  2) Edit manually (\$EDITOR)"
        echo "  3) Replace with a template"
        echo "  4) Cancel"
        echo ""

        while true; do
            read -p "Select [1-4]: " choice
            echo ""

            case "$choice" in
                1)
                    # Launch tool with customization prompt
                    echo "Launching Claude Code to help customize your Dockerfile..."
                    echo ""
                    exec "$script_path" "Read $project_subdir/Dockerfile - it has instructions for you in the comments. Follow those instructions to customize this Dockerfile for the project."
                    ;;
                2)
                    ${EDITOR:-vi} "$project_dockerfile"
                    echo "✓ Dockerfile edited"
                    echo ""
                    echo "Run '$tool_name --rebuild' to rebuild with changes"
                    exit 0
                    ;;
                3)
                    # Continue to template selection
                    break
                    ;;
                4)
                    echo "Cancelled."
                    exit 0
                    ;;
                "")
                    echo "Invalid selection: (empty)"
                    echo "Please enter 1, 2, 3, or 4"
                    echo ""
                    ;;
                *)
                    echo "Invalid selection: $choice"
                    echo "Please enter 1, 2, 3, or 4"
                    echo ""
                    ;;
            esac
        done
    fi

    # List available templates
    echo "Select a Dockerfile template to start with:"
    echo ""

    local templates=()
    local i=1
    for template in "$custom_dir"/Dockerfile.*; do
        if [ -f "$template" ]; then
            local name=$(basename "$template")
            local desc=""

            # Extract description from first comment line
            desc=$(head -n 5 "$template" | grep -m1 "^#" | sed 's/^# //')

            templates+=("$template")
            echo "  $i) $name"
            if [ -n "$desc" ]; then
                echo "     $desc"
            fi
            echo ""
            ((i++))
        fi
    done

    if [ ${#templates[@]} -eq 0 ]; then
        print_error "No templates found in $custom_dir"
        echo ""
        echo "Run the ansible playbook to install templates:"
        echo "  ansible-playbook playbooks/imports/optional/common/play-install-claude-yolo.yml"
        exit 1
    fi

    echo "Tip: Use 'project-template' if none of the examples match your stack"
    echo ""

    local selection
    local selected_template
    local template_name

    while true; do
        read -p "Select template [1-${#templates[@]}]: " selection
        echo ""

        if [ -z "$selection" ]; then
            echo "Invalid selection: (empty)"
            echo "Please enter a number between 1 and ${#templates[@]}"
            echo ""
            continue
        fi

        if [ "$selection" -ge 1 ] && [ "$selection" -le ${#templates[@]} ] 2>/dev/null; then
            selected_template="${templates[$((selection-1))]}"
            template_name=$(basename "$selected_template")
            break
        else
            echo "Invalid selection: $selection"
            echo "Please enter a number between 1 and ${#templates[@]}"
            echo ""
        fi
    done

    echo "Selected: $template_name"
    echo ""

    # Create project subdir if needed
    mkdir -p "$project_subdir"

    # Copy template
    cp "$selected_template" "$project_dockerfile"
    echo "✓ Created: $project_dockerfile"
    echo ""

    # Offer Claude-assisted customization
    echo "Options:"
    echo "  1) Use Claude to customize it (launches $tool_name)"
    echo "  2) Edit manually (\$EDITOR)"
    echo "  3) Use as-is"
    echo ""

    while true; do
        read -p "Select [1-3]: " edit_choice
        echo ""

        case "$edit_choice" in
            1)
                echo "Launching Claude Code to help customize..."
                echo ""
                exec "$script_path" "Read $project_subdir/Dockerfile - it has instructions for you in the comments. Follow those instructions to customize this Dockerfile for the project."
                ;;
            2)
                ${EDITOR:-vi} "$project_dockerfile"
                echo "✓ Dockerfile edited"
                echo ""
                echo "Run '$tool_name --rebuild' to build, then '$tool_name' to start"
                exit 0
                ;;
            3)
                echo "✓ Using template as-is"
                echo ""
                echo "Run '$tool_name' to build and start with this template"
                exit 0
                ;;
            "")
                echo "Invalid selection: (empty)"
                echo "Please enter 1, 2, or 3"
                echo ""
                ;;
            *)
                echo "Invalid selection: $edit_choice"
                echo "Please enter 1, 2, or 3"
                echo ""
                ;;
        esac
    done
}

# Generate comprehensive prompt for creating a new Dockerfile
# Args: $1 = project_subdir, $2 = tool_name
get_dockerfile_creation_prompt() {
    local project_subdir="$1"
    local tool_name="$2"
    local base_image="claude-yolo:latest"
    local rebuild_cmd="$tool_name --rebuild"

    if [ "$tool_name" = "ccb" ]; then
        base_image="claude-browser:latest"
    fi

    cat << 'PROMPT_EOF'
# PROJECT-SPECIFIC DOCKERFILE CREATION FOR CCY/CCB

You are helping the user create a custom Dockerfile for this project to use with ccy (Claude Code YOLO mode) or ccb (Claude Code Browser mode).

## YOUR MISSION

Create an optimized, project-specific Dockerfile through a collaborative planning process. You MUST:

1. **Enter Planning Mode** - Use your planning capabilities
2. **Investigate** the project thoroughly
3. **Ask Questions** to understand requirements
4. **Propose Features** for user approval
5. **Create Dockerfile** with validation
6. **Provide Next Steps** with clear instructions

## IMPORTANT CONTEXT: How CCY/CCB Work

### What is CCY/CCB?

- **ccy** = Claude Code running in Docker with `--dangerously-skip-permissions` (YOLO mode)
- **ccb** = ccy + browser automation (Playwright, Chrome, Firefox, WebKit)
- **Purpose**: Safe rapid iteration without permission prompts, isolated from host system

### The Container Environment

**Base Image: PROMPT_EOF
    echo "$base_image"
    cat << 'PROMPT_EOF'**
Pre-installed tools:
- **System**: Debian slim, git, gh CLI, SSH, ripgrep, jq, yq, vim, tini
- **Languages**: Node.js 20, npm, Python 3 (system version)
- **Claude Code**: Latest version, auto-updated
- **User**: root (inside container only, safe due to Docker user namespace mapping)

PROMPT_EOF

    if [ "$tool_name" = "ccb" ]; then
        cat << 'PROMPT_EOF'
**For ccb (browser mode), the base also includes:**
- Playwright MCP server
- Chrome, Chromium, Firefox, WebKit browsers
- Chrome DevTools Protocol CLI
- GUI support (Wayland/X11)

PROMPT_EOF
    fi

    cat << 'PROMPT_EOF'
**Runtime Configuration:**
- **Volumes**:
  - `$PWD:/workspace` - Your project (read/write)
  - `~/.claude:/root/.claude` - Claude Code state
  - `~/.claude-tokens/ccy/tokens:/root/.claude-tokens/ccy/tokens` - API tokens
  - `~/.ssh/<keys>:/root/.ssh/<keys>` - SSH keys (optional)
  - `~/.gitconfig` - Git config (optional)
- **Working Dir**: `/workspace`
- **Network**: Can auto-connect to Docker networks for container-to-container communication
- **Isolation**: rootless Docker recommended, proper user namespace mapping

### What Goes in the Custom Dockerfile?

**Purpose**: Add project-specific tools and dependencies that aren't in the base image

**Common additions:**
- **Language versions**: Python 3.12, Go, Rust, Ruby, Java, etc.
- **Language tools**: pytest, black, golangci-lint, cargo-clippy, rubocop
- **Database clients**: postgresql-client, mysql-client, mongodb-tools
- **Cloud CLIs**: aws-cli, gcloud, azure-cli, terraform, pulumi
- **Build tools**: make, cmake, gradle, maven, cargo
- **Testing tools**: selenium (for non-ccb projects)
- **Formatters/Linters**: prettier, eslint, shellcheck, yamllint
- **Package managers**: pip, poetry, pipenv, composer, bundle

**What NOT to include:**
- Application code (it's mounted from host at runtime)
- Project dependencies installed by package managers (npm install, pip install, go mod download - these run in the container at dev time)
- Secrets or credentials
- Things already in base image (Node.js 20, git, gh, Claude Code)

**Best Practices:**
- Use `--mount=type=cache` for apt, npm, pip, go, cargo caches (faster rebuilds)
- Keep it minimal - only add what's actually needed
- Use specific versions when version matters
- Set PATH and environment variables as needed
- Add verification commands (e.g., `RUN go version`)
- Include comprehensive comments explaining each section

## YOUR WORKFLOW

### Step 1: Investigation Phase

Thoroughly investigate the project:

**Check for Project Files:**
- [ ] `package.json`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`
- [ ] `requirements.txt`, `Pipfile`, `poetry.lock`, `pyproject.toml`
- [ ] `go.mod`, `go.sum`
- [ ] `Cargo.toml`, `Cargo.lock`
- [ ] `Gemfile`, `Gemfile.lock`
- [ ] `composer.json`, `composer.lock`
- [ ] `build.gradle`, `pom.xml`
- [ ] `.tool-versions`, `.python-version`, `.nvmrc`
- [ ] `Makefile`, `justfile`, `taskfile.yml`
- [ ] Existing `Dockerfile`, `docker-compose.yml`, `.dockerignore`
- [ ] CI/CD configs (`.github/workflows/`, `.gitlab-ci.yml`, etc.)
- [ ] Testing configs (`pytest.ini`, `jest.config.js`, etc.)
- [ ] Linter configs (`.eslintrc`, `.pylintrc`, `.golangci.yml`, etc.)

**Analyze Findings:**
- What languages/frameworks are used?
- What versions are specified?
- What build tools are configured?
- What testing frameworks are used?
- Are there any database requirements?
- Are there any cloud provider integrations?
- What's the typical development workflow?

### Step 2: Interactive Discovery

Ask the user targeted questions based on your investigation. Examples:

**For a Node.js project:**
> I see you're using Node.js with TypeScript. A few questions:
> 1. Do you need a specific Python version for any build scripts? (I see a requirements.txt)
> 2. Do you run tests in the container? I see Jest configured.
> 3. Do you need any database clients installed? (PostgreSQL, MySQL, etc.)
> 4. Do you use any cloud CLIs? (aws, gcloud, etc.)
> 5. Do you need Docker-in-Docker for integration tests?

**For a Python project:**
> I see this is a Python project with poetry. A few questions:
> 1. What Python version do you need? (I see 3.11 in pyproject.toml)
> 2. Do you need any system libraries? (libpq for postgres, etc.)
> 3. Do you use any Python tools that should be pre-installed? (black, mypy, pytest)
> 4. Do you need any non-Python tools? (Node.js for frontend builds, etc.)

**For a Go project:**
> I see this is a Go project. A few questions:
> 1. What Go version do you need? (I see go1.21 in go.mod)
> 2. Do you need CGO? (would require build-essential)
> 3. Do you use golangci-lint or other Go tools?
> 4. Any database clients or cloud CLIs needed?

**Always ask about:**
- Whether this is for ccy or ccb (browser automation needs?)
- Database clients
- Cloud CLIs
- Testing requirements
- Build tool requirements

### Step 3: Feature Proposal

Based on investigation and answers, propose a comprehensive feature list:

PROMPT_EOF

    # Output the feature proposal header with proper base image
    cat << PROPOSAL_EOF
\`\`\`
PROPOSED DOCKERFILE FEATURES
════════════════════════════════════════════════════════════════
BASE IMAGE: $base_image
(Includes: Node.js 20, git, gh, Claude Code, ripgrep, jq, yq, vim)
PROPOSAL_EOF

    cat << 'PROMPT_EOF'

ADDITIONS PROPOSED:

✓ Python 3.12
  - Reasoning: pyproject.toml specifies 3.12
  - Tools: poetry, pytest, black, mypy

✓ PostgreSQL Client
  - Reasoning: You mentioned using PostgreSQL in development
  - Includes: postgresql-client-15

✓ AWS CLI v2
  - Reasoning: .github/workflows/ shows AWS deployments
  - Includes: aws-cli

✓ Cache Mounts
  - apt cache (faster rebuilds)
  - pip cache (faster Python installs)
  - npm cache (faster Node installs)

✓ Environment Variables
  - PYTHONUNBUFFERED=1 (better logging)
  - PIP_NO_CACHE_DIR=1 (smaller image)

OMITTED:
✗ Docker-in-Docker - You mentioned not running integration tests in container
✗ MySQL client - You're using PostgreSQL exclusively

DOCKERFILE SIZE ESTIMATE: ~500MB (vs ~300MB base image)
BUILD TIME ESTIMATE: ~3-5 minutes first build, ~30s with cache

Does this look good? Any changes?
```

### Step 4: Refinement

Allow user to refine the proposal:
- Add missing tools
- Remove unnecessary tools
- Change versions
- Adjust configurations

### Step 5: Create Dockerfile

Once approved:

PROMPT_EOF

    # Output the exact paths clearly
    cat << PATHS_EOF
**IMPORTANT: Exact file path for this project:**
- Directory: \`$project_subdir\`
- Dockerfile: \`$project_subdir/Dockerfile\`

1. **Create directory** if needed:
   \`\`\`bash
   mkdir -p $project_subdir
   \`\`\`

2. **Write Dockerfile** to \`$project_subdir/Dockerfile\`

PATHS_EOF

    cat << 'PROMPT_EOF'
3. **Include comprehensive comments**:
   - Explain each section
   - Document why each tool is included
   - Add links to relevant documentation
   - Include optimization notes

PROMPT_EOF

    # Output Dockerfile example with proper variable interpolation
    cat << EXAMPLE_EOF
4. **Format Example**:
   \`\`\`dockerfile
   # Project-Specific CCY Container Extension
   # Generated by Claude Code --custom-docker
   #
   # This Dockerfile extends $base_image with project-specific tools.
   # Rebuild with: $rebuild_cmd

   FROM $base_image

   # ============================================================================
   # Python 3.12 + Development Tools
   # ============================================================================
   # Project uses Python 3.12 (specified in pyproject.toml)
   # Installing: python3.12, poetry, pytest, black, mypy

   RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \\
       --mount=type=cache,target=/var/lib/apt,sharing=locked \\
       apt-get update && \\
       apt-get install -y --no-install-recommends \\
           python3.12 \\
           python3.12-venv \\
           python3-pip \\
       && rm -rf /var/lib/apt/lists/*

   # Install Python tools with cache mount for faster rebuilds
   RUN --mount=type=cache,target=/root/.cache/pip \\
       pip3 install --upgrade pip && \\
       pip3 install poetry pytest black mypy

   # ... etc ...

   # Verify installations
   RUN python3.12 --version && \\
       poetry --version
   \`\`\`
EXAMPLE_EOF

    cat << 'PROMPT_EOF'

### Step 6: Validation

After creating the Dockerfile:

1. **Basic syntax check** - ensure FROM, RUN, ENV commands are valid
2. **Check for common mistakes**:
   - Missing continuation backslashes
   - Unmatched quotes
   - Invalid package names
3. **Report validation results**

### Step 7: Print Next Steps

PROMPT_EOF

    # Output the next steps with proper variable interpolation
    cat << NEXTSTEPS_EOF
\`\`\`
════════════════════════════════════════════════════════════════════════════════
✓ Dockerfile created and validated: $project_subdir/Dockerfile
════════════════════════════════════════════════════════════════════════════════

NEXT STEPS:
  1. Exit this session: /exit or Ctrl+D
  2. Rebuild the image: $rebuild_cmd
  3. Launch with custom image: $tool_name

The custom image will be cached as: claude-yolo:<project-name>
It will automatically rebuild only when Dockerfile changes.

MODIFY LATER:
  • $tool_name --custom-docker    (guided creation/update workflow)
  • vim $project_subdir/Dockerfile  (manual editing)

SAVE AS TEMPLATE (Optional):
  You can save this Dockerfile as a template for future projects:

  sudo cp $project_subdir/Dockerfile \\
    /opt/claude-yolo/custom-dockerfiles/Dockerfile.example-<name>

  Templates in that directory are never overwritten and appear in the
  template selection menu for new projects.

  📚 Documentation: https://github.com/LongTermSupport/fedora-desktop/blob/main/docs/containerization.md#custom-dockerfiles

════════════════════════════════════════════════════════════════════════════════
\`\`\`
NEXTSTEPS_EOF

    cat << 'PROMPT_EOF'

## IMPORTANT REQUIREMENTS

1. **MUST enter planning mode** - This is a planning task, not immediate execution
2. **MUST investigate first** - Don't ask questions before understanding the project
3. **MUST ask questions** - Don't assume requirements
4. **MUST propose before creating** - Get explicit approval
5. **MUST include comprehensive comments** - Educational value is important
6. **MUST use cache mounts** - Optimization is important
7. **MUST verify installations** - Add `RUN` commands to verify
8. **MUST explain next steps** - User needs to know to rebuild
9. **MUST validate** - Check for syntax errors before declaring success

## TEMPLATES FOR REFERENCE

You can reference these example templates (in /opt/claude-yolo/custom-dockerfiles/):
- `Dockerfile.project-template` - Blank with examples
- `Dockerfile.example-ansible` - Ansible + testing tools
- `Dockerfile.example-golang` - Go + development tools

But DON'T just copy these - create a custom solution based on THIS project's needs.

## GETTING STARTED

Begin by:
1. Entering planning mode
2. Investigating the project structure
3. Reading key configuration files
4. Then proceed with your questions

Ready? Start investigating!
PROMPT_EOF
}

# Generate prompt for analyzing and improving existing Dockerfile
# Args: $1 = project_subdir, $2 = tool_name
get_dockerfile_improvement_prompt() {
    local project_subdir="$1"
    local tool_name="$2"
    local dockerfile_path="$project_subdir/Dockerfile"
    local rebuild_cmd="$tool_name --rebuild"

    cat << 'PROMPT_EOF'
# ANALYZE AND IMPROVE EXISTING DOCKERFILE

You are helping the user improve an existing custom Dockerfile for ccy/ccb.

## YOUR MISSION

1. **Read the current Dockerfile** at PROMPT_EOF
    echo "$dockerfile_path"
    cat << 'PROMPT_EOF'
2. **Investigate the project** to understand current and potential needs
3. **Analyze the Dockerfile** for:
   - Missing tools that the project needs
   - Unnecessary tools that could be removed
   - Optimization opportunities (cache mounts, layer ordering, etc.)
   - Version updates available
   - Security improvements
   - Documentation improvements
4. **Ask questions** about any unclear requirements
5. **Propose specific improvements** for user approval
6. **Update the Dockerfile** with improvements
7. **Validate** the changes

## WORKFLOW

### Step 1: Read and Analyze

Read PROMPT_EOF
    echo "$dockerfile_path"
    cat << 'PROMPT_EOF' and understand:
- What tools are currently installed?
- What versions are specified?
- Are cache mounts used?
- Is the documentation clear?
- Are there any obvious issues?

### Step 2: Investigate Project

Scan the project for:
- New dependencies added since Dockerfile creation
- Updated version requirements
- New build tools configured
- Changes in development workflow

### Step 3: Propose Improvements

Present findings in this format:

```
DOCKERFILE ANALYSIS
════════════════════════════════════════════════════════════════

CURRENT STATE:
  • Python 3.11 installed
  • PostgreSQL client installed
  • No cache mounts (slow rebuilds)
  • Missing: black, mypy (now in pyproject.toml)

PROPOSED IMPROVEMENTS:

✓ Add cache mounts (HIGH PRIORITY)
  - apt cache mount (faster rebuilds)
  - pip cache mount (faster Python installs)
  - Estimated rebuild time: 5min → 30sec

✓ Update Python to 3.12 (MEDIUM)
  - Reasoning: pyproject.toml now specifies 3.12
  - Breaking change: Requires testing

✓ Add Python dev tools (HIGH)
  - black, mypy, pytest (found in pyproject.toml)
  - Reasoning: Currently installed manually each time

✓ Add version verification (LOW)
  - Add RUN commands to verify installations
  - Helps catch build failures early

OPTIONAL REMOVALS:
✗ Node.js packages (unused)
  - Found npm packages but no usage in project
  - Keep or remove?

Do you want to proceed with these improvements?
```

### Step 4: Apply Improvements

Once approved, update the Dockerfile with:
- Clear comments explaining changes
- Preserve any custom user configurations
- Use best practices (cache mounts, minimal layers, etc.)

### Step 5: Validation and Next Steps

After updating:

```
════════════════════════════════════════════════════════════════════════════════
✓ Dockerfile updated: PROMPT_EOF
    echo "$dockerfile_path"
    cat << 'PROMPT_EOF'
════════════════════════════════════════════════════════════════════════════════

CHANGES MADE:
  ✓ Added cache mounts for apt and pip
  ✓ Updated Python 3.11 → 3.12
  ✓ Added black, mypy, pytest
  ✓ Added version verification commands

NEXT STEPS:
  1. Exit this session: /exit or Ctrl+D
  2. Rebuild with changes: PROMPT_EOF
    echo "$rebuild_cmd"
    cat << 'PROMPT_EOF'
  3. Test in container: PROMPT_EOF
    echo "$tool_name"
    cat << 'PROMPT_EOF'

The rebuild will use cache mounts for faster builds (~30 seconds).

════════════════════════════════════════════════════════════════════════════════
```

## IMPORTANT

- **Preserve user customizations** - Don't remove things without asking
- **Explain all changes** - User should understand why each change is made
- **Ask before major changes** - Version upgrades, package removals, etc.
- **Validate changes** - Check syntax and test build if possible

Ready? Start by reading the current Dockerfile!
PROMPT_EOF
}

# Guided Dockerfile creation with comprehensive AI planning
# Args: $1 = script_path ($0), $2 = project_subdir (".claude/ccy" or ".claude/ccb"), $3 = tool_name (for display)
create_dockerfile_guided() {
    local script_path="$1"
    local project_subdir="$2"
    local tool_name="$3"
    local project_dockerfile="$project_subdir/Dockerfile"

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "AI-Guided Dockerfile Creation for $(basename "$PWD")"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""

    # Check if Dockerfile already exists
    if [ -f "$project_dockerfile" ]; then
        echo "Found existing Dockerfile: $project_dockerfile"
        echo ""
        echo "Options:"
        echo "  1) Analyze and propose improvements (AI investigates current setup)"
        echo "  2) Replace with new (start fresh)"
        echo "  3) Cancel"
        echo ""

        while true; do
            read -p "Select [1-3]: " choice
            echo ""

            case "$choice" in
                1)
                    # Analyze existing Dockerfile
                    echo "Launching Claude Code to analyze your Dockerfile..."
                    echo ""
                    exec "$script_path" --prompt "$(get_dockerfile_improvement_prompt "$project_subdir" "$tool_name")"
                    ;;
                2)
                    # Start fresh
                    echo "Starting fresh Dockerfile creation..."
                    echo ""
                    break
                    ;;
                3)
                    echo "Cancelled."
                    exit 0
                    ;;
                "")
                    echo "Invalid selection: (empty)"
                    echo "Please enter 1, 2, or 3"
                    echo ""
                    ;;
                *)
                    echo "Invalid selection: $choice"
                    echo "Please enter 1, 2, or 3"
                    echo ""
                    ;;
            esac
        done
    fi

    # Show intro for new Dockerfile creation
    echo "This will launch Claude Code in an interactive planning session to create"
    echo "a custom Dockerfile optimized for your project."
    echo ""
    echo "Claude will:"
    echo "  • Enter planning mode to investigate your project"
    echo "  • Scan project files (package.json, go.mod, requirements.txt, etc.)"
    echo "  • Ask clarifying questions about your workflow and needs"
    echo "  • Propose features and optimizations for your approval"
    echo "  • Create the Dockerfile with comprehensive comments and validation"
    echo ""
    read -p "Press Enter to continue..."
    echo ""

    # Launch with comprehensive prompt
    echo "Launching Claude Code..."
    echo ""
    exec "$script_path" --prompt "$(get_dockerfile_creation_prompt "$project_subdir" "$tool_name")"
}

# Export functions
export -f custom_dockerfile
export -f get_dockerfile_creation_prompt
export -f get_dockerfile_improvement_prompt
export -f create_dockerfile_guided
