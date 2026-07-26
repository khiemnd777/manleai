package public_catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetBySlug(ctx context.Context, slug string) (*Catalog, error) {
	return r.readCatalog(ctx, `SELECT public.read_public_catalog($1)::text`, slug)
}

func (r *Repository) GetFirstPublished(ctx context.Context) (*Catalog, error) {
	return r.readCatalog(ctx, `SELECT public.read_first_public_catalog()::text`)
}

// readCatalog deliberately consumes only the database-owned safe projection.
// Public requests have no direct RLS visibility on tenant base tables because
// row policies cannot hide sensitive columns from an otherwise visible row.
func (r *Repository) readCatalog(ctx context.Context, query string, args ...any) (*Catalog, error) {
	var raw sql.NullString
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !raw.Valid || raw.String == "" || raw.String == "null" {
		return nil, ErrNotFound
	}

	var catalog Catalog
	if err := json.Unmarshal([]byte(raw.String), &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}
