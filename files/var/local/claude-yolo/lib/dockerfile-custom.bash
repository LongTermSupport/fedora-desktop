#!/bin/bash
# Custom Dockerfile Management Library
# Shared custom Dockerfile workflow for claude-yolo and claude-browser
#
# Version: 1.0.0

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

# Export functions
export -f custom_dockerfile
