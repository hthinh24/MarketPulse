package okx

// OKXInstrument Mapping struct for GET API /api/v5/public/instruments
type OKXInstrument struct {
	InstId   string `json:"instId"`
	BaseCcy  string `json:"baseCcy"`
	QuoteCcy string `json:"quoteCcy"`
	State    string `json:"state"`
}

type OKXInstrumentsResponse struct {
	Code string          `json:"code"`
	Data []OKXInstrument `json:"data"`
}

// OKXArg Struct for OKX subscribe
type OKXArg struct {
	Channel string `json:"channel"`
	InstId  string `json:"instId"`
}

type OKXSubscribePayload struct {
	Op   string   `json:"op"`
	Args []OKXArg `json:"args"`
}

type OKXTradeData struct {
	InstId string `json:"instId"`
	Px     string `json:"px"`
	Sz     string `json:"sz"`
	Ts     string `json:"ts"`
	Side   string `json:"side"`
}

type OKXWsPayload struct {
	Event string         `json:"event,omitempty"`
	Arg   OKXArg         `json:"arg"`
	Data  []OKXTradeData `json:"data"`
}
