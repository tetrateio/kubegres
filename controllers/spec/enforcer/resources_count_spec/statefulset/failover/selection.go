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

// Rejection reasons, reported per candidate so you can see why each Replica was passed over,
// not just which one won.
const (
	RejectionProbeFailed   = "probe_failed"
	RejectionNotInRecovery = "not_in_recovery"
	RejectionNoWalPosition = "no_wal_position"
	RejectionNotStreaming  = "not_streaming"
	RejectionStaleTimeline = "stale_timeline"
	RejectionLagExceeded   = "lag_exceeded"
	RejectionNotBestLsn    = "behind_selected_replica"
)

// Candidate is one Replica considered for promotion, plus what Kubegres learned about its
// replication state.
type Candidate struct {
	InstanceIndex   int32
	StatefulSetName string
	PodName         string

	// Health is the replication state. It only means anything when ProbeErr is nil.
	Health replicahealth.Status
	// ProbeErr is set when the Replica could not be reached or queried.
	ProbeErr error
}

// SelectionConstraints are the limits applied on top of "furthest advanced wins".
type SelectionConstraints struct {
	// MaxReplicationLagBytes is how far behind the reference position the promoted Replica may
	// be. Zero turns the check off.
	MaxReplicationLagBytes int64

	// ReferenceLSN is the last WAL position seen on the healthy Primary. Zero means we have
	// none, and lag is measured against the best candidate instead.
	ReferenceLSN postgres.LSN

	// RequireStreaming rejects candidates without a live WAL stream.
	RequireStreaming bool
}

// Rejection records a candidate that was not promoted, and why.
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

// SelectionOutcome is the result of an election.
type SelectionOutcome struct {
	// Winner is the Replica to promote, or nil if none may be.
	Winner *Candidate

	// Reason is the metrics.DecisionReason* explaining a win.
	Reason string

	// BlockedReason is the metrics.BlockReason* explaining a refusal. It is empty when Winner is
	// set, and also when no candidate could be reached: that case is reported by
	// AllProbesFailed instead, since whether it blocks depends on the fallback setting.
	BlockedReason string

	// AllProbesFailed means every candidate was unreachable. It is the one outcome the caller
	// may answer by falling back to readiness-based selection.
	AllProbesFailed bool

	// Explanation is a readable summary for logs, Events and the Kubegres status.
	Explanation string

	// Rejections lists every candidate that was passed over.
	Rejections []Rejection

	// ProbedCount is how many candidates reported their replication state.
	ProbedCount int

	// WinnerLagBytes is how far the winner trails the reference position.
	WinnerLagBytes int64
}

// SelectByWalPosition picks the Replica that would lose the least data if promoted.
//
// The rule is not just "highest LSN". Each promotion starts a new PostgreSQL timeline, so after
// an earlier failover the surviving Replicas can sit on different ones. LSNs from different
// timelines are not comparable, and a Replica that is ahead on an old timeline holds data the
// cluster already threw away. Promoting it would make that data authoritative again, which is
// worse than today's readiness-only selection.
//
// So candidates are grouped by timeline and everything below the highest is dropped first. Only
// within the survivors does the furthest-advanced position win.
func SelectByWalPosition(candidates []Candidate, constraints SelectionConstraints) SelectionOutcome {
	outcome := SelectionOutcome{}

	// Work in instance-index order so a tie resolves the same way every time.
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

	// Lag is measured against the last position seen on the healthy Primary. If we have none -
	// the operator never saw it, or restarted since - fall back to the best candidate's own
	// position. That still limits how far apart the Replicas are, but it cannot tell that they
	// are all equally far behind the Primary that died.
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

// filterUnhealthy drops candidates that must not be promoted, records why, and counts how many
// reported their state at all.
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

		// An instance that left recovery was already promoted and started its own timeline.
		// Exclude it before comparing timelines, so it cannot win on that higher timeline.
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

// discardStaleTimelines keeps only the candidates on the highest timeline seen.
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

// furthestAdvanced returns the candidate holding the most WAL. Ties go to the lower instance
// index so repeated elections agree.
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
