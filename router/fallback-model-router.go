package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerFallbackModelRoutes(apiRouter *gin.RouterGroup) {
	fallbackModelsRoute := apiRouter.Group("/fallback_models")
	fallbackModelsRoute.Use(middleware.RootAuth())
	{
		fallbackModelsRoute.GET("", controller.GetFallbackModels)
		fallbackModelsRoute.PUT("", controller.UpdateFallbackModels)
		fallbackModelsRoute.POST("/test", controller.TestFallbackModel)
	}
}
