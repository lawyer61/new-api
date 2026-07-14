package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

type ResolvedFallbackAttempt struct {
	Channel *model.Channel
	Model   string
}

const (
	ginKeyFallbackModel        = "fallback_model"
	ginKeyFallbackAttemptIndex = "fallback_attempt_index"
	ginKeyFallbackAttemptModel = "fallback_attempt_model"
)

func NormalizeAndValidateFallbackModelSettings(settings model_setting.FallbackModelSettings) (model_setting.FallbackModelSettings, error) {
	conflictingModels, err := enabledChannelModelNames()
	if err != nil {
		return model_setting.FallbackModelSettings{}, err
	}

	seenNames := make(map[string]struct{}, len(settings.Models))
	normalizedModels := make([]model_setting.FallbackModel, 0, len(settings.Models))
	for modelIndex, fallbackModel := range settings.Models {
		name := strings.TrimSpace(fallbackModel.Name)
		if name == "" {
			return model_setting.FallbackModelSettings{}, fmt.Errorf("fallback model #%d name is required", modelIndex+1)
		}
		if _, ok := seenNames[name]; ok {
			return model_setting.FallbackModelSettings{}, fmt.Errorf("fallback model %q is duplicated", name)
		}
		seenNames[name] = struct{}{}
		if _, ok := conflictingModels[name]; ok {
			return model_setting.FallbackModelSettings{}, fmt.Errorf("fallback model %q conflicts with an enabled channel model", name)
		}

		groups := normalizeUniqueStrings(fallbackModel.Groups)
		if len(groups) == 0 {
			return model_setting.FallbackModelSettings{}, fmt.Errorf("fallback model %q must have at least one group", name)
		}
		if len(fallbackModel.Attempts) == 0 {
			return model_setting.FallbackModelSettings{}, fmt.Errorf("fallback model %q must have at least one attempt", name)
		}

		attempts := make([]model_setting.FallbackModelAttempt, 0, len(fallbackModel.Attempts))
		for attemptIndex, attempt := range fallbackModel.Attempts {
			normalizedAttempt, err := normalizeAndValidateFallbackAttempt(name, attemptIndex, attempt)
			if err != nil {
				return model_setting.FallbackModelSettings{}, err
			}
			attempts = append(attempts, normalizedAttempt)
		}

		normalizedModels = append(normalizedModels, model_setting.FallbackModel{
			Name:     name,
			Enabled:  fallbackModel.Enabled,
			Groups:   groups,
			Attempts: attempts,
		})
	}

	return model_setting.FallbackModelSettings{Models: normalizedModels}, nil
}

func normalizeAndValidateFallbackAttempt(fallbackName string, attemptIndex int, attempt model_setting.FallbackModelAttempt) (model_setting.FallbackModelAttempt, error) {
	if attempt.ChannelID <= 0 {
		return model_setting.FallbackModelAttempt{}, fmt.Errorf("fallback model %q attempt #%d channel_id must be positive", fallbackName, attemptIndex+1)
	}
	attemptModel := strings.TrimSpace(attempt.Model)
	if attemptModel == "" {
		return model_setting.FallbackModelAttempt{}, fmt.Errorf("fallback model %q attempt #%d model is required", fallbackName, attemptIndex+1)
	}

	channel, err := model.GetChannelById(attempt.ChannelID, true)
	if err != nil {
		return model_setting.FallbackModelAttempt{}, fmt.Errorf("fallback model %q attempt #%d channel #%d not found: %w", fallbackName, attemptIndex+1, attempt.ChannelID, err)
	}
	if channel.Status != common.ChannelStatusEnabled {
		return model_setting.FallbackModelAttempt{}, fmt.Errorf("fallback model %q attempt #%d channel #%d is disabled", fallbackName, attemptIndex+1, attempt.ChannelID)
	}
	if !channelExposesModel(channel, attemptModel) {
		return model_setting.FallbackModelAttempt{}, fmt.Errorf("fallback model %q attempt #%d channel #%d does not expose model %q", fallbackName, attemptIndex+1, attempt.ChannelID, attemptModel)
	}

	return model_setting.FallbackModelAttempt{
		ChannelID: attempt.ChannelID,
		Model:     attemptModel,
	}, nil
}

func GetVisibleFallbackModels(groups []string) []model_setting.FallbackModel {
	groupSet := stringSet(normalizeUniqueStrings(groups))
	if len(groupSet) == 0 {
		return nil
	}

	settings := model_setting.GetFallbackModelSettings()
	models := make([]model_setting.FallbackModel, 0, len(settings.Models))
	for _, fallbackModel := range settings.Models {
		if !fallbackModel.Enabled {
			continue
		}
		if groupsOverlap(groupSet, fallbackModel.Groups) {
			models = append(models, fallbackModel)
		}
	}
	return models
}

func GetFallbackModelForGroups(name string, groups []string) (model_setting.FallbackModel, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model_setting.FallbackModel{}, false
	}
	for _, fallbackModel := range GetVisibleFallbackModels(groups) {
		if fallbackModel.Name == name {
			return fallbackModel, true
		}
	}
	return model_setting.FallbackModel{}, false
}

func GetFallbackModelByName(name string) (model_setting.FallbackModel, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model_setting.FallbackModel{}, false
	}
	settings := model_setting.GetFallbackModelSettings()
	for _, fallbackModel := range settings.Models {
		if strings.TrimSpace(fallbackModel.Name) == name {
			return fallbackModel, true
		}
	}
	return model_setting.FallbackModel{}, false
}

