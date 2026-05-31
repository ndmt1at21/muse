package leaderboard

import (
	"testing"
	"time"

	"github.com/muse/gamekit/types"
)

func TestContribution(t *testing.T) {
	rewards := []types.Reward{{PrizeID: "p1"}, {PrizeID: ""}, {PrizeID: "p2"}}
	meta := map[string]any{"score": float64(120), "points": 5, "collected": int64(3)}
	cases := []struct {
		metric string
		want   int64
		max    bool
	}{
		{types.MetricHighScore, 120, true},
		{types.MetricTotalScore, 120, false},
		{types.MetricTotalWins, 2, false}, // two non-empty prize ids
		{types.MetricTotalPlays, 1, false},
		{types.MetricQuestPoints, 5, false},
		{types.MetricCollectionCount, 3, false},
	}
	for _, c := range cases {
		v, isMax := Contribution(c.metric, rewards, meta)
		if v != c.want || isMax != c.max {
			t.Errorf("%s: got (%d,%v) want (%d,%v)", c.metric, v, isMax, c.want, c.max)
		}
	}
}

func TestApplyMetric(t *testing.T) {
	meta := map[string]any{"score": 80}
	if got := ApplyMetric(types.MetricHighScore, 100, nil, meta); got != 100 {
		t.Errorf("high_score keeps max: got %d want 100", got)
	}
	if got := ApplyMetric(types.MetricHighScore, 50, nil, meta); got != 80 {
		t.Errorf("high_score takes higher: got %d want 80", got)
	}
	if got := ApplyMetric(types.MetricTotalScore, 50, nil, meta); got != 130 {
		t.Errorf("total_score accumulates: got %d want 130", got)
	}
	if got := ApplyMetric(types.MetricTotalPlays, 4, nil, nil); got != 5 {
		t.Errorf("total_plays increments: got %d want 5", got)
	}
}

func TestWindowKey(t *testing.T) {
	now := time.Date(2027, 1, 6, 10, 0, 0, 0, time.UTC) // Wednesday, ISO week 1
	if k := WindowKey(types.TimeWindow{Type: types.WindowFixed}, now); k != "fixed" {
		t.Errorf("fixed window key: %q", k)
	}
	if k := WindowKey(types.TimeWindow{Type: types.WindowRecurring, Period: types.PeriodDaily}, now); k != "2027-01-06" {
		t.Errorf("daily window key: %q", k)
	}
	if k := WindowKey(types.TimeWindow{Type: types.WindowRecurring, Period: types.PeriodMonthly}, now); k != "2027-01" {
		t.Errorf("monthly window key: %q", k)
	}
	wk := WindowKey(types.TimeWindow{Type: types.WindowRecurring, Period: types.PeriodWeekly}, now)
	if wk != "2027-W01" {
		t.Errorf("weekly window key: %q", wk)
	}
}

func TestIsOpen(t *testing.T) {
	now := time.Date(2027, 1, 6, 10, 0, 0, 0, time.UTC)
	past := now.Add(-48 * time.Hour)
	future := now.Add(48 * time.Hour)
	if !IsOpen(types.TimeWindow{Type: types.WindowRecurring}, now) {
		t.Error("recurring always open")
	}
	if IsOpen(types.TimeWindow{Type: types.WindowFixed, Start: &future}, now) {
		t.Error("fixed before start should be closed")
	}
	if IsOpen(types.TimeWindow{Type: types.WindowFixed, End: &past}, now) {
		t.Error("fixed after end should be closed")
	}
	if !IsOpen(types.TimeWindow{Type: types.WindowFixed, Start: &past, End: &future}, now) {
		t.Error("fixed within window should be open")
	}
}
