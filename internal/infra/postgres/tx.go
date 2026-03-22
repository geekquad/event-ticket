package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

type contextKey struct{}

type executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type transactor struct {
	db *sql.DB
}

func NewTransactor(db *sql.DB) *transactor {
	return &transactor{db: db}
}

func (t *transactor) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, contextKey{}, tx)

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %w (original error: %v)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func txFromContext(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(contextKey{}).(*sql.Tx)
	return tx
}

func execFromContext(ctx context.Context, db *sql.DB) executor {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return db
}
