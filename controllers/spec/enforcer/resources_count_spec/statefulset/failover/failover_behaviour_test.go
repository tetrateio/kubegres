package failover

import (
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	"reactive-tech.io/kubegres/controllers/ctx/status"
	"reactive-tech.io/kubegres/controllers/metrics"
	"reactive-tech.io/kubegres/controllers/states"
	"reactive-tech.io/kubegres/controllers/states/statefulset"
	"reactive-tech.io/kubegres/internal/postgres"
	"reactive-tech.io/kubegres/internal/replicahealth"
)

// fakeProber returns fixed replication state, keyed by instance index, so selection can be
// tested without a PostgreSQL server.
type fakeProber struct {
	health      map[int32]replicahealth.Status
	probeErrs   map[int32]error
	primaryLSN  postgres.LSN
	primaryErr  error
	probedCalls int
}

func (p *fakeProber) ProbeReplicas(replicas []statefulset.StatefulSetWrapper) []Candidate {
	p.probedCalls++

	candidates := make([]Candidate, 0, len(replicas))
	for _, replicaStatefulSet := range replicas {
		candidates = append(candidates, Candidate{
			InstanceIndex:   replicaStatefulSet.InstanceIndex,
			StatefulSetName: replicaStatefulSet.StatefulSet.Name,
			PodName:         replicaStatefulSet.Pod.Pod.Name,
			Health:          p.health[replicaStatefulSet.InstanceIndex],
			ProbeErr:        p.probeErrs[replicaStatefulSet.InstanceIndex],
		})
	}
	return candidates
}

func (p *fakeProber) ProbePrimaryWalPosition() (postgres.LSN, error) {
	return p.primaryLSN, p.primaryErr
}

func streamingAt(t *testing.T, timeline uint32, lsnText string) replicahealth.Status {
	t.Helper()

	parsed, err := postgres.ParseLSN(lsnText)
	require.NoError(t, err)

	return replicahealth.Status{
		InRecovery:         true,
		TimelineID:         timeline,
		ReplayLSN:          parsed,
		ReceiveLSN:         parsed,
		WalReceiverPresent: true,
		WalReceiverStatus:  "streaming",
	}
}

// clusterBuilder builds the smallest KubegresContext and ResourcesStates the failover logic
// reads, with no Kubernetes API server.
type clusterBuilder struct {
	kubegres *v1.Kubegres
	states   states.ResourcesStates
}

func newCluster(t *testing.T) *clusterBuilder {
	t.Helper()

	replicas := int32(3)
	return &clusterBuilder{
		kubegres: &v1.Kubegres{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "default"},
			Spec:       v1.KubegresSpec{Replicas: &replicas},
			Status:     v1.KubegresStatus{EnforcedReplicas: 3},
		},
	}
}

func (b *clusterBuilder) withPrimary(instanceIndex int32, isReady bool) *clusterBuilder {
	wrapper := newStatefulSetWrapper("postgres-primary", instanceIndex, isReady)
	b.states.StatefulSets.Primary = wrapper
	b.states.StatefulSets.All.Add(wrapper)
	return b
}

func (b *clusterBuilder) withoutPrimary() *clusterBuilder {
	b.states.StatefulSets.Primary = statefulset.StatefulSetWrapper{}
	return b
}

func (b *clusterBuilder) withReplica(instanceIndex int32, isReady bool) *clusterBuilder {
	wrapper := newStatefulSetWrapper("postgres-replica", instanceIndex, isReady)
	b.states.StatefulSets.Replicas.All.Add(wrapper)
	b.states.StatefulSets.All.Add(wrapper)
	b.states.StatefulSets.Replicas.NbreDeployed++
	if isReady {
		b.states.StatefulSets.Replicas.NbreReady++
	}
	return b
}

