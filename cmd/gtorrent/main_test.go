package main

import "testing"

func TestBrowserURL(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		want       string
	}{
		{name: "port only", listenAddr: ":8080", want: "http://127.0.0.1:8080"},
		{name: "localhost", listenAddr: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "all interfaces v4", listenAddr: "0.0.0.0:8080", want: "http://127.0.0.1:8080"},
		{name: "all interfaces v6", listenAddr: "[::]:8080", want: "http://127.0.0.1:8080"},
		{name: "named host", listenAddr: "localhost:8080", want: "http://localhost:8080"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := browserURL(tc.listenAddr); got != tc.want {
				t.Fatalf("browserURL(%q) = %q, want %q", tc.listenAddr, got, tc.want)
			}
		})
	}
}

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		wantName  string
		wantArgs  []string
		wantError bool
	}{
		{name: "linux", goos: "linux", wantName: "xdg-open", wantArgs: []string{"http://localhost:8080"}},
		{name: "darwin", goos: "darwin", wantName: "open", wantArgs: []string{"http://localhost:8080"}},
		{name: "windows", goos: "windows", wantName: "rundll32", wantArgs: []string{"url.dll,FileProtocolHandler", "http://localhost:8080"}},
		{name: "unsupported", goos: "plan9", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args, err := browserCommand(tc.goos, "http://localhost:8080")
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName {
				t.Fatalf("name = %q, want %q", name, tc.wantName)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("len(args) = %d, want %d", len(args), len(tc.wantArgs))
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d] = %q, want %q", i, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}
