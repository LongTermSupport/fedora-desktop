# Fixture include for shell-snapshot-dropped-name, deployed by clean.yml: nothing to report.
clean-show() {
    local -a paths=(/a /b)
    printf '%s\n' "${paths[@]}" "${#paths[@]}" "$#" "$?"
    for name in "${paths[@]}"; do
        echo "$name"
    done
}
