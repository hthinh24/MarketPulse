package http

import (
	"MarketPulse/internal/dto"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
)

type candleService interface {
	GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) (*dto.CandleHistoryResponse, error)
	GetActiveExchanges(ctx context.Context) ([]string, error)
	GetAvailableSymbols(ctx context.Context, exchange string) ([]string, error)
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

	exchanges := group.Group("/exchanges")
	exchanges.GET("", c.GetActiveExchanges)

	symbols := group.Group(":exchangeId/symbols")
	symbols.GET("", c.GetAvailableSymbols)
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

	candleHistory, err := c.candleService.GetHistoricalCandles(ctx, &req)
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
		Data:    candleHistory,
	})
}

func (c *candleController) GetActiveExchanges(ctx *gin.Context) {
	exchanges, err := c.candleService.GetActiveExchanges(ctx)
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
		Data:    exchanges,
	})
}

func (c *candleController) GetAvailableSymbols(ctx *gin.Context) {
	exchange := ctx.Param("exchangeId")
	if exchange == "" {
		ctx.JSON(http.StatusBadRequest, dto.APIResponse{
			Code:    http.StatusBadRequest,
			Message: "Missing required parameter: exchange",
			Data:    nil,
		})
		return
	}

	symbols, err := c.candleService.GetAvailableSymbols(ctx, exchange)
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
		Data:    symbols,
	})
}
