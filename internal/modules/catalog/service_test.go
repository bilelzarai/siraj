package catalog

import (
	"testing"

	"siraj/internal/platform/apperr"
)

func TestValidateDomainIDs(t *testing.T) {
	many := make([]int64, MaxUserDomains+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	cases := []struct {
		name string
		ids  []int64
		ok   bool
	}{
		{"one domain is enough", []int64{1}, true},
		{"several in order", []int64{3, 1, 2}, true},
		{"empty list is refused (keep at least one)", nil, false},
		{"duplicates are refused", []int64{1, 1}, false},
		{"non-positive ids are refused", []int64{0}, false},
		{"over the limit is refused", many, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDomainIDs(tc.ids)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok {
				e, isApp := apperr.As(err)
				if !isApp || e.Status != 422 {
					t.Fatalf("want 422 invalid, got %v", err)
				}
			}
		})
	}
}
