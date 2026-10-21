package handler

import "testing"

func TestValidateTurnstileConfig(t *testing.T) {
	tests := []struct {
		name     string
		required string
		secret   string
		wantErr  bool
	}{
		{name: "optional and unset", required: "", secret: "", wantErr: false},
		{name: "optional with secret", required: "", secret: "s", wantErr: false},
		{name: "required with secret", required: "true", secret: "s", wantErr: false},
		{name: "required without secret", required: "true", secret: "", wantErr: true},
		{name: "required accepts yes", required: "yes", secret: "", wantErr: true},
		{name: "required accepts one", required: "1", secret: "  ", wantErr: true},
		{name: "false is optional", required: "false", secret: "", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CHAT_TURNSTILE_REQUIRED", tc.required)
			t.Setenv("CHAT_TURNSTILE_SECRET", tc.secret)
			err := ValidateTurnstileConfig()
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateTurnstileConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
