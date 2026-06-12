#!/usr/bin/env bash
set -e

# Make all Ansible playbooks directly executable with proper shebang
# This allows running playbooks by path instead of `ansible-playbook path/to/playbook.yml`

SHEBANG="#!/usr/bin/env ansible-playbook"
PLAYBOOK_DIR="playbooks"
COUNT_UPDATED=0
COUNT_SKIPPED=0

echo "🔧 Making all playbooks executable..."

# Find all .yml files in playbooks directory
while IFS= read -r -d '' playbook; do
    # Check if a shebang already exists anywhere in the first three lines. Checking only
    # line 1 (the old behaviour) let a file whose shebang had drifted below `---` get a
    # second shebang prepended — the duplicate-shebang defect (ANS-15). grep -xF matches
    # the whole line exactly so a commented mention cannot false-positive.
    first_line=$(head -n 1 "$playbook")

    if head -n 3 "$playbook" | grep -qxF "$SHEBANG"; then
        if [[ "$first_line" == "$SHEBANG" ]]; then
            echo "  ✓ Already has shebang: $playbook"
        else
            echo "  ⚠ Shebang present but not on line 1 (leaving as-is): $playbook"
        fi
        COUNT_SKIPPED=$((COUNT_SKIPPED + 1))
    else
        echo "  + Adding shebang to: $playbook"

        # Create temp file with shebang + original content
        temp_file=$(mktemp)
        echo "$SHEBANG" > "$temp_file"
        cat "$playbook" >> "$temp_file"

        # Replace original file
        mv "$temp_file" "$playbook"
        COUNT_UPDATED=$((COUNT_UPDATED + 1))
    fi

    # Make executable
    chmod +x "$playbook"

done < <(find "$PLAYBOOK_DIR" -type f -name "*.yml" -print0)

echo ""
echo "✅ Done!"
echo "   Updated: $COUNT_UPDATED playbooks"
echo "   Skipped: $COUNT_SKIPPED playbooks (already had shebang)"
echo ""
echo "Now you can run playbooks directly:"
echo "   ./playbooks/playbook-main.yml"
echo "   ./playbooks/imports/optional/common/play-vscode.yml"
