package routes

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDev2ModelNameUsesConfigValue(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "")
	viper.Reset()

	got := dev2ModelName(nil, nil, &config.Config{
		Dev2: config.Dev2Config{
			ModelName: "configured-model",
		},
	})

	require.Equal(t, "configured-model", got)
}

func TestDev2ModelNameFallsBackToEnv(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "env-model")
	viper.Reset()

	require.Equal(t, "env-model", dev2ModelName(nil, nil, nil))
}

func TestDev2ModelNameFallsBackToViper(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "")
	viper.Reset()
	viper.Set("dev2.model_name", "viper-model")

	require.Equal(t, "viper-model", dev2ModelName(nil, nil, nil))
}

func TestDev2RefundUsageRecordIDsDedupesRequestShapes(t *testing.T) {
	req := dev2UsageRefundRequest{
		UsageRecordIDs: []int64{10, 0, 10, -1, 11},
	}
	req.UsageRecords = append(req.UsageRecords, struct {
		ID         int64   `json:"id"`
		Units      float64 `json:"units"`
		ActualCost float64 `json:"actual_cost"`
	}{ID: 11, Units: 99, ActualCost: 99})
	req.UsageRecords = append(req.UsageRecords, struct {
		ID         int64   `json:"id"`
		Units      float64 `json:"units"`
		ActualCost float64 `json:"actual_cost"`
	}{ID: 12, Units: 99, ActualCost: 99})

	require.Equal(t, []int64{10, 11, 12}, dev2RefundUsageRecordIDs(req))
}

func TestDev2RefundAmountsUseAuthoritativeOriginalActualCost(t *testing.T) {
	originals := []*dbent.UsageLog{
		{ID: 1, TotalCost: 9, ActualCost: 0},
		{ID: 2, TotalCost: 2.5, ActualCost: 1.25},
	}

	units, credit := dev2RefundAmountsFromOriginals(originals)

	require.Equal(t, 11.5, units)
	require.Equal(t, 1.25, credit)
}
