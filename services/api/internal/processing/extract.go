package processing

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExtractedText is normalized, untrusted document content. It is data only:
// never executed, never treated as privileged instructions.
type ExtractedText struct {
	Text     string
	Sections []SectionMeta
}

// SectionMeta records page/section provenance preserved during extraction.
type SectionMeta struct {
	Page      *int   `json:"page,omitempty"`
	Section   string `json:"section,omitempty"`
	CharStart int    `json:"charStart"`
	CharEnd   int    `json:"charEnd"`
}

type extractError struct {
	code    string
	message string
}

func (e *extractError) Error() string { return e.message }

// MaxExtractBytes bounds extraction memory (12 MiB input cap).
const MaxExtractBytes = 12 << 20

// Extract selects a local extractor by content type and normalizes the result.
func Extract(data []byte, contentType, filename string) (ExtractedText, error) {
	if len(data) == 0 {
		return ExtractedText{}, &extractError{code: ErrEmptyContent, message: "document content is empty"}
	}
	if len(data) > MaxExtractBytes {
		return ExtractedText{}, &extractError{code: ErrMalformedContent, message: "document exceeds maximum processable size"}
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	var raw string
	var sections []SectionMeta
	var err error
	switch ct {
	case "text/plain":
		raw, err = extractText(data)
	case "text/markdown":
		raw, sections, err = extractMarkdown(data)
	case "application/pdf":
		raw, sections, err = extractPDF(data)
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		raw, sections, err = extractDOCX(data)
	default:
		// Fall back to filename extension for robustness.
		lower := strings.ToLower(filename)
		switch {
		case strings.HasSuffix(lower, ".txt"):
			raw, err = extractText(data)
		case strings.HasSuffix(lower, ".md"):
			raw, sections, err = extractMarkdown(data)
		case strings.HasSuffix(lower, ".pdf"):
			raw, sections, err = extractPDF(data)
		case strings.HasSuffix(lower, ".docx"):
			raw, sections, err = extractDOCX(data)
		default:
			return ExtractedText{}, &extractError{code: ErrUnsupportedContent, message: "unsupported content type: " + contentType}
		}
	}
	if err != nil {
		return ExtractedText{}, err
	}
	normalized := normalizeWhitespace(raw)
	if strings.TrimSpace(normalized) == "" {
		return ExtractedText{}, &extractError{code: ErrEmptyContent, message: "document contains no extractable text"}
	}
	return ExtractedText{Text: normalized, Sections: sections}, nil
}

func extractText(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", &extractError{code: ErrMalformedContent, message: "text content is not valid UTF-8"}
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", &extractError{code: ErrMalformedContent, message: "text content contains NUL bytes"}
	}
	return string(data), nil
}

func extractMarkdown(data []byte) (string, []SectionMeta, error) {
	raw, err := extractText(data)
	if err != nil {
		return "", nil, err
	}
	// Preserve structure: keep headings as section markers, drop code-fence
	// markers but keep the code text (it may be meaningful knowledge).
	var out []string
	var sections []SectionMeta
	offset := 0
	inFence := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(trimmed, "#") {
			title := strings.Trim(strings.TrimLeft(trimmed, "#"), " ")
			if title != "" {
				sections = append(sections, SectionMeta{Section: title, CharStart: offset})
			}
		}
		out = append(out, line)
		offset += len(line) + 1
	}
	for i := range sections {
		if i+1 < len(sections) {
			sections[i].CharEnd = sections[i+1].CharStart
		} else {
			sections[i].CharEnd = offset
		}
	}
	return strings.Join(out, "\n"), sections, nil
}

// extractPDF handles text-based PDFs by decoding Tj/TJ text-showing operators
// and capturing page breaks. Scanned (image-only) PDFs yield OCR_REQUIRED.
func extractPDF(data []byte) (string, []SectionMeta, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("%PDF-")) {
		return "", nil, &extractError{code: ErrMalformedContent, message: "malformed PDF: missing %PDF- signature"}
	}
	pages := splitPDFPages(data)
	var parts []string
	var sections []SectionMeta
	offset := 0
	for i, page := range pages {
		text := decodePDFPageText(page)
		if strings.TrimSpace(text) != "" {
			n := i + 1
			sections = append(sections, SectionMeta{Page: &n, CharStart: offset})
			parts = append(parts, text)
			offset += len(text) + 2
		}
	}
	if len(parts) == 0 {
		if looksScanned(data) {
			return "", nil, &extractError{code: ErrOCRRequired, message: "PDF appears to be scanned images: OCR is required and not supported"}
		}
		return "", nil, &extractError{code: ErrEmptyContent, message: "PDF contains no extractable text"}
	}
	for i := range sections {
		end := offset
		if i+1 < len(sections) {
			end = sections[i+1].CharStart
		}
		sections[i].CharEnd = end
	}
	return strings.Join(parts, "\n\n"), sections, nil
}

