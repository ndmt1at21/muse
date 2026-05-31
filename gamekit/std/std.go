// Package std assembles a Registry pre-loaded with the SDK's built-in
// extension points. It lives in its own package (not the root gamekit package)
// so the leaf handler/seed/validator packages can import gamekit for the
// interface types without an import cycle.
package std

import (
	"github.com/muse/gamekit"
	"github.com/muse/gamekit/handlers"
	"github.com/muse/gamekit/seeds"
	"github.com/muse/gamekit/types"
	"github.com/muse/gamekit/validators"
	"github.com/muse/gamekit/verifiers"
)

// Registry returns a registry pre-loaded with the built-in extension points:
//
//	seed generators: none, drop_sequence
//	reward handlers: probability, score_to_tier, collect_items, lucky_item
//	validators:      basic, time_and_score_range, drop_plan
//	quest verifiers: daily_checkin, share_social, invite_friend, scan_qr,
//	                 view_page, answer_question, external_event
//
// Game shapes map to combinations of these:
//
//	spin_wheel / scratch_card → none + probability + basic
//	egg_catcher               → none + score_to_tier + time_and_score_range
//	gift_catcher              → drop_sequence + collect_items + drop_plan
//
// Consumers register additional shapes on top without touching the engine.
func Registry() *gamekit.Registry {
	r := gamekit.NewRegistry()

	r.RegisterSeed("none", seeds.None{})
	r.RegisterSeed("drop_sequence", seeds.DropSequence{})

	r.RegisterHandler("probability", handlers.Probability{})
	r.RegisterHandler("score_to_tier", handlers.ScoreToTier{})
	r.RegisterHandler("collect_items", handlers.CollectItems{})
	r.RegisterHandler("lucky_item", handlers.LuckyItem{})

	r.RegisterValidator("basic", validators.Basic{})
	r.RegisterValidator("time_and_score_range", validators.TimeAndScoreRange{})
	r.RegisterValidator("drop_plan", validators.DropPlan{})

	r.RegisterVerifier(types.QuestDailyCheckin, verifiers.DailyCheckin{})
	r.RegisterVerifier(types.QuestShareSocial, verifiers.ShareSocial{})
	r.RegisterVerifier(types.QuestInviteFriend, verifiers.InviteFriend{})
	r.RegisterVerifier(types.QuestScanQR, verifiers.ScanQR{})
	r.RegisterVerifier(types.QuestViewPage, verifiers.ViewPage{})
	r.RegisterVerifier(types.QuestAnswerQuestion, verifiers.AnswerQuestion{})
	r.RegisterVerifier(types.QuestExternalEvent, verifiers.ExternalEvent{})

	return r
}
