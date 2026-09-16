package user

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/budget"
)

func TestUserBudgetSubject(t *testing.T) {
	t.Parallel()
	u := &User{ID: uuid.New(), Username: "ann"}
	if s := u.BudgetSubject(); s != nil {
		t.Fatalf("a user without a budget has no subject, got %+v", s)
	}
	usd, period := 12.5, "monthly"
	u.BudgetUSD, u.BudgetPeriod = &usd, &period
	s := u.BudgetSubject()
	if s == nil || s.Kind != budget.KindUser || s.ID != u.ID.String() || s.Name != "ann" || s.Budget.USD != 12.5 || s.Budget.Period != "monthly" {
		t.Fatalf("subject = %+v", s)
	}
}
