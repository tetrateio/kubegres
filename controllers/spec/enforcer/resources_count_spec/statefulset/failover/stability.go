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
	"sync"
	"time"

	v1 "reactive-tech.io/kubegres/api/v1"
)

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
