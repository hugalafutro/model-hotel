package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// mockModelRows: implements pgx.Rows for testing scanModels
// ---------------------------------------------------------------------------

type mockModelRows struct {
	rows     [][]any
	index    int
	scanErr  error
	closeErr error
	closed   bool
}

func (m *mockModelRows) Close() {
	m.closed = true
}

func (m *mockModelRows) Err() error {
	return m.closeErr
}

func (m *mockModelRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (m *mockModelRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (m *mockModelRows) Next() bool {
	if m.index < len(m.rows) {
		m.index++
		return true
	}
	return false
}

func (m *mockModelRows) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	if m.index == 0 || m.index > len(m.rows) {
		return errors.New("scan called before Next or past end")
	}
	vals := m.rows[m.index-1]
	if len(vals) != len(dest) {
		return errors.New("column count mismatch")
	}
	for i, v := range vals {
		assignToDest(dest[i], v)
	}
	return nil
}

func (m *mockModelRows) Values() ([]any, error) {
	return nil, nil
}

func (m *mockModelRows) RawValues() [][]byte {
	return nil
}

func (m *mockModelRows) Conn() *pgx.Conn {
	return nil
}

func assignToDest(dest any, val any) {
	switch d := dest.(type) {
	case *uuid.UUID:
		if v, ok := val.(uuid.UUID); ok {
			*d = v
		}
	case *string:
		if v, ok := val.(string); ok {
			*d = v
		}
	case *int:
		switch v := val.(type) {
		case int:
			*d = v
		case *int:
			if v != nil {
				*d = *v
			}
		}
	case **int:
		switch v := val.(type) {
		case int:
			*d = &v
		case *int:
			*d = v
		}
	case *float64:
		switch v := val.(type) {
		case float64:
			*d = v
		case *float64:
			if v != nil {
				*d = *v
			}
		}
	case **float64:
		switch v := val.(type) {
		case float64:
			*d = &v
		case *float64:
			*d = v
		}
	case *bool:
		if v, ok := val.(bool); ok {
			*d = v
		}
	case *time.Time:
		if v, ok := val.(time.Time); ok {
			*d = v
		}
	}
}

// ---------------------------------------------------------------------------
// buildRow builds a single row of 22 values in scan order from a Model
// ---------------------------------------------------------------------------

func buildRow(t *testing.T, m *Model) []any {
	t.Helper()
	return []any{
		m.ID,                           // 0  - ID
		m.ProviderID,                   // 1  - ProviderID
		m.ModelID,                      // 2  - ModelID
		m.Name,                         // 3  - Name
		m.Description,                  // 4  - Description
		m.DisplayName,                  // 5  - DisplayName
		m.Capabilities,                 // 6  - Capabilities
		m.Params,                       // 7  - Params
		m.Modality,                     // 8  - Modality
		m.InputModalities,              // 9  - InputModalities
		m.OutputModalities,             // 10 - OutputModalities
		m.ContextLength,                // 11 - ContextLength (*int)
		m.MaxOutputTokens,              // 12 - MaxOutputTokens (*int)
		m.InputPricePerMillion,         // 13 - InputPricePerMillion (*float64)
		m.InputPricePerMillionCacheHit, // 14 - InputPricePerMillionCacheHit (*float64)
		m.OutputPricePerMillion,        // 15 - OutputPricePerMillion (*float64)
		m.OwnedBy,                      // 16 - OwnedBy
		m.Enabled,                      // 17 - Enabled
		m.DisabledManually,             // 18 - DisabledManually
		m.CreatedAt,                    // 19 - CreatedAt
		m.LastSeenAt,                   // 20 - LastSeenAt
		m.ProviderName,                 // 21 - ProviderName
		m.ProviderEnabled,              // 22 - ProviderEnabled
	}
}

// ---------------------------------------------------------------------------
// TestScanModels_SingleRow
// ---------------------------------------------------------------------------

func TestScanModels_SingleRow(t *testing.T) {
	id := uuid.New()
	providerID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)

	expected := &Model{
		ID:                           id,
		ProviderID:                   providerID,
		ModelID:                      "gpt-4",
		Name:                         "GPT-4",
		Description:                  "The latest GPT model",
		DisplayName:                  "GPT-4 Turbo",
		Capabilities:                 "{streaming,vision}",
		Params:                       "{temperature:0.7}",
		Modality:                     "text",
		InputModalities:              "[\"text\"]",
		OutputModalities:             "[\"text\"]",
		ContextLength:                intPtr(128000),
		MaxOutputTokens:              intPtr(4096),
		InputPricePerMillion:         float64Ptr(10.0),
		InputPricePerMillionCacheHit: float64Ptr(5.0),
		OutputPricePerMillion:        float64Ptr(30.0),
		OwnedBy:                      "openai",
		Enabled:                      true,
		DisabledManually:             false,
		CreatedAt:                    now,
		LastSeenAt:                   now,
		ProviderName:                 "OpenAI",
		ProviderEnabled:              true,
	}

	rows := &mockModelRows{
		rows: [][]any{buildRow(t, expected)},
	}

	models, err := scanModels(rows)
	if err != nil {
		t.Fatalf("scanModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	got := models[0]
	assertModelEqual(t, expected, got)
}

// ---------------------------------------------------------------------------
// TestScanModels_MultipleRows
// ---------------------------------------------------------------------------

func TestScanModels_MultipleRows(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)

	models := []*Model{
		{
			ID:              uuid.New(),
			ProviderID:      uuid.New(),
			ModelID:         "gpt-4",
			Name:            "GPT-4",
			Enabled:         true,
			ProviderEnabled: true,
			CreatedAt:       now,
			LastSeenAt:      now,
			ProviderName:    "OpenAI",
		},
		{
			ID:              uuid.New(),
			ProviderID:      uuid.New(),
			ModelID:         "claude-3",
			Name:            "Claude 3",
			Enabled:         true,
			ProviderEnabled: true,
			CreatedAt:       now,
			LastSeenAt:      now,
			ProviderName:    "Anthropic",
		},
		{
			ID:              uuid.New(),
			ProviderID:      uuid.New(),
			ModelID:         "gemini-pro",
			Name:            "Gemini Pro",
			Enabled:         false,
			ProviderEnabled: true,
			CreatedAt:       now,
			LastSeenAt:      now,
			ProviderName:    "Google",
		},
	}

	var rowData [][]any
	for _, m := range models {
		rowData = append(rowData, buildRow(t, m))
	}

	rows := &mockModelRows{rows: rowData}
	result, err := scanModels(rows)
	if err != nil {
		t.Fatalf("scanModels returned error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result))
	}

	for i, expected := range models {
		assertModelEqual(t, expected, result[i])
	}
}

// ---------------------------------------------------------------------------
// TestScanModels_EmptyRows
// ---------------------------------------------------------------------------

func TestScanModels_EmptyRows(t *testing.T) {
	rows := &mockModelRows{rows: [][]any{}}
	models, err := scanModels(rows)
	if err != nil {
		t.Fatalf("scanModels returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// TestScanModels_ScanError
// ---------------------------------------------------------------------------

func TestScanModels_ScanError(t *testing.T) {
	expectedErr := errors.New("scan failure")
	now := time.Now().Truncate(time.Microsecond)

	m := &Model{
		ID:         uuid.New(),
		ProviderID: uuid.New(),
		ModelID:    "error-model",
		CreatedAt:  now,
		LastSeenAt: now,
	}

	rows := &mockModelRows{
		rows:    [][]any{buildRow(t, m)},
		scanErr: expectedErr,
	}

	// read the row (will call Next which returns true), then Scan fails
	models, err := scanModels(rows)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if models != nil {
		t.Errorf("expected nil models on error, got %d entries", len(models))
	}
}

// ---------------------------------------------------------------------------
// TestScanModels_NilableFields
// ---------------------------------------------------------------------------

func TestScanModels_NilableFields(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)

	m := &Model{
		ID:                           uuid.New(),
		ProviderID:                   uuid.New(),
		ModelID:                      "nil-fields-model",
		Name:                         "Nil Fields",
		Description:                  "Model with nil pointer fields",
		DisplayName:                  "",
		Capabilities:                 "{}",
		Params:                       "{}",
		Modality:                     "text",
		InputModalities:              "[\"text\"]",
		OutputModalities:             "[\"text\"]",
		ContextLength:                nil,
		MaxOutputTokens:              nil,
		InputPricePerMillion:         nil,
		InputPricePerMillionCacheHit: nil,
		OutputPricePerMillion:        nil,
		OwnedBy:                      "test",
		Enabled:                      true,
		DisabledManually:             false,
		CreatedAt:                    now,
		LastSeenAt:                   now,
		ProviderName:                 "TestProvider",
		ProviderEnabled:              true,
	}

	rows := &mockModelRows{
		rows: [][]any{buildRow(t, m)},
	}

	models, err := scanModels(rows)
	if err != nil {
		t.Fatalf("scanModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	got := models[0]
	assertModelEqual(t, m, got)

	if got.ContextLength != nil {
		t.Errorf("ContextLength should be nil, got %d", *got.ContextLength)
	}
	if got.MaxOutputTokens != nil {
		t.Errorf("MaxOutputTokens should be nil, got %d", *got.MaxOutputTokens)
	}
	if got.InputPricePerMillion != nil {
		t.Errorf("InputPricePerMillion should be nil, got %f", *got.InputPricePerMillion)
	}
	if got.InputPricePerMillionCacheHit != nil {
		t.Errorf("InputPricePerMillionCacheHit should be nil, got %f", *got.InputPricePerMillionCacheHit)
	}
	if got.OutputPricePerMillion != nil {
		t.Errorf("OutputPricePerMillion should be nil, got %f", *got.OutputPricePerMillion)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func intPtr(v int) *int {
	return &v
}

func float64Ptr(v float64) *float64 {
	return &v
}

func assertModelEqual(t *testing.T, expected, got *Model) {
	t.Helper()

	if expected.ID != got.ID {
		t.Errorf("ID: expected %v, got %v", expected.ID, got.ID)
	}
	if expected.ProviderID != got.ProviderID {
		t.Errorf("ProviderID: expected %v, got %v", expected.ProviderID, got.ProviderID)
	}
	if expected.ModelID != got.ModelID {
		t.Errorf("ModelID: expected %q, got %q", expected.ModelID, got.ModelID)
	}
	if expected.Name != got.Name {
		t.Errorf("Name: expected %q, got %q", expected.Name, got.Name)
	}
	if expected.Description != got.Description {
		t.Errorf("Description: expected %q, got %q", expected.Description, got.Description)
	}
	if expected.DisplayName != got.DisplayName {
		t.Errorf("DisplayName: expected %q, got %q", expected.DisplayName, got.DisplayName)
	}
	if expected.Capabilities != got.Capabilities {
		t.Errorf("Capabilities: expected %q, got %q", expected.Capabilities, got.Capabilities)
	}
	if expected.Params != got.Params {
		t.Errorf("Params: expected %q, got %q", expected.Params, got.Params)
	}
	if expected.Modality != got.Modality {
		t.Errorf("Modality: expected %q, got %q", expected.Modality, got.Modality)
	}
	if expected.InputModalities != got.InputModalities {
		t.Errorf("InputModalities: expected %q, got %q", expected.InputModalities, got.InputModalities)
	}
	if expected.OutputModalities != got.OutputModalities {
		t.Errorf("OutputModalities: expected %q, got %q", expected.OutputModalities, got.OutputModalities)
	}
	if expected.OwnedBy != got.OwnedBy {
		t.Errorf("OwnedBy: expected %q, got %q", expected.OwnedBy, got.OwnedBy)
	}
	if expected.Enabled != got.Enabled {
		t.Errorf("Enabled: expected %v, got %v", expected.Enabled, got.Enabled)
	}
	if expected.DisabledManually != got.DisabledManually {
		t.Errorf("DisabledManually: expected %v, got %v", expected.DisabledManually, got.DisabledManually)
	}
	if !expected.CreatedAt.Equal(got.CreatedAt) {
		t.Errorf("CreatedAt: expected %v, got %v", expected.CreatedAt, got.CreatedAt)
	}
	if !expected.LastSeenAt.Equal(got.LastSeenAt) {
		t.Errorf("LastSeenAt: expected %v, got %v", expected.LastSeenAt, got.LastSeenAt)
	}
	if expected.ProviderName != got.ProviderName {
		t.Errorf("ProviderName: expected %q, got %q", expected.ProviderName, got.ProviderName)
	}
	if expected.ProviderEnabled != got.ProviderEnabled {
		t.Errorf("ProviderEnabled: expected %v, got %v", expected.ProviderEnabled, got.ProviderEnabled)
	}

	// Nilable fields
	assertIntPtrEqual(t, "ContextLength", expected.ContextLength, got.ContextLength)
	assertIntPtrEqual(t, "MaxOutputTokens", expected.MaxOutputTokens, got.MaxOutputTokens)
	assertFloat64PtrEqual(t, "InputPricePerMillion", expected.InputPricePerMillion, got.InputPricePerMillion)
	assertFloat64PtrEqual(t, "InputPricePerMillionCacheHit", expected.InputPricePerMillionCacheHit, got.InputPricePerMillionCacheHit)
	assertFloat64PtrEqual(t, "OutputPricePerMillion", expected.OutputPricePerMillion, got.OutputPricePerMillion)
}

func assertIntPtrEqual(t *testing.T, name string, expected, got *int) {
	t.Helper()
	if expected == nil && got == nil {
		return
	}
	if expected == nil {
		t.Errorf("%s: expected nil, got %d", name, *got)
		return
	}
	if got == nil {
		t.Errorf("%s: expected %d, got nil", name, *expected)
		return
	}
	if *expected != *got {
		t.Errorf("%s: expected %d, got %d", name, *expected, *got)
	}
}

func assertFloat64PtrEqual(t *testing.T, name string, expected, got *float64) {
	t.Helper()
	if expected == nil && got == nil {
		return
	}
	if expected == nil {
		t.Errorf("%s: expected nil, got %f", name, *got)
		return
	}
	if got == nil {
		t.Errorf("%s: expected %f, got nil", name, *expected)
		return
	}
	if *expected != *got {
		t.Errorf("%s: expected %f, got %f", name, *expected, *got)
	}
}
