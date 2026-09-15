// Package budget caps what a virtual key or a user may spend, in dollars, per
// calendar period. Spend is the sum of request_logs.cost_usd, which is what the
// dashboard's spend views show, so a budget refuses at the number the operator
// sees. Rows with no price count as nothing: a budget cannot police what the
// gateway could not price, and the spend tile says how many such rows there are.
package budget

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Periods are the calendar windows a budget can run over, all UTC.
const (
	PeriodDay   = "day"
	PeriodWeek  = "week" // Monday to Sunday
	PeriodMonth = "month"
)

// MaxUSD bounds a budget the way rate limits are bounded: large enough for any
// real deployment, small enough that a typo cannot become an unlimited stand-in.
const MaxUSD = 10_000_000

// Budget is a spending cap over one calendar period.
type Budget struct {
	USD    float64
	Period string
}

// From pairs the two nullable columns into a Budget; nil when the budget is off.
func From(usd *float64, period *string) *Budget {
	if usd == nil || period == nil {
		return nil
	}
	return &Budget{USD: *usd, Period: *period}
}

// Columns splits a Budget back into its two nullable columns.
func (b *Budget) Columns() (usd *float64, period *string) {
	if b == nil {
		return nil, nil
	}
	return &b.USD, &b.Period
}

// Validate checks the two fields as a pair, the way the database constraint
// does, so the API refuses what the row would.
func Validate(usd *float64, period *string) error {
	if (usd == nil) != (period == nil) {
		return errors.New("budget_usd and budget_period go together: set both or neither")
	}
	if usd == nil {
		return nil
	}
	// NaN fails neither bound below (every comparison with it is false), so it
	// is named; an infinity fails one of them already. The JSON decoder and
	// the row's CHECK refuse NaN upstream and downstream, and this keeps the
	// contract whole without either.
	if math.IsNaN(*usd) {
		return errors.New("budget_usd must be a number")
	}
	if *usd <= 0 {
		return errors.New("budget_usd must be > 0 (use null for no budget)")
	}
	if *usd > MaxUSD {
		return fmt.Errorf("budget_usd must be <= %d", MaxUSD)
	}
	switch *period {
	case PeriodDay, PeriodWeek, PeriodMonth:
		return nil
	}
	return fmt.Errorf("budget_period must be one of day, week, month; got %q", *period)
}

// PeriodStart is the UTC instant the period holding now began.
func PeriodStart(period string, now time.Time) time.Time {
	now = now.UTC()
	y, m, d := now.Date()
	switch period {
	case PeriodWeek:
		// Monday-based: Sunday is 0 in Go's Weekday, so it counts as day 6.
		back := (int(now.Weekday()) + 6) % 7
		return time.Date(y, m, d-back, 0, 0, 0, 0, time.UTC)
	case PeriodMonth:
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
}

// PeriodEnd is the UTC instant the period holding now ends (exclusive).
func PeriodEnd(period string, now time.Time) time.Time {
	start := PeriodStart(period, now)
	switch period {
	case PeriodWeek:
		return start.AddDate(0, 0, 7)
	case PeriodMonth:
		return start.AddDate(0, 1, 0)
	default:
		return start.AddDate(0, 0, 1)
	}
}
