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
	ID                   string   `yaml:"id" json:"id"`
	Name                 string   `yaml:"name" json:"name"`
	Description          string   `yaml:"description" json:"description"`
	MinTermMonths        int      `yaml:"min_term_months" json:"min_term_months"`
	MaxTermMonths        int      `yaml:"max_term_months" json:"max_term_months"`
	MinAmount            int64    `yaml:"min_amount" json:"min_amount"`
	MaxAmount            int64    `yaml:"max_amount" json:"max_amount"`
	BaseRate             float64  `yaml:"base_rate" json:"base_rate"`
	MinBusinessAgeMonths int      `yaml:"min_business_age_months" json:"min_business_age_months,omitempty"`
	YoungMaxAmount       int64    `yaml:"young_max_amount" json:"young_max_amount,omitempty"`
	Collateral           string   `yaml:"collateral" json:"collateral"`
	OrgTypes             []string `yaml:"org_types" json:"org_types,omitempty"`
	FieldGroups          []string `yaml:"field_groups" json:"field_groups,omitempty"`
	Documents            []string `yaml:"documents" json:"documents,omitempty"`
	Scoring              []string `yaml:"scoring" json:"scoring,omitempty"`
}

// OfferRules — правила предварительного предложения (секция offer в products.yaml).
type OfferRules struct {
	Products            []string `yaml:"products"`
	MaxDebtToEquity     float64  `yaml:"max_debt_to_equity"`
	HeadroomShare       float64  `yaml:"headroom_share"`
	MinAmount           int64    `yaml:"min_amount"`
	NoBKICap            int64    `yaml:"no_bki_cap"`
	RoundTo             int64    `yaml:"round_to"`
	TermMonths          int      `yaml:"term_months"`
	RateDiscount        float64  `yaml:"rate_discount"`
	ValidityDays        int      `yaml:"validity_days"`
	YoungBusinessMonths int      `yaml:"young_business_months"`
}

type file struct {
	BaseGroups            []string          `yaml:"base_groups"`
	BaseDocuments         []string          `yaml:"base_documents"`
	BaseOptionalDocuments []string          `yaml:"base_optional_documents"`
	Documents             map[string]string `yaml:"documents"`
	FieldGroups           map[string]Group  `yaml:"field_groups"`
	Offer                 OfferRules        `yaml:"offer"`
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
		if p.MinAmount <= 0 || p.MaxAmount < p.MinAmount {
			return fmt.Errorf("product %q: invalid amount range %d–%d", p.ID, p.MinAmount, p.MaxAmount)
		}
		if p.BaseRate <= 0 {
			return fmt.Errorf("product %q: base rate must be positive", p.ID)
		}
		if p.MinBusinessAgeMonths < 0 || p.YoungMaxAmount < 0 {
			return fmt.Errorf("product %q: business age rules must not be negative", p.ID)
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
	return c.validateOffer()
}

func (c *Catalog) validateOffer() error {
	o := c.f.Offer
	if len(o.Products) == 0 {
		return fmt.Errorf("offer: no products")
	}
	for _, id := range o.Products {
		p, ok := c.byID[id]
		if !ok {
			return fmt.Errorf("offer: unknown product %q", id)
		}
		if p.Collateral != CollateralNone {
			return fmt.Errorf("offer: product %q requires collateral, the offer is only for unsecured products", id)
		}
	}
	switch {
	case o.MaxDebtToEquity <= 0, o.HeadroomShare <= 0, o.HeadroomShare > 1:
		return fmt.Errorf("offer: max_debt_to_equity must be positive and headroom_share in (0; 1]")
	case o.MinAmount <= 0, o.NoBKICap < o.MinAmount, o.RoundTo <= 0:
		return fmt.Errorf("offer: invalid amounts (min_amount, no_bki_cap, round_to)")
	case o.TermMonths <= 0, o.ValidityDays <= 0, o.YoungBusinessMonths <= 0, o.RateDiscount < 0:
		return fmt.Errorf("offer: term_months, validity_days and young_business_months must be positive, rate_discount not negative")
	}
	return nil
}

// OfferRules возвращает правила предварительного предложения.
func (c *Catalog) OfferRules() OfferRules {
	o := c.f.Offer
	o.Products = slices.Clone(o.Products)
	return o
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

// OptionalDocuments возвращает виды необязательных документов (базовый набор, общий для всех продуктов).
func (c *Catalog) OptionalDocuments() []string { return slices.Clone(c.f.BaseOptionalDocuments) }

// OrgTypes возвращает типы организаций, которым доступен продукт (пустой список в каталоге — обоим).
func (c *Catalog) OrgTypes(p Product) []string {
	if len(p.OrgTypes) == 0 {
		return []string{"IP", "OOO"}
	}
	return slices.Clone(p.OrgTypes)
}

// Available сообщает, подходит ли продукт организации данного типа и параметрам заявки.
// Нулевые значения amount и termMonths не проверяются. Возраст бизнеса проверяет AvailableForAge.
func (c *Catalog) Available(p Product, orgType string, amount int64, termMonths int) bool {
	if len(p.OrgTypes) > 0 && !slices.Contains(p.OrgTypes, orgType) {
		return false
	}
	if amount > 0 && (amount < p.MinAmount || (p.MaxAmount > 0 && amount > p.MaxAmount)) {
		return false
	}
	if termMonths > 0 && (termMonths < p.MinTermMonths || termMonths > p.MaxTermMonths) {
		return false
	}
	return true
}

// AvailableForAge сообщает, подходит ли продукт бизнесу возрастом ageMonths с суммой amount: продукт может
// требовать минимальный возраст бизнеса, а молодому бизнесу (младше young_business_months) — ограничивать сумму.
func (c *Catalog) AvailableForAge(p Product, ageMonths int, amount int64) bool {
	if ageMonths < p.MinBusinessAgeMonths {
		return false
	}
	if ageMonths < c.f.Offer.YoungBusinessMonths && p.YoungMaxAmount > 0 && amount > p.YoungMaxAmount {
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
