package marketplace

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// HasEntitlement returns true if the tenant/user has an active entitlement for the package
// (no expiry or active_until in the future). When db is nil, returns false.
func HasEntitlement(ctx context.Context, db *sql.DB, tenantID, userID, packageID string) (bool, error) {
	if db == nil {
		return false, nil
	}
	var until sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT active_until FROM marketplace_entitlements
		WHERE tenant_id = $1 AND user_id = $2 AND package_id = $3`,
		tenantID, userID, packageID,
	).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !until.Valid {
		return true, nil
	}
	return until.Time.After(time.Now().UTC()), nil
}

// GrantEntitlement upserts a row (source: manual, trial, stripe, admin).
func GrantEntitlement(ctx context.Context, db *sql.DB, tenantID, userID, packageID, source string, activeUntil *time.Time) error {
	if db == nil {
		return errors.New("nil db")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO marketplace_entitlements (tenant_id, user_id, package_id, source, active_until, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (tenant_id, user_id, package_id)
		DO UPDATE SET source = EXCLUDED.source, active_until = EXCLUDED.active_until`,
		tenantID, userID, packageID, source, activeUntil,
	)
	return err
}
