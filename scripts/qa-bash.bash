#!/usr/bin/bash
# Bash QA validation for entire project
# Finds and validates all bash/shell scripts

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ERRORS=0
CHECKED=0

echo "=== Bash/Shell QA - Repository-wide ==="
echo ""

# Find all bash/shell files
echo "→ Discovering bash/shell files..."

BASH_FILES=()

# Find .sh and .bash files
# Excludes .ansible/roles/ (galaxy-installed cache) but keeps roles/vendor/ (first-party tracked)
while IFS= read -r -d '' file; do
    BASH_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f \( -name "*.sh" -o -name "*.bash" \) \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    -print0)

# Find executable bash scripts (bash shebang)
while IFS= read -r file; do
    first_line=$(head -n1 "$file" 2>/dev/null)
    if [[ "$first_line" =~ ^#!/.*bash ]] || [[ "$first_line" == "#!/bin/sh" ]] || [[ "$first_line" == "#!/usr/bin/sh" ]]; then
        BASH_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -name "*.sh" \
    ! -name "*.bash")

echo "  Found ${#BASH_FILES[@]} bash/shell files"
echo ""

# Syntax check each file
for file in "${BASH_FILES[@]}"; do
    rel_path="${file#$REPO_ROOT/}"
    echo "→ Checking: $rel_path"

    if bash -n "$file" 2>/dev/null; then
        echo "  ✓ Syntax valid"
        ((CHECKED++))
    else
        echo "  ✗ Syntax error"
        bash -n "$file" 2>&1  # Show the error
        ((ERRORS++))
    fi
done

echo ""

# Optional: shellcheck (if available)
if command -v shellcheck &>/dev/null && [ ${#BASH_FILES[@]} -gt 0 ]; then
    echo "→ Running shellcheck on all bash files..."
    if shellcheck "${BASH_FILES[@]}" 2>&1; then
        echo "  ✓ Shellcheck passed"
    else
        echo "  ⚠ Shellcheck warnings found (non-blocking)"
    fi
else
    echo "  ⊘ shellcheck not installed (optional - install with: dnf install ShellCheck)"
fi

echo ""
echo "Checked: $CHECKED files"
if [ $ERRORS -eq 0 ]; then
    echo "✓ All syntax checks passed"
    exit 0
else
    echo "✗ $ERRORS files with syntax errors"
    exit 1
fi
