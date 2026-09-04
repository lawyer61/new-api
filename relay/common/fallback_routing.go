package common

func (info *RelayInfo) PublicModelName() string {
	if info == nil {
		return ""
	}
	if info.FallbackModelName != "" {
		return info.FallbackModelName
	}
	return info.OriginModelName
}

func (info *RelayInfo) RouteModelName() string {
	if info == nil {
		return ""
	}
	if info.FallbackAttemptModelName != "" {
		return info.FallbackAttemptModelName
	}
	return info.OriginModelName
}

func (info *RelayInfo) IsFallbackRouting() bool {
	return info != nil && info.FallbackModelName != "" && info.FallbackAttemptModelName != ""
}
