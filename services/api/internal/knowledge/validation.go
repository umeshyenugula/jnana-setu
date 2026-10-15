package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
)

// Limits (also documented in README + docs/MODULE2_KNOWLEDGE.md).
const (
	MaxUploadBytes = 10 << 20 // 10 MiB per file / text submission
	MaxTextChars   = 200_000  // ~200k chars per text submission/version
)

var allowedExtensions = map[string]string{
	".txt":  "text/plain",
	".md":   "text/markdown",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// ValidateTitle enforces 1..200 chars.
func ValidateTitle(title string) *ServiceError {
	n := len([]rune(strings.TrimSpace(title)))
	if n < 1 || n > 200 {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "title must be 1-200 characters", Status: 400}
	}
	return nil
}

// ValidateTextContent rejects empty / oversized / binary-looking text.
func ValidateTextContent(text string) *ServiceError {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "text content must not be empty", Status: 400}
	}
	if len([]rune(text)) > MaxTextChars {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "text content exceeds maximum length", Status: 400}
	}
	if strings.IndexByte(text, 0) >= 0 {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "text content must not contain NUL bytes", Status: 400}
	}
	return nil
}

type ValidatedFile struct {
	ContentType string
	Extension   string
	Size        int64
	Checksum    string
}

// ValidateUpload checks extension, size, emptiness, declared content type and
// basic magic signatures. It never executes or renders the content.
func ValidateUpload(filename, declaredContentType string, data []byte) (ValidatedFile, *ServiceError) {
	fail := func(msg string) (ValidatedFile, *ServiceError) {
		return ValidatedFile{}, &ServiceError{Code: "INVALID_REQUEST", Message: msg, Status: 400}
	}
	ext := strings.ToLower(path.Ext(strings.TrimSpace(filename)))
	expected, ok := allowedExtensions[ext]
	if !ok {
		return fail("unsupported file type: only .txt, .md, .pdf, .docx are accepted")
	}
	if len(data) == 0 {
		return fail("file must not be empty")
	}
	if len(data) > MaxUploadBytes {
		return fail("file exceeds maximum size of 10 MiB")
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(declaredContentType, ";")[0]))
	if declared != "" && declared != "application/octet-stream" && !compatibleContentType(ext, declared) {
		return fail("content type does not match file extension")
	}
	if err := checkSignature(ext, data); err != nil {
		return fail(err.Message)
	}
	sum := sha256.Sum256(data)
	return ValidatedFile{
		ContentType: expected,
		Extension:   ext,
		Size:        int64(len(data)),
		Checksum:    hex.EncodeToString(sum[:]),
	}, nil
}

func compatibleContentType(ext, declared string) bool {
	switch ext {
	case ".txt":
		return declared == "text/plain"
	case ".md":
		return declared == "text/markdown" || declared == "text/plain" || declared == "text/x-markdown"
	case ".pdf":
		return declared == "application/pdf"
	case ".docx":
		return declared == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
			declared == "application/octet-stream" ||
			declared == "application/zip"
	default:
		return false
	}
}

func checkSignature(ext string, data []byte) *ServiceError {
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "malformed PDF: missing %PDF- signature", Status: 400}
		}
	case ".docx":
		// DOCX is a ZIP container.
		if len(data) < 4 || data[0] != 'P' || data[1] != 'K' || data[2] != 0x03 || data[3] != 0x04 {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "malformed DOCX: missing ZIP signature", Status: 400}
		}
	case ".txt", ".md":
		if bytes.IndexByte(data, 0) >= 0 {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "text file must not contain NUL bytes", Status: 400}
		}
	}
	return nil
}

// SafeObjectExtension maps a stored content type back to a safe suffix.
func SafeObjectExtension(contentType string) string {
	switch strings.ToLower(contentType) {
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/pdf":
		return ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	default:
		return ".bin"
	}
}
