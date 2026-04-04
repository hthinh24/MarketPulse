package binance

type BinanceOpenSymbol struct {
	Symbol     string `json:"symbol"`
	Status     string `json:"status"`
	BaseAsset  string `json:"baseAsset"`
	QuoteAsset string `json:"quoteAsset"`
}

type BinanceExchangeInfo struct {
	Symbols []BinanceOpenSymbol `json:"symbols"`
}

type BinanceWsPayload struct {
	Stream string           `json:"stream"`
	Data   BinanceTradeData `json:"data"`
}

type BinanceTradeData struct {
	Symbol    string `json:"s"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	IsMaker   bool   `json:"m"`
	IgnoreM   bool   `json:"M"`
}
