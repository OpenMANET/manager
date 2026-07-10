package board

// ExecutionProfile holds recommended Go runtime tuning for a board type.
// A zero-valued field means "no board recommendation — leave the default".
// New per-board runtime knobs are added as additional fields here (e.g. GOGC,
// MemoryLimitBytes) and wired through applyRuntimeTuning with the same
// explicit-config > board-profile > runtime-default precedence.
type ExecutionProfile struct {
	// GOMAXPROCS is the recommended number of OS threads executing Go code.
	// 0 means "leave Go's default" (runtime.NumCPU()).
	GOMAXPROCS int
}

// ExecutionProfileFor returns the recommended execution profile for the given
// board model ID. Unknown or unlisted boards get the zero profile, which
// applies no runtime overrides.
func ExecutionProfileFor(modelID string) ExecutionProfile {
	switch modelID {
	case BCM2711_RAVEN_USB:
		return ExecutionProfile{GOMAXPROCS: 2}
	default:
		return ExecutionProfile{}
	}
}

// CurrentExecutionProfile reads the board configuration and returns the
// execution profile for the running board. On any error reading the board
// configuration it returns the zero profile, so the daemon falls back to Go
// runtime defaults.
func CurrentExecutionProfile() ExecutionProfile {
	info, err := newBoardConfigInfoFn()
	if err != nil {
		return ExecutionProfile{}
	}

	return ExecutionProfileFor(info.Model.ID)
}
