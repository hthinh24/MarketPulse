package entity

import "time"

type Exchange struct {
	Code      string    `gorm:"primaryKey;type:varchar(20)"`
	Name      string    `gorm:"type:varchar(50);not null"`
	Status    string    `gorm:"type:varchar(20);default:'ACTIVE'"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type ExchangeSymbol struct {
	ID           uint   `gorm:"primaryKey"`
	ExchangeCode string `gorm:"uniqueIndex:idx_exchange_symbol"`
	Symbol       string `gorm:"uniqueIndex:idx_exchange_symbol"`
	BaseCoin     string
	QuoteCoin    string
	Status       string
	UpdatedAt    time.Time
}

func (Exchange) TableName() string {
	return "exchanges"
}

func (ExchangeSymbol) TableName() string {
	return "exchange_symbols"
}
