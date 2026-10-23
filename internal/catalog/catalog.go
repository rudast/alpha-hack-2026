// Package catalog — каталог кредитных продуктов: срок, правила допуска, группы полей формы
// и обязательные документы. Данные лежат в products.yaml и встроены в бинарник.
package catalog

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed products.yaml
var productsYAML []byte

const (
	CollateralNone     = "none"
	CollateralOptional = "optional"
	CollateralRequired = "required"
)

type Field struct {
	Key      string   `yaml:"key" json:"key"`
	Label    string   `yaml:"label" json:"label"`
	Type     string   `yaml:"type" json:"type"` // text, number, money, bool, select
	Required bool     `yaml:"required" json:"required"`
	Options  []string `yaml:"options" json:"options,omitempty"`
}

type Group struct {
	Title  string  `yaml:"title" json:"title"`
	Fields []Field `yaml:"fields" json:"fields"`
}

type Product struct {
	ID            string   `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	MinTermMonths int      `yaml:"min_term_months" json:"min_term_months"`
	MaxTermMonths int      `yaml:"max_term_months" json:"max_term_months"`
	MaxAmount     int64    `yaml:"max_amount" json:"max_amount,omitempty"`
	Collateral    string   `yaml:"collateral" json:"collateral"`
	OrgTypes      []string `yaml:"org_types" json:"org_types,omitempty"`
	FieldGroups   []string `yaml:"field_groups" json:"field_groups,omitempty"`
	Documents     []string `yaml:"documents" json:"documents,omitempty"`
	Scoring       []string `yaml:"scoring" json:"scoring,omitempty"`
}

type file struct {
	BaseGroups            []string          `yaml:"base_groups"`
	BaseDocuments         []string          `yaml:"base_documents"`
	BaseOptionalDocuments []string          `yaml:"base_optional_documents"`
	Documents             map[string]string `yaml:"documents"`
	FieldGroups           map[string]Group  `yaml:"field_groups"`
	Products              []Product         `yaml:"products"`
}

type Catalog struct {
	f    file
	byID map[string]Product
}

// Load разбирает встроенный каталог и проверяет его целостность: уникальные идентификаторы,
// корректные сроки, существующие группы полей и документы.
func Load() (*Catalog, error) {
	var f file
	if err := yaml.Unmarshal(productsYAML, &f); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	c := &Catalog{f: f, byID: make(map[string]Product, len(f.Products))}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return c, nil
}

func (c *Catalog) validate() error {
	for _, g := range c.f.BaseGroups {
		if _, ok := c.f.FieldGroups[g]; !ok {
			return fmt.Errorf("base group %q is not defined", g)
		}
	}
	for _, d := range slices.Concat(c.f.BaseDocuments, c.f.BaseOptionalDocuments) {
		if _, ok := c.f.Documents[d]; !ok {
			return fmt.Errorf("base document %q is not defined", d)
		}
	}
	for name, g := range c.f.FieldGroups {
		for _, fld := range g.Fields {
			if fld.Type == "select" && len(fld.Options) == 0 {
				return fmt.Errorf("group %q field %q: select without options", name, fld.Key)
			}
		}
	}

	for _, p := range c.f.Products {
		if p.ID == "" {
			return fmt.Errorf("product %q has no id", p.Name)
		}
		if _, dup := c.byID[p.ID]; dup {
			return fmt.Errorf("duplicate product id %q", p.ID)
		}
		if p.MinTermMonths <= 0 || p.MaxTermMonths < p.MinTermMonths {
			return fmt.Errorf("product %q: invalid term range %d–%d", p.ID, p.MinTermMonths, p.MaxTermMonths)
		}
		switch p.Collateral {
		case CollateralNone, CollateralOptional, CollateralRequired:
		default:
			return fmt.Errorf("product %q: unknown collateral mode %q", p.ID, p.Collateral)
		}
		for _, t := range p.OrgTypes {
			if t != "IP" && t != "OOO" {
				return fmt.Errorf("product %q: unknown org type %q", p.ID, t)
			}
		}
		for _, g := range p.FieldGroups {
			if _, ok := c.f.FieldGroups[g]; !ok {
				return fmt.Errorf("product %q: field group %q is not defined", p.ID, g)
			}
		}
		for _, d := range p.Documents {
			if _, ok := c.f.Documents[d]; !ok {
				return fmt.Errorf("product %q: document %q is not defined", p.ID, d)
			}
		}
		c.byID[p.ID] = p
	}
	return nil
}

func (c *Catalog) Products() []Product { return slices.Clone(c.f.Products) }

func (c *Catalog) Product(id string) (Product, bool) {
	p, ok := c.byID[id]
	return p, ok
}

func (c *Catalog) Group(name string) (Group, bool) {
	g, ok := c.f.FieldGroups[name]
	return g, ok
}

func (c *Catalog) DocumentTitle(kind string) string { return c.f.Documents[kind] }

// Groups возвращает группы полей продукта: базовые, затем собственные.
func (c *Catalog) Groups(p Product) []string {
	return slices.Concat(c.f.BaseGroups, p.FieldGroups)
}

// RequiredDocuments возвращает виды обязательных документов продукта: базовые и собственные, без повторов.
func (c *Catalog) RequiredDocuments(p Product) []string {
	var out []string
	for _, d := range slices.Concat(c.f.BaseDocuments, p.Documents) {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// Available сообщает, подходит ли продукт организации данного типа и параметрам заявки.
// Нулевые значения amount и termMonths не проверяются.
func (c *Catalog) Available(p Product, orgType string, amount int64, termMonths int) bool {
	if len(p.OrgTypes) > 0 && !slices.Contains(p.OrgTypes, orgType) {
		return false
	}
	if p.MaxAmount > 0 && amount > p.MaxAmount {
		return false
	}
	if termMonths > 0 && (termMonths < p.MinTermMonths || termMonths > p.MaxTermMonths) {
		return false
	}
	return true
}

// MissingFormFields возвращает обязательные поля формы, которые не заполнены, в виде «группа.поле».
// form — содержимое applications.form: объект с блоками по группам полей.
func (c *Catalog) MissingFormFields(p Product, form map[string]any) []string {
	var missing []string
	for _, name := range c.Groups(p) {
		block, _ := form[name].(map[string]any)
		for _, fld := range c.f.FieldGroups[name].Fields {
			if fld.Required && !filled(block[fld.Key]) {
				missing = append(missing, name+"."+fld.Key)
			}
		}
	}
	return missing
}

func filled(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	default:
		return true
	}
}
