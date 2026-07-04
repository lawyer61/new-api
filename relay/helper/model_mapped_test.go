package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperUsesFallbackAttemptModelForMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("model_mapping", `{"channel-public":"upstream-private"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName:          "auto",
		FallbackModelName:        "auto",
		FallbackAttemptModelName: "channel-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "channel-public",
		},
	}
	request := &dto.GeneralOpenAIRequest{Model: "auto"}

	require.NoError(t, ModelMappedHelper(ctx, info, request))
	require.Equal(t, "auto", info.OriginModelName)
	require.Equal(t, "upstream-private", info.UpstreamModelName)
	require.Equal(t, "upstream-private", request.Model)
	require.True(t, info.IsModelMapped)
}
