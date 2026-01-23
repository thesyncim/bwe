package interceptor

import (
	"errors"
	"time"

	"github.com/pion/interceptor"

	"bwe/pkg/bwe"
)

// FactoryOption configures the BWEInterceptorFactory.
type FactoryOption func(*BWEInterceptorFactory) error

// BWEInterceptorFactory creates BWEInterceptor instances for each PeerConnection.
// Register this factory with the interceptor registry to enable receiver-side
// bandwidth estimation.
type BWEInterceptorFactory struct {
	config       bwe.BandwidthEstimatorConfig
	rembInterval time.Duration
	senderSSRC   uint32
	onREMB       func(bitrate float32, ssrcs []uint32)
}

// WithInitialBitrate sets the initial bandwidth estimate.
// Default: 300000 (300 kbps)
func WithInitialBitrate(bitrate int64) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		f.config.RateControllerConfig.InitialBitrate = bitrate
		return nil
	}
}

// WithMinBitrate sets the minimum bandwidth estimate.
// Default: 10000 (10 kbps)
func WithMinBitrate(bitrate int64) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		f.config.RateControllerConfig.MinBitrate = bitrate
		return nil
	}
}

// WithMaxBitrate sets the maximum bandwidth estimate.
// Default: 50000000 (50 Mbps)
func WithMaxBitrate(bitrate int64) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		f.config.RateControllerConfig.MaxBitrate = bitrate
		return nil
	}
}

// WithFactoryREMBInterval sets how often REMB packets are sent.
// Default: 1 second
func WithFactoryREMBInterval(interval time.Duration) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		if interval <= 0 {
			return errors.New("REMB interval must be positive")
		}
		f.rembInterval = interval
		return nil
	}
}

// WithFactorySenderSSRC sets the sender SSRC for REMB packets.
// This identifies the receiver generating the REMB.
// Default: 0 (many implementations use 0)
func WithFactorySenderSSRC(ssrc uint32) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		f.senderSSRC = ssrc
		return nil
	}
}

// WithFactoryOnREMB sets a callback that is invoked each time a REMB packet is sent.
// The callback receives the bitrate estimate and the SSRCs included in the REMB.
func WithFactoryOnREMB(fn func(bitrate float32, ssrcs []uint32)) FactoryOption {
	return func(f *BWEInterceptorFactory) error {
		f.onREMB = fn
		return nil
	}
}

// NewBWEInterceptorFactory creates a new factory for BWEInterceptor instances.
// Configure the factory using FactoryOption functions.
//
// Example:
//
//	factory, err := NewBWEInterceptorFactory(
//	    WithInitialBitrate(500000),
//	    WithFactoryREMBInterval(500*time.Millisecond),
//	)
//	if err != nil {
//	    return err
//	}
//	registry.Add(factory)
func NewBWEInterceptorFactory(opts ...FactoryOption) (*BWEInterceptorFactory, error) {
	f := &BWEInterceptorFactory{
		config:       bwe.DefaultBandwidthEstimatorConfig(),
		rembInterval: time.Second,
		senderSSRC:   0,
	}
	for _, opt := range opts {
		if err := opt(f); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// NewInterceptor creates a new BWEInterceptor for a PeerConnection.
// This method is called by the interceptor registry when setting up a connection.
func (f *BWEInterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	// Create a new BandwidthEstimator with factory config
	estimator := bwe.NewBandwidthEstimator(f.config, nil)

	// Build options list
	opts := []InterceptorOption{
		WithREMBInterval(f.rembInterval),
		WithSenderSSRC(f.senderSSRC),
	}
	if f.onREMB != nil {
		opts = append(opts, WithOnREMB(f.onREMB))
	}

	// Create interceptor with configured options
	i := NewBWEInterceptor(estimator, opts...)

	return i, nil
}
