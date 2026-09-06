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

// Package replicahealth models the PostgreSQL-level replication health of a database
// instance. Kubernetes readiness only tells us that a Pod accepts connections; these are the
// facts that tell us whether promoting that instance would lose data.
package replicahealth

import (
	"time"

	"reactive-tech.io/kubegres/internal/postgres"
)

// Status is a point-in-time observation of one PostgreSQL instance's replication state.
type Status struct {
	// InRecovery is pg_is_in_recovery(). An instance that is not in recovery is already a
	// primary on its own timeline and must never be promoted again.
	InRecovery bool

	// TimelineID is the instance's current timeline. Every promotion forks a new timeline, so
	// two replicas on different timelines carry divergent histories and their LSNs are not
	// comparable.
	TimelineID uint32

	// ReplayLSN is pg_last_wal_replay_lsn(): how far the instance has actually applied WAL.
	ReplayLSN postgres.LSN

	// ReceiveLSN is pg_last_wal_receive_lsn(): how far the instance has durably received WAL.
	// It is never behind ReplayLSN and is the better predictor of where the instance will end
	// up once promoted, because promotion replays whatever WAL is already on disk.
	ReceiveLSN postgres.LSN

	// WalReceiverStatus is pg_stat_wal_receiver.status ("streaming", "catchup", ...). It is
	// empty when no WAL receiver process is running, which is how a broken WAL stream shows up.
	WalReceiverStatus string

	// WalReceiverPresent reports whether a WAL receiver process exists at all.
	WalReceiverPresent bool

	// LastMsgReceiptAge is how long ago the WAL receiver last heard from the primary. It is
	// negative when unknown (no receiver, or the column was NULL).
	LastMsgReceiptAge time.Duration

	// ObservedAt is when the probe ran.
	ObservedAt time.Time
}

// PromotionLSN is the WAL position the instance would hold immediately after being promoted.
//
// Promotion replays every WAL record already present locally, so the received position — not
// the currently replayed one — is what decides how much of the failed primary's history
// survives.
func (s Status) PromotionLSN() postgres.LSN {
	return postgres.Max(s.ReplayLSN, s.ReceiveLSN)
}

// IsStreaming reports whether the instance currently has a live WAL stream from its upstream.
//
// A replica can be Kubernetes-ready and still not be streaming — for instance after
// "requested WAL segment has already been removed" tears the receiver down. Such a replica is
// frozen at whatever it last replayed and is a poor promotion candidate.
func (s Status) IsStreaming() bool {
	return s.WalReceiverPresent && s.WalReceiverStatus == "streaming"
}
