package processing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"github.com/stretchr/testify/require"
)

type stubAuth struct {
	members map[string]identity.Membership
	orgs    map[string]identity.Organization
}

func (s *stubAuth) GetMembership(ctx context.Context, orgID, userID string) (identity.Membership, bool, error) {
	_, _ = ctx, orgID
	m, ok := s.members[orgID+":"+userID]
	return m, ok, nil
}

func (s *stubAuth) GetOrganization(ctx context.Context, id string) (identity.Organization, error) {
	_ = ctx
	o, ok := s.orgs[id]
	if !ok {
		return identity.Organization{}, errors.New("not found")
	}
	return o, nil
}

type stubContent struct {
	versions map[string]VersionContent
	err      error
}

func (s *stubContent) FetchVersion(ctx context.Context, orgID, docID, versionID string) (VersionContent, error) {
	_ = ctx
	if s.err != nil {
		return VersionContent{}, s.err
	}
	vc, ok := s.versions[orgID+"/"+docID+"/"+versionID]
	if !ok {
		return VersionContent{}, &ServiceError{Code: "NOT_FOUND", Message: "version not found", Status: 404}
	}
	return vc, nil
}

type stubReporter struct {
	results []string
}

func (s *stubReporter) MarkProcessing(ctx context.Context, orgID, docID string) error { return nil }
func (s *stubReporter) ReportResult(ctx context.Context, orgID, docID, versionID string, ok bool, detail string) error {
	_ = ctx
	s.results = append(s.results, docID+"/"+versionID)
	return nil
}

type stubOutbox struct {
	pending    []OutboxEntry
	dispatched []string
	done       []string
}

func (s *stubOutbox) ListPending(ctx context.Context, limit int) ([]OutboxEntry, error) {
	_ = ctx
	if limit > 0 && len(s.pending) > limit {
		return s.pending[:limit], nil
	}
	return s.pending, nil
}

func (s *stubOutbox) MarkDispatched(ctx context.Context, id string) error {
	_, _ = ctx, id
	s.dispatched = append(s.dispatched, id)
	return nil
}

func (s *stubOutbox) MarkDone(ctx context.Context, id string, ok bool, detail string) error {
	_, _ = ctx, ok
	s.done = append(s.done, id+":"+detail)
	return nil
}

func testService(content *stubContent) (*Service, *MemoryStores, *stubReporter, *stubOutbox) {
	auth := &stubAuth{
		members: map[string]identity.Membership{
			"org-1:owner":   {OrganizationID: "org-1", UserID: "owner", Role: identity.RoleOwner},
			"org-1:viewer":  {OrganizationID: "org-1", UserID: "viewer", Role: identity.RoleViewer},
			"org-2:owner2":  {OrganizationID: "org-2", UserID: "owner2", Role: identity.RoleOwner},
		},
		orgs: map[string]identity.Organization{
			"org-1": {ID: "org-1", Status: identity.OrganizationActive},
			"org-2": {ID: "org-2", Status: identity.OrganizationActive},
		},
	}
	stores := NewMemoryStores()
	reporter := &stubReporter{}
	outbox := &stubOutbox{}
	svc := NewService(DefaultConfig(), stores, stores, &StubEmbedder{}, content, reporter, auth, outbox)
	return svc, stores, reporter, outbox
}

func submitAndProcess(t *testing.T, svc *Service, userID string, in SubmitInput) Job {
	t.Helper()
	job, err := svc.SubmitJob(context.Background(), userID, in)
	require.NoError(t, err)
	done, err := svc.Process(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, JobCompleted, done.Status)
	return done
}

func TestPipelineSuccess(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("# Guide\n\nCustomer support hours are 9-5."), ContentType: "text/markdown", IsActive: true, CurrentVersionID: "ver-1"},
	}}
	svc, stores, _, _ := testService(content)
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index"})
	active, err := stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.True(t, active[0].IsActive)
	require.NoError(t, ValidateEmbeddings([][]float32{active[0].Embedding}))
	require.Equal(t, "ver-1", active[0].VersionID)
}

func TestDuplicateProcessingRequests(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("hello"), ContentType: "text/plain", IsActive: true},
	}}
	svc, _, _, _ := testService(content)
	j1, err := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index", IdempotencyKey: "dup"})
	require.NoError(t, err)
	j2, err := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index", IdempotencyKey: "dup"})
	require.NoError(t, err)
	require.Equal(t, j1.ID, j2.ID)
}

func TestJobRetryBehavior(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{}}
	svc, _, _, _ := testService(content)
	job, err := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "missing", VersionID: "v", Operation: "index"})
	require.NoError(t, err)
	_, err = svc.Process(context.Background(), job.ID)
	require.Error(t, err)
	failed, _ := svc.jobs.GetJob(context.Background(), job.ID)
	require.Equal(t, JobFailed, failed.Status)
	require.Equal(t, ErrDocumentNotFound, *failed.ErrorCode)

	// Fix content, retry succeeds.
	content.versions["org-1/missing/v"] = VersionContent{Data: []byte("now here"), ContentType: "text/plain", IsActive: true}
	retried, err := svc.RetryJob(context.Background(), "owner", job.ID)
	require.NoError(t, err)
	require.Equal(t, JobQueued, retried.Status)
	done, err := svc.Process(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, JobCompleted, done.Status)
	require.Equal(t, 2, done.Attempts)
}

