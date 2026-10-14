package netprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNoInterface is an address that no interface of the host carries.
var ErrNoInterface = errors.New("on no interface of this host")

// MultipleInterfacesError is an address that more than one interface carries.
type MultipleInterfacesError struct {
	Names []string
}

func (e *MultipleInterfacesError) Error() string {
	return "on more than one interface: " + strings.Join(e.Names, ", ")
}

type linkInfo struct {
	Name  string `json:"ifname"`
	Addrs []struct {
		Local string `json:"local"`
	} `json:"addr_info"`
}

// InterfaceOf is the name of the one interface in `ip -j addr show` JSON that carries addr.
func InterfaceOf(data []byte, addr string) (string, error) {
	var links []linkInfo
	if err := json.Unmarshal(data, &links); err != nil {
		return "", fmt.Errorf("ip addr show: unreadable JSON: %w", err)
	}
	var found []string
	for _, link := range links {
		for _, a := range link.Addrs {
			if a.Local == addr {
				found = append(found, link.Name)
				break
			}
		}
	}
	slices.Sort(found)
	found = slices.Compact(found)
	switch len(found) {
	case 0:
		return "", ErrNoInterface
	case 1:
		return found[0], nil
	default:
		return "", &MultipleInterfacesError{Names: found}
	}
}
