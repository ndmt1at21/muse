package grpcsvc

import (
	"github.com/muse/gamekit/types"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Proto enum <-> domain token converters. The domain and persistence layers
// keep the historical lowercase token (e.g. "active", "magic_link"); see
// pkg/enumx. The zero/UNSPECIFIED enum value maps to "" and vice versa.

func pbGameStatus(s types.GameStatus) gamev1.GameStatus {
	return enumx.FromToken[gamev1.GameStatus](string(s), "GAME_STATUS_", gamev1.GameStatus_value)
}
func domGameStatus(e gamev1.GameStatus) types.GameStatus {
	return types.GameStatus(enumx.Token(e, "GAME_STATUS_"))
}

func pbWalletScope(s string) gamev1.WalletScope {
	return enumx.FromToken[gamev1.WalletScope](s, "WALLET_SCOPE_", gamev1.WalletScope_value)
}
func domWalletScope(e gamev1.WalletScope) string {
	return enumx.Token(e, "WALLET_SCOPE_")
}

func pbRewardStatus(s types.RewardStatus) gamev1.RewardStatus {
	return enumx.FromToken[gamev1.RewardStatus](string(s), "REWARD_STATUS_", gamev1.RewardStatus_value)
}

func pbTaskStatus(s types.TaskStatus) gamev1.TaskStatus {
	return enumx.FromToken[gamev1.TaskStatus](string(s), "TASK_STATUS_", gamev1.TaskStatus_value)
}
func domTaskStatus(e gamev1.TaskStatus) types.TaskStatus {
	return types.TaskStatus(enumx.Token(e, "TASK_STATUS_"))
}

func pbContactType(s types.ContactType) gamev1.ContactType {
	return enumx.FromToken[gamev1.ContactType](string(s), "CONTACT_TYPE_", gamev1.ContactType_value)
}
func domContactType(e gamev1.ContactType) types.ContactType {
	return types.ContactType(enumx.Token(e, "CONTACT_TYPE_"))
}

func pbCampaignStatus(s types.CampaignStatus) gamev1.CampaignStatus {
	return enumx.FromToken[gamev1.CampaignStatus](string(s), "CAMPAIGN_STATUS_", gamev1.CampaignStatus_value)
}
func domCampaignStatus(e gamev1.CampaignStatus) types.CampaignStatus {
	return types.CampaignStatus(enumx.Token(e, "CAMPAIGN_STATUS_"))
}

func pbQuestType(s string) gamev1.QuestType {
	return enumx.FromToken[gamev1.QuestType](s, "QUEST_TYPE_", gamev1.QuestType_value)
}
func domQuestType(e gamev1.QuestType) string {
	return enumx.Token(e, "QUEST_TYPE_")
}

func pbQuestStatus(s string) gamev1.QuestStatus {
	return enumx.FromToken[gamev1.QuestStatus](s, "QUEST_STATUS_", gamev1.QuestStatus_value)
}
func domQuestStatus(e gamev1.QuestStatus) string {
	return enumx.Token(e, "QUEST_STATUS_")
}

func pbQuestRewardType(s string) gamev1.QuestRewardType {
	return enumx.FromToken[gamev1.QuestRewardType](s, "QUEST_REWARD_TYPE_", gamev1.QuestRewardType_value)
}
func domQuestRewardType(e gamev1.QuestRewardType) string {
	return enumx.Token(e, "QUEST_REWARD_TYPE_")
}

func pbLeaderboardMetric(s string) gamev1.LeaderboardMetric {
	return enumx.FromToken[gamev1.LeaderboardMetric](s, "LEADERBOARD_METRIC_", gamev1.LeaderboardMetric_value)
}
func domLeaderboardMetric(e gamev1.LeaderboardMetric) string {
	return enumx.Token(e, "LEADERBOARD_METRIC_")
}

func pbLeaderboardStatus(s string) gamev1.LeaderboardStatus {
	return enumx.FromToken[gamev1.LeaderboardStatus](s, "LEADERBOARD_STATUS_", gamev1.LeaderboardStatus_value)
}
func domLeaderboardStatus(e gamev1.LeaderboardStatus) string {
	return enumx.Token(e, "LEADERBOARD_STATUS_")
}

func pbEntryState(s string) gamev1.EntryState {
	return enumx.FromToken[gamev1.EntryState](s, "ENTRY_STATE_", gamev1.EntryState_value)
}

func pbLedgerReason(s string) gamev1.LedgerReason {
	return enumx.FromToken[gamev1.LedgerReason](s, "LEDGER_REASON_", gamev1.LedgerReason_value)
}

func pbMilestoneStatus(s string) gamev1.MilestoneStatus {
	return enumx.FromToken[gamev1.MilestoneStatus](s, "MILESTONE_STATUS_", gamev1.MilestoneStatus_value)
}

func pbMilestoneMode(s string) gamev1.MilestoneMode {
	return enumx.FromToken[gamev1.MilestoneMode](s, "MILESTONE_MODE_", gamev1.MilestoneMode_value)
}

func pbIntegrationType(s string) gamev1.IntegrationType {
	return enumx.FromToken[gamev1.IntegrationType](s, "INTEGRATION_TYPE_", gamev1.IntegrationType_value)
}
func domIntegrationType(e gamev1.IntegrationType) string {
	return enumx.Token(e, "INTEGRATION_TYPE_")
}

func pbIntegrationStatus(s string) gamev1.IntegrationStatus {
	return enumx.FromToken[gamev1.IntegrationStatus](s, "INTEGRATION_STATUS_", gamev1.IntegrationStatus_value)
}
func domIntegrationStatus(e gamev1.IntegrationStatus) string {
	return enumx.Token(e, "INTEGRATION_STATUS_")
}
