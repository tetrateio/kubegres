package sql

import (
	"database/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"strings"
	"sync"
	"time"
)

type (
	// ConnectionSupplier is an interface that provides methods to access a database connection.
	ConnectionSupplier interface {
		// DB returns the underlying database connection.
		DB() *sql.DB
		// DSN returns the Data Source Name used to connect to the database.
		DSN() string
		// Close closes the database connection.
		Close() error
		// Reconnect attempts to reconnect to the database using the provided DSN.
		Reconnect(dsn string) error
	}

	// Connection is a thread-safe structure that holds a database connection and its DSN.
	Connection struct {
		mu sync.Mutex
		db *sql.DB

		dsn string
	}
)

// NewConnection creates a new Connection instance and establishes a connection to the database using the provided DSN.
func NewConnection(dsn string) (*Connection, error) {
	c := &Connection{dsn: dsn}
	if err := c.connect(dsn); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Connection) connect(dsn string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dsn = dsn
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	c.db = db
	return nil
}

func (c *Connection) DB() *sql.DB {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.db
}

func (c *Connection) DSN() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.dsn
}

func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *Connection) Reconnect(dsn string) error {
	if err := c.Close(); err != nil {
		return err
	}
	return c.connect(dsn)
}

// WaitUntilReady checks if the database connection is ready by pinging it at regular intervals
func WaitUntilReady(c ConnectionSupplier, timeout, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	var err error
	for {
		if err = c.DB().Ping(); err == nil {
			return nil // Database is ready
		}

		select {
		case <-t.C:
			continue // Retry after the interval
		case <-time.After(timeout):
			return err // Timeout reached, return error
		}
	}
}

var (
	_ ConnectionSupplier = (*DynamicDSNConnection)(nil)
	_ DSNDataSupplier    = (*DynamicDSNConnection)(nil)
)

// DynamicDSNConnection is a thread-safe connection supplier that allows dynamic updates to the DSN.
// It embeds the Connection struct and provides additional functionality to handle dynamic DSN changes.
// It is useful for scenarios where the database connection parameters may change at runtime, such as in
// a Kubernetes environment where the database host, port, or credentials may be updated dynamically.
type DynamicDSNConnection struct {
	*Connection
	mu      sync.Mutex
	DSNData *DSNData
}

// NewDynamicDSNConnection is a constructor for DynamicDSNConnection
func NewDynamicDSNConnection(data *DSNData) (*DynamicDSNConnection, error) {
	d := &DynamicDSNConnection{DSNData: data}
	conn, err := NewConnection(d.DSNData.Build())
	if err != nil {
		return nil, err
	}
	d.Connection = conn
	return d, nil
}

// DSNDataSupplier is implemented by connections whose parameters can be read back, so a
// connection to one instance can be derived from another's: same credentials, different
// endpoint.
type DSNDataSupplier interface {
	Data() *DSNData
}

// Data returns the mutable DSN parameters backing this connection.
func (d *DynamicDSNConnection) Data() *DSNData {
	return d.DSNData
}

func (d *DynamicDSNConnection) DB() *sql.DB {
	d.mu.Lock()
	defer d.mu.Unlock()

	if newDsn := d.DSNData.Build(); newDsn != d.DSN() {
		if err := d.Reconnect(newDsn); err != nil {
			return nil
		}
		// DSN is updated by Reconnect; no need to assign directly.
	}
	return d.Connection.DB()
}

// DSNData holds the data required to build a Data Source Name (DSN) for connecting to a PostgreSQL database.
//
// One DSNData is shared between the reconcilers that write it and the goroutines that read it
// to open connections, so every field access goes through mu. Use Apply to change it.
type DSNData struct {
	mu *sync.RWMutex

	HostAddr       string
	Host           string
	Port           string
	Username       string
	Password       string
	Database       string
	SSLMode        string
	RootCertPath   string
	ClientCertPath string
	ClientKeyPath  string
}

// NewDSNData is a constructor for DSNData with default values.
func NewDSNData() *DSNData {
	return &DSNData{
		mu:       &sync.RWMutex{},
		Host:     "localhost",
		Port:     "5432",
		Username: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}
}

// lock returns the guard, allowing a DSNData built as a bare struct literal. Such a value is
// not shared with any other goroutine yet, so attaching the guard lazily is safe.
func (b *DSNData) lock() *sync.RWMutex {
	if b.mu == nil {
		b.mu = &sync.RWMutex{}
	}
	return b.mu
}

// Apply changes the DSNData under its write lock.
func (b *DSNData) Apply(mutate func(*DSNData)) {
	mu := b.lock()
	mu.Lock()
	defer mu.Unlock()

	mutate(b)
}

// Snapshot returns a copy of the connection parameters, safe to change freely.
//
// It is how a replica connection is derived from the primary's: same credentials, database and
// TLS material, different endpoint.
func (b *DSNData) Snapshot() *DSNData {
	mu := b.lock()
	mu.RLock()
	defer mu.RUnlock()

	clone := *b
	clone.mu = &sync.RWMutex{}
	return &clone
}

// Build constructs the Data Source Name (DSN) string from the DSNData fields.
func (b *DSNData) Build() string {
	mu := b.lock()
	mu.RLock()
	defer mu.RUnlock()

	return b.build()
}

// build requires the caller to hold at least a read lock.
func (b *DSNData) build() string {
	var sb strings.Builder
	if b.HostAddr != "" {
		sb.WriteString("hostaddr=")
		sb.WriteString(b.HostAddr)
	}
	if b.Host != "" {
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString("host=")
		sb.WriteString(b.Host)
	}

	sb.WriteString(" port=")
	sb.WriteString(b.Port)
	sb.WriteString(" user=")
	sb.WriteString(b.Username)

	if b.Password != "" {
		sb.WriteString(" password=")
		sb.WriteString(b.Password)
	}

	sb.WriteString(" dbname=")
	sb.WriteString(b.Database)
	sb.WriteString(" sslmode=")
	sb.WriteString(b.SSLMode)

	if b.RootCertPath != "" {
		sb.WriteString(" sslrootcert=")
		sb.WriteString(b.RootCertPath)
	}
	if b.ClientCertPath != "" {
		sb.WriteString(" sslcert=")
		sb.WriteString(b.ClientCertPath)
	}
	if b.ClientKeyPath != "" {
		sb.WriteString(" sslkey=")
		sb.WriteString(b.ClientKeyPath)
	}

	return sb.String()
}

func (b *DSNData) String() string {
	masked := b.Snapshot()
	if masked.Password != "" {
		masked.Password = "******" // Mask the password for security
	}
	return masked.build()
}
