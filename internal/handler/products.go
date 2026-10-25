package handler

import (
	"net/http"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
)

// ProductCatalog — каталог кредитных продуктов (статичный, встроен в бинарник).
type ProductCatalog interface {
	Products() []catalog.Product
	Groups(p catalog.Product) []string
	Group(name string) (catalog.Group, bool)
	RequiredDocuments(p catalog.Product) []string
	OptionalDocuments() []string
	DocumentTitle(kind string) string
	OrgTypes(p catalog.Product) []string
}

type ProductHandler struct {
	catalog ProductCatalog
}

func NewProductHandler(c ProductCatalog) *ProductHandler {
	return &ProductHandler{catalog: c}
}

type fieldGroupView struct {
	Name   string          `json:"name"`
	Title  string          `json:"title"`
	Fields []catalog.Field `json:"fields"`
}

type documentView struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type productDocumentsView struct {
	Required []documentView `json:"required"`
	Optional []documentView `json:"optional"`
}

type productView struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Description    string               `json:"description"`
	Rate           float64              `json:"rate"`
	MinAmount      int64                `json:"min_amount"`
	MaxAmount      int64                `json:"max_amount"`
	MinTermMonths  int                  `json:"min_term_months"`
	MaxTermMonths  int                  `json:"max_term_months"`
	Collateral     string               `json:"collateral"`
	OrgTypes       []string             `json:"org_types"`
	Needs9Months   bool                 `json:"needs_9_months"`
	YoungMaxAmount *int64               `json:"young_max_amount"`
	FieldGroups    []fieldGroupView     `json:"field_groups"`
	Documents      productDocumentsView `json:"documents"`
}

// List — каталог продуктов для калькулятора и мастера заявки. Доступен без входа: калькулятор
// открыт до авторизации.
func (h *ProductHandler) List(w http.ResponseWriter, _ *http.Request) {
	products := h.catalog.Products()
	out := make([]productView, 0, len(products))
	for _, p := range products {
		v := productView{
			ID: p.ID, Name: p.Name, Description: p.Description, Rate: p.BaseRate,
			MinAmount: p.MinAmount, MaxAmount: p.MaxAmount, MinTermMonths: p.MinTermMonths, MaxTermMonths: p.MaxTermMonths,
			Collateral: p.Collateral, OrgTypes: h.catalog.OrgTypes(p), Needs9Months: p.MinBusinessAgeMonths >= 9,
			FieldGroups: []fieldGroupView{},
			Documents:   productDocumentsView{Required: h.documents(h.catalog.RequiredDocuments(p)), Optional: h.documents(h.catalog.OptionalDocuments())},
		}
		if p.YoungMaxAmount > 0 {
			young := p.YoungMaxAmount
			v.YoungMaxAmount = &young
		}
		for _, name := range h.catalog.Groups(p) {
			g, _ := h.catalog.Group(name)
			v.FieldGroups = append(v.FieldGroups, fieldGroupView{Name: name, Title: g.Title, Fields: g.Fields})
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": out})
}

func (h *ProductHandler) documents(kinds []string) []documentView {
	out := make([]documentView, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, documentView{Kind: k, Title: h.catalog.DocumentTitle(k)})
	}
	return out
}
