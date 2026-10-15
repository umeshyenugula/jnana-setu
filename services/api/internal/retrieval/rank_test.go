package retrieval

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func cand(id, doc, ver, text string, dist float64) Candidate {
	return Candidate{ChunkID: id, DocumentID: doc, VersionID: ver, Text: text, TokenCount: 10, Distance: dist, SourceMeta: map[string]any{"page": 1}}
}

func TestRankOrderingAndStableTies(t *testing.T) {
	in := []Candidate{
		cand("c-b", "d1", "v1", "beta text here", 0.4),
		cand("c-a", "d1", "v1", "alpha text here", 0.4),
		cand("c-c", "d1", "v1", "gamma text here", 0.1),
	}
	out := Rank(in, RankConfig{TopK: 5, MinSimilarity: -1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 3)
	require.Equal(t, "c-c", out[0].ChunkID)
	require.Equal(t, "c-a", out[1].ChunkID) // tie broken by chunk id, deterministic
	require.Equal(t, "c-b", out[2].ChunkID)
	require.Equal(t, 1, out[0].Rank)
	require.InDelta(t, 0.9, out[0].Similarity, 1e-9)
}

func TestRankThreshold(t *testing.T) {
	in := []Candidate{cand("c1", "d1", "v1", "some text", 0.85)}
	out := Rank(in, RankConfig{TopK: 5, MinSimilarity: 0.2, MaxEvidenceTokens: 4000}, 20)
	require.Empty(t, out) // similarity 0.15 < 0.2
	out = Rank(in, RankConfig{TopK: 5, MinSimilarity: 0.1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 1)
}

func TestRankDuplicatesDropped(t *testing.T) {
	in := []Candidate{
		cand("c1", "d1", "v1", "Identical passage text.", 0.1),
		cand("c2", "d2", "v2", "identical  PASSAGE text.", 0.2),
		cand("c3", "d3", "v3", "different passage here.", 0.3),
	}
	out := Rank(in, RankConfig{TopK: 5, MinSimilarity: -1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 2)
	require.Equal(t, "c1", out[0].ChunkID)
}

func TestRankBudgetAndTopK(t *testing.T) {
	var in []Candidate
	for i := 0; i < 10; i++ {
		c := cand(string(rune('a'+i))+"-id", "d", "v", "unique words number "+string(rune('a'+i)), 0.01*float64(i))
		c.TokenCount = 100
		in = append(in, c)
	}
	out := Rank(in, RankConfig{TopK: 3, MinSimilarity: -1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 3)
	out = Rank(in, RankConfig{TopK: 10, MinSimilarity: -1, MaxEvidenceTokens: 250}, 20)
	require.Len(t, out, 2) // budget protection keeps highest-ranked
}

func TestRankMalformedSkipped(t *testing.T) {
	in := []Candidate{
		{ChunkID: "bad", DocumentID: "d", VersionID: "v", Text: "x", Distance: nan()},
		cand("good", "d", "v", "valid text here", 0.2),
	}
	out := Rank(in, RankConfig{TopK: 5, MinSimilarity: -1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 1)
	require.Equal(t, "good", out[0].ChunkID)
}

func TestRankMetaValidation(t *testing.T) {
	c := cand("c1", "d1", "v1", "hello world", 0.1)
	c.SourceMeta = map[string]any{"page": 2, "": "drop", "fn": func() {}} //nolint
	out := Rank([]Candidate{c}, RankConfig{TopK: 5, MinSimilarity: -1, MaxEvidenceTokens: 4000}, 20)
	require.Len(t, out, 1)
	require.Equal(t, 2, out[0].SourceMeta["page"])
	require.NotContains(t, out[0].SourceMeta, "")
	require.NotContains(t, out[0].SourceMeta, "fn")
}

func nan() float64 {
	var z float64
	return z / z
}
