package failover_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/spec/enforcer/resources_count_spec/statefulset/failover"
)

func TestAnEmptySpecKeepsEveryGateOff(t *testing.T) {
	// A Kubegres resource written before these fields existed must behave exactly as it did.
	config := failover.ResolveConfig(v1.KubegresFailover{})

	require.False(t, config.IntelligentFailoverEnabled)
	require.Zero(t, config.PrimaryStabilityWindow)
	require.Zero(t, config.MinHealthyReplicas)
	require.Zero(t, config.MaxReplicationLagBytes,
		"the lag limit only applies once WAL-aware selection is on")
	require.True(t, config.FallbackToLegacy)
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestIntelligentFailoverDefaults(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		IntelligentFailover: &v1.IntelligentFailoverConfig{Enabled: true},
	})

	require.True(t, config.IntelligentFailoverEnabled)
	require.Equal(t, int64(16*1024*1024), config.MaxReplicationLagBytes, "one WAL segment")
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
	require.True(t, config.FallbackToLegacy)
	require.False(t, config.RequireStreaming)
	require.False(t, config.AllowUnsafeManualPromotion)
}

func TestExplicitIntelligentFailoverValuesAreHonoured(t *testing.T) {
	maxLag := resource.MustParse("64Mi")
	fallback := false

	config := failover.ResolveConfig(v1.KubegresFailover{
		IntelligentFailover: &v1.IntelligentFailoverConfig{
			Enabled:                    true,
			MaxReplicationLag:          &maxLag,
			HealthCheckTimeout:         &metav1.Duration{Duration: 12 * time.Second},
			FallbackToLegacy:           &fallback,
			RequireStreamingReplica:    true,
			AllowUnsafeManualPromotion: true,
		},
	})

	require.Equal(t, int64(64*1024*1024), config.MaxReplicationLagBytes)
	require.Equal(t, 12*time.Second, config.HealthCheckTimeout)
	require.False(t, config.FallbackToLegacy)
	require.True(t, config.RequireStreaming)
	require.True(t, config.AllowUnsafeManualPromotion)
}

func TestAnExplicitZeroLagMeansNoCeiling(t *testing.T) {
	// Unset takes the default. An explicit zero means the user wants no limit at all.
	noCeiling := resource.MustParse("0")

	config := failover.ResolveConfig(v1.KubegresFailover{
		IntelligentFailover: &v1.IntelligentFailoverConfig{
			Enabled:           true,
			MaxReplicationLag: &noCeiling,
		},
	})

	require.Zero(t, config.MaxReplicationLagBytes)
}

func TestZeroDurationsAndCountsFallBackToTheDefaults(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		PrimaryStabilityWindow: &metav1.Duration{},
		MinHealthyReplicas:     ptr(int32(0)),
		IntelligentFailover: &v1.IntelligentFailoverConfig{
			Enabled:            true,
			HealthCheckTimeout: &metav1.Duration{},
		},
	})

	require.Zero(t, config.PrimaryStabilityWindow)
	require.Zero(t, config.MinHealthyReplicas)
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestStabilityWindowAndReplicaGateAreIndependentOfTheFeatureFlag(t *testing.T) {
	// Both gates are useful without WAL-aware selection, so they sit beside it, not under it.
	config := failover.ResolveConfig(v1.KubegresFailover{
		PrimaryStabilityWindow: &metav1.Duration{Duration: 45 * time.Second},
		MinHealthyReplicas:     ptr(int32(2)),
	})

	require.False(t, config.IntelligentFailoverEnabled)
	require.Equal(t, 45*time.Second, config.PrimaryStabilityWindow)
	require.Equal(t, int32(2), config.MinHealthyReplicas)
}

func TestSelectionConstraintsProjectTheConfig(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		IntelligentFailover: &v1.IntelligentFailoverConfig{
			Enabled:                 true,
			RequireStreamingReplica: true,
		},
	})

	constraints := config.SelectionConstraints(42)

	require.Equal(t, int64(16*1024*1024), constraints.MaxReplicationLagBytes)
	require.True(t, constraints.RequireStreaming)
	require.EqualValues(t, 42, constraints.ReferenceLSN)
}

func ptr[T any](value T) *T {
	return &value
}
