package bwe

import (
	"time"
)

// REMBSchedulerConfig configures REMB packet scheduling.
type REMBSchedulerConfig struct {
	// Interval is the regular REMB send interval (default: 1 second).
	Interval time.Duration

	// DecreaseThreshold is the minimum relative decrease to trigger immediate REMB.
	// Default: 0.03 (3% decrease triggers immediate send).
	DecreaseThreshold float64

	// SenderSSRC is the SSRC to use in REMB packets (receiver's SSRC).
	SenderSSRC uint32
}

// DefaultREMBSchedulerConfig returns default scheduler configuration.
func DefaultREMBSchedulerConfig() REMBSchedulerConfig {
	return REMBSchedulerConfig{
		Interval:          time.Second,
		DecreaseThreshold: 0.03, // 3%
		SenderSSRC:        0,    // Will be set by transport
	}
}

// REMBScheduler manages REMB packet timing.
// It sends REMB at regular intervals and immediately on significant decreases.
type REMBScheduler struct {
	config    REMBSchedulerConfig
	lastSent  time.Time
	lastValue int64
}

// NewREMBScheduler creates a new REMB scheduler.
func NewREMBScheduler(config REMBSchedulerConfig) *REMBScheduler {
	return &REMBScheduler{
		config: config,
	}
}

// ShouldSendREMB determines if a REMB packet should be sent now.
// Returns true if either:
//   - Regular interval has elapsed since last send
//   - Estimate decreased by >= DecreaseThreshold (e.g., 3%)
//
// Parameters:
//   - estimate: Current bandwidth estimate in bits per second
//   - now: Current time
func (s *REMBScheduler) ShouldSendREMB(estimate int64, now time.Time) bool {
	// Check for immediate decrease trigger
	if s.lastValue > 0 {
		decrease := float64(s.lastValue-estimate) / float64(s.lastValue)
		if decrease >= s.config.DecreaseThreshold {
			return true
		}
	}

	// Check for regular interval
	if s.lastSent.IsZero() || now.Sub(s.lastSent) >= s.config.Interval {
		return true
	}

	return false
}

// BuildAndRecordREMB creates a REMB packet and records the send.
// Call this after ShouldSendREMB returns true.
//
// Parameters:
//   - estimate: Bandwidth estimate in bits per second
//   - ssrcs: Media SSRCs the estimate applies to
//   - now: Current time
//
// Returns the marshaled REMB packet bytes.
func (s *REMBScheduler) BuildAndRecordREMB(estimate int64, ssrcs []uint32, now time.Time) ([]byte, error) {
	data, err := BuildREMB(s.config.SenderSSRC, uint64(estimate), ssrcs)
	if err != nil {
		return nil, err
	}

	s.lastSent = now
	s.lastValue = estimate
	return data, nil
}

// MaybeSendREMB combines ShouldSendREMB and BuildAndRecordREMB.
// Returns (packet, true) if REMB should be sent, (nil, false) otherwise.
//
// This is the primary API for the scheduler.
func (s *REMBScheduler) MaybeSendREMB(estimate int64, ssrcs []uint32, now time.Time) ([]byte, bool, error) {
	if !s.ShouldSendREMB(estimate, now) {
		return nil, false, nil
	}

	data, err := s.BuildAndRecordREMB(estimate, ssrcs, now)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// LastSentValue returns the last estimate value that was sent in a REMB.
// Returns 0 if no REMB has been sent yet.
func (s *REMBScheduler) LastSentValue() int64 {
	return s.lastValue
}

// LastSentTime returns when the last REMB was sent.
// Returns zero time if no REMB has been sent yet.
func (s *REMBScheduler) LastSentTime() time.Time {
	return s.lastSent
}

// Reset clears scheduler state (last sent time and value).
func (s *REMBScheduler) Reset() {
	s.lastSent = time.Time{}
	s.lastValue = 0
}
