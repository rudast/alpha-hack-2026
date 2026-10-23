package domain

// IdempotencyOutcome — что хранилище ответило на попытку начать запрос с ключом идемпотентности.
type IdempotencyOutcome int

const (
	IdempotencyAcquired   IdempotencyOutcome = iota // ключ новый, запрос нужно выполнить
	IdempotencyReplay                               // запрос с этим ключом уже выполнен, нужно повторить ответ
	IdempotencyInProgress                           // запрос с этим ключом сейчас выполняется
	IdempotencyMismatch                             // ключ уже использован для другого запроса
)

// StoredResponse — сохранённый ответ, который повторяется при повторном запросе с тем же ключом.
type StoredResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
}
