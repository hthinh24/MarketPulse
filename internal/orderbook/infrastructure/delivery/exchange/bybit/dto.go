package bybit

// REST: /v5/market/instruments-info
type BybitInstrument struct {
	Symbol    string `json:"symbol"`
	QuoteCoin string `json:"quoteCoin"`
	Status    string `json:"status"`
}

type BybitInstrumentResult struct {
	Category string            `json:"category"`
	List     []BybitInstrument `json:"list"`
}

type BybitInstrumentResponse struct {
	RetCode int                   `json:"retCode"`
	Result  BybitInstrumentResult `json:"result"`
}

// REST: /v5/market/orderbook
type BybitOrderBookResult struct {
	S  string     `json:"s"`
	B  [][]string `json:"b"` // bids: [[price, size]]
	A  [][]string `json:"a"` // asks: [[price, size]]
	U  int64      `json:"u"`
	Ts int64      `json:"ts"`
}

type BybitOrderBookResponse struct {
	RetCode int                  `json:"retCode"`
	Result  BybitOrderBookResult `json:"result"`
}

// WebSocket message
type BybitWSOrderBookData struct {
	S   string     `json:"s"` // symbol
	B   [][]string `json:"b"` // bids
	A   [][]string `json:"a"` // asks
	U   int64      `json:"u"` // updateId
	Seq int64      `json:"seq"`
}

type BybitWSOrderBookMessage struct {
	Topic string               `json:"topic"`
	Type  string               `json:"type"` // "snapshot" | "delta"
	Ts    int64                `json:"ts"`
	Data  BybitWSOrderBookData `json:"data"`
}

type BybitWsCommandMessage struct {
	Op   string   `json:"op"`   // "subscribe" | "unsubscribe"
	Args []string `json:"args"` // orderbook.{depth}.{symbol}, e.g. ["orderbook.50.BTCUSDT"]
}
