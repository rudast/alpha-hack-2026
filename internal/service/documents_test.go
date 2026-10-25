package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// fakeDocuments хранит файлы в памяти: ключ — id заявки, затем вид/имя файла.
type fakeDocuments struct {
	files map[string]map[string][]byte
}

func newFakeDocuments() *fakeDocuments { return &fakeDocuments{files: map[string]map[string][]byte{}} }

func (f *fakeDocuments) Save(appID, kind, name string, content io.Reader) (domain.Document, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return domain.Document{}, err
	}
	if f.files[appID] == nil {
		f.files[appID] = map[string][]byte{}
	}
	f.files[appID][kind+"/"+name] = data
	return domain.Document{ID: kind + "/" + name, Kind: kind, Filename: name, Size: int64(len(data)),
		Status: domain.DocumentUploaded, Path: appID + "/" + kind + "/" + name}, nil
}

func (f *fakeDocuments) List(appID string) ([]domain.Document, error) {
	docs := []domain.Document{}
	for key, data := range f.files[appID] {
		kind, name, _ := strings.Cut(key, "/")
		docs = append(docs, domain.Document{ID: key, Kind: kind, Filename: name, Size: int64(len(data)), Path: appID + "/" + key})
	}
	return docs, nil
}

func (f *fakeDocuments) Delete(appID, id string) error {
	if _, ok := f.files[appID][id]; !ok {
		return domain.ErrDocumentNotFound
	}
	delete(f.files[appID], id)
	return nil
}

func documentsOf(s *ApplicationService) *fakeDocuments { return s.documents.(*fakeDocuments) }

var pdf = []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n")

func TestUploadDocument(t *testing.T) {
	ctx := context.Background()

	t.Run("a PDF is stored and listed with the application", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		a, _ := s.Create(ctx, clientA, completeExpress())
		doc, err := s.UploadDocument(ctx, clientA, a.ID, "passport", `C:\scans\паспорт.pdf`, bytes.NewReader(pdf))
		if err != nil || doc.Filename != "паспорт.pdf" || doc.Kind != "passport" || doc.Status != domain.DocumentUploaded {
			t.Fatalf("got %+v, %v", doc, err)
		}
		got, _ := s.Get(ctx, clientA, a.ID)
		if len(got.Documents) != 1 || got.Documents[0].Kind != "passport" {
			t.Fatalf("documents %+v", got.Documents)
		}
	})

	t.Run("rejected files", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		a, _ := s.Create(ctx, clientA, completeExpress())
		big := append(append([]byte{}, pdf...), make([]byte, domain.MaxDocumentSize)...)
		cases := map[string]struct {
			kind, name string
			data       []byte
			want       error
		}{
			"unknown kind":          {"selfie", "a.pdf", pdf, domain.ErrInvalidDocument},
			"not a PDF or an image": {"passport", "a.pdf", []byte("just some text"), domain.ErrInvalidDocument},
			"empty file":            {"passport", "a.pdf", nil, domain.ErrInvalidDocument},
			"too large":             {"passport", "big.pdf", big, domain.ErrDocumentTooLarge},
		}
		for name, tc := range cases {
			if _, err := s.UploadDocument(ctx, clientA, a.ID, tc.kind, tc.name, bytes.NewReader(tc.data)); !errors.Is(err, tc.want) {
				t.Errorf("%s: got %v, want %v", name, err, tc.want)
			}
		}
		if docs, _ := documentsOf(s).List(a.ID); len(docs) != 0 {
			t.Fatalf("rejected files must not be stored, have %v", docs)
		}
	})

	t.Run("only a draft of the own company", func(t *testing.T) {
		s, _ := newApplications(t, "OOO")
		a, _ := s.Create(ctx, clientA, completeExpress())
		if _, err := s.UploadDocument(ctx, clientB, a.ID, "passport", "a.pdf", bytes.NewReader(pdf)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("other company: got %v", err)
		}
		if _, err := s.Submit(ctx, clientA, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UploadDocument(ctx, clientA, a.ID, "passport", "a.pdf", bytes.NewReader(pdf)); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("submitted application: got %v, want ErrConflict", err)
		}
	})
}

func TestDeleteDocument(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())
	doc, _ := s.UploadDocument(ctx, clientA, a.ID, "passport", "a.pdf", bytes.NewReader(pdf))

	if err := s.DeleteDocument(ctx, clientA, a.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDocument(ctx, clientA, a.ID, doc.ID); !errors.Is(err, domain.ErrDocumentNotFound) {
		t.Fatalf("second delete: got %v, want ErrDocumentNotFound", err)
	}
}

func TestSubmit_DocumentsGoToTheWorkerTask(t *testing.T) {
	ctx := context.Background()
	s, _ := newApplications(t, "OOO")
	a, _ := s.Create(ctx, clientA, completeExpress())
	_, _ = s.UploadDocument(ctx, clientA, a.ID, "passport", "a.pdf", bytes.NewReader(pdf))

	if _, err := s.Submit(ctx, clientA, a.ID); err != nil {
		t.Fatal(err)
	}
	docs := publishedBy(s).tasks[0].Documents
	if len(docs) != 1 || docs[0].Path != a.ID+"/passport/a.pdf" {
		t.Fatalf("task documents %+v", docs)
	}
}

func TestDocumentFilename(t *testing.T) {
	for raw, want := range map[string]string{
		`C:\Users\me\выписка.pdf`: "выписка.pdf",
		"../../etc/passwd":        "passwd",
		"..hidden.pdf":            "hidden.pdf",
		"a:b?.pdf":                "a_b_.pdf",
		"   ":                     "",
	} {
		if got := documentFilename(raw); got != want {
			t.Errorf("%q: got %q, want %q", raw, got, want)
		}
	}
}
