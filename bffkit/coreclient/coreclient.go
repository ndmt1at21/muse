// Package coreclient is the BFFs' typed gRPC client to Core. It owns the
// connection and propagates the request trace id to Core as x-trace-id
// metadata, so one trace spans BFF → Core.
package coreclient

import (
	"context"

	gamev1 "github.com/muse/pkg/gen/game/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client bundles the Core service clients over one connection.
type Client struct {
	conn        *grpc.ClientConn
	Engine      gamev1.EngineServiceClient
	GameConfig  gamev1.GameConfigServiceClient
	Reward      gamev1.RewardServiceClient
	Fulfillment gamev1.FulfillmentServiceClient
	Tenant      gamev1.TenantServiceClient
	Merchant    gamev1.MerchantServiceClient
	Identity    gamev1.IdentityServiceClient
	Player      gamev1.PlayerServiceClient
	Campaign    gamev1.CampaignServiceClient
	Quest       gamev1.QuestServiceClient
	Leaderboard gamev1.LeaderboardServiceClient
	Wallet      gamev1.WalletServiceClient
	Integration gamev1.IntegrationServiceClient
}

// Dial connects to Core at addr (host:port). Insecure transport is fine on the
// internal network; mTLS is a deployment concern.
func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:        conn,
		Engine:      gamev1.NewEngineServiceClient(conn),
		GameConfig:  gamev1.NewGameConfigServiceClient(conn),
		Reward:      gamev1.NewRewardServiceClient(conn),
		Fulfillment: gamev1.NewFulfillmentServiceClient(conn),
		Tenant:      gamev1.NewTenantServiceClient(conn),
		Merchant:    gamev1.NewMerchantServiceClient(conn),
		Identity:    gamev1.NewIdentityServiceClient(conn),
		Player:      gamev1.NewPlayerServiceClient(conn),
		Campaign:    gamev1.NewCampaignServiceClient(conn),
		Quest:       gamev1.NewQuestServiceClient(conn),
		Leaderboard: gamev1.NewLeaderboardServiceClient(conn),
		Wallet:      gamev1.NewWalletServiceClient(conn),
		Integration: gamev1.NewIntegrationServiceClient(conn),
	}, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

// WithTrace attaches the trace id as outgoing x-trace-id metadata.
func WithTrace(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "x-trace-id", traceID)
}

// Scope builds a proto Scope from tenant/merchant ids.
func Scope(tenantID, merchantID string) *gamev1.Scope {
	return &gamev1.Scope{TenantId: tenantID, MerchantId: merchantID}
}
