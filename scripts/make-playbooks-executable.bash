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
    # Check if file already has the shebang
    first_line=$(head -n 1 "$playbook")

    if [[ "$first_line" == "$SHEBANG" ]]; then
        echo "  ✓ Already has shebang: $playbook"
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
echo "   ./playbooks/imports/optional/common/play-install-flatpaks.yml"