func decodePDFPageText(page []byte) string {
	parts := []string{decodePDFTextOps(page)}
	for searchFrom := 0; searchFrom < len(page); {
		filter := bytes.Index(page[searchFrom:], []byte("/FlateDecode"))
		if filter < 0 {
			break
		}
		filter += searchFrom
		streamStart := bytes.Index(page[filter:], []byte("stream"))
		if streamStart < 0 {
			break
		}
		streamStart += filter + len("stream")
		if streamStart < len(page) && page[streamStart] == '\r' {
			streamStart++
		}
		if streamStart < len(page) && page[streamStart] == '\n' {
			streamStart++
		}
		streamEnd := bytes.Index(page[streamStart:], []byte("endstream"))
		if streamEnd < 0 {
			break
		}
		streamEnd += streamStart
		reader, err := zlib.NewReader(bytes.NewReader(page[streamStart:streamEnd]))
		if err == nil {
			decoded, readErr := io.ReadAll(io.LimitReader(reader, MaxExtractBytes))
			_ = reader.Close()
			if readErr == nil {
				parts = append(parts, decodePDFTextOps(decoded))
			}
		}
		searchFrom = streamEnd + len("endstream")
	}
	return strings.Join(parts, " ")
}

func splitPDFPages(data []byte) [][]byte {
	// Split on "/Type /Page" (not "/Type /Pages") page-object markers.
	marker := []byte("/Type /Page")
	var pages [][]byte
	start := 0
	for i := 0; i+len(marker) <= len(data); {
		idx := bytes.Index(data[i:], marker)
		if idx < 0 {
			break
		}
		pos := i + idx
		after := data[pos+len(marker):]
		i = pos + len(marker)
		if len(after) > 0 && (after[0] == 's' || after[0] == 'S') {
			continue // "/Pages" tree node, not a page
		}
		pages = append(pages, data[start:pos])
		start = pos
	}
	pages = append(pages, data[start:])
	if len(pages) <= 2 {
		// No real page markers (header + body only): treat as one page.
		return [][]byte{data}
	}
	// Drop the file header preceding the first page marker so page
	// numbering starts at 1.
	return pages[1:]
}

// decodePDFTextOps extracts literal "(...)" and hex "<...>" strings shown by
// Tj/TJ operators, handling the standard PDF escape sequences.
func decodePDFTextOps(page []byte) string {
	var sb strings.Builder
	flush := func(s string) {
		if strings.TrimSpace(s) != "" {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(s)
		}
	}
	i := 0
	for i < len(page) {
		switch page[i] {
		case '(':
			depth := 1
			var lit []byte
			i++
			for i < len(page) && depth > 0 {
				c := page[i]
				if c == '\\' && i+1 < len(page) {
					i++
					esc := page[i]
					switch esc {
					case 'n':
						lit = append(lit, '\n')
					case 'r':
						lit = append(lit, '\r')
					case 't':
						lit = append(lit, '\t')
					case '(', ')', '\\':
						lit = append(lit, esc)
					default:
						lit = append(lit, esc)
					}
				} else if c == '(' {
					depth++
					lit = append(lit, c)
				} else if c == ')' {
					depth--
					if depth > 0 {
						lit = append(lit, c)
					}
				} else {
					lit = append(lit, c)
				}
				i++
			}
			// Only keep strings followed by a text-showing operator.
			rest := page[i:]
			if hasTextOpPrefix(rest) {
				flush(string(lit))
			}
		case '<':
			if i+1 < len(page) && page[i+1] == '<' {
				i += 2
				continue
			}
			j := i + 1
			for j < len(page) && page[j] != '>' {
				j++
			}
			if j < len(page) {
				if hex := decodeHexString(string(page[i+1 : j])); strings.TrimSpace(hex) != "" {
					rest := page[j+1:]
					if hasTextOpPrefix(rest) {
						flush(hex)
					}
				}
				i = j + 1
				continue
			}
			i++
		default:
			i++
		}
	}
	return sb.String()
}

func hasTextOpPrefix(rest []byte) bool {
	rest = bytes.TrimLeft(rest, " \t\r\n")
	for _, op := range []string{"Tj", "TJ", "'", "\""} {
		if bytes.HasPrefix(rest, []byte(op)) {
			return true
		}
	}
	// Real PDFs commonly place strings in a TJ array with numeric spacing
	// values between them, so the operator is not adjacent to each string.
	if len(rest) > 512 {
		rest = rest[:512]
	}
	return bytes.Contains(rest, []byte("TJ"))
}

