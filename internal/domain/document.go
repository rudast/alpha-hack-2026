package domain

import (
	"errors"
	"time"
)

var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrInvalidDocument  = errors.New("invalid document")
	ErrDocumentTooLarge = errors.New("document is too large")
)

// MaxDocumentSize — наибольший размер загружаемого файла.
const MaxDocumentSize = 10 << 20

// DocumentUploaded — статус загруженного документа. Разбор документов не хранится:
// воркер скоринга читает файлы из тома сам.
const DocumentUploaded = "UPLOADED"

// Document — файл, загруженный к заявке. Сами файлы лежат в Docker-томе по схеме
// <id заявки>/<вид>/<имя файла>, в базе о них ничего не хранится: список документов
// заявки — это содержимое её папки.
type Document struct {
	ID         string    `json:"id"` // непрозрачный идентификатор для удаления (кодирует вид и имя файла)
	Kind       string    `json:"kind"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	Status     string    `json:"status"`
	UploadedAt time.Time `json:"created_at"`
	Path       string    `json:"-"` // путь внутри тома: передаётся воркеру в задаче
}
