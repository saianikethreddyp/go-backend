// Package audit records sensitive admin actions, such as viewing a customer's
// contacts, so access can be reviewed later.
package audit

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Logger writes audit entries.
type Logger struct{ pool *pgxpool.Pool }

func NewLogger(pool *pgxpool.Pool) *Logger { return &Logger{pool: pool} }

// Record stores that adminID performed action on the target.
func (l *Logger) Record(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID) error {
	_, err := l.pool.Exec(ctx,
		`INSERT INTO audit_log (admin_id, action, target_type, target_id) VALUES ($1, $2, $3, $4)`,
		adminID, action, targetType, targetID)
	return err
}
