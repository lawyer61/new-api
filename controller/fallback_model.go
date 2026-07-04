package controller

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type fallbackModelChannelSummary struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Type   int      `json:"type"`
	Status int      `json:"status"`
	Groups []string `json:"groups"`
	Models []string `json:"models"`
}

type fallbackModelSettingsResponse struct {
	Models   []model_setting.FallbackModel `json:"models"`
	Channels []fallbackModelChannelSummary `json:"channels"`
}

type fallbackModelTestRequest struct {
	Model string `json:"model"`
}

type fallbackModelTestAttemptResult struct {
	Index       int     `json:"index"`
	ChannelID   int     `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	Model       string  `json:"model"`
	Success     bool    `json:"success"`
	Message     string  `json:"message"`
	ErrorCode   string  `json:"error_code,omitempty"`
	Time        float64 `json:"time"`
}

type fallbackModelTestResponse struct {
	Model    string                           `json:"model"`
	Success  bool                             `json:"success"`
	Message  string                           `json:"message"`
	Attempts []fallbackModelTestAttemptResult `json:"attempts"`
}

func GetFallbackModels(c *gin.Context) {
	channels, err := fallbackModelChannelSummaries()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := model_setting.GetFallbackModelSettings()
	common.ApiSuccess(c, fallbackModelSettingsResponse{
		Models:   append([]model_setting.FallbackModel(nil), settings.Models...),
		Channels: channels,
	})
}

func UpdateFallbackModels(c *gin.Context) {
	var request model_setting.FallbackModelSettings
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	normalized, err := service.NormalizeAndValidateFallbackModelSettings(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	raw, err := common.Marshal(normalized.Models)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption("fallback_model_setting.models", string(raw)); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAudit(c, "fallback_model.update", map[string]interface{}{
		"models": len(normalized.Models),
	})

	channels, err := fallbackModelChannelSummaries()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, fallbackModelSettingsResponse{
		Models:   normalized.Models,
		Channels: channels,
	})
}

func TestFallbackModel(c *gin.Context) {
	var request fallbackModelTestRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	fallbackName := strings.TrimSpace(request.Model)
	fallbackModel, ok := service.GetFallbackModelByName(fallbackName)
	if !ok {
		common.ApiErrorMsg(c, "fallback model not found")
		return
	}
	if !fallbackModel.Enabled {
		common.ApiErrorMsg(c, "fallback model is disabled")
		return
	}

	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := testFallbackModelChain(c.Request.Context(), fallbackModel, testUserID)
	common.ApiSuccess(c, result)
}

func fallbackModelChannelSummaries() ([]fallbackModelChannelSummary, error) {
	var channels []model.Channel
	if err := model.DB.Omit("key").Order("id asc").Find(&channels).Error; err != nil {
		return nil, err
	}

	summaries := make([]fallbackModelChannelSummary, 0, len(channels))
	for _, channel := range channels {
		summaries = append(summaries, fallbackModelChannelSummary{
			ID:     channel.Id,
			Name:   channel.Name,
			Type:   channel.Type,
			Status: channel.Status,
			Groups: normalizeFallbackModelStringList(channel.GetGroups()),
			Models: normalizeFallbackModelStringList(channel.GetModels()),
		})
	}
	return summaries, nil
}

func normalizeFallbackModelStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func testFallbackModelChain(ctx context.Context, fallbackModel model_setting.FallbackModel, testUserID int) fallbackModelTestResponse {
	response := fallbackModelTestResponse{
		Model:    fallbackModel.Name,
		Success:  false,
		Attempts: []fallbackModelTestAttemptResult{},
	}

	validAttempts := 0
	for attemptIndex, attempt := range fallbackModel.Attempts {
		resolved, ok := service.ResolveFallbackAttempt(attempt, "/v1/chat/completions")
		if !ok {
			continue
		}
		validAttempts++

		start := time.Now()
		testResult := testChannel(ctx, resolved.Channel, testUserID, resolved.Model, string(constant.EndpointTypeOpenAI), false)
		elapsed := float64(time.Since(start).Milliseconds()) / 1000.0

		attemptResult := fallbackModelTestAttemptResult{
			Index:       attemptIndex,
			ChannelID:   resolved.Channel.Id,
			ChannelName: resolved.Channel.Name,
			Model:       resolved.Model,
			Success:     testResult.localErr == nil && testResult.newAPIError == nil,
			Time:        elapsed,
		}
		if testResult.localErr != nil {
			attemptResult.Message = testResult.localErr.Error()
		}
		if testResult.newAPIError != nil {
			attemptResult.Message = testResult.newAPIError.Error()
			attemptResult.ErrorCode = string(testResult.newAPIError.GetErrorCode())
		}
		response.Attempts = append(response.Attempts, attemptResult)

		if attemptResult.Success {
			response.Success = true
			response.Message = ""
			return response
		}

		newAPIError := testResult.newAPIError
		if newAPIError == nil && testResult.localErr != nil {
			newAPIError = types.NewOpenAIError(testResult.localErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if !shouldRetry(testResult.context, newAPIError, len(fallbackModel.Attempts)-attemptIndex-1) {
			break
		}
	}

	if validAttempts == 0 {
		response.Message = "fallback model has no valid attempts"
		return response
	}
	if len(response.Attempts) > 0 {
		response.Message = response.Attempts[len(response.Attempts)-1].Message
	}
	return response
}
