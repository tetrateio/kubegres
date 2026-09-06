package sql_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	kubegresSQL "reactive-tech.io/kubegres/internal/sql"
)

func TestAPrimaryConnectionIDIsUnchangedByTheReplicaAddressing(t *testing.T) {
	// Every existing call site builds ConnectionID{Name, Namespace} and means "the primary".
	// Adding replica addressing must leave those keys byte-identical.
	primary := kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}

	require.True(t, primary.IsPrimary())
	require.Equal(t, "default/postgres", primary.String())
}

func TestReplicaConnectionIDsAreDistinctPerInstance(t *testing.T) {
	first := kubegresSQL.ReplicaConnectionID("default", "postgres", 2)
	second := kubegresSQL.ReplicaConnectionID("default", "postgres", 3)
	primary := kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}

	require.False(t, first.IsPrimary())
	require.Equal(t, "default/postgres#2", first.String())
	require.NotEqual(t, first, second)
	require.NotEqual(t, first, primary)
}

func TestStoreKeepsPrimaryAndReplicaConnectionsSideBySide(t *testing.T) {
	store := kubegresSQL.NewConnectionStore()

	primaryID := kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}
	replicaID := kubegresSQL.ReplicaConnectionID("default", "postgres", 2)

	primaryConn := &fakeConnection{}
	replicaConn := &fakeConnection{}
	store.Set(primaryID, primaryConn)
	store.Set(replicaID, replicaConn)

	got, found := store.Get(primaryID)
	require.True(t, found)
	require.Same(t, primaryConn, got)

	got, found = store.Get(replicaID)
	require.True(t, found)
	require.Same(t, replicaConn, got)
}

func TestReplicaKeysScopesToOneCluster(t *testing.T) {
	store := kubegresSQL.NewConnectionStore()

	store.Set(kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}, &fakeConnection{})
	store.Set(kubegresSQL.ReplicaConnectionID("default", "postgres", 2), &fakeConnection{})
	store.Set(kubegresSQL.ReplicaConnectionID("default", "postgres", 3), &fakeConnection{})
	store.Set(kubegresSQL.ReplicaConnectionID("default", "other", 2), &fakeConnection{})
	store.Set(kubegresSQL.ReplicaConnectionID("elsewhere", "postgres", 2), &fakeConnection{})

	keys := store.ReplicaKeys("default", "postgres")

	require.Len(t, keys, 2, "the primary and other clusters' replicas must not be included")
	for _, key := range keys {
		require.False(t, key.IsPrimary())
		require.Equal(t, "default", key.Namespace)
		require.Equal(t, "postgres", key.Name)
	}
}

func TestDeleteClosesTheConnection(t *testing.T) {
	// Kubegres numbers every new replica with a fresh index, so a cluster that has failed over
	// often would otherwise leak one live *sql.DB per replica it has ever had.
	store := kubegresSQL.NewConnectionStore()
	replicaID := kubegresSQL.ReplicaConnectionID("default", "postgres", 2)

	conn := &fakeConnection{}
	store.Set(replicaID, conn)

	require.NoError(t, store.Delete(replicaID))
	require.True(t, conn.closed)

	_, found := store.Get(replicaID)
	require.False(t, found)

	require.NoError(t, store.Delete(replicaID), "deleting an absent key is a no-op")
}

func TestKeysListsEverythingHeld(t *testing.T) {
	store := kubegresSQL.NewConnectionStore()
	require.Empty(t, store.Keys())

	store.Set(kubegresSQL.ConnectionID{Name: "postgres", Namespace: "default"}, &fakeConnection{})
	store.Set(kubegresSQL.ReplicaConnectionID("default", "postgres", 2), &fakeConnection{})

	require.Len(t, store.Keys(), 2)
}
