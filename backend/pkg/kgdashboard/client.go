package kgdashboard

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/sirupsen/logrus"
)

// neo4jClient wraps a Neo4j driver with read-only session management and a
// lifetime health flag. It is safe for concurrent use: the underlying driver
// is a connection pool.
type neo4jClient struct {
	driver  neo4j.DriverWithContext
	db      string
	enabled atomic.Bool
}

// newNeo4jClient opens a driver and verifies connectivity. On failure it
// returns a disabled client and a nil error so the rest of PentAGI can boot;
// the dashboard subsystem then degrades to empty results. This mirrors the
// pattern used by pkg/graphiti.NewClient.
func newNeo4jClient(uri, user, password, database string, maxConns int, timeout time.Duration) (*neo4jClient, error) {
	if uri == "" {
		return &neo4jClient{db: database}, nil
	}

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""), func(c *neo4j.Config) {
		c.MaxConnectionPoolSize = maxConns
		c.SocketConnectTimeout = timeout
		c.ConnectionAcquisitionTimeout = timeout
	})
	if err != nil {
		logrus.WithError(err).WithField("neo4j_uri", uri).Warn("failed to build neo4j driver; dashboard will be disabled")
		return &neo4jClient{db: database}, nil
	}

	// Verify connectivity with a bounded timeout; a slow/unreachable Neo4j
	// must not block startup — we keep the driver object and flip the flag back
	// on later if a subsequent call succeeds (see runReadTx).
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		logrus.WithError(err).WithField("neo4j_uri", uri).Warn("neo4j connectivity check failed; dashboard will be disabled")
		// Close the driver to release any half-open resources; the service
		// stays disabled and reports empty results.
		_ = driver.Close(context.Background())
		return &neo4jClient{db: database}, nil
	}

	c := &neo4jClient{driver: driver, db: database}
	c.enabled.Store(true)
	logrus.WithField("neo4j_uri", uri).Info("neo4j dashboard client connected")
	return c, nil
}

// isEnabled reports whether the dashboard subsystem is currently usable.
func (c *neo4jClient) isEnabled() bool {
	return c != nil && c.driver != nil && c.enabled.Load()
}

// close releases the driver pool. Safe to call on a disabled client.
func (c *neo4jClient) close(ctx context.Context) error {
	if c == nil || c.driver == nil {
		return nil
	}
	return c.driver.Close(ctx)
}

// runReadTx runs cypher in a read transaction and feeds each record to the
// visitor. On a connection-level failure it flips the enabled flag off so
// subsequent dashboard calls short-circuit; on a successful call it flips it
// back on (self-heal). The visitor returns an error to abort the iteration,
// which is propagated verbatim. Any error is logged but the service callers
// receive it so they can decide to retry or degrade.
//
// We never wrap a context cancel into a "disabled" state: a canceled request
// is a client choice, not a connectivity problem.
func (c *neo4jClient) runReadTx(ctx context.Context, cypher string, params map[string]any, visit func(*neo4j.Record) error) error {
	if !c.isEnabled() {
		return errNeo4jDisabled
	}

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: c.db,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		for res.Next(ctx) {
			if err := visit(res.Record()); err != nil {
				return nil, err
			}
		}
		return nil, res.Err()
	})
	if err == nil {
		// Self-heal: a successful call means Neo4j is reachable again.
		if !c.enabled.Load() {
			c.enabled.Store(true)
		}
		return nil
	}

	// Distinguish transport-level failures from query errors. A canceled
	// context is the caller's choice and must not toggle the flag.
	if ctx.Err() != nil {
		return err
	}

	logrus.WithError(err).WithField("cypher", truncate(cypher, 160)).Warn("neo4j dashboard read failed")
	if isConnectivityError(err) {
		c.enabled.Store(false)
	}
	return err
}

// errNeo4jDisabled is returned by runReadTx when the subsystem is disabled.
var errNeo4jDisabled = fmt.Errorf("kgdashboard: neo4j is not enabled or unavailable")

// isConnectivityError reports whether err is a Neo4j driver connectivity
// class of error (driver cannot reach the server, handshake failure, ...).
// We rely on the typed error exposed by neo4j-go-driver v5 instead of
// fragile string matching.
func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	var ce *neo4j.ConnectivityError
	if errors.As(err, &ce) {
		return true
	}
	return neo4j.IsConnectivityError(err)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
