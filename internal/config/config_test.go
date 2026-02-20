package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "unix ok", cfg: Config{Mode: ModeUnix, UnixSocket: "/tmp/rt.sock"}},
		{name: "http ok", cfg: Config{Mode: ModeHTTP, HTTPURL: "http://127.0.0.1/RPC2"}},
		{name: "missing socket", cfg: Config{Mode: ModeUnix}, wantErr: true},
		{name: "missing url", cfg: Config{Mode: ModeHTTP}, wantErr: true},
		{name: "invalid mode", cfg: Config{Mode: "tcp"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
