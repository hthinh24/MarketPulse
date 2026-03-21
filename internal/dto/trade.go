package dto

type BinanceWsPayload struct {
	Stream string `json:"stream"`
	Data   Trade  `json:"data"`
}

type Trade struct {
	Symbol    string `json:"s"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	IsMaker   bool   `json:"m"`
	IgnoreM   bool   `json:"M"`
}
