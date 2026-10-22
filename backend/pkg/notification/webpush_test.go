package notification

import "testing"

func TestValidatePushEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{name: "trusted provider", endpoint: "https://fcm.googleapis.com/push/id"},
		{name: "trusted subdomain", endpoint: "https://updates.push.services.mozilla.com/wpush/id"},
		{name: "plain HTTP", endpoint: "http://fcm.googleapis.com/push/id", wantErr: true},
		{name: "loopback", endpoint: "https://127.0.0.1/push", wantErr: true},
		{name: "untrusted host", endpoint: "https://attacker.invalid/push", wantErr: true},
		{name: "suffix confusion", endpoint: "https://fcm.googleapis.com.attacker.invalid/push", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePushEndpoint(tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePushEndpoint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
