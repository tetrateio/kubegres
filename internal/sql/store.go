package sql

import "sync"

// ConnectionID represents a unique identifier for a database connection.
type ConnectionID struct {
	Name      string
	Namespace string
}

func (c ConnectionID) String() string {
	return c.Namespace + "/" + c.Name
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
