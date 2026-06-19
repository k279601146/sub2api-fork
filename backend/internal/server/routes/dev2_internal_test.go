package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDev2ModelNameUsesConfigValue(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "env-model")
	viper.Reset()

	got := dev2ModelName(&config.Config{
		Dev2: config.Dev2Config{
			ModelName: "configured-model",
		},
	})

	require.Equal(t, "configured-model", got)
}

func TestDev2ModelNameFallsBackToEnv(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "env-model")
	viper.Reset()

	require.Equal(t, "env-model", dev2ModelName(nil))
}

func TestDev2ModelNameFallsBackToViper(t *testing.T) {
	t.Setenv("DEV2_MODEL_NAME", "")
	viper.Reset()
	viper.Set("dev2.model_name", "viper-model")

	require.Equal(t, "viper-model", dev2ModelName(nil))
}
