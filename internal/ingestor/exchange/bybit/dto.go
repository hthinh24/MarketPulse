package bybit

// BybitInstrument Bybit discovery API response structures
type BybitInstrument struct {
	Symbol    string `json:"symbol"`
	BaseCoin  string `json:"baseCoin"`
	QuoteCoin string `json:"quoteCoin"`
	Status    string `json:"status"`
}

type BybitResult struct {
	List []BybitInstrument `json:"list"`
}

type BybitInstrumentsResponse struct {
	RetCode int         `json:"retCode"`
	Result  BybitResult `json:"result"`
}

type BybitSubscribePayload struct {
	Op   string   `json:"op"`
	Args []string `json:"args"`
}

type BybitTradeData struct {
	T int64  `json:"T"` // Timestamp (miliseconds)
	S string `json:"s"` // Symbol
	P string `json:"p"` // Price
	V string `json:"v"` // Volume

	// Side from Bybit is represented for "Buy" / "Sell"
	Side string `json:"S"`
}

type BybitWsPayload struct {
	Topic   string           `json:"topic,omitempty"`
	Op      string           `json:"op,omitempty"`      // Use to check pong response / subscribe
	Success bool             `json:"success,omitempty"` // TRUE or FALSE
	RetMsg  string           `json:"ret_msg,omitempty"`
	Data    []BybitTradeData `json:"data"`
}
