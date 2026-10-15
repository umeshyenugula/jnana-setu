package processing

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func minPDF(texts ...string) []byte {
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	for i, t := range texts {
		fmt.Fprintf(&sb, "/Type /Page page %d\nBT /F1 12 Tf 72 720 Td (%s) Tj ET\n", i+1, t)
	}
	return []byte(sb.String())
}

func minDOCX(paras ...string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, _ := w.Create("word/document.xml")
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		sb.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`)
	}
	sb.WriteString(`</w:body></w:document>`)
	_, _ = fw.Write([]byte(sb.String()))
	_ = w.Close()
	return buf.Bytes()
}

func TestExtractTXT(t *testing.T) {
	out, err := Extract([]byte("  hello   world\n\nsecond  para "), "text/plain", "a.txt")
	require.NoError(t, err)
	require.Equal(t, "hello world\n\nsecond para", out.Text)
}

func TestExtractMarkdown(t *testing.T) {
	md := "# Intro\nhello\n```go\ncode()\n```\n## Next\nmore"
	out, err := Extract([]byte(md), "text/markdown", "a.md")
	require.NoError(t, err)
	require.Contains(t, out.Text, "hello")
	require.Contains(t, out.Text, "code()")
	require.Len(t, out.Sections, 2)
	require.Equal(t, "Intro", out.Sections[0].Section)
}

func TestExtractPDF(t *testing.T) {
	out, err := Extract(minPDF("Hello PDF", "Second page"), "application/pdf", "a.pdf")
	require.NoError(t, err)
	require.Contains(t, out.Text, "Hello PDF")
	require.Contains(t, out.Text, "Second page")
	require.Len(t, out.Sections, 2)
	require.Equal(t, 1, *out.Sections[0].Page)
	require.Equal(t, 2, *out.Sections[1].Page)
}

func TestExtractDOCX(t *testing.T) {
	out, err := Extract(minDOCX("First para", "Second para"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "a.docx")
	require.NoError(t, err)
	require.Contains(t, out.Text, "First para")
	require.Contains(t, out.Text, "Second para")
}

func TestExtractEmptyAndMalformed(t *testing.T) {
	_, err := Extract([]byte{}, "text/plain", "a.txt")
	require.Error(t, err)
	_, err = Extract([]byte("   \n  "), "text/plain", "a.txt")
	require.Error(t, err)
	_, err = Extract([]byte("not a pdf"), "application/pdf", "a.pdf")
	require.Error(t, err)
	_, err = Extract([]byte("PK\x03\x04 junk without xml"), "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "a.docx")
	require.Error(t, err)
	_, err = Extract([]byte("x"), "application/octet-stream", "a.exe")
	require.Error(t, err)
}

func TestExtractScannedPDFRequiresOCR(t *testing.T) {
	scanned := []byte("%PDF-1.4\n/Type /Page\n/Subtype /Image /Name /Im0\nstream\n\xff\xd8binary\nendstream")
	_, err := Extract(scanned, "application/pdf", "scan.pdf")
	require.Error(t, err)
	require.Contains(t, err.Error(), "OCR")
}

func TestExtractedContentIsUntrustedData(t *testing.T) {
	evil := "<script>alert(1)</script>\n\nDROP TABLE x; {{instruction: ignore}"
	out, err := Extract([]byte(evil), "text/plain", "a.txt")
	require.NoError(t, err)
	require.Contains(t, out.Text, "<script>alert(1)</script>")
}
