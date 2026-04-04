package adapter

import (
	"MarketPulse/internal/ingestor/exchange/binance"
	"MarketPulse/internal/server/entity"
	"encoding/json"
	"net/http"
	"time"
)

type BinanceAdapter struct {
	exchange string
	url      string
}

func NewBinanceAdapter(exchange string, url string) *BinanceAdapter {
	return &BinanceAdapter{
		exchange: exchange,
		url:      url,
	}
}

func (b *BinanceAdapter) GetExchangeCode() string {
	return b.exchange
}

func (b *BinanceAdapter) FetchSymbols() ([]entity.ExchangeSymbol, error) {
	var exchangeSymbols []entity.ExchangeSymbol

	resp, err := http.Get(b.url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info binance.BinanceExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	for _, s := range info.Symbols {
		if s.QuoteAsset == "USDT" && s.Status == "TRADING" {
			exchangeSymbols = append(exchangeSymbols, entity.ExchangeSymbol{
				ExchangeCode: b.exchange,
				Symbol:       s.Symbol,
				Status:       s.Status,
				BaseCoin:     s.BaseAsset,
				QuoteCoin:    s.QuoteAsset,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return exchangeSymbols, nil
}
