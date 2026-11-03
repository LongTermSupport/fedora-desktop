package main

import "net/http"

// client builds a transport in the command, which is in the rule's scope.
func client() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}} // want `http.Transport built outside package matrix`
}
