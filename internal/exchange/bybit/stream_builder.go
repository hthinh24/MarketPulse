package bybit

import (
	"encoding/json"
	"net/http"
)

func GetActiveUSDTStreams() ([]string, error) {
	resp, err := http.Get("https://api.bybit.com/v5/market/instruments-info?category=spot")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info BybitInstrumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	var streams []string
	for _, s := range info.Result.List {
		if s.QuoteCoin == "USDT" && s.Status == "Trading" {
			// Bybit Format "publicTrade.<symbol>"
			streams = append(streams, "publicTrade."+s.Symbol)
		}
	}
	return streams, nil
}

func ChunkSlice(slice []string, chunkSize int) [][]string {
	var chunks [][]string
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}
