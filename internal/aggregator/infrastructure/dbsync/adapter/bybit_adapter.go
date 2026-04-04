package adapter

import (
	"MarketPulse/internal/ingestor/exchange/bybit"
	"MarketPulse/internal/server/entity"
	"encoding/json"
	"net/http"
	"time"
)

type BybitAdapter struct {
	exchange string
	url      string
}

func NewBybitAdapter(exchange string, url string) *BybitAdapter {
	return &BybitAdapter{
		exchange: exchange,
		url:      url,
	}
}

func (b *BybitAdapter) GetExchangeCode() string {
	return b.exchange
}

func (b *BybitAdapter) FetchSymbols() ([]entity.ExchangeSymbol, error) {
	var exchangeSymbols []entity.ExchangeSymbol

	resp, err := http.Get(b.url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info bybit.BybitInstrumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	for _, s := range info.Result.List {
		if s.QuoteCoin == "USDT" && s.Status == "Trading" {
			exchangeSymbols = append(exchangeSymbols, entity.ExchangeSymbol{
				ExchangeCode: b.exchange,
				Symbol:       s.Symbol,
				Status:       s.Status,
				BaseCoin:     s.BaseCoin,
				QuoteCoin:    s.QuoteCoin,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return exchangeSymbols, nil
}
