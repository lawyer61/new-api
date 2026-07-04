package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fallbackModelsAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Models   []model_setting.FallbackModel `json:"models"`
		Channels []struct {
			ID     int      `json:"id"`
			Name   string   `json:"name"`
			Type   int      `json:"type"`
			Status int      `json:"status"`
			Groups []string `json:"groups"`
			Models []string `json:"models"`
		} `json:"channels"`
	} `json:"data"`
}

type fallbackModelTestAPIResponse struct {
	Success bool                      `json:"success"`
	Message string                    `json:"message"`
	Data    fallbackModelTestResponse `json:"data"`
}

func TestUpdateFallbackModelsValidatesAndStoresSettings(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Log{}))
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:     91,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "fallback-target",
		Models: "upstream-a",
		Group:  "default",
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/fallback_models", strings.NewReader(`{
		"models": [
			{
				"name": " auto ",
				"enabled": true,
				"groups": [" default ", "default"],
				"attempts": [{"channel_id": 91, "model": " upstream-a "}]
			}
		]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateFallbackModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload fallbackModelsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	require.Equal(t, "auto", payload.Data.Models[0].Name)
	require.Equal(t, []string{"default"}, payload.Data.Models[0].Groups)
	require.Equal(t, "upstream-a", payload.Data.Models[0].Attempts[0].Model)

	var option model.Option
	require.NoError(t, db.First(&option, "`key` = ?", "fallback_model_setting.models").Error)
	require.Contains(t, option.Value, `"name":"auto"`)
	require.Equal(t, "auto", model_setting.GetFallbackModelSettings().Models[0].Name)
}

func TestGetFallbackModelsReturnsSettingsAndChannelSummaries(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withFallbackModelSettingsForController(t, model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{Name: "auto", Enabled: true, Groups: []string{"default"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 92, Model: "upstream-a"}}},
		},
	})
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:     92,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "summary-target",
		Models: "upstream-a,upstream-b",
		Group:  "default,vip",
	})
	require.NotNil(t, db)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/fallback_models", nil)

	GetFallbackModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload fallbackModelsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Models, 1)
	require.Equal(t, "auto", payload.Data.Models[0].Name)
	require.Len(t, payload.Data.Channels, 1)
	require.Equal(t, 92, payload.Data.Channels[0].ID)
	require.Equal(t, []string{"default", "vip"}, payload.Data.Channels[0].Groups)
	require.Equal(t, []string{"upstream-a", "upstream-b"}, payload.Data.Channels[0].Models)
}

func TestGetFallbackModelsReturnsEmptyModelArrayWhenStoredModelsAreNull(t *testing.T) {
	setupModelListControllerTestDB(t)
	withFallbackModelSettingsForController(t, model_setting.FallbackModelSettings{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/fallback_models", nil)

	GetFallbackModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload fallbackModelsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.NotNil(t, payload.Data.Models)
	require.Empty(t, payload.Data.Models)
}

func TestFallbackModelTestSupportsEmbeddingRelayFormat(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = true
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = originalSelfUseMode
	})
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "fallback-test-root",
		Password: "password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1000000,
	}).Error)
	service.InitHttpClient()

	var requestPath string
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestPath = r.URL.Path
		requestBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2]}],
			"model": "mapped-embedding",
			"usage": {"prompt_tokens": 4, "total_tokens": 4}
		}`))
	}))
	defer server.Close()

	modelMapping := `{"channel-public":"mapped-embedding"}`
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:           93,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-test",
		Status:       common.ChannelStatusEnabled,
		Name:         "embedding-target",
		Models:       "channel-public",
		Group:        "default",
		BaseURL:      &server.URL,
		ModelMapping: &modelMapping,
	})
	withFallbackModelSettingsForController(t, model_setting.FallbackModelSettings{
		Models: []model_setting.FallbackModel{
			{Name: "auto-embedding", Enabled: true, Groups: []string{"default"}, Attempts: []model_setting.FallbackModelAttempt{{ChannelID: 93, Model: "channel-public"}}},
		},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/fallback_models/test", strings.NewReader(`{"model":"auto-embedding","relay_format":"embedding"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1001)

	TestFallbackModel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload fallbackModelTestAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.True(t, payload.Data.Success, payload.Data.Message)
	require.Equal(t, "/v1/embeddings", requestPath)
	require.Contains(t, requestBody, `"model":"mapped-embedding"`)
	require.Contains(t, requestBody, `"new-api fallback embedding test"`)
}
