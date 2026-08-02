package pgxexec

import (
	_ "embed"
	"testing"

	"github.com/krhubert/pgxexec/internal/sqlc/gensqlc"

	"github.com/jackc/pgx/v5"
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
