// Command tool is outside teamchat's internal tree: a class-only line is judged here too.
package main

import (
	"errors"
	"fmt"
	"os"

	"fix/internal/teamchat/matrix"
)

func main() {
	fmt.Fprintln(os.Stderr, "failed:", matrix.Class(errors.New("x"))) // want `a failed call's class printed without its detail`
}
