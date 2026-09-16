package virtualkey

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/budget"
)

func TestVirtualKeyBudgetSubject(t *testing.T) {
	t.Parallel()
	k := &VirtualKey{ID: uuid.New(), Name: "ci"}
	if s := k.BudgetSubject(); s != nil {
		t.Fatalf("a key without a budget has no subject, got %+v", s)
	}
	usd, period := 3.0, "daily"
	k.BudgetUSD, k.BudgetPeriod = &usd, &period
	s := k.BudgetSubject()
	if s == nil || s.Kind != budget.KindKey || s.ID != k.ID.String() || s.Name != "ci" || s.Budget.USD != 3 || s.Budget.Period != "daily" {
		t.Fatalf("subject = %+v", s)
	}
}
