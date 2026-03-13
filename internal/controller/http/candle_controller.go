package http

import (
	"MarketPulse/internal/dto"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
)

type candleService interface {
	GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) ([]*dto.CandleResponse, error)
}

type candleController struct {
	candleService candleService
}

func NewCandleController(candleService candleService) *candleController {
	return &candleController{candleService: candleService}
}

func (c *candleController) RegisterRoutes(group *gin.RouterGroup) {
	trade := group.Group("/candles")
	trade.GET("", c.GetHistoricalCandles)
}

func (c *candleController) GetHistoricalCandles(ctx *gin.Context) {
	var req dto.GetCandlesRequest

	if err := ctx.ShouldBindQuery(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, dto.APIResponse{
			Code:    http.StatusBadRequest,
			Message: "Invalid parameters: " + err.Error(),
			Data:    nil,
		})
		return
	}

	if req.Limit == 0 {
		req.Limit = 100
	}

	candles, err := c.candleService.GetHistoricalCandles(ctx, &req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, dto.APIResponse{
			Code:    http.StatusInternalServerError,
			Message: "Internal server error",
			Data:    nil,
		})
		return
	}

	ctx.JSON(http.StatusOK, dto.APIResponse{
		Code:    http.StatusOK,
		Message: "Success",
		Data:    candles,
	})
}
