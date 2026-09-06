package failover_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"reactive-tech.io/kubegres/controllers/metrics"
	"reactive-tech.io/kubegres/controllers/spec/enforcer/resources_count_spec/statefulset/failover"
	"reactive-tech.io/kubegres/internal/postgres"
	"reactive-tech.io/kubegres/internal/replicahealth"
)

// replica builds a healthy streaming standby candidate at the given timeline and WAL position.
func replica(t *testing.T, instanceIndex int32, timeline uint32, lsn string) failover.Candidate {
	t.Helper()

	parsed, err := postgres.ParseLSN(lsn)
	require.NoError(t, err)

	return failover.Candidate{
		InstanceIndex:   instanceIndex,
		StatefulSetName: "postgres-" + string(rune('0'+instanceIndex)),
		Health: replicahealth.Status{
			InRecovery:         true,
			TimelineID:         timeline,
			ReplayLSN:          parsed,
			ReceiveLSN:         parsed,
			WalReceiverPresent: true,
			WalReceiverStatus:  "streaming",
		},
	}
}

func lsn(t *testing.T, text string) postgres.LSN {
	t.Helper()
	parsed, err := postgres.ParseLSN(text)
	require.NoError(t, err)
	return parsed
}

func rejectionFor(outcome failover.SelectionOutcome, instanceIndex int32) (failover.Rejection, bool) {
	for _, rejection := range outcome.Rejections {
		if rejection.InstanceIndex == instanceIndex {
			return rejection, true
		}
	}
	return failover.Rejection{}, false
}

func TestPromotesTheFurthestAdvancedReplica(t *testing.T) {
	// The scenario from the design document: three ready Replicas, the middle one holds the
	// most WAL. Readiness-based selection would take index 1 simply because it sorts first.
	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/3A"),
		replica(t, 2, 1, "0/3C"),
		replica(t, 3, 1, "0/38"),
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)
	require.Equal(t, metrics.DecisionReasonHighestLsn, outcome.Reason)
	require.Empty(t, outcome.BlockedReason)
	require.Equal(t, 3, outcome.ProbedCount)
	require.False(t, outcome.AllProbesFailed)

	// The losers are reported so an operator can see the election, not just its winner.
	rejection, found := rejectionFor(outcome, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionNotBestLsn, rejection.Reason)
}

// TestNeverPromotesAcrossAnAbandonedTimeline is the regression test for the failure mode that
// naive LSN comparison introduces: the numerically highest position belongs to a Replica left
// on a superseded timeline by an earlier promotion. Picking it would resurrect a history the
// cluster already abandoned.
func TestNeverPromotesAcrossAnAbandonedTimeline(t *testing.T) {
	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/FF00"), // furthest ahead in raw bytes, but on the old timeline
		replica(t, 2, 2, "0/3C"),
		replica(t, 3, 2, "0/3A"),
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex,
		"the highest raw LSN sits on an abandoned timeline and must not win")
	require.Equal(t, uint32(2), outcome.Winner.Health.TimelineID)

	rejection, found := rejectionFor(outcome, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionStaleTimeline, rejection.Reason)
	require.Contains(t, rejection.Detail, "abandoned history")
}

func TestBlocksWhenEveryCandidateIsOnAStaleTimeline(t *testing.T) {
	// A Replica that has already left recovery is excluded before timelines are compared, so
	// what is left here are two standbys both trailing a timeline nobody is serving.
	promoted := replica(t, 3, 3, "0/50")
	promoted.Health.InRecovery = false

	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/3A"),
		replica(t, 2, 1, "0/3C"),
		promoted,
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	// Both surviving standbys share timeline 1, so one of them is still promotable: the
	// already-promoted instance is excluded, not treated as raising the bar.
	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)

	rejection, found := rejectionFor(outcome, 3)
	require.True(t, found)
	require.Equal(t, failover.RejectionNotInRecovery, rejection.Reason)
}

func TestPrefersTheReceivedPositionOverTheReplayedOne(t *testing.T) {
	// Promotion replays whatever WAL is already on disk, so a Replica that has received more
	// than it has replayed will end up further ahead than its replay position suggests.
	behindOnReplay := replica(t, 1, 1, "0/10")
	behindOnReplay.Health.ReceiveLSN = lsn(t, "0/FF")

	candidates := []failover.Candidate{
		behindOnReplay,
		replica(t, 2, 1, "0/20"),
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(1), outcome.Winner.InstanceIndex)
	require.Equal(t, lsn(t, "0/FF"), outcome.Winner.Health.PromotionLSN())
}

func TestUnreachableReplicasAreReportedRatherThanPromoted(t *testing.T) {
	unreachable := replica(t, 1, 1, "0/FF")
	unreachable.ProbeErr = errors.New("dial tcp 10.1.2.3:5432: i/o timeout")

	candidates := []failover.Candidate{unreachable, replica(t, 2, 1, "0/3A")}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)
	require.Equal(t, 1, outcome.ProbedCount)

	rejection, found := rejectionFor(outcome, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionProbeFailed, rejection.Reason)
	require.Contains(t, rejection.Detail, "i/o timeout")
}

func TestAllProbesFailedIsSignalledSeparatelyFromBlocking(t *testing.T) {
	// Whether "nothing could be reached" blocks the failover depends on the fallback policy,
	// which is the caller's decision, so selection reports the fact rather than the verdict.
	first := replica(t, 1, 1, "0/3A")
	first.ProbeErr = errors.New("connection refused")
	second := replica(t, 2, 1, "0/3C")
	second.ProbeErr = errors.New("connection refused")

	outcome := failover.SelectByWalPosition([]failover.Candidate{first, second}, failover.SelectionConstraints{})

	require.Nil(t, outcome.Winner)
	require.True(t, outcome.AllProbesFailed)
	require.Empty(t, outcome.BlockedReason)
	require.Equal(t, 0, outcome.ProbedCount)
}

