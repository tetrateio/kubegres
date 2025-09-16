package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"reactive-tech.io/kubegres/internal/replicationslot"
)

var (
	ErrAlreadyExist = errors.New("slot already exist")
	ErrNotFound     = errors.New("not found")
)

type Repository interface {
	CreateSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error)
	GetSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error)
	DeleteSlot(ctx context.Context, name string) error
	ListAll(ctx context.Context) ([]replicationslot.ReplicationSlot, error)
}

func New(db Querier) Repository {
	return &repo{
		db: db,
	}
}

// Querier is an interface that abstracts sql.DB operations.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type repo struct {
	db Querier
}

func (r *repo) ListAll(ctx context.Context) ([]replicationslot.ReplicationSlot, error) {
	listAllStmt := `
		SELECT
			slot_name, plugin, slot_type, datoid, database,
			active, active_pid, xmin, catalog_xmin, restart_lsn
		FROM pg_replication_slots
		`
	rows, err := r.db.QueryContext(ctx, listAllStmt)
	if err != nil {
		return nil, fmt.Errorf("failed to list replication slots: %w", err)
	}
	defer rows.Close()
	var slots []replicationslot.ReplicationSlot
	for rows.Next() {
		var rs replicationSlotDb
		if err := rows.Scan(
			&rs.SlotName,
			&rs.Plugin,
			&rs.SlotType,
			&rs.Datoid,
			&rs.Database,
			&rs.Active,
			&rs.ActivePid,
			&rs.Xmin,
			&rs.CatalogXmin,
			&rs.RestartLSN,
		); err != nil {
			return nil, fmt.Errorf("failed to scan replication slot: %w", err)
		}
		slots = append(slots, toReplicationSlot(rs))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error occurred while iterating over replication slots: %w", err)
	}
	return slots, nil
}

type replicationSlotDb struct {
	SlotName    string
	Plugin      sql.NullString
	SlotType    string
	Datoid      sql.NullInt64
	Database    sql.NullString
	Active      bool
	ActivePid   sql.NullInt64
	Xmin        sql.NullInt64
	CatalogXmin sql.NullInt64
	RestartLSN  sql.NullString
}

func toReplicationSlot(s replicationSlotDb) replicationslot.ReplicationSlot {
	slot := replicationslot.ReplicationSlot{
		Name:   s.SlotName,
		Active: s.Active,
	}
	if s.ActivePid.Valid {
		slot.ActivePid = s.ActivePid.Int64
	}
	return slot
}

// CreateSlot creates a new physical replication slot and returns its details.
func (r *repo) CreateSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error) {
	createSlotStmt := "SELECT pg_create_physical_replication_slot($1)"
	_, err := r.db.ExecContext(ctx, createSlotStmt, name)
	if err != nil {
		// TODO(piotrkpc): handle errors better using pg error codes
		if strings.Contains(err.Error(), "already exists") {
			return replicationslot.ReplicationSlot{}, fmt.Errorf("%w: %s", ErrAlreadyExist, err.Error())
		}
		return replicationslot.ReplicationSlot{}, fmt.Errorf("failed to create replication slot: %w", err)
	}

	slot, err := r.GetSlot(ctx, name)
	if err != nil {
		return replicationslot.ReplicationSlot{}, fmt.Errorf("failed to find created replication slot: %w", err)
	}
	return slot, nil
}

func (r *repo) GetSlot(ctx context.Context, name string) (replicationslot.ReplicationSlot, error) {
	findSlotStmt := `
		SELECT
			slot_name, plugin, slot_type, datoid, database,
			active, active_pid, xmin, catalog_xmin, restart_lsn
		FROM pg_replication_slots
		WHERE slot_name = $1`

	var rs replicationSlotDb
	err := r.db.QueryRowContext(ctx, findSlotStmt, name).Scan(
		&rs.SlotName,
		&rs.Plugin,
		&rs.SlotType,
		&rs.Datoid,
		&rs.Database,
		&rs.Active,
		&rs.ActivePid,
		&rs.Xmin,
		&rs.CatalogXmin,
		&rs.RestartLSN,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return replicationslot.ReplicationSlot{}, fmt.Errorf("replication slot '%s': %w", name, ErrNotFound)
		}
		return replicationslot.ReplicationSlot{}, fmt.Errorf("failed to scan replication slot details: %w", err)
	}
	return toReplicationSlot(rs), nil
}

func (r *repo) DeleteSlot(ctx context.Context, name string) error {
	deleteSlotStmt := "SELECT pg_drop_replication_slot($1)"
	_, err := r.db.ExecContext(ctx, deleteSlotStmt, name)
	if err != nil {
		// TODO(piotrkpc): handle errors better using pg error codes
		if strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("replication slot '%s': %w", name, ErrNotFound)
		}
		return fmt.Errorf("failed to delete replication slot '%s': %w", name, err)
	}
	return nil
}
