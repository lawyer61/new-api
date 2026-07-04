package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRunFallbackRelayTriesConfiguredAttemptsUntilSuccess(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-first",
		Status: common.ChannelStatusEnabled,
		Name:   "first",
		Models: "channel-model-a",
		Group:  "default",
	})
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:     2,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-second",
		Status: common.ChannelStatusEnabled,
		Name:   "second",
		Models: "channel-model-b",
		Group:  "default",
	})
	require.NotNil(t, db)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"ping"}]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "auto",
		TokenGroup:      "default",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		Request: &dto.GeneralOpenAIRequest{
			Model: "auto",
			Messages: []dto.Message{
				{Role: "user", Content: "ping"},
			},
		},
	}
	fallbackModel := model_setting.FallbackModel{
		Name:    "auto",
		Enabled: true,
		Groups:  []string{"default"},
		Attempts: []model_setting.FallbackModelAttempt{
			{ChannelID: 1, Model: "channel-model-a"},
			{ChannelID: 2, Model: "channel-model-b"},
		},
	}

	var seen []string
	err := runFallbackRelay(ctx, info, fallbackModel, types.RelayFormatOpenAI, func(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
		seen = append(seen, fmt.Sprintf("%d:%s", c.GetInt("channel_id"), relayInfo.RouteModelName()))
		if len(seen) == 1 {
			return types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		return nil
	})

	require.Nil(t, err)
	require.Equal(t, []string{"1:channel-model-a", "2:channel-model-b"}, seen)
	require.Equal(t, []string{"1", "2"}, ctx.GetStringSlice("use_channel"))
	require.Equal(t, "auto", info.PublicModelName())
	require.Equal(t, "channel-model-b", info.RouteModelName())
}

func TestRunFallbackRelaySendsRealOpenAIRequestsThroughConfiguredAttempts(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	service.InitHttpClient()

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	requestsByChannel := map[int][]string{}
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestsByChannel[1] = append(requestsByChannel[1], string(body))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"first failed","type":"upstream_error","code":"bad_response"}}`))
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestsByChannel[2] = append(requestsByChannel[2], string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-fallback-test",
			"object": "chat.completion",
			"created": 1710000000,
			"model": "mapped-b",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "pong"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer secondServer.Close()

	mappingA := `{"channel-model-a":"mapped-a"}`
	mappingB := `{"channel-model-b":"mapped-b"}`
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:           1,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-first",
		Status:       common.ChannelStatusEnabled,
		Name:         "first",
		Models:       "channel-model-a",
		Group:        "default",
		BaseURL:      &firstServer.URL,
		ModelMapping: &mappingA,
	})
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:           2,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-second",
		Status:       common.ChannelStatusEnabled,
		Name:         "second",
		Models:       "channel-model-b",
		Group:        "default",
		BaseURL:      &secondServer.URL,
		ModelMapping: &mappingB,
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"ping"}]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "auto")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1001)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 2001)
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	ctx.Set("token_name", "fallback-test")

	info := &relaycommon.RelayInfo{
		OriginModelName: "auto",
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		UserId:          1001,
		TokenId:         2001,
		StartTime:       time.Now(),
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			CacheRatio:      1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		Request: &dto.GeneralOpenAIRequest{
			Model: "auto",
			Messages: []dto.Message{
				{Role: "user", Content: "ping"},
			},
		},
	}
	info.InitRequestConversionChain()
	fallbackModel := model_setting.FallbackModel{
		Name:    "auto",
		Enabled: true,
		Groups:  []string{"default"},
		Attempts: []model_setting.FallbackModelAttempt{
			{ChannelID: 1, Model: "channel-model-a"},
			{ChannelID: 2, Model: "channel-model-b"},
		},
	}

	err := runFallbackRelay(ctx, info, fallbackModel, types.RelayFormatOpenAI, func(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
		return relayByFormat(c, relayInfo, types.RelayFormatOpenAI, nil)
	})

	require.Nil(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, requestsByChannel[1], 1)
	require.Len(t, requestsByChannel[2], 1)
	require.Contains(t, requestsByChannel[1][0], `"model":"mapped-a"`)
	require.Contains(t, requestsByChannel[2][0], `"model":"mapped-b"`)
	require.Equal(t, []string{"1", "2"}, ctx.GetStringSlice("use_channel"))
}

func insertFallbackRelayTestChannel(t *testing.T, channel model.Channel) {
	t.Helper()
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
}
