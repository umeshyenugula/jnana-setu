package knowledge

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store against Supabase PostgreSQL. All statements
// are context-aware with a 5s timeout; document+version+outbox rows are
// written in a single transaction (transactional outbox).
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}

func (s *PostgresStore) CreateDocumentWithVersion(ctx context.Context, doc Document, ver Version, outbox ProcessingRequest) (Document, Version, ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		insert into public.knowledge_documents
		(id, organization_id, creator_user_id, title, source_type, is_active, processing_status)
		values ($1,$2,$3,$4,$5,$6,$7)`,
		doc.ID, doc.OrganizationID, doc.CreatorUserID, doc.Title, doc.SourceType, doc.IsActive, doc.ProcessingStatus); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, mapPGError(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.knowledge_document_versions
		(id, document_id, version_number, content_type, original_filename, storage_path, content_size, checksum_sha256, creator_user_id)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ver.ID, ver.DocumentID, ver.VersionNumber, ver.ContentType, ver.OriginalFilename, ver.StoragePath, ver.ContentSize, ver.ChecksumSHA256, ver.CreatorUserID); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, mapPGError(err)
	}
	if _, err := tx.Exec(ctx, `
		update public.knowledge_documents set current_version_id=$2, updated_at=now() where id=$1`,
		doc.ID, ver.ID); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, mapPGError(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.knowledge_processing_outbox
		(id, organization_id, document_id, version_id, operation, idempotency_key, status)
		values ($1,$2,$3,$4,$5,$6,'pending')`,
		outbox.ID, outbox.OrganizationID, outbox.DocumentID, outbox.VersionID, outbox.Operation, outbox.IdempotencyKey); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, mapPGError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, mapPGError(err)
	}
	currentVersionID := ver.ID
	doc.CurrentVersionID = &currentVersionID
	return doc, ver, outbox, nil
}

