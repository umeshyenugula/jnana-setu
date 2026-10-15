package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Storage persists original version bytes in the private bucket.
// Implementations: MemoryStorage (tests/dev) and SupabaseStorage (server-side,
// service-role key only — never exposed to the browser).
type Storage interface {
	Put(ctx context.Context, path, contentType string, data []byte) error
	Get(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

const BucketName = "knowledge-originals"

// ------------------------------------------------------------------ memory

type MemoryStorage struct {
	mu      map[string]storedObject
	failPut error
}

type storedObject struct {
	contentType string
	data        []byte
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{mu: map[string]storedObject{}}
}

func (s *MemoryStorage) InjectPutFailure(err error) { s.failPut = err }

func (s *MemoryStorage) Put(ctx context.Context, path, contentType string, data []byte) error {
	_ = ctx
	if s.failPut != nil {
		return s.failPut
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid storage path")
	}
	cp := append([]byte(nil), data...)
	s.mu[path] = storedObject{contentType: contentType, data: cp}
	return nil
}

func (s *MemoryStorage) Get(ctx context.Context, path string) ([]byte, error) {
	_ = ctx
	o, ok := s.mu[path]
	if !ok {
		return nil, fmt.Errorf("object not found")
	}
	return append([]byte(nil), o.data...), nil
}

func (s *MemoryStorage) Delete(ctx context.Context, path string) error {
	_ = ctx
	delete(s.mu, path)
	return nil
}

func (s *MemoryStorage) DeletePrefix(ctx context.Context, prefix string) error {
	_ = ctx
	for p := range s.mu {
		if strings.HasPrefix(p, prefix) {
			delete(s.mu, p)
		}
	}
	return nil
}

// --------------------------------------------------------------- supabase

// SupabaseStorage talks to Supabase Storage REST with the service-role key.
// Server-side only.
type SupabaseStorage struct {
	baseURL    string
	serviceKey string
	bucket     string
	http       *http.Client
}

func NewSupabaseStorage(baseURL, serviceKey string) *SupabaseStorage {
	return &SupabaseStorage{
		baseURL:    strings.TrimRight(baseURL, "/"),
		serviceKey: serviceKey,
		bucket:     BucketName,
		http:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *SupabaseStorage) Put(ctx context.Context, path, contentType string, data []byte) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s/%s", s.baseURL, s.bucket, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)
	req.Header.Set("apikey", s.serviceKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("storage upload failed: %s", resp.Status)
	}
	return nil
}

func (s *SupabaseStorage) Get(ctx context.Context, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/storage/v1/object/authenticated/%s/%s", s.baseURL, s.bucket, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)
	req.Header.Set("apikey", s.serviceKey)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage fetch failed: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, int64(MaxUploadBytes)+1))
}

func (s *SupabaseStorage) Delete(ctx context.Context, path string) error {
	return s.DeletePrefix(ctx, path)
}

func (s *SupabaseStorage) DeletePrefix(ctx context.Context, prefix string) error {
	url := fmt.Sprintf("%s/storage/v1/object/%s", s.baseURL, s.bucket)
	_ = url
	// Supabase deletes via POST /object/<bucket> with {"prefixes":[...]}.
	endpoint := fmt.Sprintf("%s/storage/v1/object/%s", s.baseURL, s.bucket)
	body := fmt.Sprintf(`{"prefixes":["%s"]}`, prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)
	req.Header.Set("apikey", s.serviceKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("storage delete failed: %s", resp.Status)
	}
	return nil
}
