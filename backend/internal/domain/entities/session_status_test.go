package entities

import "testing"

func TestNormalizeSessionStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{input: "stable", want: "active", valid: true},
		{input: "running", want: "running", valid: true},
		{input: "active", want: "active", valid: true},
		{input: "resuming", want: "resuming", valid: true},
		{input: "terminating", want: "terminating", valid: true},
		{input: "terminated", want: "terminated", valid: true},
		{input: "unknown-runtime-state", valid: false},
		{input: "", valid: false},
	}
	for _, test := range tests {
		got, valid := NormalizeSessionStatus(test.input)
		if got != test.want || valid != test.valid {
			t.Errorf("NormalizeSessionStatus(%q) = (%q, %t), want (%q, %t)", test.input, got, valid, test.want, test.valid)
		}
	}
}
