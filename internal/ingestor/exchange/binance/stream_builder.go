package binance

import (
	"encoding/json"
	"net/http"
	"strings"
)

func GetActiveUSDTStreams() ([]string, error) {
	resp, err := http.Get("https://api.binance.com/api/v3/exchangeInfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info BinanceExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	var streams []string
	for _, s := range info.Symbols {
		if s.QuoteAsset == "USDT" && s.Status == "TRADING" {
			// Stream coin format: <symbol>@trade
			streamName := strings.ToLower(s.Symbol) + "@trade"
			streams = append(streams, streamName)
		}
	}
	return streams, nil
}

// ChunkSlice helper function to split a slice to many chunks
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
