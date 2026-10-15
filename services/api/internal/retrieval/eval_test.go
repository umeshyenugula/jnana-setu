package retrieval

import (
	"context"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"github.com/stretchr/testify/require"
)

// evalFixture is the Module 4 retrieval test set: known questions, expected
// source documents, and expected evidence. It runs against the real local
// embedding path (processing.StubEmbedder, word-overlap semantics) so the
// measured metrics reflect the actual unit-norm 384-dim pipeline.
type evalCase struct {
	question   string
	expectDoc  string // "" means insufficient evidence is the correct outcome
	expectText string
}

var evalDocs = []StoredChunk{
	{Candidate: Candidate{ChunkID: "bill-refund", DocumentID: doc1, VersionID: ver1, Text: "refund period is 14 days from purchase date", TokenCount: 8}, Embedding: stubVec("refund period is 14 days from purchase date"), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
	{Candidate: Candidate{ChunkID: "bill-invoice", DocumentID: doc1, VersionID: ver1, Text: "contact billing support for invoice copies", TokenCount: 6}, Embedding: stubVec("contact billing support for invoice copies"), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
	{Candidate: Candidate{ChunkID: "ship-standard", DocumentID: doc2, VersionID: ver1, Text: "standard shipping takes five business days", TokenCount: 6}, Embedding: stubVec("standard shipping takes five business days"), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
	{Candidate: Candidate{ChunkID: "ship-express", DocumentID: doc2, VersionID: ver1, Text: "express shipping arrives in two days", TokenCount: 6}, Embedding: stubVec("express shipping arrives in two days"), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
	{Candidate: Candidate{ChunkID: "ret-policy", DocumentID: "77777777-7777-4777-8777-777777777777", VersionID: ver1, Text: "returns are accepted within 30 days with receipt", TokenCount: 8}, Embedding: stubVec("returns are accepted within 30 days with receipt"), OrgID: orgA, Active: true, DocActive: true, IsCurrent: true},
}

var evalCases = []evalCase{
	{question: "what is the refund period", expectDoc: doc1, expectText: "refund period is 14 days from purchase date"}, // direct
	{question: "how long does standard shipping take", expectDoc: doc2},                                                // direct
	{question: "tell me about the refund period length", expectDoc: doc1},                                              // paraphrase
	{question: "invoice copies contact", expectDoc: doc1},                                                              // terminology
	{question: "how fast is express shipping", expectDoc: doc2},                                                        // overlapping answers
	{question: "do you sell gift cards", expectDoc: ""},                                                                // absent -> insufficient
	{question: "what color is the sky on Mars", expectDoc: ""},                                                         // absent -> insufficient
}

func stubVec(text string) []float32 {
	vecs, err := (&processing.StubEmbedder{}).Embed(context.Background(), []string{text})
	if err != nil {
		panic(err)
	}
	return vecs[0]
}

func evalService() *Service {
	repo := NewMemoryRepository()
	repo.Seed(evalDocs)
	cfg := DefaultConfig()
	cfg.Rank.TopK = 3
	// Calibrated for the word-overlap semantics of the local test path:
	// answerable cases score >= 0.26, absent-knowledge cases <= 0.15.
	cfg.Rank.MinSimilarity = 0.2
	return NewService(cfg, repo, &processing.StubEmbedder{})
}

// TestRetrievalEvaluation measures Recall@K, Precision@K and MRR on the test
// set and asserts quality floors. Absent-knowledge cases must yield the
// explicit insufficient-evidence status.
func TestRetrievalEvaluation(t *testing.T) {
	svc := evalService()
	const k = 3
	var rrSum, recallSum, precSum float64
	answerable := 0
	for _, c := range evalCases {
		res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: c.question, TopK: k})
		require.NoError(t, err, c.question)
		if c.expectDoc == "" {
			require.Equal(t, StatusInsufficientEvidence, res.Status, c.question)
			require.Empty(t, res.Evidence, c.question)
			continue
		}
		answerable++
		require.Equal(t, StatusResultsFound, res.Status, c.question)
		firstRank := 0
		hits := 0
		for _, e := range res.Evidence {
			if e.DocumentID == c.expectDoc {
				hits++
				if firstRank == 0 {
					firstRank = e.Rank
				}
			}
		}
		require.Greater(t, hits, 0, "expected doc missing for: "+c.question)
		if c.expectText != "" {
			require.Equal(t, c.expectText, res.Evidence[0].Text, c.question)
		}
		rrSum += 1 / float64(firstRank)
		if hits > 0 {
			recallSum++
		}
		precSum += float64(hits) / float64(len(res.Evidence))
	}
	mrr := rrSum / float64(answerable)
	recall := recallSum / float64(answerable)
	precision := precSum / float64(answerable)
	t.Logf("eval: n=%d answerable=%d recall@%d=%.3f precision@%d=%.3f mrr=%.3f", len(evalCases), answerable, k, recall, k, precision, mrr)
	require.GreaterOrEqual(t, recall, 0.8)
	require.GreaterOrEqual(t, mrr, 0.7)
	require.GreaterOrEqual(t, precision, 0.3)
}

// TestEvalAmbiguousOverlap documents behavior for overlapping answers: both
// shipping chunks share vocabulary, so both may surface in stable order.
func TestEvalAmbiguousOverlap(t *testing.T) {
	svc := evalService()
	res, err := svc.Retrieve(context.Background(), Query{OrganizationID: orgA, Text: "shipping days", TopK: 2})
	require.NoError(t, err)
	require.Equal(t, StatusResultsFound, res.Status)
	require.Len(t, res.Evidence, 2)
	for _, e := range res.Evidence {
		require.Equal(t, doc2, e.DocumentID)
	}
	// Stable tie handling: identical scores resolve by chunk id deterministically.
	require.Equal(t, "ship-express", res.Evidence[0].ChunkID)
	require.Equal(t, "ship-standard", res.Evidence[1].ChunkID)
}
