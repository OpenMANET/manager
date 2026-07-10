package openmanet

import (
	"testing"

	"github.com/openmanet/openmanetd/internal/util/board"
)

func TestResolveGOMAXPROCS(t *testing.T) {
	tests := []struct {
		name   string
		cfgVal int
		prof   board.ExecutionProfile
		want   int
	}{
		{name: "explicit config wins over board", cfgVal: 4, prof: board.ExecutionProfile{GOMAXPROCS: 2}, want: 4},
		{name: "explicit config on board with no profile", cfgVal: 3, prof: board.ExecutionProfile{}, want: 3},
		{name: "board default when config auto", cfgVal: 0, prof: board.ExecutionProfile{GOMAXPROCS: 2}, want: 2},
		{name: "no override when both zero", cfgVal: 0, prof: board.ExecutionProfile{}, want: 0},
		{name: "negative config treated as auto, board applies", cfgVal: -1, prof: board.ExecutionProfile{GOMAXPROCS: 2}, want: 2},
		{name: "negative config, no board profile", cfgVal: -5, prof: board.ExecutionProfile{}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveGOMAXPROCS(tt.cfgVal, tt.prof)
			if got != tt.want {
				t.Errorf("resolveGOMAXPROCS(%d, %+v) = %d, want %d", tt.cfgVal, tt.prof, got, tt.want)
			}
		})
	}
}
