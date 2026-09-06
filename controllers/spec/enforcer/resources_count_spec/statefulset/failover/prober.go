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
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/metrics"
	"reactive-tech.io/kubegres/controllers/states/statefulset"
	"reactive-tech.io/kubegres/internal/postgres"
	"reactive-tech.io/kubegres/internal/replicahealth"
	replicaHealthRepo "reactive-tech.io/kubegres/internal/replicahealth/repo"
)

// Prober reads the PostgreSQL replication state of database instances.
type Prober interface {
	// ProbeReplicas queries every given Replica concurrently and returns one Candidate each,
	// carrying either its replication state or the error that prevented reading it.
	ProbeReplicas(replicas []statefulset.StatefulSetWrapper) []Candidate

	// ProbePrimaryWalPosition returns pg_current_wal_lsn() from the cluster's Primary.
	ProbePrimaryWalPosition() (postgres.LSN, error)
}

// connectionProber is the Prober backed by the operator's PostgreSQL connections.
type connectionProber struct {
	kubegresContext ctx.KubegresContext
	timeout         time.Duration
}

// NewProber builds a Prober that reaches Replicas over the shared ConnectionStore.
func NewProber(kubegresContext ctx.KubegresContext, timeout time.Duration) Prober {
	return &connectionProber{kubegresContext: kubegresContext, timeout: timeout}
}

func (p *connectionProber) ProbeReplicas(replicas []statefulset.StatefulSetWrapper) []Candidate {
	candidates := make([]Candidate, len(replicas))

	// Query the Replicas concurrently: a failover is already an outage, and probing a wedged
	// Replica serially would add the full timeout per Replica to the recovery time.
	var waitGroup sync.WaitGroup
	for i, replicaStatefulSet := range replicas {
		candidates[i] = Candidate{
			InstanceIndex:   replicaStatefulSet.InstanceIndex,
			StatefulSetName: replicaStatefulSet.StatefulSet.Name,
			PodName:         replicaStatefulSet.Pod.Pod.Name,
		}

		waitGroup.Add(1)
		go func(slot int, replicaStatefulSet statefulset.StatefulSetWrapper) {
			defer waitGroup.Done()

			health, err := p.probeOne(replicaStatefulSet)
			candidates[slot].Health = health
			candidates[slot].ProbeErr = err
		}(i, replicaStatefulSet)
	}
	waitGroup.Wait()

	for _, candidate := range candidates {
		if candidate.ProbeErr == nil {
			continue
		}

		metrics.ReplicaQueryErrors.WithLabelValues(
			p.kubegresContext.Kubegres.Namespace,
			p.kubegresContext.Kubegres.Name,
			metrics.InstanceIndexLabel(candidate.InstanceIndex),
		).Inc()

		p.kubegresContext.Log.Error(candidate.ProbeErr,
			"FailOver: unable to read the replication state of a Replica.",
			"Replica", candidate.StatefulSetName)
	}

	return candidates
}

func (p *connectionProber) probeOne(replicaStatefulSet statefulset.StatefulSetWrapper) (replicahealth.Status, error) {
	podIP := replicaStatefulSet.Pod.Pod.Status.PodIP
	if podIP == "" {
		return replicahealth.Status{}, fmt.Errorf("Replica %q has no Pod IP", replicaStatefulSet.StatefulSet.Name)
	}

	connection, err := p.kubegresContext.GetReplicaSQLConnection(
		replicaStatefulSet.InstanceIndex, podIP, p.port())
	if err != nil {
		return replicahealth.Status{}, err
	}

	db := connection.DB()
	if db == nil {
		return replicahealth.Status{}, fmt.Errorf("no usable connection to Replica %q", replicaStatefulSet.StatefulSet.Name)
	}

	queryCtx, cancel := context.WithTimeout(p.kubegresContext.Ctx, p.timeout)
	defer cancel()

	return replicaHealthRepo.New(db).Probe(queryCtx)
}

func (p *connectionProber) ProbePrimaryWalPosition() (postgres.LSN, error) {
	connection, found := p.kubegresContext.GetSQLConnection()
	if !found {
		return 0, errors.New("no connection to the Primary is available")
	}

	db := connection.DB()
	if db == nil {
		return 0, errors.New("no usable connection to the Primary")
	}

	queryCtx, cancel := context.WithTimeout(p.kubegresContext.Ctx, p.timeout)
	defer cancel()

	return replicaHealthRepo.New(db).CurrentWalLSN(queryCtx)
}

func (p *connectionProber) port() int32 {
	if port := p.kubegresContext.Kubegres.Spec.Port; port > 0 {
		return port
	}
	return ctx.DefaultContainerPortNumber
}
