package pgxexec_test

import (
	"context"
	"fmt"
	"os"

	"github.com/krhubert/pgxexec"
	"github.com/krhubert/pgxexec/internal/sqlc/gensqlc"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	if err := pgxexec.ExecuteInTx(ctx, exec, func(tx *gensqlc.Queries) error {
		user, err := tx.UserCreate(ctx, gensqlc.UserCreateParams{
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
}
