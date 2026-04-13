package device

import "testing"

func TestIsLikelySCODeviceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "sco token", in: "SCO (CVSD)", want: true},
		{name: "hfp token", in: "BlueALSA HFP Audio Gateway", want: true},
		{name: "hsp token", in: "Headset HSP Device", want: true},
		{name: "handsfree token", in: "Handsfree AG", want: true},
		{name: "bluealsa token", in: "bluealsa:DEV=AA:BB,PROFILE=sco", want: true},
		{name: "non sco device", in: "bcm2835 Headphones", want: false},
		{name: "empty", in: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isLikelySCODeviceName(tt.in); got != tt.want {
				t.Fatalf("isLikelySCODeviceName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
