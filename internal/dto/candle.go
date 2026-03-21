package dto

type GetCandlesRequest struct {
	Symbol      string `form:"symbol" binding:"required"`
	Interval    string `form:"interval" binding:"required,oneof=1m 15m 1h 1d"`
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=1000"`
	EndTime     int64  `form:"endTime" binding:"omitempty"`
	ByPassCache bool   `form:"byPassCache" binding:"omitempty"`
}

type CandleResponse struct {
	OpenTime int64   `json:"openTime"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
}

type CandleUpdatedEvent struct {
	Symbol   string  `json:"symbol"`
	OpenTime int64   `json:"openTime"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
}
