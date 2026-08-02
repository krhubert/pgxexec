package pgxexec_test

import (
	"context"
	"fmt"
	"os"

	"github.com/krhubert/pgxexec"
	"github.com/krhubert/pgxexec/internal/sqlc/gensqlc"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TxQueries is a type alias for pgxexec.Tx with the specific sqlc.Queries type,
// use as a convenient shorthand for working with transactions in the context
// of the generated sqlc queries.
type TxQueries = pgxexec.Tx[*gensqlc.Queries]

func Example() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, "postgres://postgres:@localhost:5432/postgres")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to connect to database:", err)
		os.Exit(1)
	}

	exec := pgxexec.NewExecutor(pool, gensqlc.New(pool))
	if err := exec.Queries().UserDeleteById(ctx, 1); err != nil {
		fmt.Fprintln(os.Stderr, "query now() failed:", err)
		os.Exit(1)
	}
	if err := exec.Queries().UserDeleteById(ctx, 1); err != nil {
		fmt.Fprintln(os.Stderr, "query now() failed:", err)
		os.Exit(1)
	}

	if err := exec.ExecInTx(ctx, func(tx *gensqlc.Queries) error {
		user, err := tx.UserInsert(ctx, gensqlc.UserInsertParams{
			Email:    "testuser@localhost.dev",
			Password: []byte("password"),
		})
		if err != nil {
			return err
		}

		fmt.Println("created user with ID:", user.Id)

		if err := tx.UserDeleteById(ctx, user.Id); err != nil {
			return err
		}

		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "transaction failed:", err)
		os.Exit(1)

	}

	// Nested transaction
	if err := exec.InTx(ctx, func(tx TxQueries) error {
		user, err := tx.Queries().UserInsert(ctx, gensqlc.UserInsertParams{
			Email:    "testuser@localhost.dev",
			Password: []byte("password"),
		})
		if err != nil {
			return err
		}

		fmt.Println("created user with ID:", user.Id)

		return tx.ExecInTx(ctx, func(txq *gensqlc.Queries) error {
			return txq.UserDeleteById(ctx, user.Id)
		})
	}); err != nil {
		fmt.Fprintln(os.Stderr, "transaction failed:", err)
		os.Exit(1)

	}
}
