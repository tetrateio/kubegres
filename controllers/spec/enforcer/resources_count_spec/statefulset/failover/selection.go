/*
Copyright 2021 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package failover

import (
	"fmt"
	"sort"
	"strconv"

	"reactive-tech.io/kubegres/controllers/metrics"
	"reactive-tech.io/kubegres/internal/postgres"
	"reactive-tech.io/kubegres/internal/replicahealth"
)

// Rejection reasons, reported per candidate so that an operator can see why each Replica was
// passed over rather than only which one won.
const (
	RejectionProbeFailed   = "probe_failed"
	RejectionNotInRecovery = "not_in_recovery"
	RejectionNoWalPosition = "no_wal_position"
	RejectionNotStreaming  = "not_streaming"
	RejectionStaleTimeline = "stale_timeline"
	RejectionLagExceeded   = "lag_exceeded"
	RejectionNotBestLsn    = "behind_selected_replica"
)

// Candidate is one Replica considered for promotion, together with whatever Kubegres managed to
// learn about its PostgreSQL replication state.
type Candidate struct {
	InstanceIndex   int32
	StatefulSetName string
	PodName         string

	// Health is the observed replication state; only meaningful when ProbeErr is nil.
	Health replicahealth.Status
	// ProbeErr is non-nil when the Replica could not be reached or queried.
	ProbeErr error
}

// SelectionConstraints are the guardrails applied on top of "furthest advanced wins".
type SelectionConstraints struct {
	// MaxReplicationLagBytes is the ceiling on how far behind the reference position the
	// promoted Replica may be. Zero disables the check.
	MaxReplicationLagBytes int64

	// ReferenceLSN is the last WAL position observed on the healthy Primary. Zero means no
	// observation is available, in which case lag is measured against the best candidate.
	ReferenceLSN postgres.LSN

	// RequireStreaming rejects candidates without a live WAL stream.
	RequireStreaming bool
}

// Rejection records one candidate that was not promoted, and why.
type Rejection struct {
	InstanceIndex int32
	Reason        string
	Detail        string
}

func (r Rejection) String() string {
	if r.Detail == "" {
		return "index " + strconv.Itoa(int(r.InstanceIndex)) + ": " + r.Reason
	}
	return "index " + strconv.Itoa(int(r.InstanceIndex)) + ": " + r.Reason + " (" + r.Detail + ")"
}

// SelectionOutcome is the result of a WAL-aware election.
type SelectionOutcome struct {
	// Winner is the Replica to promote, or nil when none may be promoted.
	Winner *Candidate

	// Reason is the metrics.DecisionReason* explaining a win.
	Reason string

	// BlockedReason is the metrics.BlockReason* explaining a refusal. It is empty when Winner
	// is set. It is also empty when no candidate could be reached at all: that case is
	// signalled by AllProbesFailed, because whether it blocks depends on the fallback policy.
	BlockedReason string

	// AllProbesFailed reports that every candidate was unreachable, which is the one outcome
	// the caller may choose to answer by falling back to readiness-based selection.
	AllProbesFailed bool

	// Explanation is a human-readable summary for logs, Events and the Kubegres status.
	Explanation string

	// Rejections lists every candidate that was passed over.
	Rejections []Rejection

	// ProbedCount is how many candidates reported their replication state.
	ProbedCount int

	// WinnerLagBytes is how far the winner trails the reference position.
	WinnerLagBytes int64
}

// SelectByWalPosition picks the Replica that would lose the least committed history if promoted.
//
// The rule is not simply "highest LSN". Every promotion forks a new PostgreSQL timeline, so
// after an earlier emergency failover the surviving Replicas can sit on different timelines
// carrying divergent histories. An LSN on an abandoned timeline is not comparable with an LSN
// on the current one, and a Replica that is numerically ahead on a superseded lineage is
// carrying data that the cluster has already agreed to discard. Promoting it would resurrect
// that lineage as authoritative — a worse outcome than today's readiness-only selection.
//
// So candidates are first grouped by timeline and everything below the highest observed
// timeline is discarded; only within that surviving group does the furthest-advanced position
// win.
func SelectByWalPosition(candidates []Candidate, constraints SelectionConstraints) SelectionOutcome {
	outcome := SelectionOutcome{}

	// Iterate in instance-index order throughout, so that a tie resolves the same way on every
	// reconciliation rather than following Go's map or slice ordering of the moment.
	ordered := make([]Candidate, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].InstanceIndex < ordered[j].InstanceIndex
	})

	if len(ordered) == 0 {
		outcome.BlockedReason = metrics.BlockReasonNoHealthyCandidate
		outcome.Explanation = "there are no Replicas to consider for promotion"
		return outcome
	}

	healthy := outcome.filterUnhealthy(ordered, constraints)

	if outcome.ProbedCount == 0 {
		outcome.AllProbesFailed = true
		outcome.Explanation = fmt.Sprintf("none of the %d Replica(s) could be queried for their replication state", len(ordered))
		return outcome
	}

	if len(healthy) == 0 {
		outcome.BlockedReason = metrics.BlockReasonNoHealthyCandidate
		outcome.Explanation = fmt.Sprintf("all %d reachable Replica(s) failed their replication health checks: %s",
			outcome.ProbedCount, formatRejections(outcome.Rejections))
		return outcome
	}

	onCurrentTimeline := outcome.discardStaleTimelines(healthy)
	if len(onCurrentTimeline) == 0 {
		outcome.BlockedReason = metrics.BlockReasonStaleTimeline
		outcome.Explanation = "every reachable Replica is on a superseded PostgreSQL timeline"
		return outcome
	}

	winner := furthestAdvanced(onCurrentTimeline)

	for _, candidate := range onCurrentTimeline {
		if candidate.InstanceIndex == winner.InstanceIndex {
			continue
		}
		outcome.Rejections = append(outcome.Rejections, Rejection{
			InstanceIndex: candidate.InstanceIndex,
			Reason:        RejectionNotBestLsn,
			Detail: fmt.Sprintf("WAL position %s is behind %s",
				candidate.Health.PromotionLSN(), winner.Health.PromotionLSN()),
		})
	}

	// Lag is measured against the last position seen on the healthy Primary. When that is
	// unknown — the operator never observed it, or was restarted since — fall back to the best
	// candidate's own position. That still bounds divergence between Replicas, but it cannot
	// detect that every surviving Replica is equally far behind the Primary that died.
	reference := postgres.Max(constraints.ReferenceLSN, winner.Health.PromotionLSN())
	outcome.WinnerLagBytes = reference.Distance(winner.Health.PromotionLSN())

	if constraints.MaxReplicationLagBytes > 0 && outcome.WinnerLagBytes > constraints.MaxReplicationLagBytes {
		outcome.Rejections = append(outcome.Rejections, Rejection{
			InstanceIndex: winner.InstanceIndex,
			Reason:        RejectionLagExceeded,
			Detail: fmt.Sprintf("%d bytes behind %s, limit is %d bytes",
				outcome.WinnerLagBytes, reference, constraints.MaxReplicationLagBytes),
		})
		outcome.BlockedReason = metrics.BlockReasonLagExceeded
		outcome.Explanation = fmt.Sprintf(
			"the best Replica (index %d, WAL position %s) is %d bytes behind the last known Primary position %s, "+
				"which exceeds the configured maximum replication lag of %d bytes",
			winner.InstanceIndex, winner.Health.PromotionLSN(), outcome.WinnerLagBytes, reference,
			constraints.MaxReplicationLagBytes)
		return outcome
	}

	outcome.Winner = &winner
	outcome.Reason = metrics.DecisionReasonHighestLsn
	outcome.Explanation = fmt.Sprintf(
		"promoting Replica index %d: timeline %d, WAL position %s, %d bytes behind the reference position %s",
		winner.InstanceIndex, winner.Health.TimelineID, winner.Health.PromotionLSN(), outcome.WinnerLagBytes, reference)

	return outcome
}

// filterUnhealthy drops candidates that must never be promoted, recording why, and counts how
// many reported their state at all.
func (o *SelectionOutcome) filterUnhealthy(candidates []Candidate, constraints SelectionConstraints) []Candidate {
	var healthy []Candidate

	for _, candidate := range candidates {
		if candidate.ProbeErr != nil {
			o.Rejections = append(o.Rejections, Rejection{
				InstanceIndex: candidate.InstanceIndex,
				Reason:        RejectionProbeFailed,
				Detail:        candidate.ProbeErr.Error(),
			})
			continue
		}

		o.ProbedCount++

		// An instance that has left recovery has already been promoted and has forked its own
		// timeline. Promoting it again would make that fork authoritative, so it is excluded
		// before timeline comparison rather than winning it on a spuriously higher timeline.
		if !candidate.Health.InRecovery {
			o.Rejections = append(o.Rejections, Rejection{
				InstanceIndex: candidate.InstanceIndex,
				Reason:        RejectionNotInRecovery,
				Detail:        "the instance is not a standby",
			})
			continue
		}

		if candidate.Health.PromotionLSN().IsZero() {
			o.Rejections = append(o.Rejections, Rejection{
				InstanceIndex: candidate.InstanceIndex,
				Reason:        RejectionNoWalPosition,
				Detail:        "the instance has not replayed or received any WAL",
			})
			continue
		}

		if constraints.RequireStreaming && !candidate.Health.IsStreaming() {
			detail := "no WAL receiver is running"
			if candidate.Health.WalReceiverPresent {
				detail = "the WAL receiver is in state " + strconv.Quote(candidate.Health.WalReceiverStatus)
			}
			o.Rejections = append(o.Rejections, Rejection{
				InstanceIndex: candidate.InstanceIndex,
				Reason:        RejectionNotStreaming,
				Detail:        detail,
			})
			continue
		}

		healthy = append(healthy, candidate)
	}

	return healthy
}

// discardStaleTimelines keeps only the candidates on the highest timeline observed.
func (o *SelectionOutcome) discardStaleTimelines(candidates []Candidate) []Candidate {
	var highestTimeline uint32
	for _, candidate := range candidates {
		if candidate.Health.TimelineID > highestTimeline {
			highestTimeline = candidate.Health.TimelineID
		}
	}

	var current []Candidate
	for _, candidate := range candidates {
		if candidate.Health.TimelineID == highestTimeline {
			current = append(current, candidate)
			continue
		}

		o.Rejections = append(o.Rejections, Rejection{
			InstanceIndex: candidate.InstanceIndex,
			Reason:        RejectionStaleTimeline,
			Detail: fmt.Sprintf("on timeline %d while timeline %d exists; its WAL position %s belongs to an abandoned history",
				candidate.Health.TimelineID, highestTimeline, candidate.Health.PromotionLSN()),
		})
	}

	return current
}

// furthestAdvanced returns the candidate holding the most WAL, breaking ties on the lower
// instance index so that repeated elections agree.
func furthestAdvanced(candidates []Candidate) Candidate {
	winner := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.Health.PromotionLSN() > winner.Health.PromotionLSN() {
			winner = candidate
		}
	}
	return winner
}

func formatRejections(rejections []Rejection) string {
	if len(rejections) == 0 {
		return "none"
	}

	formatted := ""
	for i, rejection := range rejections {
		if i > 0 {
			formatted += "; "
		}
		formatted += rejection.String()
	}
	return formatted
}
