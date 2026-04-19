package proxy

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
)

type FailoverHandler struct {
	cfg          *config.Config
	providerRepo *provider.Repository
	modelRepo    *model.Repository
	dbPool       *pgxpool.Pool
}

func NewFailoverHandler(
	cfg *config.Config,
	providerRepo *provider.Repository,
	modelRepo *model.Repository,
	dbPool *pgxpool.Pool,
) *FailoverHandler {
	return &FailoverHandler{
		cfg:          cfg,
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
		dbPool:       dbPool,
	}
}

type FailoverGroup struct {
	ID            string   `json:"id"`
	DisplayModel  string   `json:"display_model"`
	PriorityOrder []string `json:"priority_order"`
}

func (f *FailoverHandler) GetProviderForModel(ctx context.Context, modelID string) (*provider.Provider, error) {
	group, err := f.getFailoverGroup(ctx, modelID)
	if err == nil && group != nil {
		for _, id := range group.PriorityOrder {
			providerUUID, _ := uuid.Parse(id)
			prov, provErr := f.providerRepo.Get(ctx, providerUUID)
			if provErr == nil && prov.Enabled {
				if f.isProviderHealthy(ctx, prov) {
					return prov, nil
				}
			}
		}
	}

	models, err := f.modelRepo.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query models: %w", err)
	}

	for _, m := range models {
		if m.ModelID == modelID && m.Enabled {
			prov, err := f.providerRepo.Get(ctx, m.ProviderID)
			if err != nil {
				continue
			}
			if prov.Enabled && f.isProviderHealthy(ctx, prov) {
				return prov, nil
			}
		}
	}

	return nil, fmt.Errorf("no available provider for model %s", modelID)
}

func (f *FailoverHandler) getFailoverGroup(ctx context.Context, modelID string) (*FailoverGroup, error) {
	query := `SELECT id, display_model, priority_order FROM model_failover_groups WHERE display_model = $1`

	var id, displayModel string
	var priorityOrder []string
	err := f.dbPool.QueryRow(ctx, query, modelID).Scan(&id, &displayModel, &priorityOrder)
	if err != nil {
		return nil, err
	}

	return &FailoverGroup{
		ID:            id,
		DisplayModel:  displayModel,
		PriorityOrder: priorityOrder,
	}, nil
}

func (f *FailoverHandler) isProviderHealthy(ctx context.Context, prov *provider.Provider) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", prov.BaseURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	_ = resp

	return true
}
