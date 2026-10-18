package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
)

// MembershipResolver is the Module 1 context Module 2 integrates with.
// identity.Store satisfies it; tests may substitute a stub. Module 2 never
// reads Module 1's tables directly — only this interface.
type MembershipResolver interface {
	GetMembership(ctx context.Context, organizationID, userID string) (identity.Membership, bool, error)
	GetOrganization(ctx context.Context, id string) (identity.Organization, error)
}

type Service struct {
	store   Store
	storage Storage
	members MembershipResolver
}

func NewService(store Store, storage Storage, members MembershipResolver) *Service {
	return &Service{store: store, storage: storage, members: members}
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- authorization (Scope D) ----

func (s *Service) requireMember(ctx context.Context, orgID, userID string) (identity.Membership, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(userID) == "" {
		return identity.Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "organization and user are required", Status: 400}
	}
	m, ok, err := s.members.GetMembership(ctx, orgID, userID)
	if err != nil {
		return identity.Membership{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify membership", Status: 500}
	}
	if !ok {
		// Do not leak existence: arbitrary org IDs without membership => 404.
		return identity.Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	org, err := s.members.GetOrganization(ctx, orgID)
	if err != nil {
		return identity.Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	if org.Status != identity.OrganizationActive {
		return identity.Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "organization is not active", Status: 403}
	}
	return m, nil
}

// requireWriter allows owner/admin only. member/viewer are read-only.
func (s *Service) requireWriter(ctx context.Context, orgID, userID string) (identity.Membership, error) {
	m, err := s.requireMember(ctx, orgID, userID)
	if err != nil {
		return identity.Membership{}, err
	}
	if m.Role != identity.RoleOwner && m.Role != identity.RoleAdmin {
		return identity.Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	return m, nil
}

// ---- helpers ----

func storagePath(orgID, docID string, versionNumber int, ext string) string {
	return fmt.Sprintf("org/%s/doc/%s/v%d/%s%s", orgID, docID, versionNumber, newHex(8), ext)
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---- create from text ----

type CreateTextInput struct {
	Title          string
	ContentText    string
	IdempotencyKey string
}

func (s *Service) CreateFromText(ctx context.Context, orgID, userID string, in CreateTextInput) (Document, Version, ProcessingRequest, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	if err := ValidateTitle(in.Title); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	if err := ValidateTextContent(in.ContentText); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	if strings.TrimSpace(in.IdempotencyKey) != "" {
		if existing, ok, _ := s.store.FindOutboxByIdempotencyKey(ctx, in.IdempotencyKey); ok {
			return Document{}, Version{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: fmt.Sprintf("duplicate request: idempotency key already used for document %s", existing.DocumentID), Status: 409}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	docID := newUUID()
	verID := newUUID()
	data := []byte(in.ContentText)
	sum := checksum(data)
	sp := storagePath(orgID, docID, 1, ".txt")
	if err := s.storage.Put(ctx, sp, "text/plain", data); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not store document content", Status: 500}
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		key = "txt-" + newHex(16)
	}
	now := time.Now().UTC()
	doc := Document{ID: docID, OrganizationID: orgID, CreatorUserID: userID, Title: strings.TrimSpace(in.Title), SourceType: SourceText, IsActive: true, ProcessingStatus: ProcessingPending, CreatedAt: now, UpdatedAt: now}
	ver := Version{ID: verID, DocumentID: docID, VersionNumber: 1, ContentType: "text/plain", StoragePath: sp, ContentSize: int64(len(data)), ChecksumSHA256: &sum, CreatorUserID: userID, CreatedAt: now}
	out := ProcessingRequest{ID: newUUID(), OrganizationID: orgID, DocumentID: docID, VersionID: verID, Operation: OperationIndex, IdempotencyKey: key, Status: "pending", CreatedAt: now, UpdatedAt: now}
	created, createdVer, createdOut, err := s.store.CreateDocumentWithVersion(ctx, doc, ver, out)
	if err != nil {
		_ = s.storage.Delete(ctx, sp)
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	return created, createdVer, createdOut, nil
}

// ---- create from file ----

type CreateFileInput struct {
	Title              string
	Filename           string
	DeclaredContentType string
	Data               []byte
	IdempotencyKey     string
}

func (s *Service) CreateFromFile(ctx context.Context, orgID, userID string, in CreateFileInput) (Document, Version, ProcessingRequest, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSpace(in.Filename)
	}
	if err := ValidateTitle(title); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	vf, verr := ValidateUpload(in.Filename, in.DeclaredContentType, in.Data)
	if verr != nil {
		return Document{}, Version{}, ProcessingRequest{}, verr
	}
	if strings.TrimSpace(in.IdempotencyKey) != "" {
		if existing, ok, _ := s.store.FindOutboxByIdempotencyKey(ctx, in.IdempotencyKey); ok {
			return Document{}, Version{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: fmt.Sprintf("duplicate request: idempotency key already used for document %s", existing.DocumentID), Status: 409}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	docID := newUUID()
	verID := newUUID()
	sp := storagePath(orgID, docID, 1, vf.Extension)
	if err := s.storage.Put(ctx, sp, vf.ContentType, in.Data); err != nil {
		return Document{}, Version{}, ProcessingRequest{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not store document content", Status: 500}
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		key = "file-" + newHex(16)
	}
	now := time.Now().UTC()
	fname := strings.TrimSpace(in.Filename)
	doc := Document{ID: docID, OrganizationID: orgID, CreatorUserID: userID, Title: title, SourceType: SourceFile, IsActive: true, ProcessingStatus: ProcessingPending, CreatedAt: now, UpdatedAt: now}
	ver := Version{ID: verID, DocumentID: docID, VersionNumber: 1, ContentType: vf.ContentType, OriginalFilename: &fname, StoragePath: sp, ContentSize: vf.Size, ChecksumSHA256: &vf.Checksum, CreatorUserID: userID, CreatedAt: now}
	out := ProcessingRequest{ID: newUUID(), OrganizationID: orgID, DocumentID: docID, VersionID: verID, Operation: OperationIndex, IdempotencyKey: key, Status: "pending", CreatedAt: now, UpdatedAt: now}
	created, createdVer, createdOut, err := s.store.CreateDocumentWithVersion(ctx, doc, ver, out)
	if err != nil {
		_ = s.storage.Delete(ctx, sp)
		return Document{}, Version{}, ProcessingRequest{}, err
	}
	return created, createdVer, createdOut, nil
}

// ---- reads ----

func (s *Service) List(ctx context.Context, orgID, userID string, includeInactive bool, limit, offset int) ([]Document, error) {
	if _, err := s.requireMember(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListDocuments(ctx, orgID, includeInactive, limit, offset)
}

func (s *Service) Get(ctx context.Context, orgID, userID, docID string) (Document, Version, error) {
	if _, err := s.requireMember(ctx, orgID, userID); err != nil {
		return Document{}, Version{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return Document{}, Version{}, err
	}
	if doc.DeletedAt != nil {
		return Document{}, Version{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	var current Version
	if doc.CurrentVersionID != nil {
		current, err = s.store.GetVersion(ctx, orgID, docID, *doc.CurrentVersionID)
		if err != nil {
			return Document{}, Version{}, err
		}
	}
	return doc, current, nil
}

func (s *Service) ListVersions(ctx context.Context, orgID, userID, docID string) ([]Version, error) {
	if _, err := s.requireMember(ctx, orgID, userID); err != nil {
		return nil, err
	}
	return s.store.ListVersions(ctx, orgID, docID)
}

// ---- new version ----

type CreateVersionInput struct {
	ContentText         string
	Filename            string
	DeclaredContentType string
	Data                []byte
	IsFile              bool
	IdempotencyKey      string
}

func (s *Service) CreateVersion(ctx context.Context, orgID, userID, docID string, in CreateVersionInput) (Version, Document, ProcessingRequest, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	if doc.DeletedAt != nil {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "document is deleted", Status: 409}
	}
	if strings.TrimSpace(in.IdempotencyKey) != "" {
		if _, ok, _ := s.store.FindOutboxByIdempotencyKey(ctx, in.IdempotencyKey); ok {
			return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "duplicate request: idempotency key already used", Status: 409}
		}
	}
	existing, err := s.store.ListVersions(ctx, orgID, docID)
	if err != nil {
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	next := len(existing) + 1

	var contentType string
	var payload []byte
	var fname *string
	var vf ValidatedFile // set for file uploads, reused for storage-path extension
	if in.IsFile {
		var verr error
		vf, verr = ValidateUpload(in.Filename, in.DeclaredContentType, in.Data)
		if verr != nil {
			return Version{}, Document{}, ProcessingRequest{}, verr
		}
		contentType = vf.ContentType
		payload = in.Data
		fn := strings.TrimSpace(in.Filename)
		fname = &fn
	} else {
		if err := ValidateTextContent(in.ContentText); err != nil {
			return Version{}, Document{}, ProcessingRequest{}, err
		}
		contentType = "text/plain"
		payload = []byte(in.ContentText)
	}
	sum := checksum(payload)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Use the validated file's extension for uploads; fall back to content-type
	// mapping for text submissions.
	ext := SafeObjectExtension(contentType)
	if in.IsFile {
		ext = vf.Extension
	}
	sp := storagePath(orgID, docID, next, ext)
	if err := s.storage.Put(ctx, sp, contentType, payload); err != nil {
		return Version{}, Document{}, ProcessingRequest{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not store document content", Status: 500}
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		key = "ver-" + newHex(16)
	}
	now := time.Now().UTC()
	ver := Version{ID: newUUID(), DocumentID: docID, VersionNumber: next, ContentType: contentType, OriginalFilename: fname, StoragePath: sp, ContentSize: int64(len(payload)), ChecksumSHA256: &sum, CreatorUserID: userID, CreatedAt: now}
	out := ProcessingRequest{ID: newUUID(), OrganizationID: orgID, DocumentID: docID, VersionID: ver.ID, Operation: OperationReindex, IdempotencyKey: key, Status: "pending", CreatedAt: now, UpdatedAt: now}
	createdVer, updatedDoc, createdOut, err := s.store.AddVersion(ctx, ver, out)
	if err != nil {
		_ = s.storage.Delete(ctx, sp)
		return Version{}, Document{}, ProcessingRequest{}, err
	}
	return createdVer, updatedDoc, createdOut, nil
}

// ---- activation ----

func (s *Service) SetActive(ctx context.Context, orgID, userID, docID string, active bool) (Document, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return Document{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return Document{}, err
	}
	if doc.DeletedAt != nil {
		return Document{}, &ServiceError{Code: "CONFLICT", Message: "document is deleted", Status: 409}
	}
	isActive := active
	return s.store.UpdateDocument(ctx, orgID, docID, nil, &isActive)
}

func (s *Service) Rename(ctx context.Context, orgID, userID, docID, title string) (Document, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return Document{}, err
	}
	if err := ValidateTitle(title); err != nil {
		return Document{}, err
	}
	t := strings.TrimSpace(title)
	return s.store.UpdateDocument(ctx, orgID, docID, &t, nil)
}

// ---- reprocessing (no duplicate version) ----

func (s *Service) RequestReprocessing(ctx context.Context, orgID, userID, docID, idempotencyKey string) (ProcessingRequest, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return ProcessingRequest{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return ProcessingRequest{}, err
	}
	if doc.DeletedAt != nil {
		return ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "document is deleted", Status: 409}
	}
	if doc.CurrentVersionID == nil {
		return ProcessingRequest{}, &ServiceError{Code: "CONFLICT", Message: "document has no version to process", Status: 409}
	}
	key := strings.TrimSpace(idempotencyKey)
	if key != "" {
		if existing, ok, _ := s.store.FindOutboxByIdempotencyKey(ctx, key); ok {
			return existing, nil // retry-safe: return existing, no duplicate
		}
	} else {
		key = "reproc-" + newHex(16)
	}
	now := time.Now().UTC()
	out := ProcessingRequest{ID: newUUID(), OrganizationID: orgID, DocumentID: docID, VersionID: *doc.CurrentVersionID, Operation: OperationReindex, IdempotencyKey: key, Status: "pending", CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateOutbox(ctx, out)
	if err != nil {
		return ProcessingRequest{}, err
	}
	// Best-effort status projection; ignore transition conflicts.
	_, _ = s.store.SetProcessingStatus(ctx, orgID, docID, []string{ProcessingPending, ProcessingReady, ProcessingFailed}, ProcessingPending)
	return created, nil
}

// ---- deletion ----

// Delete removes the document row (soft) and enqueues index cleanup for
// Module 3. It also removes retained objects from private storage. It never
// touches Module 3's chunk/embedding tables directly.
func (s *Service) Delete(ctx context.Context, orgID, userID, docID string) (ProcessingRequest, error) {
	if _, err := s.requireWriter(ctx, orgID, userID); err != nil {
		return ProcessingRequest{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return ProcessingRequest{}, err
	}
	if doc.DeletedAt != nil {
		return ProcessingRequest{}, &ServiceError{Code: "NOT_FOUND", Message: "document not found", Status: 404}
	}
	versions, _ := s.store.ListVersions(ctx, orgID, docID)
	deleted, err := s.store.SoftDeleteDocument(ctx, orgID, docID)
	if err != nil {
		return ProcessingRequest{}, err
	}
	_ = deleted
	versionID := ""
	if doc.CurrentVersionID != nil {
		versionID = *doc.CurrentVersionID
	} else if len(versions) > 0 {
		versionID = versions[len(versions)-1].ID
	}
	now := time.Now().UTC()
	out := ProcessingRequest{ID: newUUID(), OrganizationID: orgID, DocumentID: docID, VersionID: versionID, Operation: OperationDelete, IdempotencyKey: "del-" + docID + "-" + newHex(8), Status: "pending", CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateOutbox(ctx, out)
	if err != nil {
		return ProcessingRequest{}, err
	}
	// Remove retained originals (best effort; outbox delete op remains the
	// source of truth for Module 3 index cleanup even if this fails).
	_ = s.storage.DeletePrefix(ctx, fmt.Sprintf("org/%s/doc/%s/", orgID, docID))
	return created, nil
}

// ---- status ----

type StatusView struct {
	DocumentID       string  `json:"documentId"`
	IsActive         bool    `json:"isActive"`
	ProcessingStatus string  `json:"processingStatus"`
	CurrentVersionID *string `json:"currentVersionId"`
	Deleted          bool    `json:"deleted"`
}

func (s *Service) Status(ctx context.Context, orgID, userID, docID string) (StatusView, error) {
	if _, err := s.requireMember(ctx, orgID, userID); err != nil {
		return StatusView{}, err
	}
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return StatusView{}, err
	}
	return StatusView{DocumentID: doc.ID, IsActive: doc.IsActive, ProcessingStatus: doc.ProcessingStatus, CurrentVersionID: doc.CurrentVersionID, Deleted: doc.DeletedAt != nil}, nil
}

// ReportProcessingResult is the Module 3 callback contract: Module 3 calls
// this (via a future authenticated callback endpoint or direct service call)
// with the immutable version it processed. Only pending/processing -> terminal
// transitions owned here.
func (s *Service) ReportProcessingResult(ctx context.Context, orgID, docID, versionID string, ok bool, detail string) (Document, error) {
	doc, err := s.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return Document{}, err
	}
	if doc.CurrentVersionID == nil || *doc.CurrentVersionID != versionID {
		// Stale result for a superseded version: acknowledge without mutating
		// current status to avoid clobbering a newer pending version.
		return doc, nil
	}
	_ = detail
	if ok {
		return s.store.SetProcessingStatus(ctx, orgID, docID, []string{ProcessingPending, ProcessingProcessing}, ProcessingReady)
	}
	return s.store.SetProcessingStatus(ctx, orgID, docID, []string{ProcessingPending, ProcessingProcessing}, ProcessingFailed)
}
