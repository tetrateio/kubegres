package connection

import "sync"

type (
	Key interface {
		String() string
	}

	// SyncMap is a thread-safe map implementation for keys of type struct{} and values of any type.
	SyncMap[K Key, V any] struct {
		mu   sync.Mutex
		keys map[string]K
		vals map[string]V
	}
)

// NewSyncMap is a constructor for SyncMap.
func NewSyncMap[K Key, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{
		keys: make(map[string]K),
		vals: make(map[string]V),
	}
}

func (s *SyncMap[K, V]) Get(key K) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, exists := s.vals[key.String()]
	if !exists {
		var zeroValue V
		return zeroValue, false
	}
	return value, true
}

func (s *SyncMap[K, V]) Set(key K, value V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.keys[key.String()] = key
	s.vals[key.String()] = value
}

func (s *SyncMap[K, V]) KeysAndValues() ([]K, []V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]K, 0, len(s.keys))
	values := make([]V, 0, len(s.vals))

	for _, key := range s.keys {
		keys = append(keys, key)
	}
	for _, value := range s.vals {
		values = append(values, value)
	}

	return keys, values
}
