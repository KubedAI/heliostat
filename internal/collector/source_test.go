package collector

import (
	"errors"
	"testing"
)

func TestSourceClearsErrorAfterRelist(t *testing.T) {
	rv := "100"
	s := &source{hasSynced: func() bool { return true }, listedRV: func() string { return rv }}
	s.fail(errors.New("Unauthorized"))
	if h := s.status(); h.Synced || h.Error == "" {
		t.Fatalf("failing source reads healthy: %+v", h)
	}
	rv = "250" // a relist succeeded, even though it returned no objects
	if h := s.status(); !h.Synced || h.Error != "" || h.LastSuccessAt == "" {
		t.Errorf("recovered empty source still failing: %+v", h)
	}
}
