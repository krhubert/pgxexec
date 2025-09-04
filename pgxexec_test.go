package pgxexec

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/krhubert/assert"
	"github.com/krhubert/pgxexec/internal/sqlc/gensqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExecutor(t *testing.T) {
	tests := []struct {
		name        string
		execFactory func(ctx context.Context) (Executor[gensqlc.Queries, *gensqlc.Queries], error)
		skip        string
	}{
		{
			name: "mock",
			execFactory: func(ctx context.Context) (Executor[gensqlc.Queries, *gensqlc.Queries], error) {
				db := &noopDB{row: timeRow{}}
				return NewExecutor(db, gensqlc.New(db)), nil
			},
		},
		{
			name: "pgx.Conn",
			execFactory: func(ctx context.Context) (Executor[gensqlc.Queries, *gensqlc.Queries], error) {
				conn, err := pgx.Connect(ctx, "postgres://aidouser:pass@localhost:5432/aido")
				if err != nil {
					return nil, err
				}

				return NewExecutor(conn, gensqlc.New(conn)), nil
			},
			skip: "requires a running PostgreSQL instance",
		},
		{
			name: "pgxpool.Pool",
			execFactory: func(ctx context.Context) (Executor[gensqlc.Queries, *gensqlc.Queries], error) {
				conn, err := pgxpool.New(ctx, "postgres://aidouser:pass@localhost:5432/aido")
				if err != nil {
					return nil, err
				}

				return NewExecutor(conn, gensqlc.New(conn)), nil
			},
			skip: "requires a running PostgreSQL instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != "" {
				t.Skip(tt.skip)
			}

			ctx := t.Context()

			exec, err := tt.execFactory(ctx)
			assert.NoError(t, err)

			err = exec.Queries().UserDeleteById(ctx, 1)
			assert.NoError(t, err)

			t.Run("ExecuteInTx with Executor", func(t *testing.T) {
				err = ExecuteInTx(ctx, exec, func(tx *gensqlc.Queries) error {
					return tx.UserDeleteById(ctx, 1)
				})
			})

			t.Run("ExecuteInTx with Tx", func(t *testing.T) {
				tx, err := exec.Begin(ctx)
				assert.NoError(t, err)

				err = ExecuteInTx(ctx, tx, func(tx *gensqlc.Queries) error {
					return tx.UserDeleteById(ctx, 1)
				})
				assert.NoError(t, err)

				err = tx.Commit(ctx)
				assert.NoError(t, err)
			})

			t.Run("Tx Commit", func(t *testing.T) {
				tx, err := exec.Begin(ctx)
				assert.NoError(t, err)

				err = tx.Queries().UserDeleteById(ctx, 1)
				assert.NoError(t, err)

				err = tx.Commit(ctx)
				assert.NoError(t, err)
			})

			t.Run("Tx Rollback", func(t *testing.T) {
				tx, err := exec.Begin(ctx)
				assert.NoError(t, err)

				err = tx.Queries().UserDeleteById(ctx, 1)
				assert.NoError(t, err)

				err = tx.Rollback(ctx)
				assert.NoError(t, err)
			})

			t.Run("TxOptions", func(t *testing.T) {
				tx, err := exec.BeginTx(ctx, pgx.TxOptions{})
				assert.NoError(t, err)

				err = tx.Queries().UserDeleteById(ctx, 1)
				assert.NoError(t, err)

				err = tx.Commit(ctx)
				assert.NoError(t, err)
			})
		})
	}
}

