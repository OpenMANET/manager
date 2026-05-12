package audiopool

import (
	"math"
	"testing"
)

func TestPoolDefaults(t *testing.T) {
	fp, ok := Float32Pool.Get().(*[]float32)
	if !ok {
		t.Fatal("Float32Pool.Get did not return *[]float32")
	}

	if got := len(*fp); got != FrameSize {
		t.Errorf("Float32Pool len = %d, want %d", got, FrameSize)
	}

	if got := cap(*fp); got != FrameSize {
		t.Errorf("Float32Pool cap = %d, want %d", got, FrameSize)
	}

	Float32Pool.Put(fp)

	ip, ok := Int16Pool.Get().(*[]int16)
	if !ok {
		t.Fatal("Int16Pool.Get did not return *[]int16")
	}

	if got := len(*ip); got != FrameSize {
		t.Errorf("Int16Pool len = %d, want %d", got, FrameSize)
	}

	if got := cap(*ip); got != FrameSize {
		t.Errorf("Int16Pool cap = %d, want %d", got, FrameSize)
	}

	Int16Pool.Put(ip)

	bp, ok := EncBufPool.Get().(*[]byte)
	if !ok {
		t.Fatal("EncBufPool.Get did not return *[]byte")
	}

	if got := len(*bp); got != EncBufSize {
		t.Errorf("EncBufPool len = %d, want %d", got, EncBufSize)
	}

	EncBufPool.Put(bp)
}

func TestReturnFloat32_RoundTrip(t *testing.T) {
	// Acquire a buffer, mutate it, return it, then acquire again. The pool
	// is a heuristic — Get() may return a fresh buffer — so the strongest
	// invariant we can assert is that ReturnFloat32 does not panic and that
	// any buffer obtained from Get is FrameSize-shaped.
	fp := Float32Pool.Get().(*[]float32) //nolint:forcetypeassert

	f := *fp
	for i := range f {
		f[i] = float32(i)
	}

	ReturnFloat32(f)

	got := Float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	if cap(*got) != FrameSize {
		t.Errorf("recycled cap = %d, want %d", cap(*got), FrameSize)
	}

	Float32Pool.Put(got)
}

func TestReturnFloat32_RejectsWrongCapacity(t *testing.T) {
	// A non-pooled slice (capacity != FrameSize) must be silently dropped
	// rather than poisoning the pool with an undersized buffer.
	short := make([]float32, 10)
	ReturnFloat32(short) // must not panic

	tall := make([]float32, FrameSize+1)
	ReturnFloat32(tall) // must not panic
}

func TestReturnInt16_RoundTrip(t *testing.T) {
	ip := Int16Pool.Get().(*[]int16) //nolint:forcetypeassert

	s := *ip
	for i := range s {
		s[i] = int16(i)
	}

	ReturnInt16(s)

	got := Int16Pool.Get().(*[]int16) //nolint:forcetypeassert
	if cap(*got) != FrameSize {
		t.Errorf("recycled cap = %d, want %d", cap(*got), FrameSize)
	}

	Int16Pool.Put(got)
}

func TestReturnInt16_RejectsWrongCapacity(t *testing.T) {
	short := make([]int16, 10)
	ReturnInt16(short) // must not panic

	tall := make([]int16, FrameSize+1)
	ReturnInt16(tall) // must not panic
}

func TestRMSEnergy(t *testing.T) {
	cases := []struct {
		name  string
		frame []float32
		want  float32
	}{
		{
			name:  "empty",
			frame: nil,
			want:  0,
		},
		{
			name:  "all zeros",
			frame: make([]float32, 64),
			want:  0,
		},
		{
			name:  "constant 0.5",
			frame: constantFrame(64, 0.5),
			want:  0.5,
		},
		{
			name:  "constant negative 0.5",
			frame: constantFrame(64, -0.5),
			want:  0.5,
		},
		{
			name:  "single sample",
			frame: []float32{1.0},
			want:  1.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RMSEnergy(tc.frame)

			diff := float64(got - tc.want)
			if math.Abs(diff) > 1e-6 {
				t.Errorf("RMSEnergy = %v, want %v (diff %v)", got, tc.want, diff)
			}
		})
	}
}

func TestRMSEnergy_Sine(t *testing.T) {
	// A unit-amplitude sine wave has RMS = 1/sqrt(2) ≈ 0.7071.
	const samples = 480 // 10 ms at 48 kHz

	frame := make([]float32, samples)

	for i := range frame {
		frame[i] = float32(math.Sin(2 * math.Pi * float64(i) / float64(samples)))
	}

	got := RMSEnergy(frame)

	want := float32(1.0 / math.Sqrt2)
	if math.Abs(float64(got-want)) > 1e-3 {
		t.Errorf("RMSEnergy(sine) = %v, want ≈ %v", got, want)
	}
}

func constantFrame(n int, v float32) []float32 {
	f := make([]float32, n)
	for i := range f {
		f[i] = v
	}

	return f
}