func (s *PostgresStore) GetDocument(ctx context.Context, orgID, docID string) (Document, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var d Document
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, creator_user_id, title, source_type, current_version_id,
		       is_active, processing_status, deleted_at, created_at, updated_at
		from public.knowledge_documents where id=$1 and organization_id=$2`,
		docID, orgID).Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
		&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	return d, nil
}

func (s *PostgresStore) ListDocuments(ctx context.Context, orgID string, includeInactive bool, limit, offset int) ([]Document, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	query := `
		select id, organization_id, creator_user_id, title, source_type, current_version_id,
		       is_active, processing_status, deleted_at, created_at, updated_at
		from public.knowledge_documents
		where organization_id=$1 and deleted_at is null`
	if !includeInactive {
		query += ` and is_active = true`
	}
	query += ` order by created_at asc limit $2 offset $3`
	rows, err := s.pool.Query(ctx, query, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
			&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateDocument(ctx context.Context, orgID, docID string, title *string, isActive *bool) (Document, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var d Document
	err := s.pool.QueryRow(ctx, `
		update public.knowledge_documents
		set title = coalesce($3, title),
		    is_active = coalesce($4, is_active),
		    processing_status = case when $4 = false then 'pending' else processing_status end,
		    updated_at = now()
		where id=$1 and organization_id=$2 and deleted_at is null
		returning id, organization_id, creator_user_id, title, source_type, current_version_id,
		          is_active, processing_status, deleted_at, created_at, updated_at`,
		docID, orgID, title, isActive).Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
		&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	return d, nil
}

func (s *PostgresStore) SoftDeleteDocument(ctx context.Context, orgID, docID string) (Document, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var d Document
	err := s.pool.QueryRow(ctx, `
		update public.knowledge_documents
		set deleted_at=now(), is_active=false, updated_at=now()
		where id=$1 and organization_id=$2 and deleted_at is null
		returning id, organization_id, creator_user_id, title, source_type, current_version_id,
		          is_active, processing_status, deleted_at, created_at, updated_at`,
		docID, orgID).Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
		&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	return d, nil
}

func (s *PostgresStore) GetVersion(ctx context.Context, orgID, docID, versionID string) (Version, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var v Version
	err := s.pool.QueryRow(ctx, `
		select v.id, v.document_id, v.version_number, v.content_type, v.original_filename,
		       v.storage_path, v.content_size, v.checksum_sha256, v.creator_user_id, v.created_at
		from public.knowledge_document_versions v
		join public.knowledge_documents d on d.id = v.document_id
		where v.id=$1 and v.document_id=$2 and d.organization_id=$3`,
		versionID, docID, orgID).Scan(&v.ID, &v.DocumentID, &v.VersionNumber, &v.ContentType,
		&v.OriginalFilename, &v.StoragePath, &v.ContentSize, &v.ChecksumSHA256, &v.CreatorUserID, &v.CreatedAt)
	if err != nil {
		return Version{}, &ServiceError{Code: "NOT_FOUND", Message: "version not found", Status: 404}
	}
	return v, nil
}

func (s *PostgresStore) ListVersions(ctx context.Context, orgID, docID string) ([]Version, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from public.knowledge_documents where id=$1 and organization_id=$2)`, docID, orgID).Scan(&exists); err != nil || !exists {
		return nil, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	rows, err := s.pool.Query(ctx, `
		select id, document_id, version_number, content_type, original_filename,
		       storage_path, content_size, checksum_sha256, creator_user_id, created_at
		from public.knowledge_document_versions where document_id=$1 order by version_number asc`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.DocumentID, &v.VersionNumber, &v.ContentType,
			&v.OriginalFilename, &v.StoragePath, &v.ContentSize, &v.ChecksumSHA256, &v.CreatorUserID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) AddVersion(ctx context.Context, ver Version, outbox ProcessingRequest) (Version, Document, ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	doc, err := s.GetDocument(ctx, outbox.OrganizationID, ver.DocumentID)
	if err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	defer tx.Rollback(ctx)
	var next int
	if err := tx.QueryRow(ctx, `select coalesce(max(version_number),0)+1 from public.knowledge_document_versions where document_id=$1`, ver.DocumentID).Scan(&next); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	if ver.VersionNumber != next {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "version conflict: retry listing versions", Status: 409}
	}
	if _, err := tx.Exec(ctx, `
		insert into public.knowledge_document_versions
		(id, document_id, version_number, content_type, original_filename, storage_path, content_size, checksum_sha256, creator_user_id)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ver.ID, ver.DocumentID, ver.VersionNumber, ver.ContentType, ver.OriginalFilename, ver.StoragePath, ver.ContentSize, ver.ChecksumSHA256, ver.CreatorUserID); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, mapPGError(err)
	}
	if _, err := tx.Exec(ctx, `
		update public.knowledge_documents
		set current_version_id=$2, processing_status='pending', updated_at=now()
		where id=$1 and deleted_at is null`, ver.DocumentID, ver.ID); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, mapPGError(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.knowledge_processing_outbox
		(id, organization_id, document_id, version_id, operation, idempotency_key, status)
		values ($1,$2,$3,$4,$5,$6,'pending')`,
		outbox.ID, outbox.OrganizationID, outbox.DocumentID, outbox.VersionID, outbox.Operation, outbox.IdempotencyKey); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, mapPGError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, mapPGError(err)
	}
	currentVersionID := ver.ID
	doc.CurrentVersionID = &currentVersionID
	doc.ProcessingStatus = ProcessingPending
	doc.UpdatedAt = ver.CreatedAt
	return ver, doc, outbox, nil
}

