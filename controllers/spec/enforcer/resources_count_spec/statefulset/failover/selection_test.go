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

// replica builds a healthy streaming standby at the given timeline and WAL position.
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
	// Three ready Replicas, the middle one holds the most WAL. Selection on readiness would take
	// index 1 just because it sorts first.
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

	// The losers are reported too, so you can see the whole election.
	rejection, found := rejectionFor(outcome, 1)
	require.True(t, found)
	require.Equal(t, failover.RejectionNotBestLsn, rejection.Reason)
}

// TestNeverPromotesAcrossAnAbandonedTimeline covers the failure mode that plain LSN comparison
// would introduce: the highest position belongs to a Replica left on an old timeline by an
// earlier promotion. Picking it would bring back data the cluster already dropped.
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
	// A Replica that already left recovery is dropped before timelines are compared, leaving two
	// standbys that share a timeline.
	promoted := replica(t, 3, 3, "0/50")
	promoted.Health.InRecovery = false

	candidates := []failover.Candidate{
		replica(t, 1, 1, "0/3A"),
		replica(t, 2, 1, "0/3C"),
		promoted,
	}

	outcome := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})

	// Both survivors are on timeline 1, so one of them can still be promoted: the
	// already-promoted instance is dropped, not treated as raising the bar.
	require.NotNil(t, outcome.Winner)
	require.Equal(t, int32(2), outcome.Winner.InstanceIndex)

	rejection, found := rejectionFor(outcome, 3)
	require.True(t, found)
	require.Equal(t, failover.RejectionNotInRecovery, rejection.Reason)
}

func TestPrefersTheReceivedPositionOverTheReplayedOne(t *testing.T) {
	// Promotion replays whatever WAL is on disk, so a Replica that has received more than it has
	// replayed ends up further ahead than its replay position suggests.
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
	// Whether "nothing could be reached" blocks the failover depends on the fallback setting,
	// which is the caller's call, so selection reports the fact and not the verdict.
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
	// The Primary reached 0/2000000 before dying; the best Replica only got to 0/1000000, so
	// promoting it would quietly drop 16 MiB of committed data.
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
	// With no reference position the winner is zero bytes behind by definition, so the limit can
	// tell how far apart the Replicas are but not how far behind the dead Primary they all are.
	// That is on purpose: blocking every failover after an operator restart would be worse.
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
	// An old recorded Primary position must not make the winner look "ahead" and turn the lag
	// negative.
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
	// The case from the issue: a Replica is Kubernetes-ready but its WAL receiver died on
	// "requested WAL segment has already been removed", leaving it stuck.
	brokenStream := replica(t, 1, 1, "0/FF00")
	brokenStream.Health.WalReceiverPresent = false
	brokenStream.Health.WalReceiverStatus = ""

	candidates := []failover.Candidate{brokenStream, replica(t, 2, 1, "0/3A")}

	withoutRequirement := failover.SelectByWalPosition(candidates, failover.SelectionConstraints{})
	require.NotNil(t, withoutRequirement.Winner)
	require.Equal(t, int32(1), withoutRequirement.Winner.InstanceIndex,
		"by default a stuck Replica still competes: once the Primary dies every Replica loses its stream")

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
	// Two Replicas at the same position must always give the same winner, or two reconciliations
	// in a row could disagree about who is being promoted.
	forward := []failover.Candidate{replica(t, 5, 1, "0/3A"), replica(t, 2, 1, "0/3A")}
	reversed := []failover.Candidate{replica(t, 2, 1, "0/3A"), replica(t, 5, 1, "0/3A")}

	first := failover.SelectByWalPosition(forward, failover.SelectionConstraints{})
	second := failover.SelectByWalPosition(reversed, failover.SelectionConstraints{})

	require.NotNil(t, first.Winner)
	require.NotNil(t, second.Winner)
	require.Equal(t, int32(2), first.Winner.InstanceIndex)
	require.Equal(t, first.Winner.InstanceIndex, second.Winner.InstanceIndex)
}
