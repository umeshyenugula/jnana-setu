package chat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

func testHandlers() *Handlers {
	tenants, retriever, generator, store := testDeps()
	cfg := DefaultConfig()
	cfg.RateLimitPerMin = 1000
	return &Handlers{Service: NewService(cfg, tenants, retriever, generator, store), Version: "v1"}
}

func doChat(t *testing.T, h *Handlers, body string, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	api.CorrelationMiddleware(http.HandlerFunc(h.Chat)).ServeHTTP(rec, req)
	return rec
}

func TestHTTPChatValid(t *testing.T) {
	h := testHandlers()
	rec := doChat(t, h, `{"widgetId":"wdg_shop","message":"Hello"}`, "https://shop.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success envelope")
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T", env.Data)
	}
	for _, key := range []string{"sessionId", "status", "answer", "sources"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("missing key %q in %+v", key, data)
		}
	}
	if data["status"] != StatusAnswered {
		t.Fatalf("status = %v", data["status"])
	}
	if rec.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("missing correlation ID")
	}
}

func TestHTTPChatErrors(t *testing.T) {
	h := testHandlers()
	cases := []struct {
		name   string
		body   string
		origin string
		status int
		code   string
	}{
		{"malformed json", `{oops`, "", 400, CodeInvalidRequest},
		{"unknown field", `{"widgetId":"w","message":"hi","tenantId":"x"}`, "", 400, CodeInvalidRequest},
		{"empty message", `{"widgetId":"wdg_shop","message":"  "}`, "", 400, CodeInvalidRequest},
		{"unknown widget", `{"widgetId":"wdg_nope","message":"hi"}`, "", 404, CodeWidgetNotFound},
		{"bad origin", `{"widgetId":"wdg_shop","message":"hi"}`, "https://evil.example.com", 403, CodeOriginNotAllowed},
	}
	for _, c := range cases {
		rec := doChat(t, h, c.body, c.origin)
		if rec.Code != c.status {
			t.Fatalf("%s: status=%d body=%s", c.name, rec.Code, rec.Body.String())
		}
		var env api.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s: bad envelope: %v", c.name, err)
		}
		if env.Success || env.Error.Code != c.code {
			t.Fatalf("%s: %+v", c.name, env)
		}
	}
}

func TestHTTPHistory(t *testing.T) {
	h := testHandlers()
	rec := doChat(t, h, `{"widgetId":"wdg_shop","message":"Hi there"}`, "https://shop.example.com")
	var env api.SuccessResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	session := env.Data.(map[string]any)["sessionId"].(string)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions/"+session+"?widgetId=wdg_shop", nil)
	req.Header.Set("Origin", "https://shop.example.com")
	rec2 := httptest.NewRecorder()
	api.CorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.History(w, r, session)
	})).ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// Unknown session → 404, never another tenant's content.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions/ses_ffffffffffffffffffffffffffffffff?widgetId=wdg_shop", nil)
	rec3 := httptest.NewRecorder()
	h.History(rec3, req3, "ses_ffffffffffffffffffffffffffffffff")
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec3.Code)
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	// Router-level behavior is covered in router tests; handlers assume the
	// router has already filtered methods. This pins handler purity.
	h := testHandlers()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	rec := httptest.NewRecorder()
	h.Chat(rec, req) // empty body → 400, proving the handler ran
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
