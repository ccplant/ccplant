package controllers

import "testing"

func TestValidGoogleADC(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"authorized user", `{"type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`, true},
		{"workload identity", `{"type":"external_account"}`, true},
		{"missing refresh token", `{"type":"authorized_user","client_id":"id","client_secret":"secret"}`, false},
		{"API key is not ADC", `{"type":"api_key","key":"secret"}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validGoogleADC([]byte(test.json)); got != test.want {
				t.Fatalf("validGoogleADC() = %v, want %v", got, test.want)
			}
		})
	}
}