func TestExecutorErrorHandle(t *testing.T) {
	ctx := t.Context()

	db := &errDB{row: errRow{}}
	exec := NewExecutor(db, gensqlc.New(db))

	err := exec.Queries().UserDeleteById(ctx, 1)
	assert.Error(t, err)

	t.Run("ExecuteInTx with Executor", func(t *testing.T) {
		err := ExecuteInTx(ctx, exec, func(tx *gensqlc.Queries) error {
			return tx.UserDeleteById(ctx, 1)
		})
		assert.Error(t, err)
	})

	t.Run("BeginTx TxOptions", func(t *testing.T) {
		_, err := exec.BeginTx(ctx, pgx.TxOptions{})
		assert.Error(t, err)
	})

	t.Run("Tx BeginTx", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			beginErr:     errors.New("begin error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assert.NoError(t, err)

		_, err = tx.Begin(ctx)
		assert.Error(t, err)
	})

	t.Run("ExecuteInTx with pgx.Tx", func(t *testing.T) {
		db := &errDB{row: errRow{}, noBeginError: true}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assert.NoError(t, err)

		err = ExecuteInTx(ctx, tx, func(tx *gensqlc.Queries) error {
			_, err := tx.UserGetByEmail(ctx, "")
			return err
		})
		assert.Error(t, err)
	})

	t.Run("Tx Commit", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			commitErr:    errors.New("commit error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assert.NoError(t, err)

		err = tx.Commit(ctx)
		assert.Error(t, err)
	})

	t.Run("Tx Rollback", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			rollbackErr:  errors.New("rollback error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assert.NoError(t, err)

		err = tx.Rollback(ctx)
		assert.Error(t, err)
	})

	t.Run("Query Tx Rollback Multiple Error", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			rollbackErr:  errors.New("rollback error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		err := ExecuteInTx(ctx, exec, func(tx *gensqlc.Queries) error {
			return errors.New("exec error")
		})
		assert.Error(t, err)
		errs := assert.TypeAssert[interface{ Unwrap() []error }](t, err).Unwrap()
		assert.Len(t, errs, 2)
		assert.ErrorContains(t, errs[0], "exec error")
		assert.ErrorContains(t, errs[1], "rollback error")
	})
}

type timeRow struct{}

func (timeRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("expected one destination, got %d", len(dest))
	}
	*dest[0].(*time.Time) = time.Now()
	return nil
}

type errRow struct{}

func (errRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("expected one destination, got %d", len(dest))
	}
	return errors.New("row scan error")
}

type noopDB struct {
	row pgx.Row
}

func (db *noopDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (db *noopDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (db *noopDB) QueryRow(context.Context, string, ...any) pgx.Row        { return db.row }
func (db *noopDB) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (db *noopDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return &noopTx{row: db.row}, nil
}

func (db *noopDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	return &noopTx{row: db.row}, nil
}

type noopTx struct {
	row         pgx.Row
	rollbackErr error
	commitErr   error
	beginErr    error
}

func (tx *noopTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return &noopTx{row: tx.row}, tx.beginErr
}
func (tx *noopTx) Commit(ctx context.Context) error   { return tx.commitErr }
func (tx *noopTx) Rollback(ctx context.Context) error { return tx.rollbackErr }
func (tx *noopTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *noopTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (tx *noopTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (tx *noopTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *noopTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (tx *noopTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (tx *noopTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return tx.row }
func (tx *noopTx) Conn() *pgx.Conn                                               { return nil }

type errDB struct {
	row          pgx.Row
	noBeginError bool
	rollbackErr  error
	commitErr    error
	beginErr     error
}

func (db *errDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("db exec error")
}

func (db *errDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("db query error")
}

func (db *errDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return db.row
}

func (db *errDB) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("db copy from error")
}

func (db *errDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if db.noBeginError {
		return &noopTx{
			row:         db.row,
			rollbackErr: db.rollbackErr,
			commitErr:   db.commitErr,
			beginErr:    db.beginErr,
		}, nil
	}
	return nil, errors.New("db begin error")
}

func (db *errDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if db.noBeginError {
		return &noopTx{
			row:         db.row,
			rollbackErr: db.rollbackErr,
			commitErr:   db.commitErr,
		}, nil
	}
	return nil, errors.New("db begin tx error")
}
