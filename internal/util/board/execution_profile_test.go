package board

import (
	"errors"
	"testing"
)

func TestExecutionProfileFor(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    ExecutionProfile
	}{
		{name: "raven pins gomaxprocs to 2", modelID: BCM2711_RAVEN_USB, want: ExecutionProfile{GOMAXPROCS: 2}},
		{name: "other bcm2711 no override", modelID: BCM2711_MM6108_SPI, want: ExecutionProfile{}},
		{name: "gateworks no override", modelID: GW7400, want: ExecutionProfile{}},
		{name: "unknown board no override", modelID: "acme,unknown-board", want: ExecutionProfile{}},
		{name: "empty id no override", modelID: "", want: ExecutionProfile{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExecutionProfileFor(tt.modelID)
			if got != tt.want {
				t.Errorf("ExecutionProfileFor(%q) = %+v, want %+v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestCurrentExecutionProfile(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    ExecutionProfile
	}{
		{name: "raven", modelID: BCM2711_RAVEN_USB, want: ExecutionProfile{GOMAXPROCS: 2}},
		{name: "non-raven", modelID: GW7400, want: ExecutionProfile{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := newBoardConfigInfoFn
			defer func() { newBoardConfigInfoFn = orig }()

			newBoardConfigInfoFn = func() (*Board, error) {
				return &Board{Model: Model{ID: tt.modelID}}, nil
			}

			got := CurrentExecutionProfile()
			if got != tt.want {
				t.Errorf("CurrentExecutionProfile() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCurrentExecutionProfile_BoardConfigError(t *testing.T) {
	orig := newBoardConfigInfoFn
	defer func() { newBoardConfigInfoFn = orig }()

	newBoardConfigInfoFn = func() (*Board, error) {
		return nil, errors.New("board config not available")
	}

	got := CurrentExecutionProfile()
	if got != (ExecutionProfile{}) {
		t.Errorf("CurrentExecutionProfile() with config error = %+v, want zero profile", got)
	}
}
