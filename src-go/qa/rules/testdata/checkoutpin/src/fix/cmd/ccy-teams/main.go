// Command ccy-teams names a compose file outside the pin package.
package main

import "fmt"

func main() {
	fmt.Println("podman-compose.yaml") // want `"podman-compose.yaml", a checkout file the host applies, named outside internal/checkoutpin`
}
