package main

import (
	"common/db"
	"core/proto"
	"github.com/shopspring/decimal"
	"lbapi"
)

type TransactionDirection int

const (
	TransactionDirection_To   TransactionDirection = 0
	TransactionDirection_From TransactionDirection = 1
)

type Operator struct {
	db.Model
	// from localbitcoins
	Username string `gorm:"unique_index"`
	Status   proto.OperatorStatus
	lbapi.Key

	TelegramChat int64           `gorm:"unique_index"`
	Deposit      decimal.Decimal `gorm:"type:decimal;index"`
	Note         string          `gorm:"text"`
}

type LBTransaction struct {
	ID        uint64
	Direction TransactionDirection
	lbapi.Transaction
}
