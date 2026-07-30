package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

type fallbackRelayHandler func(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError

func runFallbackRelay(c *gin.Context, relayInfo *relaycommon.RelayInfo, fallbackModel model_setting.FallbackModel, relayFormat types.RelayFormat, handler fallbackRelayHandler) *types.NewAPIError {
	if handler == nil {
		return types.NewErrorWithStatusCode(errors.New("fallback relay handler is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if !isFallbackRelayFormatSupported(relayFormat) {
		return types.NewErrorWithStatusCode(fmt.Errorf("fallback model %q does not support relay format %s", fallbackModel.Name, relayFormat), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	publicModelName := fallbackModel.Name
	relayInfo.FallbackModelName = publicModelName
	var lastError *types.NewAPIError
	validAttempts := 0

	for attemptIndex, attempt := range fallbackModel.Attempts {
		resolved, ok := service.ResolveFallbackAttempt(attempt, c.Request.URL.Path)
		if !ok {
			continue
		}
		validAttempts++
		prepareFallbackRelayAttempt(relayInfo, publicModelName, attemptIndex, resolved.Model)
		service.SetFallbackAttemptContext(c, attemptIndex, resolved.Model)

		newAPIError := setupFallbackRelayAttempt(c, relayInfo, resolved.Channel, resolved.Model)
		if newAPIError == nil {
			newAPIError = handler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			return nil
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		lastError = newAPIError

		if resolved.Channel != nil {
			processChannelError(c, *types.NewChannelError(
				resolved.Channel.Id,
				resolved.Channel.Type,
				resolved.Channel.Name,
				resolved.Channel.ChannelInfo.IsMultiKey,
				common.GetContextKeyString(c, constant.ContextKeyChannelKey),
				resolved.Channel.GetAutoBan(),
			), newAPIError)
		}

		remainingAttempts := len(fallbackModel.Attempts) - attemptIndex - 1
		if fallbackResponseStarted(c, relayInfo) || !shouldRetry(c, newAPIError, remainingAttempts) {
			break
		}
	}

	if validAttempts == 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("fallback model %q has no valid attempts", publicModelName), types.ErrorCodeModelNotFound, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if lastError != nil {
		return lastError
	}
	return types.NewErrorWithStatusCode(fmt.Errorf("fallback model %q failed without a relay error", publicModelName), types.ErrorCodeBadResponse, http.StatusBadGateway)
}

func setupFallbackRelayAttempt(c *gin.Context, relayInfo *relaycommon.RelayInfo, channel *model.Channel, attemptModel string) *types.NewAPIError {
	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, attemptModel)
	if newAPIError != nil {
		return newAPIError
	}
	addUsedChannel(c, channel.Id)

	bodyStorage, bodyErr := common.GetBodyStorage(c)
	if bodyErr != nil {
		if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
			return types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	c.Request.Body = io.NopCloser(bodyStorage)
	return nil
}

func prepareFallbackRelayAttempt(relayInfo *relaycommon.RelayInfo, publicModelName string, attemptIndex int, attemptModel string) {
	relayInfo.FallbackModelName = publicModelName
	relayInfo.FallbackAttemptIndex = attemptIndex
	relayInfo.FallbackAttemptModelName = attemptModel
	relayInfo.ChannelMeta = nil
	relayInfo.RequestConversionChain = nil
	relayInfo.FinalRequestRelayFormat = ""
	relayInfo.RuntimeHeadersOverride = nil
	relayInfo.UseRuntimeHeadersOverride = false
	relayInfo.ParamOverrideAudit = nil
	relayInfo.StreamStatus = nil
	relayInfo.UpstreamRequestBodySize = 0
}

func fallbackResponseStarted(c *gin.Context, relayInfo *relaycommon.RelayInfo) bool {
	if relayInfo != nil && (relayInfo.SendResponseCount > 0 || relayInfo.ReceivedResponseCount > 0) {
		return true
	}
	return c != nil && c.Writer != nil && c.Writer.Written()
}

func isFallbackRelayFormatSupported(relayFormat types.RelayFormat) bool {
	switch relayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini, types.RelayFormatEmbedding:
		return true
	default:
		return false
	}
}
