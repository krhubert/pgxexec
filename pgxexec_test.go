package pgxexec

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/krhubert/pgxexec/internal/sqlc/gensqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed internal/sqlc/schema.sql
var schema string

func TestExecutor(t *testing.T) {
	tests := []struct {
		name        string
		execFactory func(t *testing.T) (Executor[*gensqlc.Queries], error)
	}{
		{
			name: "pgxconn",
			execFactory: func(t *testing.T) (Executor[*gensqlc.Queries], error) {
				conn, err := pgx.Connect(t.Context(), "postgres://postgres:postgres@localhost:5432/postgres")
				assertNoError(t, err)
				_, err = conn.Exec(t.Context(), schema)
				_, err = conn.Exec(t.Context(), `delete from "user"`)
				assertNoError(t, err)
				return NewExecutor(conn, gensqlc.New(conn)), nil
			},
		},
		{
			name: "pgxpool",
			execFactory: func(t *testing.T) (Executor[*gensqlc.Queries], error) {
				pool, err := pgxpool.New(t.Context(), "postgres://postgres:postgres@localhost:5432/postgres")
				assertNoError(t, err)
				_, err = pool.Exec(t.Context(), schema)
				_, err = pool.Exec(t.Context(), `delete from "user"`)
				assertNoError(t, err)
				return NewExecutor(pool, gensqlc.New(pool)), nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			exec, err := tt.execFactory(t)
			assertNoError(t, err)

			userparam := gensqlc.UserInsertParams{
				Email:    "testuser0+" + tt.name + "@localhost.dev",
				Password: []byte("password"),
			}

			users := []gensqlc.UserInsertManyCopyFromParams{
				{
					Email:    "testuser1+" + tt.name + "@localhost.dev",
					Password: []byte("password1"),
				},
				{
					Email:    "testuser2+" + tt.name + "@localhost.dev",
					Password: []byte("password1"),
				},
			}

			t.Run("InTx", func(t *testing.T) {
				err = exec.InTx(ctx, func(tx Tx[*gensqlc.Queries]) error {
					user, err := tx.Queries().UserInsert(ctx, userparam)
					if err != nil {
						return err
					}
					return tx.InTx(ctx, func(tx Tx[*gensqlc.Queries]) error {
						tx.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
							_, err := tx.UserGetById(ctx, user.Id)
							return err
						})
						return nil
					})
				})
				assertNoError(t, err)
			})

			t.Run("ExecInTx", func(t *testing.T) {
				err = exec.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
					return tx.UserDeleteById(ctx, 1)
				})
			})

			t.Run("Tx Commit", func(t *testing.T) {
				tx, err := exec.Begin(ctx)
				assertNoError(t, err)

				_, err = tx.Queries().UserInsertManyCopyFrom(ctx, users)
				assertNoError(t, err)

				err = tx.Commit(ctx)
				assertNoError(t, err)
			})

			t.Run("Tx Rollback", func(t *testing.T) {
				tx, err := exec.Begin(ctx)
				assertNoError(t, err)

				users, err := tx.Queries().UserList(ctx)
				assertNoError(t, err)
				if len(users) != 3 {
					t.Fatalf("expected 3 user, got %d", len(users))
				}

				err = tx.Rollback(ctx)
				assertNoError(t, err)
			})

			t.Run("TxOptions", func(t *testing.T) {
				tx, err := exec.BeginTx(ctx, pgx.TxOptions{})
				assertNoError(t, err)

				err = tx.Queries().UserHardDeleteByEmail(ctx, userparam.Email)
				assertNoError(t, err)

				err = tx.Commit(ctx)
				assertNoError(t, err)
			})

			_, err = exec.Queries().UserHardDeleteAllCnt(ctx)
			assertNoError(t, err)
		})
	}
}

func TestExecutorErrorHandle(t *testing.T) {
	ctx := t.Context()

	db := &errDB{row: errRow{}}
	exec := NewExecutor(db, gensqlc.New(db))

	err := exec.Queries().UserDeleteById(ctx, 1)
	assertError(t, err)

	t.Run("ExecInTx with Executor", func(t *testing.T) {
		err := exec.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
			return tx.UserDeleteById(ctx, 1)
		})
		assertError(t, err)
	})

	t.Run("BeginTx TxOptions", func(t *testing.T) {
		_, err := exec.BeginTx(ctx, pgx.TxOptions{})
		assertError(t, err)
	})

	t.Run("Tx BeginTx", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			beginErr:     errors.New("begin error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assertNoError(t, err)

		_, err = tx.Begin(ctx)
		assertError(t, err)
	})

	t.Run("ExecInTx with pgx.Tx", func(t *testing.T) {
		db := &errDB{row: errRow{}, noBeginError: true}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assertNoError(t, err)

		err = tx.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
			_, err := tx.UserGetByEmail(ctx, "")
			return err
		})
		assertError(t, err)
	})

	t.Run("Tx Commit", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			commitErr:    errors.New("commit error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assertNoError(t, err)

		err = tx.Commit(ctx)
		assertError(t, err)
	})

	t.Run("Tx Rollback", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			rollbackErr:  errors.New("rollback error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))

		tx, err := exec.Begin(ctx)
		assertNoError(t, err)

		err = tx.Rollback(ctx)
		assertError(t, err)
	})

	t.Run("Query Tx Rollback Multiple Error", func(t *testing.T) {
		db := &errDB{
			row:          errRow{},
			noBeginError: true,
			rollbackErr:  errors.New("rollback error"),
		}
		exec := NewExecutor(db, gensqlc.New(db))
		errExec := errors.New("exec error")

		err := exec.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
			return errExec
		})

		if !errors.Is(err, errExec) {
			t.Fatalf("expected error to be %v, got %v", errExec, err)
		}

		if !errors.Is(err, db.rollbackErr) {
			t.Fatalf("expected error to be %v, got %v", db.rollbackErr, err)
		}
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

func (db *errDB) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return nil
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

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error")
	}
}
