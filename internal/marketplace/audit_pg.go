package marketplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// InsertAuditEvent records a moderation or lifecycle event when DB is configured.
func InsertAuditEvent(ctx context.Context, db *sql.DB, packageID, tenantID, actorUserID, action, oldState, newState, note string, detail map[string]any) error {
	if db == nil {
		return errors.New("nil db")
	}
	var detailArg any
	if len(detail) > 0 {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		detailArg = b
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO marketplace_audit_events (package_id, tenant_id, actor_user_id, action, old_state, new_state, note, detail, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW())`,
		packageID, tenantID, actorUserID, action, nullableText(oldState), nullableText(newState), nullableText(note), detailArg,
	)
	return err
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
