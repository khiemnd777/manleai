package training

import (
	"context"
	"database/sql"
	"errors"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListKnowledge(ctx context.Context, salonID string, ownerUserID string) ([]KnowledgeItem, error) {
	rows, err := r.db.QueryContext(ctx, knowledgeSelect()+`
		WHERE ki.salon_id = $1
		  AND s.owner_user_id = $2
		ORDER BY ki.updated_at DESC
	`, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KnowledgeItem, 0)
	for rows.Next() {
		item, err := scanKnowledgeItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveKnowledge(ctx context.Context, salonID string) ([]KnowledgeSnippet, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT title, category, body
		FROM knowledge_items
		WHERE salon_id = $1
		  AND status = 'active'
		ORDER BY updated_at DESC
		LIMIT 8
	`, salonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]KnowledgeSnippet, 0)
	for rows.Next() {
		var item KnowledgeSnippet
		if err := rows.Scan(&item.Title, &item.Category, &item.Body); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateKnowledge(ctx context.Context, salonID string, ownerUserID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	if err := r.ensureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, salonID, req.Title, req.Category, req.Body, req.Status, SourceOwner).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.getKnowledge(ctx, salonID, ownerUserID, id)
}

func (r *Repository) UpdateKnowledge(ctx context.Context, salonID string, ownerUserID string, itemID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE knowledge_items
		SET title = $1,
		    category = $2,
		    body = $3,
		    status = $4,
		    updated_at = now()
		WHERE id = $5
		  AND salon_id = $6
		  AND EXISTS (SELECT 1 FROM salons WHERE salons.id = knowledge_items.salon_id AND salons.owner_user_id = $7)
	`, req.Title, req.Category, req.Body, req.Status, itemID, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.getKnowledge(ctx, salonID, ownerUserID, itemID)
}

func (r *Repository) DeleteKnowledge(ctx context.Context, salonID string, ownerUserID string, itemID string) error {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM knowledge_items
		WHERE id = $1
		  AND salon_id = $2
		  AND EXISTS (SELECT 1 FROM salons WHERE salons.id = knowledge_items.salon_id AND salons.owner_user_id = $3)
	`, itemID, salonID, ownerUserID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListCorrections(ctx context.Context, salonID string, ownerUserID string) ([]OwnerCorrection, error) {
	rows, err := r.db.QueryContext(ctx, correctionSelect()+`
		WHERE oc.salon_id = $1
		  AND s.owner_user_id = $2
		ORDER BY oc.created_at DESC
		LIMIT 50
	`, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OwnerCorrection, 0)
	for rows.Next() {
		item, err := scanOwnerCorrection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateCorrection(ctx context.Context, salonID string, ownerUserID string, req OwnerCorrectionInput) (*OwnerCorrection, error) {
	if err := r.ensureSalonOwner(ctx, salonID, ownerUserID); err != nil {
		return nil, err
	}
	if req.CallSessionID != "" {
		if err := r.ensureSessionOwner(ctx, salonID, ownerUserID, req.CallSessionID); err != nil {
			return nil, err
		}
	}
	if req.TranscriptMessageID != "" {
		if err := r.ensureTranscriptOwner(ctx, salonID, req.CallSessionID, req.TranscriptMessageID); err != nil {
			return nil, err
		}
	}

	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO owner_corrections (salon_id, call_session_id, transcript_message_id, correction)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4)
		RETURNING id::text
	`, salonID, req.CallSessionID, req.TranscriptMessageID, req.Correction).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.getCorrection(ctx, salonID, ownerUserID, id)
}

func (r *Repository) ApplyCorrection(ctx context.Context, salonID string, ownerUserID string, correctionID string, req KnowledgeItemInput) (*KnowledgeItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var lockedID string
	err = tx.QueryRowContext(ctx, `
		SELECT oc.id::text
		FROM owner_corrections oc
		JOIN salons s ON s.id = oc.salon_id
		WHERE oc.id = $1
		  AND oc.salon_id = $2
		  AND s.owner_user_id = $3
		FOR UPDATE
	`, correctionID, salonID, ownerUserID).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var itemID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, salonID, req.Title, req.Category, req.Body, req.Status, SourceCorrection).Scan(&itemID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_corrections
		SET status = 'applied',
		    applied_knowledge_item_id = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
	`, itemID, correctionID, salonID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.getKnowledge(ctx, salonID, ownerUserID, itemID)
}

func (r *Repository) UpdateCorrectionStatus(ctx context.Context, salonID string, ownerUserID string, correctionID string, status string) (*OwnerCorrection, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE owner_corrections
		SET status = $1,
		    updated_at = now()
		WHERE id = $2
		  AND salon_id = $3
		  AND EXISTS (SELECT 1 FROM salons WHERE salons.id = owner_corrections.salon_id AND salons.owner_user_id = $4)
	`, status, correctionID, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return r.getCorrection(ctx, salonID, ownerUserID, correctionID)
}

func (r *Repository) getKnowledge(ctx context.Context, salonID string, ownerUserID string, itemID string) (*KnowledgeItem, error) {
	item, err := scanKnowledgeItem(r.db.QueryRowContext(ctx, knowledgeSelect()+`
		WHERE ki.id = $1
		  AND ki.salon_id = $2
		  AND s.owner_user_id = $3
	`, itemID, salonID, ownerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (r *Repository) getCorrection(ctx context.Context, salonID string, ownerUserID string, correctionID string) (*OwnerCorrection, error) {
	item, err := scanOwnerCorrection(r.db.QueryRowContext(ctx, correctionSelect()+`
		WHERE oc.id = $1
		  AND oc.salon_id = $2
		  AND s.owner_user_id = $3
	`, correctionID, salonID, ownerUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (r *Repository) ensureSalonOwner(ctx context.Context, salonID string, ownerUserID string) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM salons WHERE id = $1 AND owner_user_id = $2)
	`, salonID, ownerUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ensureSessionOwner(ctx context.Context, salonID string, ownerUserID string, sessionID string) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM call_sessions cs
			JOIN salons s ON s.id = cs.salon_id
			WHERE cs.id = $1
			  AND cs.salon_id = $2
			  AND s.owner_user_id = $3
		)
	`, sessionID, salonID, ownerUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ensureTranscriptOwner(ctx context.Context, salonID string, sessionID string, messageID string) error {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM call_transcript_messages
			WHERE id = $1
			  AND salon_id = $2
	`
	args := []any{messageID, salonID}
	if sessionID != "" {
		query += ` AND session_id = $3`
		args = append(args, sessionID)
	}
	query += `)`
	var exists bool
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func knowledgeSelect() string {
	return `
		SELECT ki.id::text, ki.salon_id::text, ki.title, ki.category, ki.body,
		       ki.status, ki.source, ki.created_at, ki.updated_at
		FROM knowledge_items ki
		JOIN salons s ON s.id = ki.salon_id
	`
}

func correctionSelect() string {
	return `
		SELECT oc.id::text, oc.salon_id::text, COALESCE(oc.call_session_id::text, ''),
		       COALESCE(oc.transcript_message_id::text, ''), oc.correction, oc.status,
		       COALESCE(oc.applied_knowledge_item_id::text, ''), oc.created_at, oc.updated_at
		FROM owner_corrections oc
		JOIN salons s ON s.id = oc.salon_id
	`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKnowledgeItem(row rowScanner) (*KnowledgeItem, error) {
	var item KnowledgeItem
	if err := row.Scan(&item.ID, &item.SalonID, &item.Title, &item.Category, &item.Body, &item.Status, &item.Source, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanOwnerCorrection(row rowScanner) (*OwnerCorrection, error) {
	var item OwnerCorrection
	if err := row.Scan(&item.ID, &item.SalonID, &item.CallSessionID, &item.TranscriptMessageID, &item.Correction, &item.Status, &item.AppliedKnowledgeItemID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}
