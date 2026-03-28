package batmanadv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOriginatorCount_Unique(t *testing.T) {
	originators := []Originator{
		{OrigAddress: "aa:bb:cc:dd:ee:01", HardIfname: "phy1-ap0"},
		{OrigAddress: "aa:bb:cc:dd:ee:02", HardIfname: "phy1-ap0"},
		{OrigAddress: "aa:bb:cc:dd:ee:01", HardIfname: "phy1-mesh0"}, // duplicate orig
		{OrigAddress: "aa:bb:cc:dd:ee:03", HardIfname: "phy1-ap0"},
	}
	assert.Equal(t, 3, GetOriginatorCount(originators))
}

func TestGetOriginatorCount_Empty(t *testing.T) {
	assert.Equal(t, 0, GetOriginatorCount(nil))
	assert.Equal(t, 0, GetOriginatorCount([]Originator{}))
}

func TestGetOriginatorCount_Single(t *testing.T) {
	originators := []Originator{
		{OrigAddress: "aa:bb:cc:dd:ee:01"},
	}
	assert.Equal(t, 1, GetOriginatorCount(originators))
}
