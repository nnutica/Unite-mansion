package shared

import "context"

// TxManager runs fn inside one database transaction. Repositories called with
// the ctx passed to fn join that transaction. Nested calls join the outer one.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
