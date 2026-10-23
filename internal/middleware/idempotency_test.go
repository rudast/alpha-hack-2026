package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// memIdempotency повторяет контракт Redis-хранилища в памяти.
type memIdempotency struct {
	mu      sync.Mutex
	records map[string]memRecord
}

type memRecord struct {
	fingerprint string
	done        bool
	response    domain.StoredResponse
}

func newMemIdempotency() *memIdempotency { return &memIdempotency{records: map[string]memRecord{}} }

func (m *memIdempotency) Begin(_ context.Context, scope, key, fp string) (domain.IdempotencyOutcome, domain.StoredResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := scope + ":" + key
	rec, ok := m.records[k]
	switch {
	case !ok:
		m.records[k] = memRecord{fingerprint: fp}
		return domain.IdempotencyAcquired, domain.StoredResponse{}, nil
	case rec.fingerprint != fp:
		return domain.IdempotencyMismatch, domain.StoredResponse{}, nil
	case rec.done:
		return domain.IdempotencyReplay, rec.response, nil
	}
	return domain.IdempotencyInProgress, domain.StoredResponse{}, nil
}

func (m *memIdempotency) Finish(_ context.Context, scope, key, fp string, resp domain.StoredResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[scope+":"+key] = memRecord{fingerprint: fp, done: true, response: resp}
	return nil
}

func (m *memIdempotency) Abort(_ context.Context, scope, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, scope+":"+key)
	return nil
}

// counted — хендлер, который считает свои вызовы и возвращает заданный код.
type counted struct {
	calls int
	code  int
}

func (c *counted) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	c.calls++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(c.code)
	_, _ = w.Write([]byte(`{"call":` + string(rune('0'+c.calls)) + `}`))
}

func send(h http.Handler, user, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/applications", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{UserID: user}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIdempotency_RepeatWithSameKeyReplaysTheResponse(t *testing.T) {
	handler := &counted{code: http.StatusCreated}
	h := Idempotency(newMemIdempotency())(handler)

	first := send(h, "u1", "key-12345678", `{"a":1}`)
	second := send(h, "u1", "key-12345678", `{"a":1}`)

	if handler.calls != 1 {
		t.Fatalf("the action ran %d times, want 1", handler.calls)
	}
	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replay differs: %d %q vs %d %q", second.Code, second.Body, first.Code, first.Body)
	}
	if second.Header().Get("Idempotent-Replayed") != "true" || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("only the repeated response must be marked as replayed")
	}
}

func TestIdempotency_KeyIsRequiredAndValidated(t *testing.T) {
	handler := &counted{code: http.StatusOK}
	h := Idempotency(newMemIdempotency())(handler)

	for name, key := range map[string]string{"missing": "", "too short": "abc", "has a space": "key with space 1", "too long": strings.Repeat("k", 129)} {
		if rec := send(h, "u1", key, `{}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, rec.Code)
		}
	}
	if handler.calls != 0 {
		t.Fatal("the handler must not run without a valid key")
	}
}

func TestIdempotency_SameKeyForDifferentRequestIsRejected(t *testing.T) {
	handler := &counted{code: http.StatusOK}
	h := Idempotency(newMemIdempotency())(handler)

	send(h, "u1", "key-12345678", `{"amount":1}`)
	rec := send(h, "u1", "key-12345678", `{"amount":2}`)

	if rec.Code != http.StatusUnprocessableEntity || handler.calls != 1 {
		t.Fatalf("got %d, calls %d; want 422 and 1 call", rec.Code, handler.calls)
	}
}

func TestIdempotency_KeysOfDifferentUsersDoNotCollide(t *testing.T) {
	handler := &counted{code: http.StatusOK}
	h := Idempotency(newMemIdempotency())(handler)

	send(h, "u1", "key-12345678", `{}`)
	send(h, "u2", "key-12345678", `{}`)

	if handler.calls != 2 {
		t.Fatalf("calls = %d, want 2 (one per user)", handler.calls)
	}
}

func TestIdempotency_RequestStillRunningGivesConflict(t *testing.T) {
	store := newMemIdempotency()
	// запрос с этим ключом уже начат, но не закончен
	_, _, _ = store.Begin(context.Background(), "u1", "key-12345678", fingerprintOf(`{}`))
	handler := &counted{code: http.StatusOK}

	rec := send(Idempotency(store)(handler), "u1", "key-12345678", `{}`)

	if rec.Code != http.StatusConflict || handler.calls != 0 {
		t.Fatalf("got %d, calls %d; want 409 and 0 calls", rec.Code, handler.calls)
	}
}

func TestIdempotency_ServerErrorIsNotRememberedSoRetryRunsAgain(t *testing.T) {
	handler := &counted{code: http.StatusInternalServerError}
	h := Idempotency(newMemIdempotency())(handler)

	send(h, "u1", "key-12345678", `{}`)
	handler.code = http.StatusOK
	rec := send(h, "u1", "key-12345678", `{}`)

	if handler.calls != 2 || rec.Code != http.StatusOK {
		t.Fatalf("got %d, calls %d; want a fresh run (200, 2 calls)", rec.Code, handler.calls)
	}
}

func TestIdempotency_ClientErrorIsReplayed(t *testing.T) {
	handler := &counted{code: http.StatusUnprocessableEntity}
	h := Idempotency(newMemIdempotency())(handler)

	send(h, "u1", "key-12345678", `{}`)
	rec := send(h, "u1", "key-12345678", `{}`)

	if handler.calls != 1 || rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, calls %d; want the stored 422 and 1 call", rec.Code, handler.calls)
	}
}

// fingerprintOf считает отпечаток так же, как middleware для POST /applications.
func fingerprintOf(body string) string {
	rec := &fingerprintProbe{}
	Idempotency(rec)(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), probeRequest(body))
	return rec.fp
}

type fingerprintProbe struct{ fp string }

func (p *fingerprintProbe) Begin(_ context.Context, _, _, fp string) (domain.IdempotencyOutcome, domain.StoredResponse, error) {
	p.fp = fp
	return domain.IdempotencyMismatch, domain.StoredResponse{}, nil
}
func (p *fingerprintProbe) Finish(context.Context, string, string, string, domain.StoredResponse) error {
	return nil
}
func (p *fingerprintProbe) Abort(context.Context, string, string) error { return nil }

func probeRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/applications", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "key-12345678")
	return req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{UserID: "u1"}))
}
