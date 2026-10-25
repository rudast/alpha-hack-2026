package domain

import "encoding/json"

// ApplicationResult — результат обработки заявки, который воркер скоринга присылает через Redis:
// «заявка была в статусе From, теперь в статусе To». Decision (JSON-объект решения) приходит не всегда.
type ApplicationResult struct {
	EventID       string
	ApplicationID string
	From          Status
	To            Status
	Decision      json.RawMessage
	Comment       string
}
