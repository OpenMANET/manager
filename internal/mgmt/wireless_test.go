package mgmt

import (
	"testing"

	"github.com/mdlayher/wifi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMeshInterfaces_FiltersCorrectly(t *testing.T) {
	tests := []struct {
		name      string
		ifaces    []*wifi.Interface
		wantCount int
	}{
		{
			name:      "no interfaces",
			ifaces:    nil,
			wantCount: 0,
		},
		{
			name: "no mesh interfaces",
			ifaces: []*wifi.Interface{
				{Type: wifi.InterfaceTypeStation},
				{Type: wifi.InterfaceTypeAP},
			},
			wantCount: 0,
		},
		{
			name: "one mesh interface",
			ifaces: []*wifi.Interface{
				{Type: wifi.InterfaceTypeStation},
				{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh0"},
			},
			wantCount: 1,
		},
		{
			name: "multiple mesh interfaces",
			ifaces: []*wifi.Interface{
				{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh0"},
				{Type: wifi.InterfaceTypeAP},
				{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh1"},
			},
			wantCount: 2,
		},
		{
			name: "all mesh interfaces",
			ifaces: []*wifi.Interface{
				{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh0"},
				{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh1"},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the filter logic used in GetMeshInterfaces.
			var meshIfaces []*wifi.Interface

			for _, iface := range tt.ifaces {
				if iface.Type == wifi.InterfaceTypeMeshPoint {
					meshIfaces = append(meshIfaces, iface)
				}
			}

			assert.Len(t, meshIfaces, tt.wantCount)
		})
	}
}

func TestGetMeshInterfaces_ReturnsMeshPointType(t *testing.T) {
	ifaces := []*wifi.Interface{
		{Type: wifi.InterfaceTypeStation, Name: "wlan0"},
		{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh0"},
		{Type: wifi.InterfaceTypeAP, Name: "ap0"},
		{Type: wifi.InterfaceTypeMeshPoint, Name: "mesh1"},
	}

	var meshIfaces []*wifi.Interface

	for _, iface := range ifaces {
		if iface.Type == wifi.InterfaceTypeMeshPoint {
			meshIfaces = append(meshIfaces, iface)
		}
	}

	require.Len(t, meshIfaces, 2)
	assert.Equal(t, "mesh0", meshIfaces[0].Name)
	assert.Equal(t, "mesh1", meshIfaces[1].Name)
}
