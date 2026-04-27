package config

type BroadcasterConfig struct {
	// Dispatcher command channel buffer size
	DispatcherCmdChanSize int

	// RoomWorker command channel buffer size per room
	WorkerCmdChanSize int

	// Done signal channel buffer size (max concurrent room closures)
	DoneChanSize int

	// Periodic gauge update interval in milliseconds for cmdChan queue length
	SnapshotIntervalMs int
}

func NewBroadcasterConfig() *BroadcasterConfig {
	return &BroadcasterConfig{
		DispatcherCmdChanSize: 512,
		WorkerCmdChanSize:     2048,
		DoneChanSize:          64,
		SnapshotIntervalMs:    200,
	}
}
