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

	require.Equal(t, v1.ReplicaSelectionReadiness, config.Strategy)
	require.False(t, config.SelectsOnWalPosition())
	require.Zero(t, config.PrimaryStabilityWindow)
	require.Zero(t, config.MinHealthyReplicas)
	require.Zero(t, config.MaxReplicationLagBytes,
		"the lag limit only applies under the WalPosition strategy")
	require.True(t, config.FallbackToReadiness)
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestWalPositionStrategyDefaults(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		ReplicaSelection: &v1.ReplicaSelectionConfig{Strategy: v1.ReplicaSelectionWalPosition},
	})

	require.True(t, config.SelectsOnWalPosition())
	require.Equal(t, int64(16*1024*1024), config.MaxReplicationLagBytes, "one WAL segment")
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
	require.True(t, config.FallbackToReadiness)
	require.False(t, config.RequireStreaming)
	require.False(t, config.AllowUnsafeManualPromotion)
}

// TestAnEmptyStrategyFallsBackToReadiness covers a replicaSelection block written without a
// strategy - tuning the limits but never opting in to the strategy that uses them.
func TestAnEmptyStrategyFallsBackToReadiness(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		ReplicaSelection: &v1.ReplicaSelectionConfig{},
	})

	require.Equal(t, v1.ReplicaSelectionReadiness, config.Strategy)
	require.False(t, config.SelectsOnWalPosition())
}

func TestExplicitReplicaSelectionValuesAreHonoured(t *testing.T) {
	maxLag := resource.MustParse("64Mi")
	fallback := false

	config := failover.ResolveConfig(v1.KubegresFailover{
		ReplicaSelection: &v1.ReplicaSelectionConfig{
			Strategy:                   v1.ReplicaSelectionWalPosition,
			MaxReplicationLag:          &maxLag,
			HealthCheckTimeout:         &metav1.Duration{Duration: 12 * time.Second},
			FallbackToReadiness:        &fallback,
			RequireStreamingReplica:    true,
			AllowUnsafeManualPromotion: true,
		},
	})

	require.Equal(t, int64(64*1024*1024), config.MaxReplicationLagBytes)
	require.Equal(t, 12*time.Second, config.HealthCheckTimeout)
	require.False(t, config.FallbackToReadiness)
	require.True(t, config.RequireStreaming)
	require.True(t, config.AllowUnsafeManualPromotion)
}

func TestAnExplicitZeroLagMeansNoCeiling(t *testing.T) {
	// Unset takes the default. An explicit zero means the user wants no limit at all.
	noCeiling := resource.MustParse("0")

	config := failover.ResolveConfig(v1.KubegresFailover{
		ReplicaSelection: &v1.ReplicaSelectionConfig{
			Strategy:          v1.ReplicaSelectionWalPosition,
			MaxReplicationLag: &noCeiling,
		},
	})

	require.Zero(t, config.MaxReplicationLagBytes)
}

func TestZeroDurationsAndCountsFallBackToTheDefaults(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		PrimaryStabilityWindow: &metav1.Duration{},
		MinHealthyReplicas:     ptr(int32(0)),
		ReplicaSelection: &v1.ReplicaSelectionConfig{
			Strategy:           v1.ReplicaSelectionWalPosition,
			HealthCheckTimeout: &metav1.Duration{},
		},
	})

	require.Zero(t, config.PrimaryStabilityWindow)
	require.Zero(t, config.MinHealthyReplicas)
	require.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestStabilityWindowAndReplicaGateAreIndependentOfTheStrategy(t *testing.T) {
	// Both gates are useful under either strategy, so they sit beside replicaSelection rather
	// than inside it.
	config := failover.ResolveConfig(v1.KubegresFailover{
		PrimaryStabilityWindow: &metav1.Duration{Duration: 45 * time.Second},
		MinHealthyReplicas:     ptr(int32(2)),
	})

	require.False(t, config.SelectsOnWalPosition())
	require.Equal(t, 45*time.Second, config.PrimaryStabilityWindow)
	require.Equal(t, int32(2), config.MinHealthyReplicas)
}

func TestSelectionConstraintsProjectTheConfig(t *testing.T) {
	config := failover.ResolveConfig(v1.KubegresFailover{
		ReplicaSelection: &v1.ReplicaSelectionConfig{
			Strategy:                v1.ReplicaSelectionWalPosition,
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
