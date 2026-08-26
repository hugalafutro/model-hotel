package proxy

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// The model-repository mocks the /v1/models tests drive the handler with.

// mockModelRepo is a test mock for model.Repository
type mockModelRepo struct {
	listEnabledErr    error
	listEnabledResult []*model.Model
	getResult         *model.Model
	getErr            error

	// setEnabledMu guards setEnabledCalls: noteModelGone disables out of band
	// on its own goroutine, so tests read this while the handler may write it.
	setEnabledMu    sync.Mutex
	setEnabledCalls []setEnabledCall
	setEnabledErr   error
	// setEnabledGate, when non-nil, blocks the FIRST SetEnabled call until it is
	// closed. It exists so a test can hold the detached disable write open and
	// interleave another event against it deterministically. Only the first
	// call: a test that holds the disable open still needs any compensating
	// re-enable that follows it to run to completion.
	setEnabledGate chan struct{}
	gateOnce       sync.Once
	// reEnableErr, when non-nil, fails only writes that turn a model back ON.
	// It exists to exercise a rollback that cannot be written, which needs the
	// disable to have succeeded first.
	reEnableErr error
	// revertSuperseded makes RevertAutoRetire report that it changed nothing,
	// standing in for the row having moved on — an operator disabling the model
	// by hand while the retirement was committing.
	revertSuperseded bool
	// afterConfirm, when non-nil, runs immediately after the confirm callback
	// and before the commit is recorded. It is the seam for the one interleaving
	// staging cannot prevent: a success arriving once the write is already
	// committing.
	afterConfirm func()
	// setEnabledEntered is closed by the first SetEnabled call once it has been
	// entered but before it blocks, so a test knows the write is genuinely in
	// flight rather than racing the goroutine's scheduling.
	setEnabledEntered chan struct{}
	enteredOnce       sync.Once
}

// setEnabledCall records one SetEnabled invocation for assertions.
type setEnabledCall struct {
	id      uuid.UUID
	enabled bool
	// budget is how much of its deadline the call's context had left on entry,
	// sampled before any gating so the mock's own blocking does not skew it. It
	// is what distinguishes a fresh per-write deadline from one inherited half
	// spent by the write before it.
	budget time.Duration
	// committed distinguishes a write that landed from one that was staged and
	// then abandoned. Only committed writes are observable to anything else, so
	// tests about what the rest of the system can see must assert on these.
	committed bool
}

// record appends a call under the lock.
func (m *mockModelRepo) record(c setEnabledCall) {
	m.setEnabledMu.Lock()
	defer m.setEnabledMu.Unlock()
	m.setEnabledCalls = append(m.setEnabledCalls, c)
}

// ctxBudget reports the remaining deadline, or zero when there is none.
func ctxBudget(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline)
	}
	return 0
}

// AutoRetireIfConfirmed mirrors the real repository's staging behaviour: the
// write is held open (the gate stands in for a slow UPDATE), confirm decides
// whether it commits, and an abandoned write is never recorded as committed.
func (m *mockModelRepo) AutoRetireIfConfirmed(ctx context.Context, id uuid.UUID, confirm func() bool) (bool, error) {
	const enabled = false
	budget := ctxBudget(ctx)
	if m.setEnabledEntered != nil {
		m.enteredOnce.Do(func() { close(m.setEnabledEntered) })
	}
	if m.setEnabledGate != nil {
		first := false
		m.gateOnce.Do(func() { first = true })
		if first {
			<-m.setEnabledGate
		}
	}
	if m.setEnabledErr != nil {
		// The real write fails before confirm is ever consulted.
		m.record(setEnabledCall{id: id, enabled: enabled, budget: budget})
		return false, m.setEnabledErr
	}
	ok := confirm()
	if m.afterConfirm != nil {
		// Stands in for a success landing between confirm and commit, the one
		// window staging cannot close.
		m.afterConfirm()
	}
	if !ok {
		m.record(setEnabledCall{id: id, enabled: enabled, budget: budget})
		return false, nil
	}
	m.record(setEnabledCall{id: id, enabled: enabled, budget: budget, committed: true})
	return true, nil
}

