package model_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestFallbackModelSettingsRoundTripThroughConfigManager(t *testing.T) {
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if key == "fallback_model_setting.models" {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	raw := `[{"name":"auto","enabled":true,"groups":["default"],"attempts":[{"channel_id":1,"model":"gpt-4o-mini"}]}]`
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"fallback_model_setting.models": raw,
	}))

	settings := GetFallbackModelSettings()
	require.Len(t, settings.Models, 1)
	require.Equal(t, "auto", settings.Models[0].Name)
	require.True(t, settings.Models[0].Enabled)
	require.Equal(t, []string{"default"}, settings.Models[0].Groups)
	require.Equal(t, 1, settings.Models[0].Attempts[0].ChannelID)
	require.Equal(t, "gpt-4o-mini", settings.Models[0].Attempts[0].Model)
}
