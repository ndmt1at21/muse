package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/muse/gamekit/gkerr"
	"github.com/muse/gamekit/types"
)

type gameRow struct {
	ID              string    `db:"id"`
	TenantID        string    `db:"tenant_id"`
	MerchantID      string    `db:"merchant_id"`
	CampaignID      string    `db:"campaign_id"`
	Name            string    `db:"name"`
	Type            string    `db:"type"`
	SeedGenerator   string    `db:"seed_generator"`
	RewardHandler   string    `db:"reward_handler"`
	Validator       string    `db:"validator"`
	Status          string    `db:"status"`
	Rules           []byte    `db:"rules"`
	HandlerConfig   []byte    `db:"handler_config"`
	ValidatorConfig []byte    `db:"validator_config"`
	WalletScope     string    `db:"wallet_scope"`
	Milestones      []byte    `db:"milestones"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

func (r gameRow) toDomain() (*types.Game, error) {
	g := &types.Game{
		ID:              r.ID,
		Scope:           types.Scope{TenantID: r.TenantID, MerchantID: r.MerchantID},
		CampaignID:      r.CampaignID,
		Name:            r.Name,
		Type:            r.Type,
		SeedGenerator:   r.SeedGenerator,
		RewardHandler:   r.RewardHandler,
		Validator:       r.Validator,
		Status:          types.GameStatus(r.Status),
		HandlerConfig:   json.RawMessage(r.HandlerConfig),
		ValidatorConfig: json.RawMessage(r.ValidatorConfig),
		WalletScope:     r.WalletScope,
		Milestones:      json.RawMessage(r.Milestones),
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
	if len(r.Rules) > 0 {
		if err := json.Unmarshal(r.Rules, &g.Rules); err != nil {
			return nil, gkerr.New(gkerr.ReasonInternal, "decode game rules").Wrap(err)
		}
	}
	return g, nil
}

// GetGame implements ports.GameStore.
func (db *DB) GetGame(ctx context.Context, scope types.Scope, gameID string) (*types.Game, error) {
	var row gameRow
	err := db.getContext(ctx, &row,
		`SELECT id, tenant_id, merchant_id, campaign_id, name, type, seed_generator,
		        reward_handler, validator, status, rules, handler_config, validator_config, wallet_scope, milestones, created_at, updated_at
		   FROM games WHERE id = ? AND tenant_id = ? AND merchant_id = ?`,
		gameID, scope.TenantID, scope.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, gkerr.New(gkerr.ReasonNotFound, "game not found").WithMeta("game_id", gameID)
	}
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "load game").Wrap(err)
	}
	return row.toDomain()
}

// CreateGame inserts a game (admin seed path). It fills defaults and timestamps.
func (db *DB) CreateGame(ctx context.Context, g *types.Game) (*types.Game, error) {
	now := time.Now().UTC()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	if g.SeedGenerator == "" {
		g.SeedGenerator = "none"
	}
	if g.Validator == "" {
		g.Validator = "basic"
	}
	if g.Status == "" {
		g.Status = types.StatusDraft
	}
	rules, _ := json.Marshal(g.Rules)
	hc := nonEmptyJSON(g.HandlerConfig)
	vc := nonEmptyJSON(g.ValidatorConfig)
	ms := nonEmptyJSON(g.Milestones)

	_, err := db.execContext(ctx,
		`INSERT INTO games (id, tenant_id, merchant_id, campaign_id, name, type, seed_generator,
		                    reward_handler, validator, status, rules, handler_config, validator_config, wallet_scope, milestones, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.Scope.TenantID, g.Scope.MerchantID, g.CampaignID, g.Name, g.Type, g.SeedGenerator,
		g.RewardHandler, g.Validator, string(g.Status), string(rules), string(hc), string(vc), g.WalletScope, string(ms), g.CreatedAt, g.UpdatedAt)
	if err != nil {
		return nil, gkerr.New(gkerr.ReasonInternal, "insert game").Wrap(err)
	}
	return g, nil
}

// nonEmptyJSON returns the raw JSON or "{}" when empty/null so NOT NULL JSON
// columns (MySQL) accept it.
func nonEmptyJSON(b json.RawMessage) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{}`)
	}
	return b
}
