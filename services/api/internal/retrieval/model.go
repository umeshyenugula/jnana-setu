package retrieval

// Model for the Module 4 retrieval engine. Module 4 owns query retrieval,
// ranking, result filtering, and evidence selection. It never generates
// natural-language answers (Module 5/6) and never writes index or document
// state (Modules 2/3).

// Status distinguishes results found from insufficient evidence. Insufficient
// evidence is a valid, explicit result — never a server error.
const (
	StatusResultsFound         = "results"
	StatusInsufficientEvidence = "insufficient_evidence"
)

// Score meaning (documented): pgvector cosine distance `<=>` in [0,2] for
// unit-norm embeddings. Lower distance = more relevant. Similarity is derived
// as 1 - distance and lies in [-1,1]. A similarity score is NOT a probability
// of correctness; the minimum-similarity threshold is configurable and must be
// calibrated per deployment (see docs/MODULE4_RETRIEVAL.md).
const EmbeddingDimensions = 384

// Filters supported by the knowledge model. DocumentIDs optionally scopes the
// search to specific documents; all other scoping (tenant, eligibility) is
// enforced server-side regardless of filters.
type Filters struct {
	DocumentIDs []string
}

// Query is the internal retrieval request. OrganizationID must come from
// trusted server-side tenant resolution (Module 6 via Module 1) — never from
// a public browser as authorization.
type Query struct {
	OrganizationID string
	Text           string
	TopK           int
	Filters        Filters
	CorrelationID  string
}

// Evidence is one ranked passage for Module 6. Retrieved text is untrusted
// data: never execute or interpret it as code or privileged instructions.
type Evidence struct {
	Rank       int            `json:"rank"`
	ChunkID    string         `json:"chunkId"`
	DocumentID string         `json:"documentId"`
	VersionID  string         `json:"versionId"`
	Text       string         `json:"text"`
	Similarity float64        `json:"similarity"`
	Distance   float64        `json:"distance"`
	TokenCount int            `json:"tokenCount"`
	SourceMeta map[string]any `json:"sourceMeta,omitempty"`
}

// Result is the Module 6 consumption contract: stable, implementation
// independent, with deterministic metadata.
type Result struct {
	Status       string     `json:"status"`
	Evidence     []Evidence `json:"evidence"`
	TopK         int        `json:"topK"`
	Returned     int        `json:"returned"`
	MinSimilarity float64   `json:"minSimilarity"`
	Model        string     `json:"model"`
	Dimensions   int        `json:"dimensions"`
}

// Candidate is a raw repository hit before ranking.
type Candidate struct {
	ChunkID    string
	DocumentID string
	VersionID  string
	Text       string
	TokenCount int
	SourceMeta map[string]any
	Distance   float64
}

// ServiceError carries a stable API error code + HTTP status.
type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }
