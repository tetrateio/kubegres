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
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/metrics"
	"reactive-tech.io/kubegres/controllers/states/statefulset"
	"reactive-tech.io/kubegres/internal/postgres"
)

// clusterFailOverReadinessInterval throttles the steady-state readiness probe. Reconciliations
// arrive in bursts, so without this every unrelated Pod update would query every Replica.
const clusterFailOverReadinessInterval = 30 * time.Second

var lastReadinessObservation = struct {
	mu sync.Mutex
	at map[string]time.Time
}{at: make(map[string]time.Time)}

// requeueRequest carries a "come back in N" request from the enforcement pass to the controller.
//
// It is a pointer because PrimaryToReplicaFailOver is passed by value: an enforcer holds its own
// copy, so a plain field would be thrown away before the controller could read it.
type requeueRequest struct {
	mu    sync.Mutex
	after time.Duration
}

func (r *requeueRequest) request(after time.Duration) {
	if after <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Keep the earliest one: that is the deadline worth waking for.
	if r.after == 0 || after < r.after {
		r.after = after
	}
}

func (r *requeueRequest) take() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	after := r.after
	r.after = 0
	return after
}

// RequeueAfter says how long the controller should wait before reconciling again, and clears the
// request. It is zero when nothing is waiting on a timer.
//
// The Primary stability window needs this: a Primary that stays not-ready produces no more
// events, so without a requeue the delayed failover would never happen.
func (r *PrimaryToReplicaFailOver) RequeueAfter() time.Duration {
	if r.requeue == nil {
		return 0
	}
	return r.requeue.take()
}

func (r *PrimaryToReplicaFailOver) requeueAfter(after time.Duration) {
	if r.requeue != nil {
		r.requeue.request(after)
	}
}

// ---------------------------------------------------------------------------------------
// Primary stability window
// ---------------------------------------------------------------------------------------

// hasPrimaryBeenUnhealthyLongEnough delays the failover trigger.
//
// Readiness is one bit that flips on a probe failure, so a tight probe, a brief restart or
// short-lived resource pressure all look the same as a dead Primary. An unneeded failover is not
// just churn: it deletes a Primary that would have recovered, and risks losing data.
func (r *PrimaryToReplicaFailOver) hasPrimaryBeenUnhealthyLongEnough() bool {
	window := r.config.PrimaryStabilityWindow
	if window <= 0 {
		return true
	}

	// The window gives a Primary time to recover. One whose StatefulSet is gone will not, so
	// waiting would only make the outage longer.
	if !r.isPrimaryDbDeployed() {
		return true
	}

	now := time.Now().Unix()
	unhealthySince := r.kubegresContext.Kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds

	if unhealthySince == 0 || unhealthySince > now {
		r.kubegresContext.Status.UpdateFailOver(func(status *v1.KubegresFailOverStatus) {
			status.PrimaryUnhealthySinceEpochInSeconds = now
		})
		r.requeueAfter(window)
		r.logWaitingForPrimaryToStabilise(window, 0)
		return false
	}

	unhealthyFor := time.Duration(now-unhealthySince) * time.Second
	if unhealthyFor >= window {
		return true
	}

	r.requeueAfter(window - unhealthyFor)
	r.logWaitingForPrimaryToStabilise(window, unhealthyFor)
	return false
}

// recordPrimaryIsHealthy resets the window once the Primary recovers, so a later problem is
// timed from its own start and not from an old blip.
func (r *PrimaryToReplicaFailOver) recordPrimaryIsHealthy() {
	if r.kubegresContext.Kubegres.Status.FailOver.PrimaryUnhealthySinceEpochInSeconds == 0 {
		return
	}

	r.kubegresContext.Status.UpdateFailOver(func(status *v1.KubegresFailOverStatus) {
		status.PrimaryUnhealthySinceEpochInSeconds = 0
	})
	r.kubegresContext.Log.Info("The Primary DB recovered before the failover stability window elapsed. " +
		"No failover is required.")
}

func (r *PrimaryToReplicaFailOver) logWaitingForPrimaryToStabilise(window, unhealthyFor time.Duration) {
	r.kubegresContext.Log.InfoEvent("FailOverWaitingForPrimaryStability",
		fmt.Sprintf("The Primary DB is not ready, but it has only been unhealthy for %s and "+
			"'failover.primaryStabilityWindow' is %s. Waiting to see whether it recovers on its own "+
			"before promoting a Replica.", unhealthyFor, window))
}

