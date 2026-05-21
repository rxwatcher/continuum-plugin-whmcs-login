package auth

import (
	"sort"
	"testing"

	"github.com/RXWatcher/continuum-plugin-whmcs-login/internal/whmcs"
)

// TestActiveProductIDs pins the field-presence semantics for ClientProduct.Status
// (see whmcs.ClientProduct docstring). The earlier implementation treated an
// explicit empty status the same as a missing one; this regression test
// guards the new behavior where an explicit empty *string* fails closed.
func TestActiveProductIDs(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		input    []whmcs.ClientProduct
		expected []string
	}{
		{
			name:     "active explicit",
			input:    []whmcs.ClientProduct{{PID: 1, Status: ptr("Active")}},
			expected: []string{"1"},
		},
		{
			name:     "active is trimmed and case-insensitive",
			input:    []whmcs.ClientProduct{{PID: 8, Status: ptr(" active ")}},
			expected: []string{"8"},
		},
		{
			name:     "nil status (legacy WHMCS — field omitted)",
			input:    []whmcs.ClientProduct{{PID: 2, Status: nil}},
			expected: []string{"2"},
		},
		{
			name:     "explicit empty status (defensive — treated inactive)",
			input:    []whmcs.ClientProduct{{PID: 3, Status: ptr("")}},
			expected: []string{},
		},
		{
			name:     "suspended is inactive",
			input:    []whmcs.ClientProduct{{PID: 4, Status: ptr("Suspended")}},
			expected: []string{},
		},
		{
			name:     "terminated is inactive",
			input:    []whmcs.ClientProduct{{PID: 5, Status: ptr("Terminated")}},
			expected: []string{},
		},
		{
			name:     "cancelled is inactive",
			input:    []whmcs.ClientProduct{{PID: 6, Status: ptr("Cancelled")}},
			expected: []string{},
		},
		{
			name:     "pending is inactive",
			input:    []whmcs.ClientProduct{{PID: 7, Status: ptr("Pending")}},
			expected: []string{},
		},
		{
			name: "mixed bag",
			input: []whmcs.ClientProduct{
				{PID: 10, Status: ptr("Active")},
				{PID: 11, Status: nil},
				{PID: 12, Status: ptr("")},
				{PID: 13, Status: ptr("Suspended")},
				{PID: 14, Status: ptr("Active")},
			},
			expected: []string{"10", "11", "14"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ActiveProductIDs(tc.input)
			sort.Strings(got)
			want := append([]string(nil), tc.expected...)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("ActiveProductIDs = %v, want %v", got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("ActiveProductIDs = %v, want %v", got, want)
				}
			}
		})
	}
}
