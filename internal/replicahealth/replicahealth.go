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

// Package replicahealth describes the replication state of a PostgreSQL instance: the facts
// that say whether promoting it would lose data, which Kubernetes readiness does not.
package replicahealth

import (
	"time"

	"reactive-tech.io/kubegres/internal/postgres"
)

// Status is a point-in-time observation of one PostgreSQL instance's replication state.
type Status struct {
	// InRecovery is pg_is_in_recovery(). An instance not in recovery is already a primary on
	// its own timeline and must not be promoted again.
	InRecovery bool

	// TimelineID is the instance's current timeline. Each promotion starts a new one, so LSNs
	// from different timelines are not comparable.
	TimelineID uint32

	// ReplayLSN is pg_last_wal_replay_lsn(): how far the instance has actually applied WAL.
	ReplayLSN postgres.LSN

	// ReceiveLSN is pg_last_wal_receive_lsn(): how far the instance has received WAL. Promotion
	// replays whatever is already on disk, so this predicts where it ends up.
	ReceiveLSN postgres.LSN

	// WalReceiverStatus is pg_stat_wal_receiver.status ("streaming", "catchup", ...). It is
	// empty when no WAL receiver is running, which is how a broken stream shows up.
	WalReceiverStatus string

	// WalReceiverPresent reports whether a WAL receiver process exists.
	WalReceiverPresent bool

	// LastMsgReceiptAge is how long ago the WAL receiver last heard from the primary. It is
	// negative when unknown.
	LastMsgReceiptAge time.Duration

	// ObservedAt is when the probe ran.
	ObservedAt time.Time
}

// PromotionLSN is where the instance would be right after promotion.
//
// Promotion replays every WAL record already on disk, so the received position, not the
// replayed one, decides how much of the failed primary's history survives.
func (s Status) PromotionLSN() postgres.LSN {
	return postgres.Max(s.ReplayLSN, s.ReceiveLSN)
}

// IsStreaming reports whether the instance has a live WAL stream.
//
// A replica can be ready and still not streaming, for example after "requested WAL segment has
// already been removed" kills the receiver. It is then stuck at whatever it last replayed.
func (s Status) IsStreaming() bool {
	return s.WalReceiverPresent && s.WalReceiverStatus == "streaming"
}
