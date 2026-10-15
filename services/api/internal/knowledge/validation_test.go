package knowledge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateUploadSupportedTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		ct   string
		data []byte
	}{
		{"txt", "text/plain", []byte("hello world")},
		{"md", "text/markdown", []byte("# hello")},
		{"pdf", "application/pdf", []byte("%PDF-1.7 fake")},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte{'P', 'K', 0x03, 0x04, 0x00}},
	} {
		ext := map[string]string{"txt": ".txt", "md": ".md", "pdf": ".pdf", "docx": ".docx"}[tc.name]
		vf, err := ValidateUpload("file"+ext, tc.ct, tc.data)
		require.Nil(t, err, tc.name)
		require.NotEmpty(t, vf.Checksum)
	}
}

func TestValidateUploadUnsupported(t *testing.T) {
	_, err := ValidateUpload("evil.exe", "application/octet-stream", []byte("x"))
	require.NotNil(t, err)
	require.Equal(t, 400, err.Status)
}

func TestValidateUploadOversizedAndEmpty(t *testing.T) {
	_, err := ValidateUpload("a.txt", "text/plain", []byte{})
	require.NotNil(t, err)
	big := make([]byte, MaxUploadBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	_, err = ValidateUpload("a.txt", "text/plain", big)
	require.NotNil(t, err)
}

func TestValidateUploadMalformed(t *testing.T) {
	_, err := ValidateUpload("a.pdf", "application/pdf", []byte("not a pdf"))
	require.NotNil(t, err)
	_, err = ValidateUpload("a.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("not a zip"))
	require.NotNil(t, err)
	_, err = ValidateUpload("a.txt", "application/pdf", []byte("mismatch"))
	require.NotNil(t, err)
}

func TestValidateText(t *testing.T) {
	require.NotNil(t, ValidateTextContent("   "))
	require.NotNil(t, ValidateTextContent(strings.Repeat("a", MaxTextChars+1)))
	require.Nil(t, ValidateTextContent("valid knowledge text"))
}