// ---------------------------------------------------------------------------------------
// Replication-state-aware selection
// ---------------------------------------------------------------------------------------

// eligibleReplicas returns the Replicas that pass the checks Kubegres already had: ready, and
// with a replication-slot setup matching the cluster's. The replication-state checks are added
// on top of these, not instead of them.
func (r *PrimaryToReplicaFailOver) eligibleReplicas() []statefulset.StatefulSetWrapper {
	var eligible []statefulset.StatefulSetWrapper

	for _, replicaStatefulSet := range r.resourcesStates.StatefulSets.Replicas.All.GetAllSortedByInstanceIndex() {
		if r.isStructurallyEligible(replicaStatefulSet) {
			eligible = append(eligible, replicaStatefulSet)
		}
	}

	return eligible
}

func (r *PrimaryToReplicaFailOver) isStructurallyEligible(replicaStatefulSet statefulset.StatefulSetWrapper) bool {
	return replicaStatefulSet.IsReady &&
		replicaStatefulSet.HaveReplicationSlotSet == r.kubegresContext.Kubegres.Spec.ReplicationSlots.Enabled
}

// selectReplicaByReplicationState promotes the Replica that would lose the least data, rather
// than the first one Kubernetes calls ready.
func (r *PrimaryToReplicaFailOver) selectReplicaByReplicationState() (statefulset.StatefulSetWrapper, string, error) {
	eligible := r.eligibleReplicas()

	metrics.FailOverCandidates.
		WithLabelValues(r.kubegresContext.Kubegres.Namespace, r.kubegresContext.Kubegres.Name).
		Set(float64(len(eligible)))

	if len(eligible) == 0 {
		return statefulset.StatefulSetWrapper{}, "", errors.New(r.logFailoverCannotHappenAsNoHealthyReplica())
	}

	referenceLSN := r.lastKnownPrimaryWalPosition()
	candidates := r.prober.ProbeReplicas(eligible)
	r.publishReplicaLag(candidates, referenceLSN)

	outcome := SelectByWalPosition(candidates, r.config.SelectionConstraints(referenceLSN))

	if outcome.AllProbesFailed {
		return r.handleUnreachableReplicas(outcome)
	}

	if outcome.Winner == nil {
		return statefulset.StatefulSetWrapper{}, "", r.blockFailOver(outcome.BlockedReason, outcome.Explanation)
	}

	winner, err := r.resourcesStates.StatefulSets.Replicas.All.GetByInstanceIndex(outcome.Winner.InstanceIndex)
	if err != nil {
		return statefulset.StatefulSetWrapper{}, "", err
	}

	r.logElection(outcome)

	return winner, metrics.DecisionReasonHighestLsn, nil
}

// handleUnreachableReplicas handles the case where no Replica's state can be read, usually a
// network partition between the operator and the databases.
//
// This is an availability-versus-durability choice the operator cannot make on its own: falling
// back promotes on readiness alone and risks losing data, while refusing keeps the cluster down
// until a human steps in. 'fallbackToLegacy' is how the user states their preference.
func (r *PrimaryToReplicaFailOver) handleUnreachableReplicas(outcome SelectionOutcome) (statefulset.StatefulSetWrapper, string, error) {
	if !r.config.FallbackToLegacy {
		return statefulset.StatefulSetWrapper{}, "", r.blockFailOver(metrics.BlockReasonUnreachable,
			outcome.Explanation+", and 'failover.intelligentFailover.fallbackToLegacy' is disabled, "+
				"so Kubegres will not promote a Replica it cannot verify")
	}

	r.kubegresContext.Log.InfoEvent("FailOverWalVerificationUnavailable",
		"FailOver: none of the Replicas could be queried for their replication state, so Kubegres is "+
			"falling back to selecting a Replica on Kubernetes readiness alone. The promoted Replica may "+
			"be behind the failed Primary. Set 'failover.intelligentFailover.fallbackToLegacy' to false "+
			"to require verification instead.",
		"Details", outcome.Explanation)

	newPrimary, err := r.legacySelectReplicaToPromote()
	return newPrimary, metrics.DecisionReasonFallback, err
}

