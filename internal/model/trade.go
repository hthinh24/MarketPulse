package model

type Trade struct {
	Symbol    string `json:"s"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	EventTime int64  `json:"E"`
}