func (m *mockModelRepo) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*model.Model, error) {
	budget := ctxBudget(ctx)
	if m.setEnabledEntered != nil {
		m.enteredOnce.Do(func() { close(m.setEnabledEntered) })
	}
	if m.setEnabledGate != nil {
		first := false
		m.gateOnce.Do(func() { first = true })
		if first {
			<-m.setEnabledGate
		}
	}
	err := m.setEnabledErr
	if enabled && m.reEnableErr != nil {
		err = m.reEnableErr
	}
	m.record(setEnabledCall{id: id, enabled: enabled, budget: budget, committed: err == nil})
	if err != nil {
		return nil, err
	}
	return &model.Model{ID: id, Enabled: enabled}, nil
}

// RevertAutoRetire records the undo as an enable, and honours reEnableErr and
// revertSuperseded so tests can drive both ways it fails to restore the model.
func (m *mockModelRepo) RevertAutoRetire(ctx context.Context, id uuid.UUID) (bool, error) {
	budget := ctxBudget(ctx)
	if m.reEnableErr != nil {
		m.record(setEnabledCall{id: id, enabled: true, budget: budget})
		return false, m.reEnableErr
	}
	if m.revertSuperseded {
		m.record(setEnabledCall{id: id, enabled: true, budget: budget})
		return false, nil
	}
	m.record(setEnabledCall{id: id, enabled: true, budget: budget, committed: true})
	return true, nil
}

// disableCalls returns a copy of every recorded attempt under the lock,
// committed or not.
func (m *mockModelRepo) disableCalls() []setEnabledCall {
	m.setEnabledMu.Lock()
	defer m.setEnabledMu.Unlock()
	return append([]setEnabledCall(nil), m.setEnabledCalls...)
}

// committedCalls returns only the writes that actually landed — the ones any
// other session could see.
func (m *mockModelRepo) committedCalls() []setEnabledCall {
	var out []setEnabledCall
	for _, c := range m.disableCalls() {
		if c.committed {
			out = append(out, c)
		}
	}
	return out
}

func (m *mockModelRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledResult != nil {
		return m.listEnabledResult, m.listEnabledErr
	}
	return nil, m.listEnabledErr
}

func (m *mockModelRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return m.getResult, m.getErr
}

func (m *mockModelRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *mockModelRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *mockModelRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	return nil, nil
}

// listModelsMockRepo implements ModelRepository for ListModels tests.
type listModelsMockRepo struct {
	listEnabledFunc func(ctx context.Context) ([]*model.Model, error)
}

func (m *listModelsMockRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledFunc != nil {
		return m.listEnabledFunc(ctx)
	}
	return []*model.Model{}, nil
}

func (m *listModelsMockRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *listModelsMockRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *listModelsMockRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return nil, nil
}

func (m *listModelsMockRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (m *listModelsMockRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	return nil, nil
}

// containsTestProviderPrefix checks if a model ID starts with a test provider prefix
func containsTestProviderPrefix(modelID string) bool {
	return strings.HasPrefix(modelID, "test-provider-")
}

func (m *coverageMockModelRepo) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*model.Model, error) {
	return &model.Model{ID: id, Enabled: enabled}, nil
}

func (m *coverageMockModelRepo) AutoRetireIfConfirmed(ctx context.Context, id uuid.UUID, confirm func() bool) (bool, error) {
	return confirm(), nil
}

func (m *coverageMockModelRepo) RevertAutoRetire(ctx context.Context, id uuid.UUID) (bool, error) {
	return true, nil
}

func (m *listModelsMockRepo) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*model.Model, error) {
	return &model.Model{ID: id, Enabled: enabled}, nil
}

func (m *listModelsMockRepo) AutoRetireIfConfirmed(ctx context.Context, id uuid.UUID, confirm func() bool) (bool, error) {
	return confirm(), nil
}

func (m *listModelsMockRepo) RevertAutoRetire(ctx context.Context, id uuid.UUID) (bool, error) {
	return true, nil
}
