# Fixture include for shell-snapshot-dropped-name, deployed by flagged.yml from a loop.
loop_paths=(/a /b)

loop-show() {
    printf '%s\n' "${loop_paths[@]}" # EXPECT-FINDING
    printf '%s\n' "$(_loop_fmt)" # EXPECT-FINDING
    echo "${DEFAULT_REMOTE}" # EXPECT-FINDING
}
