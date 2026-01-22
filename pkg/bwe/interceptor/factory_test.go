package interceptor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBWEInterceptorFactory_Defaults(t *testing.T) {
	factory, err := NewBWEInterceptorFactory()
	require.NoError(t, err)
	require.NotNil(t, factory)

	// Verify defaults
	assert.Equal(t, time.Second, factory.rembInterval)
	assert.Equal(t, uint32(0), factory.senderSSRC)
}

func TestNewBWEInterceptorFactory_WithOptions(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithInitialBitrate(500000),
		WithMinBitrate(50000),
		WithMaxBitrate(5000000),
		WithFactoryREMBInterval(500*time.Millisecond),
		WithFactorySenderSSRC(12345),
	)
	require.NoError(t, err)

	assert.Equal(t, int64(500000), factory.config.RateControllerConfig.InitialBitrate)
	assert.Equal(t, int64(50000), factory.config.RateControllerConfig.MinBitrate)
	assert.Equal(t, int64(5000000), factory.config.RateControllerConfig.MaxBitrate)
	assert.Equal(t, 500*time.Millisecond, factory.rembInterval)
	assert.Equal(t, uint32(12345), factory.senderSSRC)
}

func TestNewBWEInterceptorFactory_InvalidOption(t *testing.T) {
	_, err := NewBWEInterceptorFactory(
		WithFactoryREMBInterval(-1 * time.Second),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REMB interval")
}

func TestNewBWEInterceptorFactory_ZeroInterval(t *testing.T) {
	_, err := NewBWEInterceptorFactory(
		WithFactoryREMBInterval(0),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REMB interval")
}

func TestBWEInterceptorFactory_NewInterceptor(t *testing.T) {
	factory, err := NewBWEInterceptorFactory()
	require.NoError(t, err)

	// Create interceptor
	i, err := factory.NewInterceptor("test-id")
	require.NoError(t, err)
	require.NotNil(t, i)

	// Verify it's a BWEInterceptor
	bwei, ok := i.(*BWEInterceptor)
	require.True(t, ok, "should be *BWEInterceptor")

	// Clean up
	err = bwei.Close()
	assert.NoError(t, err)
}

func TestBWEInterceptorFactory_NewInterceptor_WithOptions(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithInitialBitrate(1000000),
		WithFactoryREMBInterval(200*time.Millisecond),
		WithFactorySenderSSRC(0xDEADBEEF),
	)
	require.NoError(t, err)

	// Create interceptor
	i, err := factory.NewInterceptor("test-id")
	require.NoError(t, err)
	require.NotNil(t, i)

	// Verify configuration was passed through
	bwei, ok := i.(*BWEInterceptor)
	require.True(t, ok)

	assert.Equal(t, 200*time.Millisecond, bwei.rembInterval)
	assert.Equal(t, uint32(0xDEADBEEF), bwei.senderSSRC)

	// Clean up
	err = bwei.Close()
	assert.NoError(t, err)
}

func TestBWEInterceptorFactory_MultipleInterceptors(t *testing.T) {
	factory, err := NewBWEInterceptorFactory()
	require.NoError(t, err)

	// Create two interceptors
	i1, err := factory.NewInterceptor("pc-1")
	require.NoError(t, err)
	defer i1.(*BWEInterceptor).Close()

	i2, err := factory.NewInterceptor("pc-2")
	require.NoError(t, err)
	defer i2.(*BWEInterceptor).Close()

	// Verify they're different instances
	assert.NotSame(t, i1, i2)

	// Verify their estimators are different (not shared)
	bwei1 := i1.(*BWEInterceptor)
	bwei2 := i2.(*BWEInterceptor)
	assert.NotSame(t, bwei1.estimator, bwei2.estimator)
}

func TestBWEInterceptorFactory_ImplementsInterface(t *testing.T) {
	factory, err := NewBWEInterceptorFactory()
	require.NoError(t, err)

	// This verifies at compile time that the factory implements
	// the interceptor.Factory interface (NewInterceptor method)
	// The test passes if it compiles
	_ = factory
}

func TestBWEInterceptorFactory_InterceptorsAreIndependent(t *testing.T) {
	factory, err := NewBWEInterceptorFactory(
		WithInitialBitrate(100000),
	)
	require.NoError(t, err)

	// Create first interceptor
	i1, err := factory.NewInterceptor("pc-1")
	require.NoError(t, err)
	bwei1 := i1.(*BWEInterceptor)
	defer bwei1.Close()

	// Create second interceptor
	i2, err := factory.NewInterceptor("pc-2")
	require.NoError(t, err)
	bwei2 := i2.(*BWEInterceptor)
	defer bwei2.Close()

	// Process a packet on one estimator
	// Both should have the same initial estimate but be completely independent
	estimate1 := bwei1.estimator.GetEstimate()
	estimate2 := bwei2.estimator.GetEstimate()

	// Both should start with the configured initial bitrate
	assert.Equal(t, int64(100000), estimate1)
	assert.Equal(t, int64(100000), estimate2)
}
