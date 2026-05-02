package config

type ExchangeConfig struct {
	Name               string `json:"name"`
	SymbolDiscoveryUrl string `json:"symbol_discovery_url"`
	SnapshotUrl        string `json:"snapshot_url"`
	StreamUrl          string `json:"stream_url"`
	StreamBufferSize   int    `json:"stream_buffer_size"`
	DeltaQueueSize     int    `json:"delta_queue_size"`

	RetryMaxAttempts    int `json:"retry_max_attempts"`
	RetryInitialDelayMs int `json:"retry_initial_delay_ms"`
	RetryMaxDelayMs     int `json:"retry_max_delay_ms"`

	BTreeDegree      int `json:"btree_degree"`
	SnapshotQuantity int `json:"snapshot_quantity"`
}