func (s *PostgresStore) SetProcessingStatus(ctx context.Context, orgID, docID string, from []string, to string) (Document, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var d Document
	var err error
	if len(from) == 0 {
		err = s.pool.QueryRow(ctx, `
			update public.knowledge_documents set processing_status=$3, updated_at=now()
			where id=$1 and organization_id=$2
			returning id, organization_id, creator_user_id, title, source_type, current_version_id,
			          is_active, processing_status, deleted_at, created_at, updated_at`,
			docID, orgID, to).Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
			&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	} else {
		err = s.pool.QueryRow(ctx, `
			update public.knowledge_documents set processing_status=$4, updated_at=now()
			where id=$1 and organization_id=$2 and processing_status = any($3)
			returning id, organization_id, creator_user_id, title, source_type, current_version_id,
			          is_active, processing_status, deleted_at, created_at, updated_at`,
			docID, orgID, from, to).Scan(&d.ID, &d.OrganizationID, &d.CreatorUserID, &d.Title, &d.SourceType,
			&d.CurrentVersionID, &d.IsActive, &d.ProcessingStatus, &d.DeletedAt, &d.CreatedAt, &d.UpdatedAt)
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			return Document{}, &ServiceError{Code: "CONFLICT", Message: "invalid status transition", Status: 409}
		}
		return Document{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	return d, nil
}

func (s *PostgresStore) FindOutboxByIdempotencyKey(ctx context.Context, key string) (ProcessingRequest, bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var o ProcessingRequest
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, document_id, version_id, operation, idempotency_key, status, attempts, created_at, updated_at
		from public.knowledge_processing_outbox where idempotency_key=$1`, key).Scan(
		&o.ID, &o.OrganizationID, &o.DocumentID, &o.VersionID, &o.Operation, &o.IdempotencyKey, &o.Status, &o.Attempts, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return ProcessingRequest{}, false, nil
	}
	return o, true, nil
}

func (s *PostgresStore) CreateOutbox(ctx context.Context, req ProcessingRequest) (ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
		insert into public.knowledge_processing_outbox
		(id, organization_id, document_id, version_id, operation, idempotency_key, status)
		values ($1,$2,$3,$4,$5,$6,'pending')`,
		req.ID, req.OrganizationID, req.DocumentID, req.VersionID, req.Operation, req.IdempotencyKey)
	if err != nil {
		return ProcessingRequest{}, mapPGError(err)
	}
	return s.GetOutbox(ctx, req.ID)
}

func (s *PostgresStore) GetOutbox(ctx context.Context, id string) (ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var o ProcessingRequest
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, document_id, version_id, operation, idempotency_key, status, attempts, created_at, updated_at
		from public.knowledge_processing_outbox where id=$1`, id).Scan(
		&o.ID, &o.OrganizationID, &o.DocumentID, &o.VersionID, &o.Operation, &o.IdempotencyKey, &o.Status, &o.Attempts, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return ProcessingRequest{}, &ServiceError{Code: "NOT_FOUND", Message: "processing request not found", Status: 404}
	}
	return o, nil
}

func (s *PostgresStore) ListPendingOutbox(ctx context.Context, limit int) ([]ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select id, organization_id, document_id, version_id, operation, idempotency_key, status, attempts, created_at, updated_at
		from public.knowledge_processing_outbox where status='pending' order by created_at asc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProcessingRequest{}
	for rows.Next() {
		var o ProcessingRequest
		if err := rows.Scan(&o.ID, &o.OrganizationID, &o.DocumentID, &o.VersionID, &o.Operation, &o.IdempotencyKey, &o.Status, &o.Attempts, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *PostgresStore) MarkOutbox(ctx context.Context, id, status string, attempts int, lastErr *string) (ProcessingRequest, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
		update public.knowledge_processing_outbox
		set status=$2, attempts=$3, last_error=$4, updated_at=now() where id=$1`, id, status, attempts, lastErr)
	if err != nil {
		return ProcessingRequest{}, err
	}
	return s.GetOutbox(ctx, id)
}

func mapPGError(err error) error {
	msg := err.Error()
	switch {
	case contains(msg, "duplicate key"), contains(msg, "unique"):
		if contains(msg, "idempotency_key") || contains(msg, "idempotency") {
			return &ServiceError{Code: "CONFLICT", Message: "duplicate request: idempotency key already used", Status: 409}
		}
		return &ServiceError{Code: "CONFLICT", Message: "resource already exists", Status: 409}
	case contains(msg, "foreign key"), contains(msg, "violates"):
		return &ServiceError{Code: "INVALID_REQUEST", Message: "referenced resource does not exist", Status: 400}
	default:
		return err
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
