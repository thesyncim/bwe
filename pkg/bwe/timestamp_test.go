package bwe

import (
	"testing"
	"time"
)

func TestParseAbsSendTime(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    uint32
		wantErr bool
	}{
		{
			name: "minimum value",
			data: []byte{0x00, 0x00, 0x01},
			want: 1,
		},
		{
			name: "zero value",
			data: []byte{0x00, 0x00, 0x00},
			want: 0,
		},
		{
			name: "maximum value",
			data: []byte{0xFF, 0xFF, 0xFF},
			want: 16777215, // 2^24 - 1
		},
		{
			name: "mid-range value",
			data: []byte{0x80, 0x00, 0x00},
			want: 8388608, // 2^23 = half of max
		},
		{
			name: "one second (1 << 18)",
			data: []byte{0x04, 0x00, 0x00},
			want: 262144, // 1 << 18 = 1 second in 6.18 format
		},
		{
			name: "extra bytes ignored",
			data: []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			want: 66051, // 0x010203
		},
		{
			name:    "too short - 2 bytes",
			data:    []byte{0x01, 0x02},
			wantErr: true,
		},
		{
			name:    "too short - 1 byte",
			data:    []byte{0x01},
			wantErr: true,
		},
		{
			name:    "empty input",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "nil input",
			data:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAbsSendTime(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAbsSendTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseAbsSendTime() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAbsSendTimeToDuration(t *testing.T) {
	tests := []struct {
		name  string
		value uint32
		want  time.Duration
	}{
		{
			name:  "zero",
			value: 0,
			want:  0,
		},
		{
			name:  "one second (1 << 18)",
			value: 262144, // 1 << 18
			want:  time.Second,
		},
		{
			name:  "half second",
			value: 131072, // 1 << 17
			want:  500 * time.Millisecond,
		},
		{
			name:  "quarter second",
			value: 65536, // 1 << 16
			want:  250 * time.Millisecond,
		},
		{
			name:  "64 seconds (full range)",
			value: 16777216, // 1 << 24
			want:  64 * time.Second,
		},
		{
			name:  "10 seconds",
			value: 2621440, // 10 * (1 << 18)
			want:  10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AbsSendTimeToDuration(tt.value)
			// Allow small floating point tolerance
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Microsecond {
				t.Errorf("AbsSendTimeToDuration(%d) = %v, want %v (diff: %v)", tt.value, got, tt.want, diff)
			}
		})
	}
}

func TestUnwrapAbsSendTime(t *testing.T) {
	tests := []struct {
		name string
		prev uint32
		curr uint32
		want int64
	}{
		{
			name: "no wraparound - forward",
			prev: 1000,
			curr: 2000,
			want: 1000,
		},
		{
			name: "no wraparound - backward",
			prev: 2000,
			curr: 1000,
			want: -1000,
		},
		{
			name: "no change",
			prev: 5000,
			curr: 5000,
			want: 0,
		},
		{
			name: "wraparound forward",
			// prev near max (64s - small delta), curr near zero
			// Real scenario: timestamps 16777000 -> 200
			// Raw diff: 200 - 16777000 = -16776800 (large negative)
			// But we're actually moving forward by: 16777216 - 16777000 + 200 = 416 units
			prev: 16777000,
			curr: 200,
			want: 416, // Small positive delta (wrapped forward)
		},
		{
			name: "wraparound backward",
			// prev near zero, curr near max
			// Real scenario: timestamps 200 -> 16777000
			// Raw diff: 16777000 - 200 = 16776800 (large positive)
			// But we're actually moving backward by the same amount
			prev: 200,
			curr: 16777000,
			want: -416, // Small negative delta (wrapped backward)
		},
		{
			name: "exactly at boundary",
			prev: 16777215, // max value
			curr: 0,
			want: 1, // Just crossed the boundary forward
		},
		{
			name: "cross boundary backward",
			prev: 0,
			curr: 16777215,
			want: -1, // Just crossed the boundary backward
		},
		{
			name: "large forward within half range",
			prev: 0,
			curr: 8388607, // Just under half range
			want: 8388607,
		},
		{
			name: "large backward within half range",
			prev: 8388607,
			curr: 0,
			want: -8388607,
		},
		{
			name: "exactly half range forward",
			prev: 0,
			curr: 8388608, // Exactly half range
			want: 8388608,
		},
		{
			name: "just over half range - interpreted as backward wrap",
			prev: 0,
			curr: 8388609,                      // Just over half range
			want: 8388609 - int64(AbsSendTimeMax), // Negative (backward wrap)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnwrapAbsSendTime(tt.prev, tt.curr)
			if got != tt.want {
				t.Errorf("UnwrapAbsSendTime(%d, %d) = %d, want %d", tt.prev, tt.curr, got, tt.want)
			}
		})
	}
}

func TestUnwrapAbsSendTimeDuration(t *testing.T) {
	tests := []struct {
		name string
		prev uint32
		curr uint32
		want time.Duration
	}{
		{
			name: "one second forward",
			prev: 0,
			curr: 262144, // 1 << 18 = 1 second
			want: time.Second,
		},
		{
			name: "one second backward",
			prev: 262144,
			curr: 0,
			want: -time.Second,
		},
		{
			name: "wraparound small forward jump",
			// From near the end to near the start
			// ~63.99 seconds to ~0.001 seconds
			prev: 16776192, // ~63.996 seconds
			curr: 1024,     // ~0.004 seconds
			want: time.Duration(float64((1024+16777216)-16776192) * AbsSendTimeResolution * float64(time.Second)),
		},
		{
			name: "100ms forward",
			prev: 0,
			curr: 26214, // ~100ms in abs-send-time units
			want: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnwrapAbsSendTimeDuration(tt.prev, tt.curr)
			// Allow small floating point tolerance (1ms)
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Millisecond {
				t.Errorf("UnwrapAbsSendTimeDuration(%d, %d) = %v, want %v (diff: %v)", tt.prev, tt.curr, got, tt.want, diff)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Verify the constants are correct
	if AbsSendTimeMax != 1<<24 {
		t.Errorf("AbsSendTimeMax = %d, want %d", AbsSendTimeMax, 1<<24)
	}

	// Verify resolution: 1 << 18 units should equal 1 second
	oneSecondUnits := uint32(1 << 18)
	oneSecond := AbsSendTimeToDuration(oneSecondUnits)
	if oneSecond != time.Second {
		t.Errorf("1 << 18 units = %v, want 1s", oneSecond)
	}

	// Verify that max value equals 64 seconds
	maxDuration := AbsSendTimeToDuration(AbsSendTimeMax)
	expected := 64 * time.Second
	diff := maxDuration - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("Max duration = %v, want %v", maxDuration, expected)
	}
}
