package tx

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txKey is the unexported context key for storing transactions.
type txKey struct{}

// Manager manages transactions using pgx pool.
type Manager struct {
	pool *pgxpool.Pool
}

// New creates a new transaction manager from a pool.
func New(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool}
}

// InTx executes a function inside a transaction.
// The transaction is stored in context (txKey) so that repo methods
// can access it automatically.
func (tm *Manager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	txCtx := context.WithValue(ctx, txKey{}, tx)

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
