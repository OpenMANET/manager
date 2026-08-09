package util

import (
	"testing"
)

func TestInterfaceWithoutBridge(t *testing.T) {
	tests := []struct {
		name      string
		iface     string
		want      string
		wantError bool
	}{
		{
			name:      "bridge interface with br- prefix",
			iface:     "br-eth0",
			want:      "eth0",
			wantError: false,
		},
		{
			name:      "bridge interface with br- prefix and multiple parts",
			iface:     "br-wlh0",
			want:      "wlh0",
			wantError: false,
		},
		{
			name:      "non-bridge interface",
			iface:     "eth0",
			want:      "eth0",
			wantError: true,
		},
		{
			name:      "interface with br in name but not prefix",
			iface:     "eth0-br",
			want:      "eth0-br",
			wantError: true,
		},
		{
			name:      "empty string",
			iface:     "",
			want:      "",
			wantError: true,
		},
		{
			name:      "only br- prefix",
			iface:     "br-",
			want:      "",
			wantError: false,
		},
		{
			name:      "br prefix without dash",
			iface:     "br0",
			want:      "br0",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InterfaceWithoutBridge(tt.iface)
			if (err != nil) != tt.wantError {
				t.Errorf("InterfaceWithoutBridge() error = %v, wantError %v", err, tt.wantError)

				return
			}

			if got != tt.want {
				t.Errorf("InterfaceWithoutBridge() = %v, want %v", got, tt.want)
			}
		})
	}
}
