package knowledge

import (
	"context"
	"errors"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	memberships map[string]identity.Membership
	orgs        map[string]identity.Organization
	membershipErr error
}

func (s *stubResolver) GetMembership(ctx context.Context, orgID, userID string) (identity.Membership, bool, error) {
	_ = ctx
	if s.membershipErr != nil {
		return identity.Membership{}, false, s.membershipErr
	}
	m, ok := s.memberships[orgID+":"+userID]
	return m, ok, nil
}

func (s *stubResolver) GetOrganization(ctx context.Context, id string) (identity.Organization, error) {
	_ = ctx
	o, ok := s.orgs[id]
	if !ok {
		return identity.Organization{}, &identity.ServiceError{Code: "NOT_FOUND", Message: "not found", Status: 404}
	}
	return o, nil
}

func testSetup() (*Service, *stubResolver, *MemoryStorage) {
	resolver := &stubResolver{
		memberships: map[string]identity.Membership{},
		orgs: map[string]identity.Organization{
			"org-1": {ID: "org-1", Status: identity.OrganizationActive},
			"org-2": {ID: "org-2", Status: identity.OrganizationActive},
		},
	}
	resolver.memberships["org-1:owner"] = identity.Membership{OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner}
	resolver.memberships["org-1:admin"] = identity.Membership{OrganizationID: "org-1", UserID: "admin", Role: identity.RoleAdmin}
	resolver.memberships["org-1:viewer"] = identity.Membership{OrganizationID: "org-1", UserID: "viewer", Role: identity.RoleViewer}
	resolver.memberships["org-2:owner2"] = identity.Membership{OrganizationID: "org-2", UserID: "owner2", Role: identity.RoleOwner}
	st := NewMemoryStore()
	storage := NewMemoryStorage()
	return NewService(st, storage, resolver), resolver, storage
}

func TestTextSubmission(t *testing.T) {
	svc, _, storage := testSetup()
	ctx := context.Background()
	doc, ver, out, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "FAQ", ContentText: "hello"})
	require.NoError(t, err)
	require.Equal(t, 1, ver.VersionNumber)
	require.Equal(t, OperationIndex, out.Operation)
	require.NotEmpty(t, out.IdempotencyKey)
	require.Equal(t, ver.ID, *doc.CurrentVersionID)
	got, err := storage.Get(ctx, ver.StoragePath)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
}

func TestFileUploadSuccess(t *testing.T) {
	svc, _, _ := testSetup()
	_, ver, _, err := svc.CreateFromFile(context.Background(), "org-1", "admin", CreateFileInput{
		Title: "Guide", Filename: "guide.pdf", DeclaredContentType: "application/pdf",
		Data: []byte("%PDF-1.4 body"),
	})
	require.NoError(t, err)
	require.Equal(t, "application/pdf", ver.ContentType)
	require.Equal(t, "guide.pdf", *ver.OriginalFilename)
}

func TestUnsupportedOversizedEmptyRejected(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	_, _, _, err := svc.CreateFromFile(ctx, "org-1", "owner", CreateFileInput{Filename: "x.exe", Data: []byte("x")})
	require.Error(t, err)
	_, _, _, err = svc.CreateFromFile(ctx, "org-1", "owner", CreateFileInput{Filename: "a.txt", DeclaredContentType: "text/plain", Data: []byte{}})
	require.Error(t, err)
	_, _, _, err = svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "t", ContentText: "   "})
	require.Error(t, err)
}

func TestDuplicateIdempotency(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	_, _, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "one", IdempotencyKey: "k-1"})
	require.NoError(t, err)
	_, _, _, err = svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "B", ContentText: "two", IdempotencyKey: "k-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "idempotency")
}

func TestReprocessRetryNoDuplicateVersion(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	doc, _, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "one"})
	require.NoError(t, err)
	out1, err := svc.RequestReprocessing(ctx, "org-1", "owner", doc.ID, "retry-key")
	require.NoError(t, err)
	out2, err := svc.RequestReprocessing(ctx, "org-1", "owner", doc.ID, "retry-key")
	require.NoError(t, err)
	require.Equal(t, out1.ID, out2.ID)
	vers, err := svc.ListVersions(ctx, "org-1", "owner", doc.ID)
	require.NoError(t, err)
	require.Len(t, vers, 1)
}