// blockFailOver refuses to promote, records why on the Kubegres resource, and returns the error
// that stops the enforcement pass.
func (r *PrimaryToReplicaFailOver) blockFailOver(blockedReason, explanation string) error {
	metrics.FailOverBlocked.
		WithLabelValues(r.kubegresContext.Kubegres.Namespace, r.kubegresContext.Kubegres.Name, blockedReason).
		Inc()

	message := "FailOver: refusing to promote a Replica because " + explanation +
		". The cluster requires manual intervention: promote a Replica explicitly with " +
		"'failover.promotePod', or relax 'failover.intelligentFailover' if this level of data loss is acceptable."

	err := errors.New(message)
	r.kubegresContext.Log.ErrorEvent("FailOverBlocked", err, message, "Reason", blockedReason)

	r.kubegresContext.Status.UpdateFailOver(func(status *v1.KubegresFailOverStatus) {
		status.BlockedReason = blockedReason
	})

	return err
}

// recordPromotion notes the promotion on the Kubegres resource and in the metrics.
//
// It runs only when the StatefulSet is actually relabelled as the Primary, not on every
// election. Selection runs again after the waiting phase, so counting there would count a single
// failover several times.
func (r *PrimaryToReplicaFailOver) recordPromotion(newPrimary statefulset.StatefulSetWrapper, decisionReason string) {
	metrics.FailOverDecisions.
		WithLabelValues(r.kubegresContext.Kubegres.Namespace, r.kubegresContext.Kubegres.Name, decisionReason).
		Inc()

	r.kubegresContext.Status.UpdateFailOver(func(status *v1.KubegresFailOverStatus) {
		status.LastPromotedPod = newPrimary.Pod.Pod.Name
		status.LastPromotionReason = decisionReason
		status.LastPromotionEpochInSeconds = time.Now().Unix()
		status.BlockedReason = ""
	})
}

func (r *PrimaryToReplicaFailOver) logElection(outcome SelectionOutcome) {
	r.kubegresContext.Log.InfoEvent("FailOverReplicaElected",
		"FailOver: "+outcome.Explanation,
		"Candidates queried", outcome.ProbedCount,
		"Replicas not selected", formatRejections(outcome.Rejections))
}

// lastKnownPrimaryWalPosition reads back the WAL position last seen on a healthy Primary. It is
// zero when we have none, and selection then measures lag against the best candidate instead.
func (r *PrimaryToReplicaFailOver) lastKnownPrimaryWalPosition() postgres.LSN {
	recorded := r.kubegresContext.Kubegres.Status.FailOver.LastKnownPrimaryWalLsn
	if recorded == "" {
		return 0
	}

	lsn, err := postgres.ParseLSN(recorded)
	if err != nil {
		r.kubegresContext.Log.Error(err,
			"Ignoring the recorded Primary WAL position because it could not be parsed.",
			"Recorded value", recorded)
		return 0
	}

	return lsn
}

func (r *PrimaryToReplicaFailOver) publishReplicaLag(candidates []Candidate, referenceLSN postgres.LSN) {
	if referenceLSN.IsZero() {
		return
	}

	namespace := r.kubegresContext.Kubegres.Namespace
	clusterName := r.kubegresContext.Kubegres.Name

	for _, candidate := range candidates {
		if candidate.ProbeErr != nil {
			continue
		}

		metrics.ReplicaWalLagBytes.
			WithLabelValues(namespace, clusterName, metrics.InstanceIndexLabel(candidate.InstanceIndex)).
			Set(float64(postgres.Max(referenceLSN, candidate.Health.PromotionLSN()).
				Distance(candidate.Health.PromotionLSN())))
	}
}

// ---------------------------------------------------------------------------------------
// Manual promotion safety
// ---------------------------------------------------------------------------------------

// verifyManualPromotionCandidate runs the same health checks on a 'failover.promotePod' request
// as on an automatic election.
//
// A manual request shows clear intent, but it is not evidence that the named Replica is safe:
// someone picking a Pod name out of kubectl output can see no more about WAL positions than the
// readiness-based selector could. The request goes ahead once checked, or is refused with the
// reason, unless 'allowUnsafeManualPromotion' says to promote anyway.
func (r *PrimaryToReplicaFailOver) verifyManualPromotionCandidate(candidate statefulset.StatefulSetWrapper) error {
	if !r.config.IntelligentFailoverEnabled || r.config.AllowUnsafeManualPromotion {
		return nil
	}

	referenceLSN := r.lastKnownPrimaryWalPosition()
	probed := r.prober.ProbeReplicas([]statefulset.StatefulSetWrapper{candidate})
	r.publishReplicaLag(probed, referenceLSN)

	outcome := SelectByWalPosition(probed, r.config.SelectionConstraints(referenceLSN))

	if outcome.AllProbesFailed {
		if r.config.FallbackToLegacy {
			r.kubegresContext.Log.InfoEvent("ManualFailOverWalVerificationUnavailable",
				"FailOver: the Replica requested through 'failover.promotePod' could not be queried for its "+
					"replication state, so it is being promoted unverified.",
				"Details", outcome.Explanation)
			return nil
		}
		return r.blockFailOver(metrics.BlockReasonUnsafeManualPromotion, outcome.Explanation)
	}

	if outcome.Winner == nil {
		return r.blockFailOver(metrics.BlockReasonUnsafeManualPromotion,
			fmt.Sprintf("the Replica requested through 'failover.promotePod' is not safe to promote: %s. "+
				"Set 'failover.intelligentFailover.allowUnsafeManualPromotion' to true to promote it anyway",
				outcome.Explanation))
	}

	return nil
}

