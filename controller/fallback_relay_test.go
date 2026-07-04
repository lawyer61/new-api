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
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Token{}))
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

func TestRunFallbackRelaySendsRealEmbeddingRequestsThroughConfiguredAttempts(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Token{}))
	service.InitHttpClient()

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	requestsByChannel := map[int][]string{}
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestsByChannel[1] = append(requestsByChannel[1], string(body))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"first embedding failed","type":"rate_limit","code":"rate_limit"}}`))
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestsByChannel[2] = append(requestsByChannel[2], string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2, 0.3]}],
			"model": "mapped-embedding-b",
			"usage": {"prompt_tokens": 3, "total_tokens": 3}
		}`))
	}))
	defer secondServer.Close()

	mappingA := `{"embedding-public-a":"mapped-embedding-a"}`
	mappingB := `{"embedding-public-b":"mapped-embedding-b"}`
	insertFallbackRelayTestChannel(t, model.Channel{
		Id:           1,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "sk-first",
		Status:       common.ChannelStatusEnabled,
		Name:         "first",
		Models:       "embedding-public-a",
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
		Models:       "embedding-public-b",
		Group:        "default",
		BaseURL:      &secondServer.URL,
		ModelMapping: &mappingB,
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"auto-embedding","input":"ping"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "auto-embedding")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1001)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 2001)
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	ctx.Set("token_name", "fallback-test")

	info := &relaycommon.RelayInfo{
		OriginModelName: "auto-embedding",
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		UserId:          1001,
		TokenId:         2001,
		StartTime:       time.Now(),
		RelayMode:       relayconstant.RelayModeEmbeddings,
		RelayFormat:     types.RelayFormatEmbedding,
		RequestURLPath:  "/v1/embeddings",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			CacheRatio:      1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		Request: &dto.EmbeddingRequest{
			Model: "auto-embedding",
			Input: "ping",
		},
	}
	info.InitRequestConversionChain()
	fallbackModel := model_setting.FallbackModel{
		Name:    "auto-embedding",
		Enabled: true,
		Groups:  []string{"default"},
		Attempts: []model_setting.FallbackModelAttempt{
			{ChannelID: 1, Model: "embedding-public-a"},
			{ChannelID: 2, Model: "embedding-public-b"},
		},
	}

	err := runFallbackRelay(ctx, info, fallbackModel, types.RelayFormatEmbedding, func(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
		return relayByFormat(c, relayInfo, types.RelayFormatEmbedding, nil)
	})

	require.Nil(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, requestsByChannel[1], 1)
	require.Len(t, requestsByChannel[2], 1)
	require.Contains(t, requestsByChannel[1][0], `"model":"mapped-embedding-a"`)
	require.Contains(t, requestsByChannel[2][0], `"model":"mapped-embedding-b"`)
	var embeddingResponse struct {
		Object string `json:"object"`
		Data   []struct {
			Object string `json:"object"`
		} `json:"data"`
		Model string `json:"model"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &embeddingResponse))
	require.Equal(t, "list", embeddingResponse.Object)
	require.NotEmpty(t, embeddingResponse.Data)
	require.Equal(t, "embedding", embeddingResponse.Data[0].Object)
	require.Equal(t, "mapped-embedding-b", embeddingResponse.Model)
	require.Equal(t, []string{"1", "2"}, ctx.GetStringSlice("use_channel"))
}

func insertFallbackRelayTestChannel(t *testing.T, channel model.Channel) {
	t.Helper()
	require.NoError(t, model.DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
}
