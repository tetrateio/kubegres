package ctx_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/controllers/ctx/log"
	kubegresSQL "reactive-tech.io/kubegres/internal/sql"
)

// TestGetReplicaSQLConnectionReachesTheReplica connects to a real PostgreSQL through
// GetReplicaSQLConnection, so that the DSN it builds is checked against a driver rather than
// against our own expectations.
//
// This is the test that matters for replica health checks: everything above this layer runs on a
// fake prober, so a DSN the driver quietly refuses to dial would look exactly like an unreachable
// Replica and fall back to readiness-based selection with no sign of a bug.
func TestGetReplicaSQLConnectionReachesTheReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers test in short mode")
	}

	container, err := postgres.Run(t.Context(), "postgres:14.5",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("replica-password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	mappedPort, err := container.MappedPort(t.Context(), "5432")
	require.NoError(t, err)

	kubegres := &v1.Kubegres{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "default"},
	}

	// The primary connection the DBConnectionReconciler maintains, pointed at the Service name.
	// The replica connection inherits its credentials and only replaces the endpoint.
	primaryDsn := kubegresSQL.NewDSNData()
	primaryDsn.Apply(func(d *kubegresSQL.DSNData) {
		d.Host = "postgres"
		d.Port = "5432"
		d.Password = "replica-password"
	})
	primaryConn, err := kubegresSQL.NewDynamicDSNConnection(primaryDsn)
	require.NoError(t, err)

	connectionStore := kubegresSQL.NewConnectionStore()
	connectionStore.Set(kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}, primaryConn)

	kubegresContext := ctx.KubegresContext{
		Kubegres:        kubegres,
		Ctx:             t.Context(),
		ConnectionStore: connectionStore,
		Log: log.LogWrapper{
			Kubegres: kubegres,
			Logger:   logr.Discard(),
			Recorder: record.NewFakeRecorder(16),
		},
	}

	// Stand in for the Pod IP the failover path reads out of the Replica's PodWrapper.
	replicaConn, err := kubegresContext.GetReplicaSQLConnection(2, "127.0.0.1", int32(mappedPort.Int()))
	require.NoError(t, err)

	pingCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, replicaConn.DB().PingContext(pingCtx),
		"the DSN built for a Replica must actually connect")

	// Asking again returns the same connection, re-pointed rather than replaced, so a Replica
	// that moves to a new Pod IP does not leak its old pool.
	sameConn, err := kubegresContext.GetReplicaSQLConnection(2, "127.0.0.1", int32(mappedPort.Int()))
	require.NoError(t, err)
	require.Same(t, replicaConn, sameConn)
	require.Len(t, connectionStore.ReplicaKeys("default", "postgres"), 1)

	// The primary connection is untouched by any of this.
	stillPrimary, found := connectionStore.Get(kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"})
	require.True(t, found)
	require.Same(t, primaryConn, stillPrimary)
	require.Contains(t, primaryConn.DSN(), "host=postgres")

	// Once the Replica is gone its connection is closed and dropped.
	kubegresContext.PruneReplicaSQLConnections(nil)
	require.Empty(t, connectionStore.ReplicaKeys("default", "postgres"))
}
