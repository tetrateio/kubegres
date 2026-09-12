package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	replicaHealthRepo "reactive-tech.io/kubegres/internal/replicahealth/repo"
)

// TestProbeAgainstRealPostgres runs the probe's SQL against a real server. The queries use
// pg_control_checkpoint() and pg_stat_wal_receiver, so a typo or a missing column would
// otherwise only show up during a failover.
func TestProbeAgainstRealPostgres(t *testing.T) {
	// This test requires Docker to be running.
	if testing.Short() {
		t.Skip("skipping testcontainers test in short mode")
	}

	pgContainer, err := postgres.Run(t.Context(),
		"postgres:14.5",
		testcontainers.WithCmd("postgres"),
		testcontainers.WithCmdArgs([]string{"-c", "wal_level=replica"}...),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(1*time.Minute),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Fatalf("failed to terminate container: %s", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(t.Context(), "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.PingContext(t.Context()))

	repo := replicaHealthRepo.New(db)

	status, err := repo.Probe(t.Context())
	require.NoError(t, err)

	// A fresh primary: out of recovery, on timeline 1, with no WAL receiver.
	require.False(t, status.InRecovery)
	require.Equal(t, uint32(1), status.TimelineID)
	require.False(t, status.WalReceiverPresent)
	require.False(t, status.IsStreaming())
	require.Equal(t, time.Duration(-1), status.LastMsgReceiptAge)
	require.False(t, status.ObservedAt.IsZero())

	// pg_last_wal_replay_lsn() is NULL outside recovery. The probe must read that as zero rather
	// than failing to scan.
	require.True(t, status.ReplayLSN.IsZero())
	require.True(t, status.ReceiveLSN.IsZero())
	require.True(t, status.PromotionLSN().IsZero())

	currentLSN, err := repo.CurrentWalLSN(t.Context())
	require.NoError(t, err)
	require.False(t, currentLSN.IsZero(), "a running primary always has a current WAL position")

	// Writing moves the primary's WAL position forward. That is what the operator records as the
	// reference point for replica lag.
	_, err = db.ExecContext(t.Context(), `CREATE TABLE probe_moves_wal (id int)`)
	require.NoError(t, err)

	advancedLSN, err := repo.CurrentWalLSN(t.Context())
	require.NoError(t, err)
	require.Greater(t, advancedLSN, currentLSN)
}
