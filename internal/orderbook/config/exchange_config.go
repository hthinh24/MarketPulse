package config

type ExchangeConfig struct {
	Name               string `json:"name"`
	SymbolDiscoveryUrl string `json:"symbol_discovery_url"`
	SnapshotUrl        string `json:"snapshot_url"`
	StreamUrl          string `json:"stream_url"`
	StreamBufferSize   int    `json:"stream_buffer_size"`
	DeltaQueueSize     int    `json:"delta_queue_size"`
}
