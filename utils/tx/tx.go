package tx

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Beginner starts a transaction. Both *pgxpool.Pool and *db.Pool satisfy it.
type Beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// transaction is the part of pgx.Tx the manager controls. Tests use it to
// exercise commit and rollback without a database.
type transaction interface {
	Commit(context.Context) error
	Rollback(context.Context) error
}

// txKey is the unexported context key for storing transactions.
type txKey struct{}

// Manager manages transactions using a Beginner.
type Manager struct {
	beginner Beginner
}

// New creates a new transaction manager from a transaction beginner.
func New(beginner Beginner) *Manager {
	return &Manager{beginner: beginner}
}

// InTx executes a function inside a transaction.
// The transaction is stored in context (txKey) so that repo methods
// can access it automatically.
func (tm *Manager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := tm.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	return runTx(ctx, tx, fn)
}

// runTx binds tx to ctx, then commits on success or rolls back on error or
// panic. It is the seam that makes transaction control testable without a
// database.
func runTx(ctx context.Context, tx transaction, fn func(ctx context.Context) error) error {
	txCtx := context.WithValue(ctx, txKey{}, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if txErr := fn(txCtx); txErr != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("tx failed: %w", txErr)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("commit tx: %w", commitErr)
	}
	return nil
}

// GetTx extracts an active transaction from context.
func GetTx(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return nil
}
