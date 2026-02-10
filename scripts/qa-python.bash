#!/usr/bin/bash
# Python QA validation for entire project
# Finds and validates all Python files

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ERRORS=0
CHECKED=0

echo "=== Python QA - Repository-wide ==="
echo ""

# Fail fast: check required dependencies
echo "→ Checking dependencies..."
if ! command -v ruff &>/dev/null; then
    echo "  ✗ ruff not installed"
    echo ""
    echo "Install with: sudo dnf install ruff"
    exit 1
fi
echo "  ✓ ruff found"
echo ""

# Find all Python files (exclude .git, venv, __pycache__, etc.)
echo "→ Discovering Python files..."

# Find .py files
# Excludes .ansible/roles/ (galaxy-installed cache) but keeps roles/vendor/ (first-party tracked)
# Excludes .claude/hooks-daemon/ (has its own QA system)
PY_FILES=()
while IFS= read -r -d '' file; do
    PY_FILES+=("$file")
done < <(find "$REPO_ROOT" -type f -name "*.py" \
    ! -path "*/.git/*" \
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
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
    ! -path "*/.ansible/roles/*" \
    ! -path "*/.claude/hooks-daemon/*" \
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
        ((CHECKED++)) || true
    else
        echo "  ✗ Syntax error"
        python3 -m py_compile "$file" 2>&1  # Show the error
        ((ERRORS++)) || true
    fi
done

echo ""

# Ruff linting (mandatory, default rules with auto-fix)
if [ ${#PY_FILES[@]} -gt 0 ]; then
    echo "→ Running ruff --fix (auto-fixing safe fixes)..."
    ruff check --fix "${PY_FILES[@]}" >/dev/null 2>&1 || true

    echo "→ Checking for remaining ruff errors..."
    RUFF_FAILED=0
    ruff check "${PY_FILES[@]}" 2>&1 || RUFF_FAILED=1
    if [ $RUFF_FAILED -eq 1 ]; then
        echo "  ✗ Ruff errors found (manual intervention required)"
        ((ERRORS++)) || true
    else
        echo "  ✓ Ruff passed"
    fi
fi

echo ""
echo "Checked: $CHECKED files"
if [ $ERRORS -eq 0 ]; then
    echo "✓ All Python QA checks passed"
    exit 0
else
    echo "✗ $ERRORS check(s) failed"
    exit 1
fi