func TestCancelQueuedJob(t *testing.T) {
	content := &stubContent{}
	svc, _, _, _ := testService(content)
	job, err := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "d", VersionID: "v", Operation: "index"})
	require.NoError(t, err)
	cancelled, err := svc.CancelJob(context.Background(), "owner", job.ID)
	require.NoError(t, err)
	require.Equal(t, JobCancelled, cancelled.Status)
	_, err = svc.RetryJob(context.Background(), "owner", job.ID)
	require.Error(t, err)
}

func TestMultipleVersionsSupersede(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("version one text"), ContentType: "text/plain", IsActive: true, CurrentVersionID: "ver-1"},
		"org-1/doc-1/ver-2": {Data: []byte("version two text with more words here"), ContentType: "text/plain", IsActive: true, CurrentVersionID: "ver-2"},
	}}
	svc, stores, _, _ := testService(content)
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index"})
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-2", Operation: "reindex"})
	active, _ := stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	require.NotEmpty(t, active)
	for _, c := range active {
		require.Equal(t, "ver-2", c.VersionID, "old version must not stay exposed")
	}
	v1, _ := stores.ChunksForVersion(context.Background(), "org-1", "doc-1", "ver-1")
	for _, c := range v1 {
		require.False(t, c.IsActive)
	}
}

func TestTenantIsolation(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("org one"), ContentType: "text/plain", IsActive: true},
		"org-2/doc-1/ver-1": {Data: []byte("org two"), ContentType: "text/plain", IsActive: true},
	}}
	svc, stores, _, _ := testService(content)
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index"})
	submitAndProcess(t, svc, "owner2", SubmitInput{OrganizationID: "org-2", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index"})
	// Cross-tenant submit with a user who has no membership there is denied.
	_, err := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-2", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index", IdempotencyKey: "x"})
	require.Error(t, err)
	a1, _ := stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	require.Len(t, a1, 1)
	require.Equal(t, "org one", a1[0].Text)
	a2, _ := stores.ActiveChunks(context.Background(), "org-2", "doc-1")
	require.Len(t, a2, 1)
	require.Equal(t, "org two", a2[0].Text)
	// Viewer cannot submit.
	_, err = svc.SubmitJob(context.Background(), "viewer", SubmitInput{OrganizationID: "org-1", DocumentID: "d", VersionID: "v", Operation: "index"})
	require.Error(t, err)
}

func TestInactiveDeletedHandling(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte("live text"), ContentType: "text/plain", IsActive: true},
	}}
	svc, stores, _, _ := testService(content)
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index"})
	// Document deactivated afterwards: reindex must purge, not publish.
	content.versions["org-1/doc-1/ver-1"] = VersionContent{Data: []byte("live text"), ContentType: "text/plain", IsActive: false}
	job, _ := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "reindex", IdempotencyKey: "re-1"})
	_, err := svc.Process(context.Background(), job.ID)
	require.Error(t, err)
	active, _ := stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	require.Empty(t, active)
	// Delete op purges and completes.
	del, _ := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "delete", IdempotencyKey: "del-1"})
	done, err := svc.Process(context.Background(), del.ID)
	require.NoError(t, err)
	require.Equal(t, JobCompleted, done.Status)
}

func TestPartialWriteRecovery(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-1/ver-1": {Data: []byte(strings.Repeat("word ", 300)), ContentType: "text/plain", IsActive: true},
	}}
	svc, stores, _, _ := testService(content)
	submitAndProcess(t, svc, "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-1", Operation: "index", IdempotencyKey: "first"})
	// Inject publish failure on the replacement version.
	content.versions["org-1/doc-1/ver-2"] = VersionContent{Data: []byte(strings.Repeat("new ", 300)), ContentType: "text/plain", IsActive: true}
	stores.InjectFailure("publish", errors.New("db outage"))
	job, _ := svc.SubmitJob(context.Background(), "owner", SubmitInput{OrganizationID: "org-1", DocumentID: "doc-1", VersionID: "ver-2", Operation: "reindex", IdempotencyKey: "second"})
	_, err := svc.Process(context.Background(), job.ID)
	require.Error(t, err)
	// Old set untouched; no partial active set.
	active, _ := stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	for _, c := range active {
		require.Equal(t, "ver-1", c.VersionID)
	}
	// Retry after outage succeeds and supersedes.
	retried, err := svc.RetryJob(context.Background(), "owner", job.ID)
	require.NoError(t, err)
	require.Equal(t, JobQueued, retried.Status)
	done, err := svc.Process(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, JobCompleted, done.Status)
	active, _ = stores.ActiveChunks(context.Background(), "org-1", "doc-1")
	for _, c := range active {
		require.Equal(t, "ver-2", c.VersionID)
	}
}

func TestRelayOnce(t *testing.T) {
	content := &stubContent{versions: map[string]VersionContent{
		"org-1/doc-9/ver-1": {Data: []byte("relay me"), ContentType: "text/plain", IsActive: true},
	}}
	svc, _, _, outbox := testService(content)
	outbox.pending = []OutboxEntry{{ID: "ob-1", OrgID: "org-1", DocID: "doc-9", VersionID: "ver-1", Operation: "index", IdempotencyKey: "relay-key"}}
	n, err := svc.RelayOnce(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []string{"ob-1"}, outbox.dispatched)
	// Idempotent re-run: no duplicate job.
	n, err = svc.RelayOnce(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	jobs := 0
	for _, j := range svc.jobs.(*MemoryStores).jobs {
		_ = j
		jobs++
	}
	require.Equal(t, 1, jobs)
}