func newStatefulSetWrapper(namePrefix string, instanceIndex int32, isReady bool) statefulset.StatefulSetWrapper {
	name := namePrefix + "-" + metrics.InstanceIndexLabel(instanceIndex)
	return statefulset.StatefulSetWrapper{
		IsDeployed:    true,
		IsReady:       isReady,
		InstanceIndex: instanceIndex,
		StatefulSet:   apps.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}},
		Pod: statefulset.PodWrapper{
			IsDeployed:    true,
			IsReady:       isReady,
			InstanceIndex: instanceIndex,
			Pod:           core.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-0"}},
		},
	}
}

func (b *clusterBuilder) build(t *testing.T, config Config, prober Prober) (PrimaryToReplicaFailOver, *v1.Kubegres) {
	t.Helper()

	logWrapper := log.LogWrapper{
		Kubegres: b.kubegres,
		Logger:   logr.Discard(),
		Recorder: record.NewFakeRecorder(256),
	}

	kubegresContext := ctx.KubegresContext{
		Kubegres: b.kubegres,
		Status:   &status.KubegresStatusWrapper{Kubegres: b.kubegres, Log: logWrapper},
		Ctx:      t.Context(),
		Log:      logWrapper,
	}

	if prober == nil {
		prober = &fakeProber{}
	}

	return CreatePrimaryToReplicaFailOverWithProber(kubegresContext, b.states, nil, config, prober), b.kubegres
}

// ---------------------------------------------------------------------------------------
// Primary stability window
// ---------------------------------------------------------------------------------------

func TestFailoverStartsImmediatelyWhenNoStabilityWindowIsConfigured(t *testing.T) {
	// The default must behave exactly as Kubegres did before the window existed.
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true)
	failOver, kubegres := cluster.build(t, Config{}, nil)

	require.True(t, failOver.ShouldWeFailOver())
	require.Zero(t, kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds,
		"no window means no bookkeeping to persist")
	require.Zero(t, failOver.RequeueAfter())
}

func TestFailoverWaitsOutTheStabilityWindow(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true)
	failOver, kubegres := cluster.build(t, Config{PrimaryStabilityWindow: time.Minute}, nil)

	// The first sighting of an unhealthy Primary only starts the clock.
	require.False(t, failOver.ShouldWeFailOver())
	require.NotZero(t, kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds)

	// A Pod that stays not-ready produces no more events, so we have to look again on a timer.
	require.InDelta(t, time.Minute.Seconds(), failOver.RequeueAfter().Seconds(), 1)

	// Still inside the window on the next reconciliation.
	require.False(t, failOver.ShouldWeFailOver())

	// Once it has been unhealthy for longer than the window, the failover goes ahead.
	kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds = time.Now().Add(-90 * time.Second).Unix()
	require.True(t, failOver.ShouldWeFailOver())
}

func TestAPrimaryThatRecoversResetsTheStabilityWindow(t *testing.T) {
	// A readiness blip must not count towards a later, unrelated problem.
	unhealthy := newCluster(t).withPrimary(1, false).withReplica(2, true)
	failOver, kubegres := unhealthy.build(t, Config{PrimaryStabilityWindow: time.Minute}, nil)

	require.False(t, failOver.ShouldWeFailOver())
	require.NotZero(t, kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds)

	recovered := newCluster(t).withPrimary(1, true).withReplica(2, true)
	recovered.kubegres.Status.FailOver = kubegres.Status.FailOver
	recoveredFailOver, recoveredKubegres := recovered.build(t, Config{PrimaryStabilityWindow: time.Minute}, nil)

	require.False(t, recoveredFailOver.ShouldWeFailOver())
	require.Zero(t, recoveredKubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds)
}

func TestManualFailoverIsNotDebounced(t *testing.T) {
	// The window filters out failovers caused by a readiness blip. Someone asking for a specific
	// Pod is not a blip.
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Spec.Failover.PromotePod = "postgres-replica-2-0"

	failOver, _ := cluster.build(t, Config{PrimaryStabilityWindow: time.Hour}, nil)

	require.True(t, failOver.ShouldWeFailOver())
}

