package adapter

import (
	"MarketPulse/internal/entity"
	"MarketPulse/internal/exchange/okx"
	"encoding/json"
	"net/http"
	"time"
)

type OKXAdapter struct {
	exchange string
	url      string
}

func NewOKXAdapter(exchange string, url string) *OKXAdapter {
	return &OKXAdapter{
		exchange: exchange,
		url:      url,
	}
}

func (o *OKXAdapter) GetExchangeCode() string {
	return o.exchange
}

func (o *OKXAdapter) FetchSymbols() ([]entity.ExchangeSymbol, error) {
	var exchangeSymbols []entity.ExchangeSymbol

	resp, err := http.Get(o.url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info okx.OKXInstrumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	for _, s := range info.Data {
		if s.QuoteCcy == "USDT" && s.State == "live" {
			exchangeSymbols = append(exchangeSymbols, entity.ExchangeSymbol{
				ExchangeCode: o.exchange,
				Symbol:       s.InstId,
				Status:       s.State,
				BaseCoin:     s.BaseCcy,
				QuoteCoin:    s.QuoteCcy,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return exchangeSymbols, nil
}
