package tx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dfryer1193/mjolnir/utils/db"
)

// fakeTx implements only the transaction control the manager uses. The
// embedded pgx.Tx keeps the rest of the interface satisfied; calling any other
// method would panic, which these tests never do.
type fakeTx struct {
	pgx.Tx

	commitCalls   int
	rollbackCalls int
	commitErr     error
	rollbackErr   error
}

func (f *fakeTx) Commit(context.Context) error {
	f.commitCalls++
	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rollbackCalls++
	return f.rollbackErr
}

type fakeBeginner struct {
	tx  pgx.Tx
	err error
}

func (f fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	return f.tx, f.err
}

func TestInTxCommitsOnSuccess(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	m := New(fakeBeginner{tx: tx})
	parent := context.Background()

	var bound pgx.Tx
	if err := m.InTx(parent, func(ctx context.Context) error {
		bound = GetTx(ctx)
		return nil
	}); err != nil {
		t.Fatalf("InTx() = %v, want nil", err)
	}

	if bound != tx {
		t.Errorf("GetTx() inside InTx = %v, want the bound transaction", bound)
	}
	if parentTx := GetTx(parent); parentTx != nil {
		t.Errorf("GetTx() on the parent context = %v, want nil", parentTx)
	}
	if tx.commitCalls != 1 {
		t.Errorf("commit calls = %d, want 1", tx.commitCalls)
	}
	if tx.rollbackCalls != 0 {
		t.Errorf("rollback calls = %d, want 0", tx.rollbackCalls)
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("handler failed")
	tx := &fakeTx{rollbackErr: errors.New("rollback failed")}
	m := New(fakeBeginner{tx: tx})

	err := m.InTx(context.Background(), func(context.Context) error {
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx() = %v, want error wrapping %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "tx failed") {
		t.Errorf("InTx() error = %q, want it to mention tx failed", err)
	}
	if tx.rollbackCalls != 1 {
		t.Errorf("rollback calls = %d, want 1", tx.rollbackCalls)
	}
	if tx.commitCalls != 0 {
		t.Errorf("commit calls = %d, want 0", tx.commitCalls)
	}
}

func TestInTxRollsBackAndRepanics(t *testing.T) {
	t.Parallel()

	const panicValue = "handler panicked"
	tx := &fakeTx{}
	m := New(fakeBeginner{tx: tx})

	func() {
		defer func() {
			if recovered := recover(); recovered != panicValue {
				t.Errorf("recovered %v, want %v", recovered, panicValue)
			}
			if tx.rollbackCalls != 1 {
				t.Errorf("rollback calls = %d, want 1", tx.rollbackCalls)
			}
			if tx.commitCalls != 0 {
				t.Errorf("commit calls = %d, want 0", tx.commitCalls)
			}
		}()

		_ = m.InTx(context.Background(), func(context.Context) error {
			panic(panicValue)
		})
		t.Error("InTx() returned normally, want panic")
	}()
}

func TestInTxReturnsBeginError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("pool exhausted")
	called := false
	m := New(fakeBeginner{err: sentinel})

	err := m.InTx(context.Background(), func(context.Context) error {
		called = true
		return nil
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx() = %v, want error wrapping %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "begin tx") {
		t.Errorf("InTx() error = %q, want it to mention begin tx", err)
	}
	if called {
		t.Error("handler was called after Begin failed")
	}
}

func TestInTxReturnsCommitError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("commit failed")
	tx := &fakeTx{commitErr: sentinel}
	m := New(fakeBeginner{tx: tx})

	err := m.InTx(context.Background(), func(context.Context) error {
		return nil
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx() = %v, want error wrapping %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "commit tx") {
		t.Errorf("InTx() error = %q, want it to mention commit tx", err)
	}
	if tx.commitCalls != 1 {
		t.Errorf("commit calls = %d, want 1", tx.commitCalls)
	}
}

func TestGetTxWithoutTransaction(t *testing.T) {
	t.Parallel()

	if got := GetTx(context.Background()); got != nil {
		t.Errorf("GetTx() = %v, want nil", got)
	}
}

func TestPoolsSatisfyBeginner(t *testing.T) {
	t.Parallel()

	var _ Beginner = (*pgxpool.Pool)(nil)
	var _ Beginner = (*db.Pool)(nil)
}