func SetFallbackModelContext(c *gin.Context, fallbackModel model_setting.FallbackModel) {
	if c == nil {
		return
	}
	c.Set(ginKeyFallbackModel, fallbackModel)
}

func GetFallbackModelFromContext(c *gin.Context) (model_setting.FallbackModel, bool) {
	if c == nil {
		return model_setting.FallbackModel{}, false
	}
	value, ok := c.Get(ginKeyFallbackModel)
	if !ok {
		return model_setting.FallbackModel{}, false
	}
	fallbackModel, ok := value.(model_setting.FallbackModel)
	return fallbackModel, ok
}

func SetFallbackAttemptContext(c *gin.Context, attemptIndex int, attemptModel string) {
	if c == nil {
		return
	}
	c.Set(ginKeyFallbackAttemptIndex, attemptIndex)
	c.Set(ginKeyFallbackAttemptModel, attemptModel)
}

func FallbackModelGroupsForUsingGroup(c *gin.Context, usingGroup string) []string {
	usingGroup = strings.TrimSpace(usingGroup)
	if usingGroup == "auto" {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		return GetUserAutoGroup(userGroup)
	}
	if usingGroup != "" {
		return []string{usingGroup}
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if userGroup != "" {
		return []string{userGroup}
	}
	return nil
}

func GetAllEnabledFallbackModelNames() []string {
	settings := model_setting.GetFallbackModelSettings()
	names := make([]string, 0, len(settings.Models))
	for _, fallbackModel := range settings.Models {
		if fallbackModel.Enabled && strings.TrimSpace(fallbackModel.Name) != "" {
			name := strings.TrimSpace(fallbackModel.Name)
			if !common.StringsContains(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

func GetVisibleFallbackModelNames(groups []string) []string {
	models := GetVisibleFallbackModels(groups)
	names := make([]string, 0, len(models))
	for _, fallbackModel := range models {
		name := strings.TrimSpace(fallbackModel.Name)
		if name != "" && !common.StringsContains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func AppendVisibleFallbackModelNames(modelNames []string, groups []string) []string {
	for _, fallbackName := range GetVisibleFallbackModelNames(groups) {
		if !common.StringsContains(modelNames, fallbackName) {
			modelNames = append(modelNames, fallbackName)
		}
	}
	return modelNames
}

func ResolveFallbackAttempt(attempt model_setting.FallbackModelAttempt, requestPath string) (ResolvedFallbackAttempt, bool) {
	attemptModel := strings.TrimSpace(attempt.Model)
	if attempt.ChannelID <= 0 || attemptModel == "" {
		return ResolvedFallbackAttempt{}, false
	}
	channel, err := model.CacheGetChannel(attempt.ChannelID)
	if err != nil || channel == nil {
		return ResolvedFallbackAttempt{}, false
	}
	if channel.Status != common.ChannelStatusEnabled {
		return ResolvedFallbackAttempt{}, false
	}
	if !channelExposesModel(channel, attemptModel) {
		return ResolvedFallbackAttempt{}, false
	}
	if !ChannelSupportsRequestPath(channel, requestPath, attemptModel) {
		return ResolvedFallbackAttempt{}, false
	}
	return ResolvedFallbackAttempt{
		Channel: channel,
		Model:   attemptModel,
	}, true
}

func ChannelSupportsRequestPath(channel *model.Channel, requestPath string, requestModel string) bool {
	if channel == nil {
		return false
	}
	if requestPath == "" || channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	return config != nil && config.SupportsPathForModel(requestPath, requestModel)
}

func enabledChannelModelNames() (map[string]struct{}, error) {
	if model.DB == nil {
		return nil, errors.New("database is not initialized")
	}
	var channels []model.Channel
	if err := model.DB.Select("models").Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	names := make(map[string]struct{})
	for _, channel := range channels {
		for _, modelName := range channel.GetModels() {
			modelName = strings.TrimSpace(modelName)
			if modelName != "" {
				names[modelName] = struct{}{}
			}
		}
	}
	return names, nil
}

func normalizeUniqueStrings(values []string) []string {
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

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func groupsOverlap(groupSet map[string]struct{}, groups []string) bool {
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if _, ok := groupSet[group]; ok {
			return true
		}
	}
	return false
}

func channelExposesModel(channel *model.Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	modelName = strings.TrimSpace(modelName)
	for _, exposed := range channel.GetModels() {
		if strings.TrimSpace(exposed) == modelName {
			return true
		}
	}
	return false
}

func AppendFallbackModelAdminInfo(fallbackName string, attemptIndex int, attemptModel string, adminInfo map[string]interface{}) {
	if adminInfo == nil || fallbackName == "" {
		return
	}
	adminInfo["fallback_model"] = fallbackName
	if attemptIndex >= 0 {
		adminInfo["fallback_attempt_index"] = attemptIndex
	}
	if attemptModel != "" {
		adminInfo["fallback_attempt_model"] = attemptModel
	}
}

func AppendFallbackModelAdminInfoFromContext(c *gin.Context, adminInfo map[string]interface{}) {
	fallbackModel, ok := GetFallbackModelFromContext(c)
	if !ok {
		return
	}
	attemptIndex := -1
	if value, exists := c.Get(ginKeyFallbackAttemptIndex); exists {
		if idx, ok := value.(int); ok {
			attemptIndex = idx
		}
	}
	attemptModel := c.GetString(ginKeyFallbackAttemptModel)
	AppendFallbackModelAdminInfo(fallbackModel.Name, attemptIndex, attemptModel, adminInfo)
}