func TestNoFailoverWhenAutomaticFailoverIsDisabled(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true)
	cluster.kubegres.Spec.Failover.IsDisabled = true

	failOver, _ := cluster.build(t, Config{}, nil)

	require.False(t, failOver.ShouldWeFailOver())
}

func TestNoFailoverWithoutAReadyReplica(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, false)

	failOver, _ := cluster.build(t, Config{}, nil)

	require.False(t, failOver.ShouldWeFailOver())
}

// ---------------------------------------------------------------------------------------
// Minimum healthy replica gate
// ---------------------------------------------------------------------------------------

func TestFailoverCompletesOnPrimaryReadinessAloneByDefault(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, true)
	failOver, _ := cluster.build(t, Config{}, nil)

	require.True(t, failOver.hasEnoughHealthyReplicas())
}

func TestFailoverIsHeldOpenUntilRedundancyIsRestored(t *testing.T) {
	// A promoted Primary with nothing behind it has no failover target left, so a second failure
	// cannot be recovered automatically.
	unreplicated := newCluster(t).withPrimary(1, true)
	failOver, _ := unreplicated.build(t, Config{MinHealthyReplicas: 1}, nil)
	require.False(t, failOver.hasEnoughHealthyReplicas())

	replicated := newCluster(t).withPrimary(1, true).withReplica(2, true)
	replicatedFailOver, _ := replicated.build(t, Config{MinHealthyReplicas: 1}, nil)
	require.True(t, replicatedFailOver.hasEnoughHealthyReplicas())

	stillShort := newCluster(t).withPrimary(1, true).withReplica(2, true)
	stillShortFailOver, _ := stillShort.build(t, Config{MinHealthyReplicas: 2}, nil)
	require.False(t, stillShortFailOver.hasEnoughHealthyReplicas())
}

// ---------------------------------------------------------------------------------------
// Selection routing
// ---------------------------------------------------------------------------------------

func TestLegacySelectionIsUsedWhenIntelligentFailoverIsDisabled(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true).withReplica(3, true)

	// Index 3 is furthest ahead, but with the feature off the lowest-indexed ready Replica still
	// wins, exactly as before.
	prober := &fakeProber{health: map[int32]replicahealth.Status{
		2: streamingAt(t, 1, "0/10"),
		3: streamingAt(t, 1, "0/FF"),
	}}
	failOver, _ := cluster.build(t, Config{}, prober)

	newPrimary, reason, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(2), newPrimary.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonLegacy, reason)
	require.Zero(t, prober.probedCalls, "the databases must not be queried when the feature is off")
}

func TestIntelligentSelectionPromotesTheFurthestAdvancedReplica(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true).withReplica(3, true)

	prober := &fakeProber{health: map[int32]replicahealth.Status{
		2: streamingAt(t, 1, "0/10"),
		3: streamingAt(t, 1, "0/FF"),
	}}
	failOver, _ := cluster.build(t, Config{IntelligentFailoverEnabled: true}, prober)

	newPrimary, reason, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(3), newPrimary.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonHighestLsn, reason)
}

func TestUnreadyAndSlotMismatchedReplicasAreNeverCandidates(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, false).withReplica(3, true)
	cluster.kubegres.Spec.ReplicationSlots.Enabled = true

	// Only index 3 has a replication slot, matching the cluster's setup.
	replicas := cluster.states.StatefulSets.Replicas.All.GetAllSortedByInstanceIndex()
	require.Len(t, replicas, 2)
	cluster.states.StatefulSets.Replicas.All = statefulset.StatefulSetWrappers{}
	for _, replicaStatefulSet := range replicas {
		replicaStatefulSet.HaveReplicationSlotSet = replicaStatefulSet.InstanceIndex == 3
		cluster.states.StatefulSets.Replicas.All.Add(replicaStatefulSet)
	}

	prober := &fakeProber{health: map[int32]replicahealth.Status{
		2: streamingAt(t, 1, "0/FF"),
		3: streamingAt(t, 1, "0/10"),
	}}
	failOver, _ := cluster.build(t, Config{IntelligentFailoverEnabled: true}, prober)

	newPrimary, _, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(3), newPrimary.InstanceIndex)
}

