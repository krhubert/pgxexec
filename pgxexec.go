// Package pgxexec provides a unified interface for executing SQL queries
// and transactions using pgx. It is designed to work seamlessly
// with sqlc-generated code, allowing for easy integration.
package pgxexec

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PgxTxBeginner represents a common database connection [pgx.Conn]/[pgxpool.Pool]
// interface that is capable of starting transactions.
type PgxTxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Queryable is the interface satisfied by sqlc.Queries pointer types
// (e.g. *gensqlc.Queries): a type that can rebind itself to a transaction.
type Queryable[Q any] interface {
	WithTx(tx pgx.Tx) Q
}

// Executor executes sqlc queries and manages transactions on a database.
//
// To avoid repeating the type parameter, define an alias for
// your sqlc.Queries type:
//
//	type Executor = pgxexec.Executor[*gensqlc.Queries]
type Executor[Q Queryable[Q]] struct {
	q    Q
	dbtx PgxTxBeginner
}

// NewExecutor creates a new [Executor] instance that can manage transactions and
// execute sqlc queries.
func NewExecutor[Q Queryable[Q]](dbtx PgxTxBeginner, queries Q) Executor[Q] {
	return Executor[Q]{
		q:    queries,
		dbtx: dbtx,
	}
}

// Queries returns the sqlc.Queries instance associated with the executor.
func (e Executor[Q]) Queries() Q {
	return e.q
}

// Begin starts a new transaction using the underlying [PgxTxBeginner] interface.
// see [pgx.Conn.Begin] or [pgxpool.Pool.Begin] for more details.
func (e Executor[Q]) Begin(ctx context.Context) (Tx[Q], error) {
	tx, err := e.dbtx.Begin(ctx)
	if err != nil {
		return Tx[Q]{}, fmt.Errorf("pgxexec begin: %w", err)
	}

	return newTx(tx, e.q), nil
}

// BeginTx starts a new transaction with the specified options using
// the underlying [PgxTxBeginner] interface.
//
// see [pgx.TxOptions] for more details on transaction options.
func (e Executor[Q]) BeginTx(ctx context.Context, opts pgx.TxOptions) (Tx[Q], error) {
	tx, err := e.dbtx.BeginTx(ctx, opts)
	if err != nil {
		return Tx[Q]{}, fmt.Errorf("pgxexec begin tx: %w", err)
	}

	return newTx(tx, e.q), nil
}

// ExecInTx runs fn inside a new transaction started via the underlying
// [PgxTxBeginner] interface, committing on success and rolling back on error.
func (e Executor[Q]) ExecInTx(ctx context.Context, fn func(tx Q) error) error {
	return queryInTx(e.Begin, ctx, fn)
}

// InTx runs fn inside a new transaction started via the underlying
// [PgxTxBeginner] interface, committing on success and rolling back on error.
func (e Executor[Q]) InTx(ctx context.Context, fn func(tx Tx[Q]) error) error {
	return execInTx(e.Begin, ctx, fn)
}

// Tx executes sqlc queries within a transaction.
//
// To avoid repeating the type parameter, define an alias for
// your sqlc.Queries type:
//
//	type TxQueries = pgxexec.Tx[*gensqlc.Queries]
type Tx[Q Queryable[Q]] struct {
	q  Q
	tx pgx.Tx
}

// newTx creates a new [Tx] instance with the provided transaction and queries.
func newTx[Q Queryable[Q]](tx pgx.Tx, queries Q) Tx[Q] {
	return Tx[Q]{
		q:  queries.WithTx(tx),
		tx: tx,
	}
}

// Queries returns the sqlc.Queries instance associated
// with the transaction.
func (e Tx[Q]) Queries() Q {
	return e.q
}

// Begin starts a pseudo nested transaction.
// see [pgx.Tx.Begin] for more details.
func (e Tx[Q]) Begin(ctx context.Context) (Tx[Q], error) {
	tx, err := e.tx.Begin(ctx)
	if err != nil {
		return Tx[Q]{}, fmt.Errorf("pgxexec tx begin: %w", err)
	}

	return newTx(tx, e.q), nil
}

// Commit the current transaction.
// see [pgx.Tx.Commit] for more details.
func (e Tx[Q]) Commit(ctx context.Context) error {
	if err := e.tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgxexec tx commit: %w", err)
	}
	return nil
}

// Rollback the current transaction.
// see [pgx.Tx.Rollback] for more details.
func (e Tx[Q]) Rollback(ctx context.Context) error {
	if err := e.tx.Rollback(ctx); err != nil {
		return fmt.Errorf("pgxexec tx rollback: %w", err)
	}
	return nil
}

// ExecInTx runs fn inside a nested transaction started via the underlying
// [pgx.Tx] interface, committing on success and rolling back on error.
//
// see [pgx.Tx.Begin] for more details.
func (e Tx[Q]) ExecInTx(ctx context.Context, fn func(tx Q) error) error {
	return queryInTx(e.Begin, ctx, fn)
}

// InTx runs fn inside a nested transaction started via the underlying
// [pgx.Tx] interface, committing on success and rolling back on error.
//
// see [pgx.Tx.Begin] for more details.
func (e Tx[Q]) InTx(ctx context.Context, fn func(tx Tx[Q]) error) error {
	return execInTx(e.Begin, ctx, fn)
}

func queryInTx[Q Queryable[Q]](
	begin func(ctx context.Context) (Tx[Q], error),
	ctx context.Context,
	fn func(tx Q) error,
) error {
	tx, err := begin(ctx)
	if err != nil {
		return err
	}
	return runInTx(ctx, tx, func() error { return fn(tx.Queries()) })
}

func execInTx[Q Queryable[Q]](
	begin func(ctx context.Context) (Tx[Q], error),
	ctx context.Context,
	fn func(tx Tx[Q]) error,
) error {
	tx, err := begin(ctx)
	if err != nil {
		return err
	}
	return runInTx(ctx, tx, func() error { return fn(tx) })
}

// runInTx calls fn, committing tx on success and rolling it back on error.
// If the rollback itself fails, its error is joined with fn's error.
func runInTx[Q Queryable[Q]](ctx context.Context, tx Tx[Q], fn func() error) (err error) {
	defer func() {
		if err != nil {
			if err1 := tx.Rollback(ctx); err1 != nil {
				err = errors.Join(err, err1)
			}
		}
	}()

	if err := fn(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
