package dataexport

import "testing"

func TestExportableKinds(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"cv", true},
		{"attachment", true},
		{"reference_letter", false},
		{"", false},
		{"CV", false},
		{"anything_else", false},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := exportable(tt.kind); got != tt.want {
				t.Errorf("exportable(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}
