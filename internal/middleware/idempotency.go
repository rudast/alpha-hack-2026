package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

const (
	idempotencyHeader = "Idempotency-Key"
	maxStoredBody     = 1 << 20
)

type IdempotencyStore interface {
	Begin(ctx context.Context, scope, key, fingerprint string) (domain.IdempotencyOutcome, domain.StoredResponse, error)
	Finish(ctx context.Context, scope, key, fingerprint string, resp domain.StoredResponse) error
	Abort(ctx context.Context, scope, key string) error
}

// Idempotency делает изменяющий запрос безопасным для повтора. Клиент присылает уникальный ключ
// в заголовке Idempotency-Key; первый запрос с ключом выполняется, а повтор с тем же ключом
// и тем же содержимым получает тот же самый ответ, не выполняя действие второй раз.
// Ставится после Authenticate: ключи разных пользователей не пересекаются.
func Idempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return IdempotencyWithLimit(store, maxStoredBody)
}

// IdempotencyWithLimit — то же, что Idempotency, но принимает тело запроса до maxBody байт
// (например, загрузку файла). Ответ по-прежнему хранится, только если он не больше maxStoredBody.
func IdempotencyWithLimit(store IdempotencyStore, maxBody int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			key := r.Header.Get(idempotencyHeader)
			if !validIdempotencyKey(key) {
				writeError(w, http.StatusBadRequest, "Idempotency-Key header is required (8 to 128 printable characters)")
				return
			}

			// Тело читаем целиком: оно входит в «отпечаток» запроса, а затем нужно хендлеру.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
			fingerprint := hex.EncodeToString(sum[:])

			outcome, saved, err := store.Begin(r.Context(), p.UserID, key, fingerprint)
			if err != nil {
				log.Printf("idempotency: %v", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			switch outcome {
			case domain.IdempotencyReplay:
				replay(w, saved)
				return
			case domain.IdempotencyInProgress:
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusConflict, "a request with this Idempotency-Key is still being processed")
				return
			case domain.IdempotencyMismatch:
				writeError(w, http.StatusUnprocessableEntity, "Idempotency-Key was already used for a different request")
				return
			}

			// Дальше фоновый контекст: клиент мог отключиться, а результат всё равно нужно сохранить.
			ctx := context.WithoutCancel(r.Context())
			rec := &recorder{ResponseWriter: w}
			func() {
				defer func() {
					if v := recover(); v != nil {
						_ = store.Abort(ctx, p.UserID, key)
						panic(v)
					}
				}()
				next.ServeHTTP(rec, r)
			}()

			// Ошибку сервера не запоминаем: повтор с тем же ключом должен выполниться заново.
			if rec.status() >= 500 || rec.truncated {
				_ = store.Abort(ctx, p.UserID, key)
				return
			}
			resp := domain.StoredResponse{Status: rec.status(), ContentType: w.Header().Get("Content-Type"), Body: rec.body.Bytes()}
			if err := store.Finish(ctx, p.UserID, key, fingerprint, resp); err != nil {
				log.Printf("idempotency: cannot save response: %v", err)
			}
		})
	}
}

func validIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func replay(w http.ResponseWriter, s domain.StoredResponse) {
	if s.ContentType != "" {
		w.Header().Set("Content-Type", s.ContentType)
	}
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(s.Status)
	_, _ = w.Write(s.Body)
}

// recorder пропускает ответ клиенту и запоминает его код и тело.
type recorder struct {
	http.ResponseWriter
	code      int
	body      bytes.Buffer
	truncated bool
}

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	if r.body.Len()+len(b) > maxStoredBody {
		r.truncated = true
	} else {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) status() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}
