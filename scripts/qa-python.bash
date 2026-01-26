#!/usr/bin/bash
# Python QA validation for entire project
# Finds and validates all Python files

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ERRORS=0
CHECKED=0

echo "=== Python QA - Repository-wide ==="
echo ""

# Find all Python files (exclude .git, venv, __pycache__, etc.)
echo "→ Discovering Python files..."

# Find .py files
PY_FILES=()
while IFS= read -r -d '' file; do
    PY_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.py" \
    ! -path "*/.git/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -path "*/__pycache__/*" \
    ! -path "*/.venv/*" \
    ! -path "*/venv/*" \
    -print0)

# Find executable Python scripts (shebang)
while IFS= read -r file; do
    if head -n1 "$file" 2>/dev/null | grep -q "^#!/.*python"; then
        PY_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f -executable \
    ! -path "*/.git/*" \
    ! -path "*/node_modules/*" \
    ! -path "*/untracked/*" \
    ! -name "*.py")

echo "  Found ${#PY_FILES[@]} Python files"
echo ""

# Syntax check each file
for file in "${PY_FILES[@]}"; do
    rel_path="${file#$REPO_ROOT/}"
    echo "→ Checking: $rel_path"

    if python3 -m py_compile "$file" 2>/dev/null; then
        echo "  ✓ Syntax valid"
        ((CHECKED++))
    else
        echo "  ✗ Syntax error"
        python3 -m py_compile "$file" 2>&1  # Show the error
        ((ERRORS++))
    fi
done

echo ""

# Optional: ruff check (if available)
if command -v ruff &>/dev/null && [ ${#PY_FILES[@]} -gt 0 ]; then
    echo "→ Running ruff on all Python files..."
    if ruff check "${PY_FILES[@]}" 2>&1; then
        echo "  ✓ Ruff passed"
    else
        echo "  ⚠ Ruff warnings found (non-blocking)"
    fi
else
    echo "  ⊘ ruff not installed (optional - install with: cargo install ruff)"
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
