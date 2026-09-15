package budget

import (
	"math"
	"testing"
	"time"
)

func TestValidate_PairAndBounds(t *testing.T) {
	usd := func(v float64) *float64 { return &v }
	period := func(p string) *string { return &p }
	cases := map[string]struct {
		usd    *float64
		period *string
		ok     bool
	}{
		"off":              {nil, nil, true},
		"month":            {usd(50), period(PeriodMonth), true},
		"usd without":      {usd(50), nil, false},
		"period without":   {nil, period(PeriodDay), false},
		"zero":             {usd(0), period(PeriodDay), false},
		"negative":         {usd(-1), period(PeriodWeek), false},
		"above ceiling":    {usd(MaxUSD + 1), period(PeriodMonth), false},
		"nan":              {usd(math.NaN()), period(PeriodMonth), false},
		"unknown period":   {usd(5), period("quarter"), false},
		"ceiling itself":   {usd(MaxUSD), period(PeriodMonth), true},
		"smallest budget":  {usd(0.01), period(PeriodDay), true},
		"week is a period": {usd(5), period(PeriodWeek), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(tc.usd, tc.period); (err == nil) != tc.ok {
				t.Fatalf("Validate(%v, %v) = %v, want ok=%v", tc.usd, tc.period, err, tc.ok)
			}
		})
	}
}

func TestFromAndColumns_RoundTrip(t *testing.T) {
	if From(nil, nil) != nil {
		t.Fatal("two NULLs must read as no budget")
	}
	v, p := 12.5, PeriodWeek
	b := From(&v, &p)
	if b == nil || b.USD != 12.5 || b.Period != PeriodWeek {
		t.Fatalf("From = %+v", b)
	}
	gotUSD, gotPeriod := b.Columns()
	if *gotUSD != 12.5 || *gotPeriod != PeriodWeek {
		t.Fatalf("Columns = %v %v", *gotUSD, *gotPeriod)
	}
	if u, per := (*Budget)(nil).Columns(); u != nil || per != nil {
		t.Fatal("a nil budget must write two NULLs")
	}
}

func TestPeriodBounds_CalendarUTC(t *testing.T) {
	// A Wednesday, 2026-09-16 13:45 UTC.
	now := time.Date(2026, 9, 16, 13, 45, 0, 0, time.UTC)
	cases := map[string][2]time.Time{
		PeriodDay:   {time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)},
		PeriodWeek:  {time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)},
		PeriodMonth: {time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
	}
	for period, want := range cases {
		if got := PeriodStart(period, now); !got.Equal(want[0]) {
			t.Errorf("%s start = %v, want %v", period, got, want[0])
		}
		if got := PeriodEnd(period, now); !got.Equal(want[1]) {
			t.Errorf("%s end = %v, want %v", period, got, want[1])
		}
	}
	// A Sunday belongs to the week that started the previous Monday, and a
	// non-UTC clock is read in UTC.
	sunday := time.Date(2026, 9, 20, 23, 30, 0, 0, time.FixedZone("east", 3*3600))
	if got := PeriodStart(PeriodWeek, sunday); !got.Equal(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("sunday week start = %v", got)
	}
	// Month boundaries carry across the year.
	dec := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	if got := PeriodEnd(PeriodMonth, dec); !got.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("december end = %v", got)
	}
}
