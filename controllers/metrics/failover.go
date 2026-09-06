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

// Package metrics exposes the Prometheus metrics that make failover behaviour observable:
// which Replica was promoted and why, how far behind each candidate was, and — crucially —
// whether a failover would succeed if the Primary died right now.
package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Reasons reported by FailOverDecisions.
const (
	// DecisionReasonHighestLsn: the candidate carrying the most WAL was promoted.
	DecisionReasonHighestLsn = "highest_lsn"
	// DecisionReasonFallback: replication state was unreadable, so readiness-based selection
	// was used instead.
	DecisionReasonFallback = "fallback"
	// DecisionReasonLegacy: WAL-aware selection is not enabled on this cluster.
	DecisionReasonLegacy = "legacy"
	// DecisionReasonManual: an operator named the Pod through spec.failover.promotePod.
	DecisionReasonManual = "manual"
)

// Reasons reported by FailOverBlocked.
const (
	// BlockReasonLagExceeded: the best candidate was further behind than maxReplicationLag.
	BlockReasonLagExceeded = "lag_exceeded"
	// BlockReasonStaleTimeline: every reachable candidate was on an abandoned timeline.
	BlockReasonStaleTimeline = "stale_timeline"
	// BlockReasonNoHealthyCandidate: no candidate passed the replication health checks.
	BlockReasonNoHealthyCandidate = "no_healthy_candidate"
	// BlockReasonUnreachable: no candidate could be reached and fallback is disabled.
	BlockReasonUnreachable = "unreachable"
	// BlockReasonUnsafeManualPromotion: the Pod named in promotePod failed its health checks.
	BlockReasonUnsafeManualPromotion = "unsafe_manual_promotion"
)

var (
	clusterLabels  = []string{"namespace", "cluster"}
	replicaLabels  = []string{"namespace", "cluster", "instance_index"}
	decisionLabels = []string{"namespace", "cluster", "reason"}

	// FailOverDecisions counts promotions by why the winner was chosen.
	FailOverDecisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubegres_failover_decision_total",
		Help: "Number of Replica promotions, labelled by how the promoted Replica was selected.",
	}, decisionLabels)

	// FailOverCandidates records how many Replicas were eligible at the last election. Zero
	// explains why a failover could not happen.
	FailOverCandidates = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubegres_failover_candidates",
		Help: "Number of Replicas eligible for promotion at the last failover election.",
	}, clusterLabels)

	// FailOverDuration measures detection through to the new Primary being ready.
	FailOverDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kubegres_failover_duration_seconds",
		Help:    "Time from starting a failover to the promoted Primary being ready.",
		Buckets: []float64{5, 10, 20, 30, 45, 60, 90, 120, 180, 240, 300},
	}, clusterLabels)

	// FailOverBlocked counts refusals to promote, by which guardrail refused.
	FailOverBlocked = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubegres_failover_blocked_total",
		Help: "Number of times Kubegres refused to promote a Replica because a safety check failed.",
	}, decisionLabels)

	// ClusterFailOverReady is the steady-state answer to "would a failover work right now?".
	// 1 means at least one Replica is on the current timeline and within the lag threshold.
	ClusterFailOverReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubegres_cluster_failover_ready",
		Help: "1 if at least one Replica could be safely promoted right now, 0 otherwise.",
	}, clusterLabels)

	// ReplicaQueryErrors counts failures to read a Replica's replication state.
	ReplicaQueryErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubegres_replica_query_errors_total",
		Help: "Number of failures to query a Replica's replication state.",
	}, replicaLabels)

	// ReplicaWalLagBytes is how far each Replica trails the last known Primary WAL position.
	ReplicaWalLagBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubegres_replica_wal_lag_bytes",
		Help: "WAL bytes by which a Replica trails the last known Primary WAL position.",
	}, replicaLabels)
)

func init() {
	metrics.Registry.MustRegister(
		FailOverDecisions,
		FailOverCandidates,
		FailOverDuration,
		FailOverBlocked,
		ClusterFailOverReady,
		ReplicaQueryErrors,
		ReplicaWalLagBytes,
	)
}

// InstanceIndexLabel renders a StatefulSet instance index as a metric label value.
func InstanceIndexLabel(instanceIndex int32) string {
	return strconv.Itoa(int(instanceIndex))
}

// PruneReplicaSeries drops the per-Replica series of instances that are no longer deployed.
//
// Kubegres assigns every new Replica a fresh, monotonically increasing instance index, so a
// cluster that has failed over many times would otherwise leave one permanently frozen lag
// series behind per Replica it has ever had.
func PruneReplicaSeries(namespace, cluster string, liveInstanceIndexes []int32) {
	live := make(map[string]struct{}, len(liveInstanceIndexes))
	for _, instanceIndex := range liveInstanceIndexes {
		live[InstanceIndexLabel(instanceIndex)] = struct{}{}
	}

	for _, collected := range collectReplicaLagLabels(namespace, cluster) {
		if _, stillDeployed := live[collected]; stillDeployed {
			continue
		}
		ReplicaWalLagBytes.DeleteLabelValues(namespace, cluster, collected)
	}
}

// collectReplicaLagLabels lists the instance indexes currently carrying a lag series for one
// cluster, by reading them back out of the collector.
func collectReplicaLagLabels(namespace, cluster string) []string {
	ch := make(chan prometheus.Metric, 128)
	go func() {
		ReplicaWalLagBytes.Collect(ch)
		close(ch)
	}()

	var instanceIndexes []string
	for metric := range ch {
		var dto dtoMetric
		if err := metric.Write(&dto.Metric); err != nil {
			continue
		}

		var namespaceLabel, clusterLabel, instanceLabel string
		for _, label := range dto.Metric.GetLabel() {
			switch label.GetName() {
			case "namespace":
				namespaceLabel = label.GetValue()
			case "cluster":
				clusterLabel = label.GetValue()
			case "instance_index":
				instanceLabel = label.GetValue()
			}
		}

		if namespaceLabel == namespace && clusterLabel == cluster && instanceLabel != "" {
			instanceIndexes = append(instanceIndexes, instanceLabel)
		}
	}

	return instanceIndexes
}
