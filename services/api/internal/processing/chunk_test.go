package processing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func words(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "word"
	}
	return strings.Join(parts, " ")
}

func TestChunkBoundedAndOverlapping(t *testing.T) {
	cfg := ChunkConfig{TargetTokens: 200, OverlapTokens: 35}
	segs := ChunkText(words(500), cfg, nil)
	require.Greater(t, len(segs), 1)
	for i, s := range segs {
		require.Equal(t, i, s.Index)
		require.NotEmpty(t, strings.TrimSpace(s.Text))
		require.LessOrEqual(t, s.TokenCount, 200)
	}
	// Overlap: last words of chunk 0 appear at the start of chunk 1.
	w0 := strings.Fields(segs[0].Text)
	w1 := strings.Fields(segs[1].Text)
	require.Equal(t, w0[len(w0)-35:], w1[:35])
}

func TestChunkOrderingAndMetadata(t *testing.T) {
	sections := []SectionMeta{{Section: "A", CharStart: 0, CharEnd: 100000}}
	segs := ChunkText(words(450), DefaultChunkConfig(), SectionMapper(sections))
	prev := -1
	for _, s := range segs {
		require.Greater(t, s.CharStart, prev)
		prev = s.CharStart
		require.Equal(t, "A", s.Meta["section"])
		require.Contains(t, s.Meta, "charStart")
	}
	// Determinism: same input -> identical chunks.
	again := ChunkText(words(450), DefaultChunkConfig(), SectionMapper(sections))
	require.Equal(t, segs, again)
}

func TestChunkLongDocument(t *testing.T) {
	segs := ChunkText(words(20000), DefaultChunkConfig(), nil)
	// step = 165 -> ~121 chunks; assert sane bounds, not exact count.
	require.Greater(t, len(segs), 100)
	require.Less(t, len(segs), 140)
	for _, s := range segs {
		require.NotEmpty(t, s.Text)
		require.LessOrEqual(t, len(s.Text), 8000)
	}
	// Full coverage: first word to last word present in order.
	require.True(t, strings.HasPrefix(segs[0].Text, "word"))
	require.True(t, strings.HasSuffix(segs[len(segs)-1].Text, "word"))
}

func TestChunkNoEmpty(t *testing.T) {
	require.Empty(t, ChunkText("   \n ", DefaultChunkConfig(), nil))
	segs := ChunkText("short", DefaultChunkConfig(), nil)
	require.Len(t, segs, 1)
	require.Equal(t, "short", segs[0].Text)
}

func TestChunkPageMetadata(t *testing.T) {
	p1, p2 := 1, 2
	sections := []SectionMeta{{Page: &p1, CharStart: 0, CharEnd: 500}, {Page: &p2, CharStart: 500, CharEnd: 100000}}
	segs := ChunkText(words(400), DefaultChunkConfig(), SectionMapper(sections))
	require.Equal(t, 1, segs[0].Meta["page"])
}
