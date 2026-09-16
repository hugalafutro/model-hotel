package provider

import "testing"

func TestValidateQuotaReservePercent(t *testing.T) {
	t.Parallel()
	if err := ValidateQuotaReservePercent(nil); err != nil {
		t.Fatalf("nil (unset) must pass, got %v", err)
	}
	for _, v := range []int{0, 10, 50, 90} {
		if err := ValidateQuotaReservePercent(&v); err != nil {
			t.Errorf("%d must pass, got %v", v, err)
		}
	}
	for _, v := range []int{-10, 5, 95, 100} {
		if err := ValidateQuotaReservePercent(&v); err == nil {
			t.Errorf("%d must be refused", v)
		}
	}
}

func TestReserveShare(t *testing.T) {
	t.Parallel()
	if got := (&Provider{QuotaReservePercent: 30}).ReserveShare(); got != 0.3 {
		t.Fatalf("ReserveShare = %v, want 0.3", got)
	}
	if got := (&Provider{}).ReserveShare(); got != 0 {
		t.Fatalf("ReserveShare = %v, want 0", got)
	}
}
