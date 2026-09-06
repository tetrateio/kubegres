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

package postgres

import (
	"fmt"
	"strconv"
	"strings"
)

// LSN is a PostgreSQL Log Sequence Number: a byte offset into the WAL stream.
//
// PostgreSQL renders it as two hexadecimal halves separated by a slash, e.g. "16/B374D848",
// where the left half is the high 32 bits and the right half the low 32 bits. Holding it as a
// single uint64 makes ordering and byte-distance arithmetic trivial, which is what replica
// selection needs.
type LSN uint64

// ParseLSN parses PostgreSQL's "X/Y" textual LSN representation.
//
// An empty string parses to 0: PostgreSQL returns NULL from pg_last_wal_replay_lsn() on an
// instance that has never replayed WAL, and callers scan that NULL into an empty string.
func ParseLSN(s string) (LSN, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	high, low, found := strings.Cut(s, "/")
	if !found {
		return 0, fmt.Errorf("invalid LSN %q: expected the form \"X/Y\"", s)
	}

	highBits, err := strconv.ParseUint(high, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN %q: high half is not a 32-bit hexadecimal number: %w", s, err)
	}

	lowBits, err := strconv.ParseUint(low, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN %q: low half is not a 32-bit hexadecimal number: %w", s, err)
	}

	return LSN(highBits<<32 | lowBits), nil
}

// String renders the LSN the way PostgreSQL does, so that logs and events can be
// cross-referenced against psql output directly.
func (l LSN) String() string {
	return fmt.Sprintf("%X/%X", uint32(l>>32), uint32(l))
}

// IsZero reports whether the LSN is unset, which for our purposes means the replica has not
// replayed or received any WAL at all.
func (l LSN) IsZero() bool {
	return l == 0
}

// Distance returns the number of WAL bytes separating l from other. The result is always
// non-negative; callers that need the direction should compare the LSNs first.
func (l LSN) Distance(other LSN) int64 {
	if l > other {
		return int64(l - other)
	}
	return int64(other - l)
}

// Max returns the greater of the two LSNs.
func Max(a, b LSN) LSN {
	if a > b {
		return a
	}
	return b
}
