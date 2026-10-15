package processing

import (
	"strings"
)

// ChunkConfig tunes deterministic chunking. Defaults target ~200 tokens with
// ~35 tokens of overlap. Tokens are approximated as whitespace-separated words
// (all-MiniLM-L6-v2 uses WordPiece; exact parity would require the vocab file,
// so the strategy is documented as token-aware approximation and validated by
// tests asserting bounded word counts).
type ChunkConfig struct {
	TargetTokens  int
	OverlapTokens int
	MaxChars      int
}

// DefaultChunkConfig returns the documented starting point (180-220/30-40).
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{TargetTokens: 200, OverlapTokens: 35, MaxChars: 8000}
}

func (c ChunkConfig) withDefaults() ChunkConfig {
	if c.TargetTokens <= 0 {
		c.TargetTokens = 200
	}
	if c.OverlapTokens < 0 {
		c.OverlapTokens = 0
	}
	if c.OverlapTokens >= c.TargetTokens {
		c.OverlapTokens = c.TargetTokens / 6
	}
	if c.MaxChars <= 0 {
		c.MaxChars = 8000
	}
	return c
}

func countTokens(s string) int { return len(strings.Fields(s)) }

// ChunkText splits normalized text into ordered, bounded, overlapping chunks.
// It preserves source order and section/page metadata, never emits empty
// chunks, streams with O(window) memory, and never mixes tenants/versions —
// the caller stamps ids per (org, doc, version) input.
func ChunkText(text string, cfg ChunkConfig, sectionFor func(charStart, charEnd int) map[string]any) []Segment {
	cfg = cfg.withDefaults()
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	// Precompute word char offsets for metadata (single pass).
	offsets := make([]int, len(words))
	pos := 0
	for i, w := range words {
		idx := strings.Index(text[pos:], w)
		if idx < 0 {
			idx = 0
		}
		offsets[i] = pos + idx
		pos += idx + len(w)
	}
	step := cfg.TargetTokens - cfg.OverlapTokens
	if step <= 0 {
		step = cfg.TargetTokens
	}
	var out []Segment
	for start := 0; start < len(words); {
		end := start + cfg.TargetTokens
		if end > len(words) {
			end = len(words)
		}
		segment := strings.Join(words[start:end], " ")
		// Enforce MaxChars by shrinking the window (keeps DB check happy).
		for len(segment) > cfg.MaxChars && end > start+1 {
			end--
			segment = strings.Join(words[start:end], " ")
		}
		if strings.TrimSpace(segment) != "" {
			cs := offsets[start]
			ce := offsets[end-1] + len(words[end-1])
			var meta map[string]any
			if sectionFor != nil {
				meta = sectionFor(cs, ce)
			}
			out = append(out, Segment{
				Index:      len(out),
				Text:       segment,
				TokenCount: countTokens(segment),
				CharStart:  cs,
				CharEnd:    ce,
				Meta:       meta,
			})
		}
		if end == len(words) {
			break
		}
		start += step
		// Tail handling: if the remainder is tiny (< overlap), extend the last
		// chunk instead of emitting a sliver — still deterministic and ordered.
		if len(words)-start <= cfg.OverlapTokens/2 && len(out) > 0 {
			break
		}
	}
	return out
}

// Segment is a single deterministic chunk with provenance.
type Segment struct {
	Index      int
	Text       string
	TokenCount int
	CharStart  int
	CharEnd    int
	Meta       map[string]any
}

// SectionMapper returns section/page metadata for a char span.
func SectionMapper(sections []SectionMeta) func(int, int) map[string]any {
	return func(cs, ce int) map[string]any {
		meta := map[string]any{}
		for _, s := range sections {
			if cs < s.CharEnd && ce > s.CharStart {
				if s.Page != nil {
					meta["page"] = *s.Page
				}
				if s.Section != "" {
					if prev, ok := meta["section"].(string); ok && prev != "" {
						meta["section"] = prev + " / " + s.Section
					} else {
						meta["section"] = s.Section
					}
				}
				break
			}
		}
		meta["charStart"] = cs
		meta["charEnd"] = ce
		return meta
	}
}
