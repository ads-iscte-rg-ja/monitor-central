package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
)

// jobLockNamespace keeps the job locks apart from the other advisory locks
// (migrations and retention use single-key locks, a separate key space).
const jobLockNamespace = 743819115

// Elector decides which instance runs each background job (ADR-0007). It holds
// one session-level advisory lock per job it leads, on a connection reserved
// for that purpose: the locks last as long as the session, so PostgreSQL
// releases them as soon as the instance dies and another one takes over at its
// next attempt. Leadership avoids repeated work; correctness still comes from
// every job being idempotent.
type Elector struct {
	db   *sql.DB
	mu   sync.Mutex
	conn *sql.Conn
	held map[string]bool
}

func NewElector(db *DB) *Elector {
	return &Elector{db: db.DB}
}

// Lead reports whether this instance leads the job, taking the lead if no
// other instance has it. On an error the instance gives up every lead it held.
func (e *Elector) Lead(ctx context.Context, job string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.conn == nil {
		conn, err := e.db.Conn(ctx)
		if err != nil {
			return false, err
		}
		e.conn, e.held = conn, map[string]bool{}
	}
	if e.held[job] {
		// The lock is still ours while the session is: check that it is.
		var one int
		if err := e.conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
			e.release()
			return false, err
		}
		return true, nil
	}
	var acquired bool
	if err := e.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, hashtext($2))", jobLockNamespace, job).Scan(&acquired); err != nil {
		e.release()
		return false, err
	}
	if acquired {
		e.held[job] = true
	}
	return acquired, nil
}

// Close gives up every lead at once, so that another instance can take over
// at its next attempt instead of waiting for this session to time out.
func (e *Elector) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.release()
}

// release ends the session that holds the locks. The connection is discarded,
// never returned to the pool: a pooled connection still holding a job lock
// would block every other instance from leading that job.
func (e *Elector) release() {
	if e.conn == nil {
		return
	}
	_ = e.conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = e.conn.Close()
	e.conn, e.held = nil, nil
}
