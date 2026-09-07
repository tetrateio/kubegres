package sql

import (
	"strconv"
	"sync"
)

// ConnectionID identifies a database connection held in the ConnectionStore.
//
// Name and Namespace address the Kubegres resource. Instance picks one database within it: it
// is empty for the primary and holds the StatefulSet instance index for a replica. That keeps
// every existing ConnectionID{Name, Namespace} literal pointing at the primary.
type ConnectionID struct {
	Name      string
	Namespace string
	Instance  string
}

// ReplicaConnectionID builds the ConnectionID for one replica of a Kubegres resource.
func ReplicaConnectionID(namespace, name string, instanceIndex int32) ConnectionID {
	return ConnectionID{
		Namespace: namespace,
		Name:      name,
		Instance:  strconv.Itoa(int(instanceIndex)),
	}
}

func (c ConnectionID) String() string {
	if c.Instance == "" {
		return c.Namespace + "/" + c.Name
	}
	return c.Namespace + "/" + c.Name + "#" + c.Instance
}

// IsPrimary reports whether the ID addresses a primary connection.
func (c ConnectionID) IsPrimary() bool {
	return c.Instance == ""
}

// ConnectionStore is a thread-safe store for database connections indexed by ConnectionID.
type ConnectionStore struct {
	mu      sync.Mutex
	dbConns map[ConnectionID]ConnectionSupplier
}

func NewConnectionStore() *ConnectionStore {
	return &ConnectionStore{
		dbConns: make(map[ConnectionID]ConnectionSupplier),
	}
}

func (s *ConnectionStore) Get(key ConnectionID) (ConnectionSupplier, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, exists := s.dbConns[key]
	return conn, exists
}

func (s *ConnectionStore) Set(key ConnectionID, conn ConnectionSupplier) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dbConns[key] = conn
}

// Delete removes a connection and closes it. Deleting an absent key does nothing.
//
// Without this the store would keep a dead *sql.DB for every replica the cluster ever had.
func (s *ConnectionStore) Delete(key ConnectionID) error {
	s.mu.Lock()
	conn, exists := s.dbConns[key]
	delete(s.dbConns, key)
	s.mu.Unlock()

	if !exists || conn == nil {
		return nil
	}
	return conn.Close()
}

// Keys returns the IDs held, in no particular order.
func (s *ConnectionStore) Keys() []ConnectionID {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]ConnectionID, 0, len(s.dbConns))
	for key := range s.dbConns {
		keys = append(keys, key)
	}
	return keys
}

// ReplicaKeys returns the replica connection IDs held for one Kubegres resource.
func (s *ConnectionStore) ReplicaKeys(namespace, name string) []ConnectionID {
	s.mu.Lock()
	defer s.mu.Unlock()

	var keys []ConnectionID
	for key := range s.dbConns {
		if key.Namespace == namespace && key.Name == name && !key.IsPrimary() {
			keys = append(keys, key)
		}
	}
	return keys
}
