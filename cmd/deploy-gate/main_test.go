package main

import (
	"testing"
	"time"
)

func TestParseShutdownTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "default when empty", value: "", want: defaultShutdownTimeout},
		{name: "seconds", value: "45s", want: 45 * time.Second},
		{name: "minutes", value: "5m", want: 5 * time.Minute},
		{name: "invalid", value: "soon", wantErr: true},
		{name: "zero", value: "0s", wantErr: true},
		{name: "negative", value: "-1s", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseShutdownTimeout(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
