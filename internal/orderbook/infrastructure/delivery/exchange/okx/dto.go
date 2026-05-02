package okx

// REST: /api/v5/public/instruments
type OKXInstrument struct {
	InstId   string `json:"instId"`   // e.g. "BTC-USDT"
	QuoteCcy string `json:"quoteCcy"` // e.g. "USDT"
	State    string `json:"state"`    // "live"
}

type OKXInstrumentResponse struct {
	Code string          `json:"code"` // "0" = success
	Msg  string          `json:"msg"`
	Data []OKXInstrument `json:"data"`
}

// WebSocket orderbook data (inside "data" array)
type OKXOrderBookData struct {
	Asks      [][]string `json:"asks"` // [[price, size, deprecated, orders]]
	Bids      [][]string `json:"bids"` // [[price, size, deprecated, orders]]
	Ts        string     `json:"ts"`
	SeqId     int64      `json:"seqId"`
	PrevSeqId int64      `json:"prevSeqId"` // -1 if first snapshot
}

// WebSocket orderbook message wrapper
type OKXWSMessage struct {
	Arg    OKXWSArg           `json:"arg"`
	Action string             `json:"action"` // "snapshot" | "update"
	Data   []OKXOrderBookData `json:"data"`

	// Event fields for subscribe/unsubscribe ack and error
	Event string `json:"event,omitempty"` // "subscribe", "unsubscribe", "error"
	Code  string `json:"code,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

type OKXWSArg struct {
	Channel string `json:"channel"` // "books" - latency: 100ms
	InstId  string `json:"instId"`  // "BTC-USDT"
}

// WebSocket command message for subscribe/unsubscribe
type OKXWSCommandMessage struct {
	Op   string     `json:"op"`   // "subscribe" | "unsubscribe"
	Args []OKXWSArg `json:"args"` // [{channel: "books", instId: "BTC-USDT"}]
}
