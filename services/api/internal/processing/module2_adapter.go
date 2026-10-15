package processing

import (
	"context"

	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
)

// Module2Adapter implements ContentSource, StatusReporter and OutboxRelay
// against Module 2's stores. It is the only Module 3 code that touches
// Module 2 — read-only content access plus the two documented status
// transitions. It never writes document/version tables directly and never
// touches Module 2 business logic.
type Module2Adapter struct {
	store   knowledge.Store
	storage knowledge.Storage
	service *knowledge.Service
}

func NewModule2Adapter(store knowledge.Store, storage knowledge.Storage, service *knowledge.Service) *Module2Adapter {
	return &Module2Adapter{store: store, storage: storage, service: service}
}

func (a *Module2Adapter) FetchVersion(ctx context.Context, orgID, docID, versionID string) (VersionContent, error) {
	doc, err := a.store.GetDocument(ctx, orgID, docID)
	if err != nil {
		return VersionContent{}, err
	}
	ver, err := a.store.GetVersion(ctx, orgID, docID, versionID)
	if err != nil {
		return VersionContent{}, err
	}
	data, err := a.storage.Get(ctx, ver.StoragePath)
	if err != nil {
		return VersionContent{}, &knowledge.ServiceError{Code: "STORAGE_ERROR", Message: "could not fetch version content", Status: 500}
	}
	filename := ""
	if ver.OriginalFilename != nil {
		filename = *ver.OriginalFilename
	}
	current := ""
	if doc.CurrentVersionID != nil {
		current = *doc.CurrentVersionID
	}
	return VersionContent{Data: data, ContentType: ver.ContentType, Filename: filename,
		IsActive: doc.IsActive, Deleted: doc.DeletedAt != nil, CurrentVersionID: current}, nil
}

func (a *Module2Adapter) MarkProcessing(ctx context.Context, orgID, docID string) error {
	_, err := a.store.SetProcessingStatus(ctx, orgID, docID,
		[]string{knowledge.ProcessingPending, knowledge.ProcessingReady, knowledge.ProcessingFailed},
		knowledge.ProcessingProcessing)
	return err
}

func (a *Module2Adapter) ReportResult(ctx context.Context, orgID, docID, versionID string, ok bool, detail string) error {
	_, err := a.service.ReportProcessingResult(ctx, orgID, docID, versionID, ok, detail)
	return err
}

func (a *Module2Adapter) ListPending(ctx context.Context, limit int) ([]OutboxEntry, error) {
	rows, err := a.store.ListPendingOutbox(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]OutboxEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, OutboxEntry{ID: r.ID, OrgID: r.OrganizationID, DocID: r.DocumentID,
			VersionID: r.VersionID, Operation: r.Operation, IdempotencyKey: r.IdempotencyKey})
	}
	return out, nil
}

func (a *Module2Adapter) MarkDispatched(ctx context.Context, id string) error {
	_, err := a.store.MarkOutbox(ctx, id, "dispatched", 0, nil)
	return err
}

func (a *Module2Adapter) MarkDone(ctx context.Context, keyOrID string, ok bool, detail string) error {
	row, found, err := a.store.FindOutboxByIdempotencyKey(ctx, keyOrID)
	if err != nil || !found {
		r, gerr := a.store.GetOutbox(ctx, keyOrID)
		if gerr != nil {
			return nil // best effort: never fail the pipeline on outbox bookkeeping
		}
		row = r
	}
	status := "done"
	if !ok {
		status = "failed"
	}
	_, _ = a.store.MarkOutbox(ctx, row.ID, status, row.Attempts+1, &detail)
	return nil
}
