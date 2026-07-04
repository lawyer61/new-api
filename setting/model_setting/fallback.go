package model_setting

import "github.com/QuantumNous/new-api/setting/config"

type FallbackModelAttempt struct {
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
}

type FallbackModel struct {
	Name     string                 `json:"name"`
	Enabled  bool                   `json:"enabled"`
	Groups   []string               `json:"groups"`
	Attempts []FallbackModelAttempt `json:"attempts"`
}

type FallbackModelSettings struct {
	Models []FallbackModel `json:"models"`
}

var fallbackModelSettings = FallbackModelSettings{
	Models: []FallbackModel{},
}

func init() {
	config.GlobalConfig.Register("fallback_model_setting", &fallbackModelSettings)
}

func GetFallbackModelSettings() *FallbackModelSettings {
	return &fallbackModelSettings
}
