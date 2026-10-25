package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rudast/alpha-hack-2026/internal/catalog"
	"github.com/rudast/alpha-hack-2026/internal/domain"
)

// fakeOffers повторяет контракт настоящего хранилища: у компании не больше одного ACTIVE,
// чужое предложение неотличимо от несуществующего.
type fakeOffers struct {
	items map[string]domain.Offer
	// parallel — предложение, которое «успевает сохранить» параллельный запрос перед нашим Create.
	parallel *domain.Offer
}

func newFakeOffers() *fakeOffers { return &fakeOffers{items: map[string]domain.Offer{}} }

func (f *fakeOffers) Active(_ context.Context, inn string) (domain.Offer, error) {
	for _, o := range f.items {
		if o.CompanyINN == inn && o.Status == domain.OfferActive {
			return o, nil
		}
	}
	return domain.Offer{}, domain.ErrNotFound
}

func (f *fakeOffers) GetForCompany(_ context.Context, id, inn string) (domain.Offer, error) {
	o, ok := f.items[id]
	if !ok || o.CompanyINN != inn {
		return domain.Offer{}, domain.ErrNotFound
	}
	return o, nil
}

func (f *fakeOffers) Create(ctx context.Context, o domain.Offer) (domain.Offer, bool, error) {
	if f.parallel != nil {
		f.items[f.parallel.ID] = *f.parallel
		f.parallel = nil
	}
	if _, err := f.Active(ctx, o.CompanyINN); err == nil {
		return domain.Offer{}, false, nil
	}
	o.ID = fmt.Sprintf("offer-%d", len(f.items)+1)
	o.Status = domain.OfferActive
	f.items[o.ID] = o
	return o, true, nil
}

func (f *fakeOffers) Close(_ context.Context, id, status string) error {
	o := f.items[id]
	if o.Status == domain.OfferActive {
		o.Status = status
		f.items[id] = o
	}
	return nil
}

// put кладёт готовое предложение (для тестов заявок по предложению).
func (f *fakeOffers) put(o domain.Offer) domain.Offer {
	if o.ID == "" {
		o.ID = fmt.Sprintf("offer-%d", len(f.items)+1)
	}
	f.items[o.ID] = o
	return o
}

func i64p(n int64) *int64 { return &n }

// company — компания с данными «эталонного клиента»: капитал 5 млн, долг 3 млн, 4 года, без истории БКИ.
func company(mutate ...func(*domain.Company)) domain.Company {
	registered := testNow.AddDate(-4, 0, 0)
	fetched := testNow.Add(-time.Hour)
	ratio := 0.6
	c := domain.Company{
		INN: "7700000001", OrgType: "OOO", RegistrationDate: &registered,
		TotalDebt: i64p(3_000_000), Equity: i64p(5_000_000), DebtToEquity: &ratio, FetchedAt: &fetched,
		Registry: json.RawMessage(`{"status":"ACTIVE"}`), CreditHistory: json.RawMessage(`{}`),
	}
	for _, m := range mutate {
		m(&c)
	}
	return c
}

func withHistory(c *domain.Company) {
	c.CreditHistory = json.RawMessage(`{"current_overdue_days":0,"history_months":36,"inquiries_90d":1}`)
}

func newOffers(t *testing.T, c domain.Company) (*OfferService, *fakeOffers) {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeOffers()
	s := NewOfferService(repo, &fakeCompanies{c: c}, cat)
	s.now = func() time.Time { return testNow }
	return s, repo
}

func TestOfferCalculate(t *testing.T) {
	cases := []struct {
		name    string
		company domain.Company
		product string
		amount  int64
		reason  string
	}{
		// (2 × 5 млн − 3 млн) × 0,5 = 3,5 млн; среди продуктов с таким лимитом самая низкая ставка у оборотного
		{"reference client with credit history", company(withHistory), "working_capital", 3_500_000, ""},
		{"no credit history caps the limit", company(), "working_capital", 1_000_000, ""},
		{"young business: only products without the age requirement, with their cap", company(withHistory, func(c *domain.Company) {
			r := testNow.AddDate(0, -5, 0)
			c.RegistrationDate = &r
		}), "express", 2_000_000, ""},
		{"IP does not get the OOO loan", company(withHistory, func(c *domain.Company) { c.OrgType = "IP" }), "working_capital", 3_500_000, ""},
		{"no financial data", company(func(c *domain.Company) { c.TotalDebt, c.Equity = nil, nil }), "", 0, domain.OfferReasonNoData},
		{"equity is not positive", company(func(c *domain.Company) { c.Equity = i64p(-100_000) }), "", 0, domain.OfferReasonNoEquity},
		{"current overdue", company(func(c *domain.Company) {
			c.CreditHistory = json.RawMessage(`{"current_overdue_days":12}`)
		}), "", 0, domain.OfferReasonOverdue},
		{"registry red flags", company(func(c *domain.Company) {
			c.Registry = json.RawMessage(`{"red_flags":["liquidation"]}`)
		}), "", 0, domain.OfferReasonRegistryFlags},
		// долг 7 млн при капитале 2 млн: запас до D/E = 2 отрицательный
		{"high debt load", company(func(c *domain.Company) { c.TotalDebt, c.Equity = i64p(7_000_000), i64p(2_000_000) }), "", 0, domain.OfferReasonHighDebtLoad},
		// запас (2 × 1 млн − 1,5 млн) × 0,5 = 250 тыс. — меньше минимума 300 тыс.
		{"limit below the minimum", company(withHistory, func(c *domain.Company) { c.TotalDebt, c.Equity = i64p(1_500_000), i64p(1_000_000) }), "", 0, domain.OfferReasonHighDebtLoad},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newOffers(t, tc.company)
			o, reason := s.calculate(tc.company, testNow)
			if reason != tc.reason {
				t.Fatalf("reason %q, want %q", reason, tc.reason)
			}
			if tc.reason != "" {
				return
			}
			if o.ProductID != tc.product || o.MaxAmount != tc.amount {
				t.Fatalf("got %s %d, want %s %d", o.ProductID, o.MaxAmount, tc.product, tc.amount)
			}
		})
	}
}

