package service

// DEPRECATED: This file is deprecated and has been replaced by order_book_state.go.
// The OrderBookEngine functionality has been split into:
// - OrderBookState: Pure state management (BTree-based orderbook)
// - ExchangeAdapter: Exchange-specific logic (sequence validation, resync, metrics)
// This separation allows for exchange-agnostic state handling and exchange-specific protocols.
//
// Keeping this file for backward compatibility during migration.
// It will be removed in a future refactor phase.
