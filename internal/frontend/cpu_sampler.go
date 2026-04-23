package frontend

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// cpuSampleInterval is the cadence at which /proc/stat is sampled in the
// background. Two consecutive samples at this interval are used to compute
// a usage percentage.
const cpuSampleInterval = 2 * time.Second

// cpuSampler runs a background goroutine that samples /proc/stat and
// publishes the last computed usage percentage. Reads return the most
// recent value without blocking, so request handlers no longer have to
// pay a 200ms sleep on the hot path.
//
// The percentage is stored as percent*100 in an atomic.Int64 (fixed-point)
// so it is safe to read from any goroutine and portable on 32-bit
// platforms.
type cpuSampler struct {
	value atomic.Int64
}

// newCPUSampler constructs a cpuSampler. Call Start to begin background
// sampling.
func newCPUSampler() *cpuSampler {
	return &cpuSampler{}
}

// Start launches the background sampling goroutine. The goroutine exits
// when ctx is canceled. Start must be called at most once.
func (c *cpuSampler) Start(ctx context.Context) {
	go c.loop(ctx)
}

func (c *cpuSampler) loop(ctx context.Context) {
	prevIdle, prevTotal := readCPUStat()

	t := time.NewTicker(cpuSampleInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			idle, total := readCPUStat()
			if total > prevTotal {
				idleDelta := float64(idle - prevIdle)
				totalDelta := float64(total - prevTotal)

				usage := (1.0 - idleDelta/totalDelta) * 100
				if usage < 0 {
					usage = 0
				}

				c.value.Store(int64(math.Round(usage * 100)))
			}

			prevIdle, prevTotal = idle, total
		}
	}
}

// Get returns the last sampled CPU usage percentage, rounded to two
// decimal places. Returns 0 if no sample has been produced yet, and is
// safe to call on a nil receiver so tests that build a minimal Server
// without a running sampler can still invoke handlers.
func (c *cpuSampler) Get() float64 {
	if c == nil {
		return 0
	}

	return float64(c.value.Load()) / 100
}

// readCPUStat reads /proc/stat and returns the idle jiffies and total
// jiffies from the aggregate "cpu " line. Returns (0,0) on any read or
// parse error so the caller can skip the sample.
func readCPUStat() (idle, total int64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0
		}

		var sum int64

		for _, f := range fields[1:] {
			v, _ := strconv.ParseInt(f, 10, 64)
			sum += v
		}

		idleVal, _ := strconv.ParseInt(fields[4], 10, 64)

		return idleVal, sum
	}

	return 0, 0
}