func decodeHexString(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return r
		}
		return -1
	}, s)
	if len(s)%2 == 1 {
		s += "0"
	}
	var b []byte
	for i := 0; i+1 < len(s); i += 2 {
		var v byte
		_, _ = fmt.Sscanf(s[i:i+2], "%02x", &v)
		b = append(b, v)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	// Best-effort UTF-16BE (common in PDFs with hex strings).
	var r []rune
	for i := 0; i+1 < len(b); i += 2 {
		r = append(r, rune(int(b[i])<<8|int(b[i+1])))
	}
	s2 := strings.Map(func(ch rune) rune {
		if ch == 0xFEFF || ch == 0 {
			return -1
		}
		if unicode.IsPrint(ch) || unicode.IsSpace(ch) {
			return ch
		}
		return -1
	}, string(r))
	return s2
}

func looksScanned(data []byte) bool {
	// Heuristic: image XObjects present but virtually no text operators.
	images := bytes.Count(data, []byte("/Subtype /Image")) + bytes.Count(data, []byte("/Subtype/Image"))
	hasText := bytes.Contains(data, []byte("Tj")) || bytes.Contains(data, []byte("TJ"))
	return images > 0 && !hasText
}

// extractDOCX unzips word/document.xml and concatenates w:t runs, preserving
// paragraph breaks. Malformed zips are rejected with clear errors.
func extractDOCX(data []byte) (string, []SectionMeta, error) {
	if len(data) < 4 || data[0] != 'P' || data[1] != 'K' {
		return "", nil, &extractError{code: ErrMalformedContent, message: "malformed DOCX: missing ZIP signature"}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, &extractError{code: ErrMalformedContent, message: "malformed DOCX: unreadable container"}
	}
	var docXML []byte
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", nil, &extractError{code: ErrMalformedContent, message: "malformed DOCX: cannot read document body"}
			}
			buf := make([]byte, 0, 1<<20)
			tmp := make([]byte, 32<<10)
			for {
				n, rerr := rc.Read(tmp)
				if n > 0 {
					buf = append(buf, tmp[:n]...)
					if len(buf) > MaxExtractBytes {
						rc.Close()
						return "", nil, &extractError{code: ErrMalformedContent, message: "DOCX body exceeds maximum size"}
					}
				}
				if rerr != nil {
					break
				}
			}
			rc.Close()
			docXML = buf
			break
		}
	}
	if docXML == nil {
		return "", nil, &extractError{code: ErrMalformedContent, message: "malformed DOCX: missing word/document.xml"}
	}
	text := docxText(docXML)
	if strings.TrimSpace(text) == "" {
		return "", nil, &extractError{code: ErrEmptyContent, message: "DOCX contains no extractable text"}
	}
	return text, nil, nil
}

// docxText is a small purpose-built parser: it walks the XML, emits paragraph
// breaks at </w:p>, and collects <w:t> character data. Contents are data.
func docxText(xml []byte) string {
	var sb strings.Builder
	i := 0
	for i < len(xml) {
		if xml[i] != '<' {
			// Character data outside tags is ignored; only w:t content counts.
			i++
			continue
		}
		j := bytes.IndexByte(xml[i:], '>')
		if j < 0 {
			break
		}
		tag := string(xml[i+1 : i+j])
		i = i + j + 1
		name := strings.Fields(tag)[0]
		name = strings.TrimPrefix(name, "/")
		switch {
		case name == "w:p" || name == "w:pPr":
			// paragraph open: nothing
		case name == "/w:p":
			sb.WriteString("\n\n")
		case name == "/w:t" || strings.HasPrefix(name, "w:t"):
			// handled below
		}
		if strings.HasPrefix(name, "w:t") && !strings.HasPrefix(tag, "/") {
			k := bytes.Index(xml[i:], []byte("</w:t>"))
			if k < 0 {
				break
			}
			sb.WriteString(unescapeXML(string(xml[i : i+k])))
			i += k + len("</w:t>")
		}
	}
	return sb.String()
}

func unescapeXML(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&apos;", "'")
	return s
}

// normalizeWhitespace collapses runs of spaces/tabs, normalizes line endings,
// keeps paragraph breaks, and trims. Structure (paragraphs) is preserved.
func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	paras := strings.Split(s, "\n")
	var out []string
	blank := false
	for _, p := range paras {
		fields := strings.Fields(p)
		if len(fields) == 0 {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, strings.Join(fields, " "))
	}
	joined := strings.Join(out, "\n")
	// Collapse 3+ newlines to a paragraph break.
	for strings.Contains(joined, "\n\n\n") {
		joined = strings.ReplaceAll(joined, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(joined)
}
