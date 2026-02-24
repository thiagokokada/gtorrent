package config

import (
	"os"
	"testing"
)

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

func TestEnvBoolOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		setValue *string
		fallback bool
		want     bool
	}{
		{name: "missing uses fallback true", setValue: nil, fallback: true, want: true},
		{name: "missing uses fallback false", setValue: nil, fallback: false, want: false},
		{name: "valid true", setValue: strPtr("true"), fallback: false, want: true},
		{name: "valid false", setValue: strPtr("false"), fallback: true, want: false},
		{name: "invalid uses fallback", setValue: strPtr("banana"), fallback: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const key = "GTORRENT_TEST_BOOL"
			old, hadOld := os.LookupEnv(key)
			t.Cleanup(func() {
				if hadOld {
					_ = os.Setenv(key, old)
				} else {
					_ = os.Unsetenv(key)
				}
			})

			if tc.setValue == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *tc.setValue)
			}

			if got := envBoolOrDefault(key, tc.fallback); got != tc.want {
				t.Fatalf("envBoolOrDefault() = %v, want %v", got, tc.want)
			}
		})
	}
}

func strPtr(v string) *string {
	return &v
}
