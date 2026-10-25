package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// DocumentStore — файлы заявок в Docker-томе (repository.DocumentStore).
type DocumentStore interface {
	Save(applicationID, kind, filename string, content io.Reader) (domain.Document, error)
	List(applicationID string) ([]domain.Document, error)
	Delete(applicationID, documentID string) error
}

// Допустимые типы файлов: определяются по содержимому, а не по расширению.
var allowedDocumentTypes = map[string]bool{"application/pdf": true, "image/jpeg": true, "image/png": true}

// UploadDocument сохраняет документ к черновику заявки. kind — вид документа из каталога
// (passport, account_statement…); файл — PDF, JPG или PNG не больше domain.MaxDocumentSize.
// Файл с тем же видом и именем заменяется.
func (s *ApplicationService) UploadDocument(ctx context.Context, p domain.Principal, id, kind, filename string, content io.Reader) (domain.Document, error) {
	if err := s.draftOf(ctx, p, id); err != nil {
		return domain.Document{}, err
	}
	if s.catalog.DocumentTitle(kind) == "" {
		return domain.Document{}, domain.ErrInvalidDocument
	}
	name := documentFilename(filename)
	if name == "" {
		return domain.Document{}, domain.ErrInvalidDocument
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(content, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return domain.Document{}, err
	}
	if n == 0 || !allowedDocumentTypes[http.DetectContentType(head[:n])] {
		return domain.Document{}, domain.ErrInvalidDocument
	}
	limited := &limitedReader{r: io.MultiReader(bytes.NewReader(head[:n]), content), left: domain.MaxDocumentSize}
	doc, err := s.documents.Save(id, kind, name, limited)
	if limited.exceeded {
		_ = s.documents.Delete(id, doc.ID)
		return domain.Document{}, domain.ErrDocumentTooLarge
	}
	return doc, err
}

// DeleteDocument удаляет документ черновика.
func (s *ApplicationService) DeleteDocument(ctx context.Context, p domain.Principal, id, documentID string) error {
	if err := s.draftOf(ctx, p, id); err != nil {
		return err
	}
	return s.documents.Delete(id, documentID)
}

// draftOf проверяет, что заявка принадлежит компании и её документы ещё можно менять (черновик или запрос документов).
func (s *ApplicationService) draftOf(ctx context.Context, p domain.Principal, id string) error {
	a, err := s.apps.GetForCompany(ctx, id, p.CompanyINN)
	if err != nil {
		return err
	}
	// Документы меняются до отправки, а также когда банк запросил дополнительные документы.
	if a.Status != domain.StatusDraft && a.Status != domain.StatusInfoRequested {
		return domain.ErrConflict
	}
	return nil
}

// withDocuments добавляет к заявке список её документов из тома.
func (s *ApplicationService) withDocuments(a domain.Application, err error) (domain.Application, error) {
	if err != nil {
		return a, err
	}
	docs, err := s.documents.List(a.ID)
	if err != nil {
		return domain.Application{}, err
	}
	a.Documents = docs
	return a, nil
}

// documentFilename оставляет от имени, присланного браузером, только само имя файла (без пути)
// и убирает символы, недопустимые в имени файла.
func documentFilename(raw string) string {
	name := path.Base(strings.ReplaceAll(raw, `\`, "/")) // path, а не filepath: результат не зависит от ОС сервера
	if name == "." || name == "/" {
		name = ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.TrimLeft(name, ".")
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

// limitedReader читает не больше left байт и запоминает, что файл оказался больше.
type limitedReader struct {
	r        io.Reader
	left     int64
	exceeded bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		var one [1]byte
		if n, _ := l.r.Read(one[:]); n > 0 {
			l.exceeded = true
			return 0, domain.ErrDocumentTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}
