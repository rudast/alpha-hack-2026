package repository

import (
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// DocumentStore хранит файлы заявок в каталоге (в Docker это том) по схеме <id заявки>/<вид>/<имя файла>.
// Базы для документов нет: список документов заявки — это содержимое её папки.
type DocumentStore struct {
	root string
}

// NewDocumentStore создаёт каталог root, если его нет.
func NewDocumentStore(root string) (*DocumentStore, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &DocumentStore{root: root}, nil
}

// Save записывает файл. Файл с тем же видом и именем перезаписывается. Сначала пишется во временный
// файл и затем переименовывается, чтобы воркер не прочитал файл, записанный наполовину.
func (s *DocumentStore) Save(applicationID, kind, filename string, content io.Reader) (domain.Document, error) {
	if !safeName(applicationID) || !safeName(kind) || !safeName(filename) {
		return domain.Document{}, domain.ErrInvalidDocument
	}
	dir := filepath.Join(s.root, applicationID, kind)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return domain.Document{}, err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return domain.Document{}, err
	}
	defer os.Remove(tmp.Name()) // после переименования файла уже нет, ошибка не важна
	if _, err := io.Copy(tmp, content); err != nil {
		tmp.Close()
		return domain.Document{}, err
	}
	if err := tmp.Close(); err != nil {
		return domain.Document{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, filename)); err != nil {
		return domain.Document{}, err
	}
	return s.stat(applicationID, kind, filename)
}

// List возвращает документы заявки, отсортированные по виду и имени. Нет папки — нет документов.
func (s *DocumentStore) List(applicationID string) ([]domain.Document, error) {
	docs := make([]domain.Document, 0)
	if !safeName(applicationID) {
		return docs, nil
	}
	kinds, err := os.ReadDir(filepath.Join(s.root, applicationID))
	if errors.Is(err, fs.ErrNotExist) {
		return docs, nil
	}
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		if !k.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.root, applicationID, k.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			d, err := s.stat(applicationID, k.Name(), f.Name())
			if err != nil {
				return nil, err
			}
			docs = append(docs, d)
		}
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Kind != docs[j].Kind {
			return docs[i].Kind < docs[j].Kind
		}
		return docs[i].Filename < docs[j].Filename
	})
	return docs, nil
}

// Delete удаляет документ по идентификатору из Document.ID.
func (s *DocumentStore) Delete(applicationID, documentID string) error {
	kind, filename, ok := DecodeDocumentID(documentID)
	if !ok || !safeName(applicationID) {
		return domain.ErrDocumentNotFound
	}
	err := os.Remove(filepath.Join(s.root, applicationID, kind, filename))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.ErrDocumentNotFound
	}
	return err
}

// Open открывает документ для скачивания. Закрыть файл должен вызывающий.
func (s *DocumentStore) Open(applicationID, documentID string) (io.ReadCloser, domain.Document, error) {
	kind, filename, ok := DecodeDocumentID(documentID)
	if !ok || !safeName(applicationID) {
		return nil, domain.Document{}, domain.ErrDocumentNotFound
	}
	doc, err := s.stat(applicationID, kind, filename)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, domain.Document{}, domain.ErrDocumentNotFound
	}
	if err != nil {
		return nil, domain.Document{}, err
	}
	f, err := os.Open(filepath.Join(s.root, applicationID, kind, filename))
	if err != nil {
		return nil, domain.Document{}, err
	}
	return f, doc, nil
}

func (s *DocumentStore) stat(applicationID, kind, filename string) (domain.Document, error) {
	info, err := os.Stat(filepath.Join(s.root, applicationID, kind, filename))
	if err != nil {
		return domain.Document{}, err
	}
	return domain.Document{
		ID:         base64.RawURLEncoding.EncodeToString([]byte(kind + "/" + filename)),
		Kind:       kind,
		Filename:   filename,
		Size:       info.Size(),
		Status:     domain.DocumentUploaded,
		UploadedAt: info.ModTime().UTC(),
		Path:       path.Join(applicationID, kind, filename),
	}, nil
}

// DecodeDocumentID разбирает идентификатор документа на вид и имя файла.
func DecodeDocumentID(id string) (kind, filename string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", false
	}
	kind, filename, ok = strings.Cut(string(raw), "/")
	return kind, filename, ok && safeName(kind) && safeName(filename)
}

// safeName не пропускает пустые имена, «.», «..», разделители путей и скрытые файлы,
// чтобы имя нельзя было использовать для выхода за пределы каталога.
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`+"\x00") && filepath.Base(name) == name
}
