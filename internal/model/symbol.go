package model

type ExchangeScore struct {
	Exchange         string  `json:"exchange"`
	TotalQuoteVolume float64 `json:"total_quote_volume"`
}

type ExchangeSymbolScore struct {
	Symbol string  `json:"symbol"`
	Score  float64 `json:"score"`
}
