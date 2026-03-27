package model

type TickModel struct {
	Exchange   string `json:"exchange"`
	Symbol     string `json:"symbol"`
	Price      string `json:"price"`
	Volume     string `json:"volume"`
	EventTime  int64  `json:"eventTime"`
	IsTakerBuy bool   `json:"isTakerBuy"`
}
