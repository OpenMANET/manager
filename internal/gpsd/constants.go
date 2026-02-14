package gpsd

import "time"

const (
	DefaultTAKGPSPort  string = "4349"
	DefaultGPSDAddress string = "localhost:2947"
	ATAKSAAddress      string = "239.2.3.1:6969" // ATAK Situational Awareness multicast address
	// atakMulticastTTL is the Time-To-Live value for CoT multicast packets sent to ATAK SA address
	atakMulticastTTL int = 64
	// defaultSelfMarkerType is the CoT type for self markers
	// defaultSelfMarkerType string = "a-f-G-U-C" // SELF MARKER
	// radioUnitType is the CoT type for a ground radio unit
	radioUnitType string = "a-f-G-U-U-S-R" // Gnd/RADIO UNIT;RADIO UNIT
	// defaultStaleDuration is the default duration before a CoT message is considered stale
	defaultStaleDuration time.Duration = 5 * time.Minute
	maxReconnectAttempts int           = 3
	// cotMulticastRateLimit is the minimum interval between CoT multicast sends to avoid flooding
	cotMulticastRateLimit time.Duration = 30 * time.Second // Minimum interval between CoT multicast sends
)
