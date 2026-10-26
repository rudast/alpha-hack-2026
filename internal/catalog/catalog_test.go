package catalog

import (
	"errors"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func load(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadHasAllFourteenProducts(t *testing.T) {
	c := load(t)
	if got := len(c.Products()); got != 14 {
		t.Fatalf("got %d products, want 14", got)
	}
	for _, id := range []string{"overdraft", "credit_line", "express", "auto_loan", "mortgage_business"} {
		if _, ok := c.Product(id); !ok {
			t.Errorf("product %q is missing", id)
		}
	}
}

func TestProductsWithoutExtraFieldsOnlyAskBaseSet(t *testing.T) {
	c := load(t)
	for _, id := range []string{"express", "ip_loan", "ooo_loan"} {
		p, _ := c.Product(id)
		if !slices.Equal(c.Groups(p), []string{"applicant"}) {
			t.Errorf("%s: groups %v, want only the base group", id, c.Groups(p))
		}
		if !slices.Equal(c.RequiredDocuments(p), []string{"passport", "bki_consent"}) {
			t.Errorf("%s: documents %v", id, c.RequiredDocuments(p))
		}
	}
}

func TestRequiredDocumentsHaveNoDuplicates(t *testing.T) {
	c := load(t)
	for _, p := range c.Products() {
		docs := c.RequiredDocuments(p)
		seen := map[string]bool{}
		for _, d := range docs {
			if seen[d] {
				t.Errorf("%s: duplicate document %q", p.ID, d)
			}
			seen[d] = true
		}
		if !seen["passport"] || !seen["bki_consent"] {
			t.Errorf("%s: base documents are missing", p.ID)
		}
	}
}

func TestSecuredProductsRequireCollateralBlock(t *testing.T) {
	c := load(t)
	for _, p := range c.Products() {
		if p.Collateral == CollateralRequired && !slices.Contains(p.FieldGroups, "collateral") {
			t.Errorf("%s: collateral is required but the collateral group is not asked", p.ID)
		}
	}
}

func TestAvailable(t *testing.T) {
	c := load(t)
	mortgageIP, _ := c.Product("mortgage_ip")
	upTo1M, _ := c.Product("loan_up_to_1m")
	overdraft, _ := c.Product("overdraft")

	cases := []struct {
		name   string
		p      Product
		org    string
		amount int64
		term   int
		want   bool
	}{
		{"ip product for IP", mortgageIP, "IP", 5_000_000, 60, true},
		{"ip product for OOO", mortgageIP, "OOO", 5_000_000, 60, false},
		{"amount over the cap", upTo1M, "OOO", 1_000_001, 12, false},
		{"amount at the cap", upTo1M, "OOO", 1_000_000, 12, true},
		{"term too long", overdraft, "OOO", 100_000, 13, false},
		{"term not given", overdraft, "OOO", 100_000, 0, true},
	}
	for _, tc := range cases {
		if got := c.Available(tc.p, tc.org, tc.amount, tc.term); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMissingFormFields(t *testing.T) {
	c := load(t)
	auto, _ := c.Product("auto_loan")

	empty := c.MissingFormFields(auto, map[string]any{})
	want := []string{
		"applicant.position", "applicant.has_collateral",
		"collateral.type", "collateral.description", "collateral.value",
		"vehicle.make", "vehicle.model", "vehicle.year", "vehicle.vin",
	}
	if !slices.Equal(empty, want) {
		t.Fatalf("empty form: got %v, want %v", empty, want)
	}

	form := map[string]any{
		"applicant":  map[string]any{"position": "Директор", "has_collateral": false},
		"collateral": map[string]any{"type": "vehicle", "description": "Седан", "value": 2_500_000},
		"vehicle":    map[string]any{"make": "Toyota", "model": "Camry", "year": 2024, "vin": "   "},
	}
	got := c.MissingFormFields(auto, form)
	if !slices.Equal(got, []string{"vehicle.vin"}) {
		t.Fatalf("got %v, want only the blank VIN (false and numbers count as filled)", got)
	}
}

// Базовые ставки и списки документов продуктов продублированы в конфигурации воркера скоринга
// (worker/scoring.yaml). Тест не даёт им разойтись с каталогом: иначе ставка предложения и ставка
// решения скоринга отличались бы, а воркер запрашивал бы не те документы.
func TestWorkerConfigMatchesTheCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../worker/scoring.yaml")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("worker/scoring.yaml is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		BaseDocuments []string `yaml:"base_documents"`
		Products      map[string]struct {
			BaseRate  *float64 `yaml:"base_rate"`
			Documents []string `yaml:"documents"`
		} `yaml:"products"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	c := load(t)
	if got := len(cfg.Products); got != len(c.Products()) {
		t.Errorf("worker has %d products, catalog has %d", got, len(c.Products()))
	}
	for _, p := range c.Products() {
		w, ok := cfg.Products[p.ID]
		if !ok {
			t.Errorf("%s: not configured in worker/scoring.yaml", p.ID)
			continue
		}
		if w.BaseRate == nil || *w.BaseRate != p.BaseRate {
			t.Errorf("%s: worker base_rate %v, catalog %v", p.ID, w.BaseRate, p.BaseRate)
		}
		if !slices.Equal(slices.Sorted(slices.Values(w.Documents)), slices.Sorted(slices.Values(p.Documents))) {
			t.Errorf("%s: worker documents %v, catalog %v", p.ID, w.Documents, p.Documents)
		}
	}
	if !slices.Equal(cfg.BaseDocuments, c.RequiredDocuments(Product{})) {
		t.Errorf("base documents: worker %v, catalog %v", cfg.BaseDocuments, c.RequiredDocuments(Product{}))
	}
}
