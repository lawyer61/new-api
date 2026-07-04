package service

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFallbackModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func insertFallbackTestChannel(t *testing.T, db *gorm.DB, channel model.Channel) {
	t.Helper()
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
}

func withFallbackModelSettings(t *testing.T, settings model_setting.FallbackModelSettings) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "fallback_model_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	raw, err := common.Marshal(settings.Models)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"fallback_model_setting.models": string(raw),
	}))
}

func TestNormalizeAndValidateFallbackModelSettings(t *testing.T) {
	db := setupFallbackModelTestDB(t)
	insertFallbackTestChannel(t, db, model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "primary",
		Models: "gpt-4o-mini,gpt-4.1-mini",
		Group:  "default",
	})

	validated, err := NormalizeAndValidateFallbackModelSettings(model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{
				Name:    " auto ",
				Enabled: true,
				Groups:  []string{" default ", "default"},
				Attempts: []model_setting.FallbackModelAttempt{
					{ChannelID: 1, Model: " gpt-4o-mini "},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "auto", validated.Models[0].Name)
	require.Equal(t, []string{"default"}, validated.Models[0].Groups)
	require.Equal(t, "gpt-4o-mini", validated.Models[0].Attempts[0].Model)
}

func TestValidateFallbackModelSettingsRejectsConflictsAndInvalidAttempts(t *testing.T) {
	db := setupFallbackModelTestDB(t)
	insertFallbackTestChannel(t, db, model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "primary",
		Models: "gpt-4o-mini",
		Group:  "default",
	})

	_, err := NormalizeAndValidateFallbackModelSettings(model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{
				Name:    "gpt-4o-mini",
				Enabled: true,
				Groups:  []string{"default"},
				Attempts: []model_setting.FallbackModelAttempt{
					{ChannelID: 1, Model: "gpt-4o-mini"},
				},
			},
		},
	})
	require.ErrorContains(t, err, "conflicts with an enabled channel model")

	_, err = NormalizeAndValidateFallbackModelSettings(model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{Name: "auto", Enabled: true, Groups: []string{"default"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 1, Model: "missing-model"}}},
		},
	})
	require.ErrorContains(t, err, "does not expose model")
}

func TestFallbackModelVisibilityByGroup(t *testing.T) {
	withFallbackModelSettings(t, model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{Name: "auto", Enabled: true, Groups: []string{"default"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 1, Model: "gpt-4o-mini"}}},
			{Name: "vip-auto", Enabled: true, Groups: []string{"vip"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 2, Model: "gpt-4.1"}}},
			{Name: "disabled-auto", Enabled: false, Groups: []string{"default"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 1, Model: "gpt-4o-mini"}}},
		},
	})

	models := GetVisibleFallbackModels([]string{"default"})
	require.Len(t, models, 1)
	require.Equal(t, "auto", models[0].Name)

	_, ok := GetFallbackModelForGroups("vip-auto", []string{"default"})
	require.False(t, ok)
	found, ok := GetFallbackModelForGroups("vip-auto", []string{"vip"})
	require.True(t, ok)
	require.Equal(t, "vip-auto", found.Name)
}

func TestResolveFallbackAttemptSkipsInvalidRuntimeChannels(t *testing.T) {
	db := setupFallbackModelTestDB(t)
	insertFallbackTestChannel(t, db, model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusManuallyDisabled,
		Name:   "disabled",
		Models: "gpt-4o-mini",
		Group:  "default",
	})
	insertFallbackTestChannel(t, db, model.Channel{
		Id:     2,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "healthy",
		Models: "gpt-4.1-mini",
		Group:  "default",
	})

	resolved, ok := ResolveFallbackAttempt(model_setting.FallbackModelAttempt{ChannelID: 1, Model: "gpt-4o-mini"}, "/v1/chat/completions")
	require.False(t, ok)
	require.Nil(t, resolved.Channel)

	resolved, ok = ResolveFallbackAttempt(model_setting.FallbackModelAttempt{ChannelID: 2, Model: "gpt-4.1-mini"}, "/v1/chat/completions")
	require.True(t, ok)
	require.Equal(t, 2, resolved.Channel.Id)
	require.Equal(t, "gpt-4.1-mini", resolved.Model)
}
