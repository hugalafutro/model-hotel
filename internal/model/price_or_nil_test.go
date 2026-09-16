package model

import (
	"math"
	"testing"
)

func TestPriceOrNil(t *testing.T) {
	t.Parallel()
	good := 1.5
	if got := PriceOrNil(&good); got == nil || *got != 1.5 {
		t.Fatalf("a priceable figure must pass through, got %v", got)
	}
	for name, v := range map[string]float64{"negative": -1, "nan": math.NaN(), "inf": math.Inf(1)} {
		v := v
		if got := PriceOrNil(&v); got != nil {
			t.Errorf("%s: want nil, got %v", name, *got)
		}
	}
	if got := PriceOrNil(nil); got != nil {
		t.Fatalf("nil in must be nil out, got %v", got)
	}
}
