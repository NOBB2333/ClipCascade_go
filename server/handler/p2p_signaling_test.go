package handler

import (
	"strings"
	"testing"
)

func TestGenerateSessionIDFormatAndUniqueness(t *testing.T) {
	const n = 200
	seen := make(map[string]struct{}, n)

	for i := 0; i < n; i++ {
		id := generateSessionID()
		if !strings.HasPrefix(id, "peer-") {
			t.Fatalf("invalid prefix: %s", id)
		}
		if len(id) != len("peer-")+24 {
			t.Fatalf("unexpected id length: got %d, id=%s", len(id), id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate session id: %s", id)
		}
		seen[id] = struct{}{}
	}
}