func TestOfferCalculate_TermRateAndBasis(t *testing.T) {
	c := company(withHistory)
	s, _ := newOffers(t, c)
	o, _ := s.calculate(c, testNow)

	if o.TermMonths != 12 || o.RateFrom != 17.5 { // base_rate 19 − 1,5
		t.Fatalf("term %d, rate %v; want 12 and 17.5", o.TermMonths, o.RateFrom)
	}
	if !o.ValidUntil.Equal(testNow.AddDate(0, 0, 30)) {
		t.Fatalf("valid until %v", o.ValidUntil)
	}
	b := o.Basis
	if b.TotalDebt != 3_000_000 || b.Equity != 5_000_000 || !b.BKIChecked || b.BusinessAgeMonths != 48 || !sameTime(b.FinancialsFetchedAt, c.FetchedAt) {
		t.Fatalf("basis %+v", b)
	}
}

func TestOfferCurrent(t *testing.T) {
	ctx := context.Background()

	t.Run("calculated once and then reused", func(t *testing.T) {
		s, repo := newOffers(t, company(withHistory))
		first, _, err := s.Current(ctx, "7700000001")
		if err != nil || first == nil {
			t.Fatalf("got %v, %v", first, err)
		}
		second, _, _ := s.Current(ctx, "7700000001")
		if second.ID != first.ID || len(repo.items) != 1 {
			t.Fatalf("the active offer must be reused, have %d offers", len(repo.items))
		}
	})

	t.Run("expired offer is closed and a new one calculated", func(t *testing.T) {
		s, repo := newOffers(t, company(withHistory))
		first, _, _ := s.Current(ctx, "7700000001")
		s.now = func() time.Time { return testNow.AddDate(0, 0, 31) }

		second, _, _ := s.Current(ctx, "7700000001")
		if second.ID == first.ID || repo.items[first.ID].Status != domain.OfferExpired {
			t.Fatalf("old offer status %s, new id %s", repo.items[first.ID].Status, second.ID)
		}
	})

	t.Run("revoked when company data changed after the calculation", func(t *testing.T) {
		c := company(withHistory)
		s, repo := newOffers(t, c)
		first, _, _ := s.Current(ctx, "7700000001")

		refreshed := testNow
		c.FetchedAt, c.TotalDebt = &refreshed, i64p(9_500_000) // запас (10 − 9,5) × 0,5 = 250 тыс. — ниже минимума
		s.companies = &fakeCompanies{c: c}

		second, reason, _ := s.Current(ctx, "7700000001")
		if repo.items[first.ID].Status != domain.OfferRevoked || second != nil || reason != domain.OfferReasonHighDebtLoad {
			t.Fatalf("old status %s, new %v, reason %q", repo.items[first.ID].Status, second, reason)
		}
	})

	t.Run("no offer: reason, nothing stored", func(t *testing.T) {
		s, repo := newOffers(t, company(func(c *domain.Company) { c.TotalDebt, c.Equity = nil, nil }))
		o, reason, err := s.Current(ctx, "7700000001")
		if o != nil || reason != domain.OfferReasonNoData || err != nil || len(repo.items) != 0 {
			t.Fatalf("got %v, %q, %v, stored %d", o, reason, err, len(repo.items))
		}
	})

	t.Run("a parallel request already stored an offer", func(t *testing.T) {
		s, repo := newOffers(t, company(withHistory))
		// Create упирается в предложение, сохранённое параллельным запросом; сервис перечитывает действующее
		repo.parallel = &domain.Offer{ID: "offer-parallel", CompanyINN: "7700000001", Status: domain.OfferActive, ValidUntil: testNow.AddDate(0, 0, 30)}

		o, _, err := s.Current(ctx, "7700000001")
		if err != nil || o == nil || o.ID != "offer-parallel" {
			t.Fatalf("got %v, %v; want the offer stored by the parallel request", o, err)
		}
	})
}

func TestBusinessAgeMonths(t *testing.T) {
	at := func(y int, m time.Month, d int) *time.Time {
		v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		return &v
	}
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		registered *time.Time
		want       int
	}{
		{at(2026, 1, 22), 9},
		{at(2026, 1, 23), 8}, // девятый месяц ещё не исполнился
		{at(2016, 3, 14), 127},
		{nil, 0},
		{at(2027, 1, 1), 0},
	} {
		if got := businessAgeMonths(tc.registered, now); got != tc.want {
			t.Errorf("registered %v: got %d, want %d", tc.registered, got, tc.want)
		}
	}
}
