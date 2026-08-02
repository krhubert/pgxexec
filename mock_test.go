package pgxexec

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeTx implements [pgx.Tx] in memory. The embedded pgx.Tx provides
// the query methods (which panic if called - the package never issues
// queries itself); only transaction control is implemented.
type fakeTx struct {
	pgx.Tx

	beginErr    error
	commitErr   error
	rollbackErr error

	committed  bool
	rolledBack bool
	children   []*fakeTx
}

func (t *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) {
	if t.beginErr != nil {
		return nil, t.beginErr
	}
	child := &fakeTx{}
	t.children = append(t.children, child)
	return child, nil
}

func (t *fakeTx) Commit(ctx context.Context) error {
	if t.commitErr != nil {
		return t.commitErr
	}
	t.committed = true
	return nil
}

func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.rollbackErr != nil {
		return t.rollbackErr
	}
	t.rolledBack = true
	return nil
}

// fakeDB implements [PgxTxBeginner] in memory.
type fakeDB struct {
	beginErr error
	txs      []*fakeTx
}

func (db *fakeDB) newTx() (pgx.Tx, error) {
	if db.beginErr != nil {
		return nil, db.beginErr
	}
	tx := &fakeTx{}
	db.txs = append(db.txs, tx)
	return tx, nil
}

func (db *fakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.newTx()
}

func (db *fakeDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return db.newTx()
}

// queries is a minimal stand-in for a sqlc-generated Queries type.
type queries struct {
	db any // pool or tx the queries are bound to
}

func (q *queries) WithTx(tx pgx.Tx) *queries {
	return &queries{db: tx}
}

func newFakeExecutor(db *fakeDB) Executor[*queries] {
	return NewExecutor(db, &queries{db: db})
}

func TestMockExecInTxCommit(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}

	var got *queries
	err := newFakeExecutor(db).ExecInTx(ctx, func(q *queries) error {
		got = q
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	tx := db.txs[0]
	if !tx.committed || tx.rolledBack {
		t.Errorf("committed=%v rolledBack=%v, want committed only", tx.committed, tx.rolledBack)
	}
	if got.db != pgx.Tx(tx) {
		t.Errorf("queries not rebound to transaction, bound to %T", got.db)
	}
}

func TestMockExecInTxRollbackOnError(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}
	errFn := errors.New("fn failed")

	err := newFakeExecutor(db).ExecInTx(ctx, func(q *queries) error {
		return errFn
	})
	if !errors.Is(err, errFn) {
		t.Fatalf("err = %v, want %v", err, errFn)
	}

	tx := db.txs[0]
	if tx.committed || !tx.rolledBack {
		t.Errorf("committed=%v rolledBack=%v, want rolled back only", tx.committed, tx.rolledBack)
	}
}

func TestMockExecInTxRollbackErrorJoined(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}
	exec := newFakeExecutor(db)

	errFn := errors.New("fn failed")
	errRollback := errors.New("rollback failed")

	err := exec.InTx(ctx, func(tx Tx[*queries]) error {
		db.txs[0].rollbackErr = errRollback
		return errFn
	})
	if !errors.Is(err, errFn) || !errors.Is(err, errRollback) {
		t.Fatalf("err = %v, want both %v and %v", err, errFn, errRollback)
	}
}

func TestMockBeginError(t *testing.T) {
	ctx := t.Context()
	errBegin := errors.New("no connection")
	exec := newFakeExecutor(&fakeDB{beginErr: errBegin})

	for name, err := range map[string]error{
		"Begin": func() error { _, err := exec.Begin(ctx); return err }(),
		"BeginTx": func() error {
			_, err := exec.BeginTx(ctx, pgx.TxOptions{})
			return err
		}(),
		"ExecInTx": exec.ExecInTx(ctx, func(q *queries) error { return nil }),
		"InTx":     exec.InTx(ctx, func(tx Tx[*queries]) error { return nil }),
	} {
		if !errors.Is(err, errBegin) {
			t.Errorf("%s err = %v, want %v", name, err, errBegin)
		}
	}
}

func TestMockBeginTxCommit(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}

	tx, err := newFakeExecutor(db).BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if !db.txs[0].committed {
		t.Error("tx not committed")
	}
}

func TestMockNestedBeginError(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}
	errBegin := errors.New("nested begin failed")

	err := newFakeExecutor(db).InTx(ctx, func(tx Tx[*queries]) error {
		db.txs[0].beginErr = errBegin

		for name, err := range map[string]error{
			"Begin":    func() error { _, err := tx.Begin(ctx); return err }(),
			"ExecInTx": tx.ExecInTx(ctx, func(q *queries) error { return nil }),
			"InTx":     tx.InTx(ctx, func(tx Tx[*queries]) error { return nil }),
		} {
			if !errors.Is(err, errBegin) {
				t.Errorf("%s err = %v, want %v", name, err, errBegin)
			}
		}

		db.txs[0].beginErr = nil
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMockNestedTx(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}
	errInner := errors.New("inner failed")

	err := newFakeExecutor(db).InTx(ctx, func(tx Tx[*queries]) error {
		// inner transaction fails and is rolled back,
		// outer continues and commits
		if err := tx.ExecInTx(ctx, func(q *queries) error {
			return errInner
		}); !errors.Is(err, errInner) {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	outer := db.txs[0]
	if !outer.committed {
		t.Error("outer tx not committed")
	}
	inner := outer.children[0]
	if inner.committed || !inner.rolledBack {
		t.Errorf("inner committed=%v rolledBack=%v, want rolled back only",
			inner.committed, inner.rolledBack)
	}
}

func TestMockCommitError(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}
	exec := newFakeExecutor(db)
	errCommit := errors.New("commit failed")

	err := exec.InTx(ctx, func(tx Tx[*queries]) error {
		db.txs[0].commitErr = errCommit
		return nil
	})
	if !errors.Is(err, errCommit) {
		t.Fatalf("err = %v, want %v", err, errCommit)
	}
	// failed commit triggers the deferred rollback
	if !db.txs[0].rolledBack {
		t.Error("tx not rolled back after commit error")
	}
}

func TestMockNewExecutorFn(t *testing.T) {
	ctx := t.Context()
	db := &fakeDB{}

	// custom queries type without WithTx
	type custom struct{ db any }

	base := &custom{db: db}
	exec := NewExecutorFn(db, base, func(tx pgx.Tx) *custom {
		return &custom{db: tx}
	})

	if exec.Queries() != base {
		t.Error("Queries() outside tx should return the base instance")
	}

	err := exec.ExecInTx(ctx, func(q *custom) error {
		if q.db != pgx.Tx(db.txs[0]) {
			t.Errorf("queries not rebound to tx, bound to %T", q.db)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
