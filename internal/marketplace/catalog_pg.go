package marketplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// LoadCatalogFromDB replaces c with packages and reviews loaded from PostgreSQL.
func LoadCatalogFromDB(ctx context.Context, db *sql.DB, c *Catalog) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	rows, err := db.QueryContext(ctx, `SELECT id, tenant_id, payload FROM marketplace_packages`)
	if err != nil {
		return err
	}
	defer rows.Close()

	snap := CatalogSnapshot{}
	for rows.Next() {
		var id, tenant string
		var raw []byte
		if err := rows.Scan(&id, &tenant, &raw); err != nil {
			return err
		}
		var pkg Package
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return err
		}
		if tenant != "" && pkg.TenantID == "" {
			pkg.TenantID = tenant
		}
		snap.Packages = append(snap.Packages, pkg)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	rrows, err := db.QueryContext(ctx, `SELECT package_id, payload FROM marketplace_reviews`)
	if err != nil {
		return err
	}
	defer rrows.Close()
	for rrows.Next() {
		var pkgID string
		var raw []byte
		if err := rrows.Scan(&pkgID, &raw); err != nil {
			return err
		}
		var rev Review
		if err := json.Unmarshal(raw, &rev); err != nil {
			return err
		}
		if rev.PackageID == "" {
			rev.PackageID = pkgID
		}
		snap.Reviews = append(snap.Reviews, rev)
	}
	if err := rrows.Err(); err != nil {
		return err
	}

	c.Restore(snap)
	return nil
}

// SaveCatalogSnapshotToDB replaces marketplace_* rows with the current catalog snapshot.
func SaveCatalogSnapshotToDB(ctx context.Context, db *sql.DB, c *Catalog) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	snap := c.Snapshot()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM marketplace_reviews`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marketplace_packages`); err != nil {
		return err
	}

	for _, pkg := range snap.Packages {
		raw, err := json.Marshal(pkg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO marketplace_packages (id, tenant_id, payload, updated_at) VALUES ($1, $2, $3::jsonb, NOW())`,
			pkg.ID, pkg.TenantID, string(raw)); err != nil {
			return err
		}
	}
	for _, rev := range snap.Reviews {
		r := rev
		if r.CreatedAt.IsZero() {
			r.CreatedAt = time.Now().UTC()
		}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO marketplace_reviews (id, package_id, payload, created_at) VALUES ($1, $2, $3::jsonb, $4)`,
			r.ID, r.PackageID, string(raw), r.CreatedAt); err != nil {
			return err
		}
	}

	return tx.Commit()
}
