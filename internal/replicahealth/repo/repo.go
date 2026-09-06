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

package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"reactive-tech.io/kubegres/internal/postgres"
	"reactive-tech.io/kubegres/internal/replicahealth"
)

// Querier abstracts the subset of *sql.DB this repository needs, mirroring the replication
// slot repository so that both can be unit-tested against a fake.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Repository reads replication health from a single PostgreSQL instance.
type Repository interface {
	// Probe reports the instance's recovery state, timeline and WAL positions.
	Probe(ctx context.Context) (replicahealth.Status, error)
	// CurrentWalLSN returns pg_current_wal_lsn() and is only valid on a primary.
	CurrentWalLSN(ctx context.Context) (postgres.LSN, error)
}

// New builds a Repository over the given querier.
func New(db Querier) Repository {
	return &repo{db: db, now: time.Now}
}

type repo struct {
	db  Querier
	now func() time.Time
}

// coreStateStmt reads the facts that are always available, whether or not a WAL receiver is
// running. pg_control_checkpoint() is the authoritative source for the timeline because it is
// read from the control file and therefore survives a torn-down WAL receiver.
//
// The LSN functions return NULL on an instance that is not in recovery, so they are cast to
// text and coalesced rather than scanned as a nullable type.
const coreStateStmt = `
	SELECT
		pg_is_in_recovery(),
		(SELECT timeline_id FROM pg_control_checkpoint()),
		COALESCE(pg_last_wal_replay_lsn()::text, ''),
		COALESCE(pg_last_wal_receive_lsn()::text, '')
	`

// walReceiverStmt reads the streaming state. It returns no rows when no WAL receiver process
// is running, which is exactly the "broken WAL stream" case we need to detect.
const walReceiverStmt = `
	SELECT
		status,
		COALESCE(received_tli, 0),
		COALESCE(EXTRACT(EPOCH FROM (now() - last_msg_receipt_time)), -1)
	FROM pg_stat_wal_receiver
	LIMIT 1
	`

func (r *repo) Probe(ctx context.Context) (replicahealth.Status, error) {
	status := replicahealth.Status{
		ObservedAt:        r.now(),
		LastMsgReceiptAge: -1,
	}

	var replayLSNText, receiveLSNText string
	err := r.db.QueryRowContext(ctx, coreStateStmt).Scan(
		&status.InRecovery,
		&status.TimelineID,
		&replayLSNText,
		&receiveLSNText,
	)
	if err != nil {
		return replicahealth.Status{}, fmt.Errorf("failed to read replication state: %w", err)
	}

	if status.ReplayLSN, err = postgres.ParseLSN(replayLSNText); err != nil {
		return replicahealth.Status{}, fmt.Errorf("failed to parse replay LSN: %w", err)
	}
	if status.ReceiveLSN, err = postgres.ParseLSN(receiveLSNText); err != nil {
		return replicahealth.Status{}, fmt.Errorf("failed to parse receive LSN: %w", err)
	}

	if err := r.readWalReceiver(ctx, &status); err != nil {
		return replicahealth.Status{}, err
	}

	return status, nil
}

func (r *repo) readWalReceiver(ctx context.Context, status *replicahealth.Status) error {
	var (
		receiverStatus string
		receivedTli    uint32
		lastMsgAgeSecs float64
	)

	err := r.db.QueryRowContext(ctx, walReceiverStmt).Scan(&receiverStatus, &receivedTli, &lastMsgAgeSecs)
	if errors.Is(err, sql.ErrNoRows) {
		// No WAL receiver: either this instance is a primary, or its replication stream is
		// broken. Either way there is nothing more to read and it is not an error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read WAL receiver state: %w", err)
	}

	status.WalReceiverPresent = true
	status.WalReceiverStatus = receiverStatus
	if lastMsgAgeSecs >= 0 {
		status.LastMsgReceiptAge = time.Duration(lastMsgAgeSecs * float64(time.Second))
	}

	// A streaming replica can already be following a newer timeline than the one recorded in
	// its control file, which is only rewritten at the next checkpoint. Take the higher of the
	// two so that a replica is never wrongly discarded as being on a stale timeline.
	if receivedTli > status.TimelineID {
		status.TimelineID = receivedTli
	}

	return nil
}

func (r *repo) CurrentWalLSN(ctx context.Context) (postgres.LSN, error) {
	var lsnText string
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(pg_current_wal_lsn()::text, '')`).Scan(&lsnText); err != nil {
		return 0, fmt.Errorf("failed to read current WAL LSN: %w", err)
	}

	lsn, err := postgres.ParseLSN(lsnText)
	if err != nil {
		return 0, fmt.Errorf("failed to parse current WAL LSN: %w", err)
	}
	return lsn, nil
}