func TestBlocksWhenTheBestReplicaExceedsTheLagThreshold(t *testing.T) {
	// The Primary got to 0/2000000 before dying; the best Replica only reached 0/1000000, so
	// promoting it would silently discard 16 MiB of committed history.
	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/1000000"),
		replica(t, 2, 1, "0/900000"),
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{
		MaxReplicationLagBytes: 1024,
		ReferenceLSN:           lsn(t, "0/2000000"),
	})

	require.Nil(t, outcome.Winner)
	require.Equal(t, metrics.BlockReasonLagExceeded, outcome.BlockedReason)
	require.Equal(t, int64(0x1000000), outcome.WinnerLagBytes)
	require.Contains(t, outcome.Explanation, "exceeds the configured maximum replication lag")
}

func TestPromotesWhenTheLagIsWithinTheThreshold(t *testing.T) {
	candidates := []failover.Candidate{replica(t, 1, 1, "0/1000")}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{
		MaxReplicationLagBytes: 4096,
		ReferenceLSN:           lsn(t, "0/1800"),
	})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int64(0x800), outcome.WinnerLagBytes)
}

func TestLagIsMeasuredAgainstTheBestCandidateWhenThePrimaryPositionIsUnknown(t *testing.T) {
	// Without a reference position the winner is by definition zero bytes behind, so the
	// threshold can bound divergence between Replicas but cannot detect that they are all
	// equally far behind the Primary that died. This degradation is deliberate: refusing every
	// failover after an operator restart would be worse.
	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/1000"),
		replica(t, 2, 1, "0/2000"),
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{
		MaxReplicationLagBytes: 16,
	})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)
	require.Equal(t, int64(0), outcome.WinnerLagBytes)
}

func TestAReferencePositionBehindEveryReplicaIsIgnored(t *testing.T) {
	// A stale recorded Primary position must never make the winner look "ahead" and skew the
	// lag arithmetic negative.
	candidates := []failover.Candidate{replica(t, 1, 1, "0/9000")}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{
		MaxReplicationLagBytes: 16,
		ReferenceLSN:           lsn(t, "0/1000"),
	})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int64(0), outcome.WinnerLagBytes)
}

func TestReplicasWithNoWalPositionAreExcluded(t *testing.T) {
	empty := replica(t, 1, 1, "0/0")
	candidates := []failover.Candidate{empty, replica(t, 2, 1, "0/3A")}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)

	rejection, found := rejectionFor(outcome, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionNoWalPosition, rejection.Reason)
}

func TestRequireStreamingExcludesReplicasWithABrokenWalStream(t *testing.T) {
	// The exact shape reported in the issue: a Replica is Kubernetes-ready but its WAL
	// receiver has died on "requested WAL segment has already been removed", freezing it.
	brokenStream := replica(t, 1, 1, "0/FF00")
	brokenStream.Health.WalReceiverPresent = false
	brokenStream.Health.WalReceiverStatus = ""

	candidates := []failover.Candidate{brokenStream, replica(t, 2, 1, "0/3A")}

	withoutRequirement := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})
	require.NotNil(t, withoutRequirement.Winner)
	require.Equal(t, int32(1), withoutRequirement.Winner.InstanceIndex,
		"by default a frozen Replica still competes, because after the Primary dies every Replica loses its stream")

	withRequirement := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{RequireStreaming: true})
	require.NotNil(t, withRequirement.Winner)
	require.Equal(t, int32(2), withRequirement.Winner.InstanceIndex)

	rejection, found := rejectionFor(withRequirement, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionNotStreaming, rejection.Reason)
	require.Contains(t, rejection.Detail, "no WAL receiver is running")
}

func TestBlocksWhenEveryReachableReplicaFailsItsHealthChecks(t *testing.T) {
	alreadyPrimary := replica(t, 1, 1, "0/3A")
	alreadyPrimary.Health.InRecovery = false
	neverReplayed := replica(t, 2, 1, "0/0")

	outcome := failover.SelectByWalPosition(
		[]failover.Candidate{alreadyPrimary, neverReplayed},
		failover.SelectionConstraints{},
	)

	require.Nil(t, outcome.Winner)
	require.False(t, outcome.AllProbesFailed)
	require.Equal(t, metrics.BlockReasonNoHealthyCandidate, outcome.BlockedReason)
	require.Contains(t, outcome.Explanation, "failed their replication health checks")
}

func TestNoCandidatesAtAll(t *testing.T) {
	outcome := failover.SelectByWalPosition(nil, failover.SelectionConstraints{})

	require.Nil(t, outcome.Winner)
	require.False(t, outcome.AllProbesFailed)
	require.Equal(t, metrics.BlockReasonNoHealthyCandidate, outcome.BlockedReason)
}

func TestTiesResolveDeterministicallyOnTheLowerInstanceIndex(t *testing.T) {
	// Two Replicas at exactly the same position must always yield the same winner, otherwise
	// consecutive reconciliations could disagree about who is being promoted.
	forward := []failover.Candidate{replica(t, 5, 1, "0/3A"), replica(t, 2, 1, "0/3A")}
	reversed := []failover.Candidate{replica(t, 2, 1, "0/3A"), replica(t, 5, 1, "0/3A")}

	first := failover.SelectByWalPosition(forward, failover.SelectionConstraints{})
	second := failover.SelectByWalPosition(reversed, failover.SelectionConstraints{})

	require.NotNil(t, first.Winner)
	require.NotNil(t, second.Winner)
	require.Equal(t, int32(2), first.Winner.InstanceIndex)
	require.Equal(t, first.Winner.InstanceIndex, second.Winner.InstanceIndex)
}