// ---------------------------------------------------------------------------------------
// Steady-state readiness
// ---------------------------------------------------------------------------------------

// ObserveClusterFailOverReadiness answers, while everything is healthy, the question that
// matters: if the Primary died right now, would a failover work?
//
// It also records the Primary's current WAL position, which is what replica lag is measured
// against once the Primary is gone and can no longer be asked.
func (r *PrimaryToReplicaFailOver) ObserveClusterFailOverReadiness() {
	if !r.config.IntelligentFailoverEnabled || !r.isPrimaryDbReady() {
		return
	}

	r.pruneDepartedReplicas()

	if !r.shouldObserveReadinessNow() {
		return
	}

	namespace := r.kubegresContext.Kubegres.Namespace
	clusterName := r.kubegresContext.Kubegres.Name

	referenceLSN := r.lastKnownPrimaryWalPosition()
	if currentLSN, err := r.prober.ProbePrimaryWalPosition(); err != nil {
		r.kubegresContext.Log.Info("Unable to read the Primary's current WAL position; "+
			"replication lag will be measured against the last recorded position.", "Error", err.Error())
	} else {
		referenceLSN = currentLSN
		r.kubegresContext.Status.UpdateFailOver(func(status *v1.KubegresFailOverStatus) {
			status.LastKnownPrimaryWalLsn = currentLSN.String()
			status.LastKnownPrimaryWalLsnEpochInSeconds = time.Now().Unix()
		})
	}

	eligible := r.eligibleReplicas()
	if len(eligible) == 0 {
		metrics.ClusterFailOverReady.WithLabelValues(namespace, clusterName).Set(0)
		return
	}

	candidates := r.prober.ProbeReplicas(eligible)
	r.publishReplicaLag(candidates, referenceLSN)

	outcome := SelectByWalPosition(candidates, r.config.SelectionConstraints(referenceLSN))
	if outcome.Winner != nil {
		metrics.ClusterFailOverReady.WithLabelValues(namespace, clusterName).Set(1)
		return
	}

	metrics.ClusterFailOverReady.WithLabelValues(namespace, clusterName).Set(0)
	r.kubegresContext.Log.InfoEvent("ClusterNotFailOverReady",
		"No Replica could be safely promoted if the Primary failed right now: "+outcome.Explanation)
}

// pruneDepartedReplicas closes connections and drops metric series for Replicas that are gone.
//
// Kubegres gives every new Replica a higher index, so a cluster that has failed over often would
// otherwise collect one dead connection and one frozen lag series per Replica it ever had.
func (r *PrimaryToReplicaFailOver) pruneDepartedReplicas() {
	deployed := r.resourcesStates.StatefulSets.Replicas.All.GetAllSortedByInstanceIndex()

	liveInstanceIndexes := make([]int32, 0, len(deployed))
	for _, replicaStatefulSet := range deployed {
		liveInstanceIndexes = append(liveInstanceIndexes, replicaStatefulSet.InstanceIndex)
	}

	r.kubegresContext.PruneReplicaSQLConnections(liveInstanceIndexes)
	metrics.PruneReplicaSeries(r.kubegresContext.Kubegres.Namespace, r.kubegresContext.Kubegres.Name, liveInstanceIndexes)
}

func (r *PrimaryToReplicaFailOver) shouldObserveReadinessNow() bool {
	key := r.kubegresContext.Kubegres.Namespace + "/" + r.kubegresContext.Kubegres.Name

	lastReadinessObservation.mu.Lock()
	defer lastReadinessObservation.mu.Unlock()

	now := time.Now()
	if observedAt, found := lastReadinessObservation.at[key]; found &&
		now.Sub(observedAt) < clusterFailOverReadinessInterval {
		return false
	}

	lastReadinessObservation.at[key] = now
	return true
}