func TestVersionHistoryAndCurrentSelection(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	doc, _, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "v1"})
	require.NoError(t, err)
	_, updated, _, err := svc.CreateVersion(ctx, "org-1", "owner", doc.ID, CreateVersionInput{ContentText: "v2"})
	require.NoError(t, err)
	vers, err := svc.ListVersions(ctx, "org-1", "owner", doc.ID)
	require.NoError(t, err)
	require.Len(t, vers, 2)
	require.Equal(t, 1, vers[0].VersionNumber)
	require.Equal(t, 2, vers[1].VersionNumber)
	got, _, err := svc.Get(ctx, "org-1", "owner", doc.ID)
	require.NoError(t, err)
	require.Equal(t, updated.CurrentVersionID, got.CurrentVersionID)
}

func TestActivationDeactivation(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	doc, _, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.NoError(t, err)
	_, err = svc.SetActive(ctx, "org-1", "owner", doc.ID, false)
	require.NoError(t, err)
	docs, err := svc.List(ctx, "org-1", "owner", false, 10, 0)
	require.NoError(t, err)
	require.Empty(t, docs)
	docs, err = svc.List(ctx, "org-1", "owner", true, 10, 0)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	_, err = svc.SetActive(ctx, "org-1", "owner", doc.ID, true)
	require.NoError(t, err)
}

func TestDeleteCleanupHandoff(t *testing.T) {
	svc, _, storage := testSetup()
	ctx := context.Background()
	doc, ver, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.NoError(t, err)
	out, err := svc.Delete(ctx, "org-1", "owner", doc.ID)
	require.NoError(t, err)
	require.Equal(t, OperationDelete, out.Operation)
	require.Equal(t, doc.ID, out.DocumentID)
	_, err = storage.Get(ctx, ver.StoragePath)
	require.Error(t, err) // retained object removed
	st, err := svc.Status(ctx, "org-1", "owner", doc.ID)
	require.NoError(t, err)
	require.True(t, st.Deleted)
}

func TestRolePermissions(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	_, _, _, err := svc.CreateFromText(ctx, "org-1", "viewer", CreateTextInput{Title: "A", ContentText: "x"})
	require.Error(t, err)
	require.Equal(t, 403, err.(*ServiceError).Status)
	_, _, _, err = svc.CreateFromText(ctx, "org-1", "admin", CreateTextInput{Title: "A", ContentText: "x"})
	require.NoError(t, err)
}

func TestCrossTenantDenial(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	doc, _, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.NoError(t, err)
	_, _, err = svc.Get(ctx, "org-2", "owner2", doc.ID)
	require.Error(t, err)
	_, _, _, err = svc.CreateFromText(ctx, "org-1", "owner2", CreateTextInput{Title: "B", ContentText: "y"})
	require.Error(t, err)
}

func TestUnauthenticatedDenied(t *testing.T) {
	svc, _, _ := testSetup()
	_, _, _, err := svc.CreateFromText(context.Background(), "org-1", "", CreateTextInput{Title: "A", ContentText: "x"})
	require.Error(t, err)
}

func TestDBAndStorageFailures(t *testing.T) {
	svc, _, storage := testSetup()
	st := svc.store.(*MemoryStore)
	st.InjectFailure("create", errors.New("db down"))
	_, _, _, err := svc.CreateFromText(context.Background(), "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.Error(t, err)
	storage.InjectPutFailure(errors.New("storage down"))
	st2 := NewMemoryStore()
	svc2 := NewService(st2, storage, &stubResolver{
		memberships: map[string]identity.Membership{"org-1:owner": {OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner}},
		orgs:        map[string]identity.Organization{"org-1": {ID: "org-1", Status: identity.OrganizationActive}},
	})
	_, _, _, err = svc2.CreateFromText(context.Background(), "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.Error(t, err)
}

func TestStatusTransitions(t *testing.T) {
	svc, _, _ := testSetup()
	ctx := context.Background()
	doc, ver, _, err := svc.CreateFromText(ctx, "org-1", "owner", CreateTextInput{Title: "A", ContentText: "x"})
	require.NoError(t, err)
	updated, err := svc.ReportProcessingResult(ctx, "org-1", doc.ID, ver.ID, true, "")
	require.NoError(t, err)
	require.Equal(t, ProcessingReady, updated.ProcessingStatus)
	// Stale version result must not clobber.
	_, _, _, err = svc.CreateVersion(ctx, "org-1", "owner", doc.ID, CreateVersionInput{ContentText: "v2"})
	require.NoError(t, err)
	unchanged, err := svc.ReportProcessingResult(ctx, "org-1", doc.ID, ver.ID, false, "")
	require.NoError(t, err)
	require.Equal(t, ProcessingPending, unchanged.ProcessingStatus)
	// Invalid transition rejected at store level.
	_, err = svc.store.SetProcessingStatus(ctx, "org-1", doc.ID, []string{ProcessingReady}, ProcessingProcessing)
	require.Error(t, err)
}
