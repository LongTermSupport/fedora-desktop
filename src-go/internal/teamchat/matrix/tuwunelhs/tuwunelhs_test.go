//go:build tuwunel

package tuwunelhs

import (
	"context"
	"net/http"
	"testing"
)

func TestStartAnswersVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), StartLimit)
	defer cancel()
	hs := Start(ctx, t, "example.org")
	if len(hs.RegKey) != 64 {
		t.Fatalf("registration key %d characters, want 64", len(hs.RegKey))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hs.Base+versionsPath, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("versions answered %d", resp.StatusCode)
	}
}
