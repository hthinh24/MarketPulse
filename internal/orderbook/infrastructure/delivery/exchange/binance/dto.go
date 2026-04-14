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

type BinanceSnapshotResponse struct {
	LastUpdateId int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"` // [<Price>, <Volume>] [["66000.50", "1.5"], ["65999.00", "0.2"]]
	Asks         [][]string `json:"asks"`
}

// Response trả về từ WebSocket
type BinanceDepthUpdate struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FirstUpdateId int64      `json:"U"`
	FinalUpdateId int64      `json:"u"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
}
type BinanceDepthUpdateStream struct {
	Stream string             `json:"stream"` // "btcusdt@depth"
	Data   BinanceDepthUpdate `json:"data"`
}
