package cf

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   CapabilityErrorKind
	}{
		{200, CapUnknown},
		{401, CapExpired},
		{403, CapNoAccess},
		{429, CapRateLimited},
		{500, CapUnknown},
		{0, CapUnknown},
	} {
		t.Run(string(rune(tc.status)), func(t *testing.T) {
			err := ClassifyStatus("SyncDNS", tc.status)
			if tc.status == 200 || tc.status == 0 {
				if err != nil {
					t.Fatalf("status %d: got %v want nil", tc.status, err)
				}
				return
			}
			if err == nil || err.Kind != tc.kind || err.Remedy == "" {
				t.Fatalf("status %d: got %#v", tc.status, err)
			}
		})
	}
}

func TestCapabilityErrorNeverLeaksToken(t *testing.T) {
	err := &CapabilityError{Op: "SyncDNS", Remedy: "retry", cause: errors.New("SECRET-TOKEN")}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("token leaked: %q", err.Error())
	}
}
