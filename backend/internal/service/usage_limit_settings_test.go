//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type usageLimitSettingRepoStub struct {
	values map[string]string
	err    error
}

func (s *usageLimitSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *usageLimitSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *usageLimitSettingRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *usageLimitSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}

func (s *usageLimitSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *usageLimitSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *usageLimitSettingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestUsageService_UsageLimitSettings_CustomValues(t *testing.T) {
	svc := NewUsageService(nil, nil, nil, nil, &usageLimitSettingRepoStub{
		values: map[string]string{
			SettingKeyUsageWindowLimitUnits: "250",
			SettingKeyUsageWeeklyLimitUnits: "1500",
		},
	})

	windowLimit, weeklyLimit := svc.usageLimitSettings(context.Background())
	require.Equal(t, 250.0, windowLimit)
	require.Equal(t, 1500.0, weeklyLimit)
}

func TestUsageService_UsageLimitSettings_Fallbacks(t *testing.T) {
	svc := NewUsageService(nil, nil, nil, nil, &usageLimitSettingRepoStub{
		values: map[string]string{
			SettingKeyUsageWindowLimitUnits: "-1",
			SettingKeyUsageWeeklyLimitUnits: "bad",
		},
	})

	windowLimit, weeklyLimit := svc.usageLimitSettings(context.Background())
	require.Equal(t, baseUsageWindowLimit, windowLimit)
	require.Equal(t, baseUsageWeeklyLimit, weeklyLimit)
}

func TestApplyUsageWindows_UsesConfiguredLimits(t *testing.T) {
	stats := &usagestats.UserDashboardStats{}
	reset := time.Date(2026, 5, 29, 5, 0, 0, 0, time.UTC)

	applyUsageWindows(stats, 50, 300, reset, reset.Add(7*24*time.Hour), 250, 1500)

	require.NotNil(t, stats.CurrentWindow)
	require.Equal(t, 250.0, stats.CurrentWindow.LimitUnits)
	require.Equal(t, 200.0, stats.CurrentWindow.RemainingUnits)
	require.Equal(t, 20.0, stats.CurrentWindow.UsedPercent)
	require.NotNil(t, stats.WeeklyWindow)
	require.Equal(t, 1500.0, stats.WeeklyWindow.LimitUnits)
	require.Equal(t, 1200.0, stats.WeeklyWindow.RemainingUnits)
	require.Equal(t, 20.0, stats.WeeklyWindow.UsedPercent)
}

func TestCalculateDev2RewardBalanceCost_UsesWindowBeforeBalance(t *testing.T) {
	got := calculateDev2RewardBalanceCost(20, 100, 200, 700, 30, 500)

	require.Equal(t, 0.0, got)
}

func TestCalculateDev2RewardBalanceCost_ChargesOnlyOverage(t *testing.T) {
	got := calculateDev2RewardBalanceCost(90, 100, 200, 700, 30, 500)

	require.Equal(t, 20.0, got)
}

func TestCalculateDev2RewardBalanceCost_UsesStricterWeeklyWindow(t *testing.T) {
	got := calculateDev2RewardBalanceCost(20, 100, 690, 700, 30, 500)

	require.Equal(t, 20.0, got)
}

func TestCalculateDev2RewardBalanceCost_DoesNotOverdraftBalance(t *testing.T) {
	got := calculateDev2RewardBalanceCost(100, 100, 700, 700, 30, 12.345)

	require.Equal(t, 12.35, got)
}