func TestFallbackToLegacyWhenNoReplicaCanBeQueried(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true).withReplica(3, true)

	prober := &fakeProber{probeErrs: map[int32]error{
		2: errors.New("i/o timeout"),
		3: errors.New("i/o timeout"),
	}}
	failOver, _ := cluster.build(t, Config{
		IntelligentFailoverEnabled: true,
		FallbackToLegacy:           true,
	}, prober)

	newPrimary, reason, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(2), newPrimary.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonFallback, reason)
}

func TestRefusesToPromoteUnverifiableReplicasWhenFallbackIsDisabled(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true)

	prober := &fakeProber{probeErrs: map[int32]error{2: errors.New("i/o timeout")}}
	failOver, kubegres := cluster.build(t, Config{
		IntelligentFailoverEnabled: true,
		FallbackToLegacy:           false,
	}, prober)

	_, _, err := failOver.selectReplicaToPromote()

	require.Error(t, err)
	require.Contains(t, err.Error(), "manual intervention")
	require.Equal(t, metrics.BlockReasonUnreachable, kubegres.Status.FailOver.BlockedReason)
}

func TestRefusesToPromoteAReplicaBeyondTheLagThreshold(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, false).withReplica(2, true)
	cluster.kubegres.Status.FailOver.LastKnownPrimaryWalLsn = "0/FFFFFF"

	prober := &fakeProber{health: map[int32]replicahealth.Status{2: streamingAt(t, 1, "0/10")}}
	failOver, kubegres := cluster.build(t, Config{
		IntelligentFailoverEnabled: true,
		MaxReplicationLagBytes:     1024,
	}, prober)

	_, _, err := failOver.selectReplicaToPromote()

	require.Error(t, err)
	require.Equal(t, metrics.BlockReasonLagExceeded, kubegres.Status.FailOver.BlockedReason)
}

// ---------------------------------------------------------------------------------------
// Manual promotion safety
// ---------------------------------------------------------------------------------------

func TestManualPromotionIsRefusedWhenTheReplicationSlotGenerationDoesNotMatch(t *testing.T) {
	// The race from the umbrella issue: with replication slots on, new Replicas are created
	// before the old ones are deleted, and promoting an old one loses everything written through
	// the new slots. The automatic path always rejected these; the manual path did not check.
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Spec.ReplicationSlots.Enabled = true
	cluster.kubegres.Spec.Failover.PromotePod = "postgres-replica-2-0"

	failOver, _ := cluster.build(t, Config{}, nil)

	_, _, err := failOver.selectReplicaToPromote()

	require.Error(t, err)
	require.Contains(t, err.Error(), "earlier generation of Replicas")
}

func TestManualPromotionIsRefusedWhenTheRequestedReplicaIsTooFarBehind(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Spec.Failover.PromotePod = "postgres-replica-2-0"
	cluster.kubegres.Status.FailOver.LastKnownPrimaryWalLsn = "0/FFFFFF"

	prober := &fakeProber{health: map[int32]replicahealth.Status{2: streamingAt(t, 1, "0/10")}}
	failOver, kubegres := cluster.build(t, Config{
		IntelligentFailoverEnabled: true,
		MaxReplicationLagBytes:     1024,
	}, prober)

	_, _, err := failOver.selectReplicaToPromote()

	require.Error(t, err)
	require.Contains(t, err.Error(), "allowUnsafeManualPromotion")
	require.Equal(t, metrics.BlockReasonUnsafeManualPromotion, kubegres.Status.FailOver.BlockedReason)
}

