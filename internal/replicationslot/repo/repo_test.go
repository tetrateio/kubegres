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
	replicationSlotRepo "reactive-tech.io/kubegres/internal/replicationslot/repo"
)

func TestReplicationSlotsWithTestcontainers(t *testing.T) {
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
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	require.NoError(t, db.PingContext(t.Context()))

	repo := replicationSlotRepo.New(db)
	createdSlot, err := repo.CreateSlot(t.Context(), "my_awesome_test_slot")

	require.NoError(t, err)
	require.NotNil(t, createdSlot)

	require.Equal(t, "my_awesome_test_slot", createdSlot.Name)
	require.False(t, createdSlot.Active, "Slot should be inactive until a client connects")

	_, err = repo.CreateSlot(t.Context(), "my_awesome_test_slot")
	require.ErrorIs(t, err, replicationSlotRepo.ErrAlreadyExist)

	createdSlot2, err := repo.CreateSlot(t.Context(), "my_awesome_test_slot_2")
	require.NoError(t, err)
	slots, err := repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, slots, 2)
	require.Contains(t, slots, createdSlot)
	require.Contains(t, slots, createdSlot2)

	err = repo.DeleteSlot(t.Context(), "my_awesome_test_slot")
	require.NoError(t, err)
	err = repo.DeleteSlot(t.Context(), "non_existent_slot")
	require.Error(t, err)

	_, err = repo.GetSlot(t.Context(), "my_awesome_test_slot")
	require.ErrorIs(t, err, replicationSlotRepo.ErrNotFound)

	err = repo.DeleteSlot(t.Context(), createdSlot2.Name)
	require.NoError(t, err)

	listAll, err := repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Empty(t, listAll, "All slots should be deleted")
}
