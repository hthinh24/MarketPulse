package okx

import (
	"encoding/json"
	"net/http"
)

func GetActiveUSDTStreams() ([]OKXArg, error) {
	resp, err := http.Get("https://www.okx.com/api/v5/public/instruments?instType=SPOT")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var info OKXInstrumentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	var streams []OKXArg
	for _, s := range info.Data {
		if s.QuoteCcy == "USDT" && s.State == "live" {
			streams = append(streams, OKXArg{
				Channel: "trades",
				InstId:  s.InstId,
			})
		}
	}
	return streams, nil
}

func ChunkSlice(slice []OKXArg, chunkSize int) [][]OKXArg {
	var chunks [][]OKXArg
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}
	return chunks
}
