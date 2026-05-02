package api

import (
	"fmt"
	"strings"
)

// validateStringLength checks that a string value is within [min, max] after trimming.
func validateStringLength(field, value string, min, max int) error {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < min {
		return fmt.Errorf("%s must be at least %d characters", field, min)
	}
	if len(trimmed) > max {
		return fmt.Errorf("%s must be at most %d characters", field, max)
	}
	return nil
}

// validateStringPtrLength checks that a non-nil *string is within [min, max] after trimming.
// Returns nil if the pointer is nil.
func validateStringPtrLength(field string, value *string, min, max int) error {
	if value == nil {
		return nil
	}
	return validateStringLength(field, *value, min, max)
}

// validateIntRange checks that an int value is within [min, max].
func validateIntRange(field string, value, min, max int) error {
	if value < min {
		return fmt.Errorf("%s must be at least %d", field, min)
	}
	if value > max {
		return fmt.Errorf("%s must be at most %d", field, max)
	}
	return nil
}

// validateIntPtrRange checks that a non-nil *int is within [min, max].
// Returns nil if the pointer is nil.
func validateIntPtrRange(field string, value *int, min, max int) error {
	if value == nil {
		return nil
	}
	return validateIntRange(field, *value, min, max)
}

// validateFloatRange checks that a float64 value is within [min, max].
func validateFloatRange(field string, value, min, max float64) error {
	if value < min {
		return fmt.Errorf("%s must be at least %g", field, min)
	}
	if value > max {
		return fmt.Errorf("%s must be at most %g", field, max)
	}
	return nil
}

// validateFloatPtrRange checks that a non-nil *float64 is within [min, max].
// Returns nil if the pointer is nil.
func validateFloatPtrRange(field string, value *float64, min, max float64) error {
	if value == nil {
		return nil
	}
	return validateFloatRange(field, *value, min, max)
}

// validateMapSize checks that a map has at most max entries.
func validateMapSize(field string, m map[string]bool, max int) error {
	if len(m) > max {
		return fmt.Errorf("%s must have at most %d entries", field, max)
	}
	return nil
}

// trimString returns a trimmed copy of the string.
func trimString(s string) string {
	return strings.TrimSpace(s)
}
