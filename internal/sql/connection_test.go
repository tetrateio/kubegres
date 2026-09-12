package sql_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	kubegresSQL "reactive-tech.io/kubegres/internal/sql"
)

type fakeConnection struct {
	closed bool
}

func (c *fakeConnection) DB() *sql.DB              { return nil }
func (c *fakeConnection) DSN() string              { return "" }
func (c *fakeConnection) Close() error             { c.closed = true; return nil }
func (c *fakeConnection) Reconnect(_ string) error { return nil }

func TestSnapshotDetachesFromTheOriginal(t *testing.T) {
	// A replica connection is copied from the primary's: same credentials, database and TLS
	// material, different endpoint. Changing the copy must not touch the original.
	primary := kubegresSQL.NewDSNData()
	primary.Apply(func(d *kubegresSQL.DSNData) {
		d.Host = "postgres"
		d.Password = "s3cret"
		d.Database = "appdb"
		d.RootCertPath = "/certs/ca.crt"
	})

	replica := primary.Snapshot()
	replica.Host = "10.1.2.3"

	require.Equal(t, "postgres", primary.Snapshot().Host)
	require.Contains(t, replica.Build(), "host=10.1.2.3")
	require.Contains(t, replica.Build(), "password=s3cret")
	require.Contains(t, replica.Build(), "dbname=appdb")
	require.Contains(t, replica.Build(), "sslrootcert=/certs/ca.crt")
	require.NotContains(t, replica.Build(), "host=postgres")
}

func TestStringMasksThePassword(t *testing.T) {
	dsnData := kubegresSQL.NewDSNData()
	dsnData.Apply(func(d *kubegresSQL.DSNData) { d.Password = "s3cret" })

	require.Contains(t, dsnData.String(), "password=******")
	require.NotContains(t, dsnData.String(), "s3cret")
	require.Contains(t, dsnData.Build(), "password=s3cret", "the real DSN still carries the password")
}

func TestConcurrentReadsAndWritesAreSafe(t *testing.T) {
	// The Kubegres and Secret reconcilers write this while connection users read it. Run under
	// -race to catch a regression in the locking.
	dsnData := kubegresSQL.NewDSNData()

	var waitGroup sync.WaitGroup
	for i := 0; i < 8; i++ {
		waitGroup.Add(2)

		go func() {
			defer waitGroup.Done()
			for j := 0; j < 200; j++ {
				dsnData.Apply(func(d *kubegresSQL.DSNData) { d.Password = "rotating" })
			}
		}()

		go func() {
			defer waitGroup.Done()
			for j := 0; j < 200; j++ {
				require.True(t, strings.Contains(dsnData.Build(), "user=postgres"))
				_ = dsnData.String()
				_ = dsnData.Snapshot()
			}
		}()
	}
	waitGroup.Wait()
}

func TestABareDSNDataStillBuilds(t *testing.T) {
	// Nothing builds one this way today, but a struct literal must not panic on a nil guard.
	dsnData := &kubegresSQL.DSNData{Host: "localhost", Port: "5432", Username: "postgres", Database: "postgres", SSLMode: "disable"}

	require.Equal(t, "host=localhost port=5432 user=postgres dbname=postgres sslmode=disable", dsnData.Build())
}

// TestAReplicaShapedDSNConnects builds a DSN the way GetReplicaSQLConnection does - the primary's
// settings with the endpoint swapped for a bare IP - and connects with it.
//
// The unit tests above only check the DSN string, which is not enough: the driver has its own
// idea of which keywords mean something. Putting the IP in "hostaddr", as libpq would accept,
// produces a DSN that looks right and silently dials a Unix socket instead.
func TestAReplicaShapedDSNConnects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers test in short mode")
	}

	container, err := postgres.Run(t.Context(), "postgres:14.5",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("probe-password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	mappedPort, err := container.MappedPort(t.Context(), "5432")
	require.NoError(t, err)

	// Stand in for the primary connection the DBConnectionReconciler maintains.
	primary := kubegresSQL.NewDSNData()
	primary.Apply(func(d *kubegresSQL.DSNData) {
		d.Host = "postgres-service"
		d.Port = "5432"
		d.Password = "probe-password"
	})

	// Derive the replica connection: same credentials, endpoint replaced by the Pod IP.
	replica := primary.Snapshot()
	replica.Host = "127.0.0.1"
	replica.Port = mappedPort.Port()

	conn, err := kubegresSQL.NewDynamicDSNConnection(replica)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, conn.DB().PingContext(ctx), "a replica-shaped DSN must actually connect")
}