func TestManualPromotionCanBeForcedPastTheHealthChecks(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Spec.Failover.PromotePod = "postgres-replica-2-0"
	cluster.kubegres.Status.FailOver.LastKnownPrimaryWalLsn = "0/FFFFFF"

	prober := &fakeProber{health: map[int32]replicahealth.Status{2: streamingAt(t, 1, "0/10")}}
	failOver, _ := cluster.build(t, Config{
		IntelligentFailoverEnabled: true,
		MaxReplicationLagBytes:     1024,
		AllowUnsafeManualPromotion: true,
	}, prober)

	newPrimary, reason, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(2), newPrimary.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonManual, reason)
}

func TestManualPromotionOfAHealthyReplicaIsHonoured(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true).withReplica(3, true)
	cluster.kubegres.Spec.Failover.PromotePod = "postgres-replica-3-0"

	// Index 2 holds more WAL, but a manual request names the Pod, and that is honoured once it
	// passes the health checks.
	prober := &fakeProber{health: map[int32]replicahealth.Status{
		2: streamingAt(t, 1, "0/FF"),
		3: streamingAt(t, 1, "0/10"),
	}}
	failOver, _ := cluster.build(t, Config{IntelligentFailoverEnabled: true}, prober)

	newPrimary, reason, err := failOver.selectReplicaToPromote()

	require.NoError(t, err)
	require.Equal(t, int32(3), newPrimary.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonManual, reason)
}

// ---------------------------------------------------------------------------------------
// Steady-state readiness
// ---------------------------------------------------------------------------------------

func TestSteadyStateObservationRecordsThePrimaryWalPosition(t *testing.T) {
	// The recorded position is the only thing left to measure replica lag against once the
	// Primary is gone and cannot be asked.
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Name = "steady-state-observation"

	primaryLSN, err := postgres.ParseLSN("0/ABCDEF")
	require.NoError(t, err)

	prober := &fakeProber{
		primaryLSN: primaryLSN,
		health:     map[int32]replicahealth.Status{2: streamingAt(t, 1, "0/ABCDEF")},
	}
	failOver, kubegres := cluster.build(t, Config{IntelligentFailoverEnabled: true}, prober)

	failOver.ObserveClusterFailOverReadiness()

	require.Equal(t, "0/ABCDEF", kubegres.Status.FailOver.LastKnownPrimaryWalLsn)
	require.NotZero(t, kubegres.Status.FailOver.LastKnownPrimaryWalLsnEpochInSeconds)
}

func TestSteadyStateObservationIsSkippedWhenTheFeatureIsOff(t *testing.T) {
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Name = "steady-state-disabled"

	prober := &fakeProber{}
	failOver, kubegres := cluster.build(t, Config{}, prober)

	failOver.ObserveClusterFailOverReadiness()

	require.Zero(t, prober.probedCalls)
	require.Empty(t, kubegres.Status.FailOver.LastKnownPrimaryWalLsn)
}

func TestSteadyStateObservationIsThrottled(t *testing.T) {
	// Reconciliations arrive in bursts. Without throttling, every unrelated Pod update would
	// query every Replica.
	cluster := newCluster(t).withPrimary(1, true).withReplica(2, true)
	cluster.kubegres.Name = "steady-state-throttled"

	prober := &fakeProber{health: map[int32]replicahealth.Status{2: streamingAt(t, 1, "0/10")}}
	failOver, _ := cluster.build(t, Config{IntelligentFailoverEnabled: true}, prober)

	failOver.ObserveClusterFailOverReadiness()
	firstRoundCalls := prober.probedCalls
	require.Equal(t, 1, firstRoundCalls)

	failOver.ObserveClusterFailOverReadiness()
	require.Equal(t, firstRoundCalls, prober.probedCalls)
}

func TestADeletedPrimaryIsNotDebounced(t *testing.T) {
	// The window gives a Primary time to recover. One whose StatefulSet is gone will not, so
	// waiting would only make the outage longer.
	cluster := newCluster(t).withoutPrimary().withReplica(2, true)
	failOver, _ := cluster.build(t, Config{PrimaryStabilityWindow: time.Hour}, nil)

	require.True(t, failOver.ShouldWeFailOver())
	require.Zero(t, failOver.RequeueAfter())
}
