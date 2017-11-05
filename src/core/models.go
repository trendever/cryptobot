package main

import (
	"common/db"
	"github.com/shopspring/decimal"
	"lbapi"
)

type AccountStatus int

const (
	AccountStatus_Ready    AccountStatus = 0
	AccountStatus_Busy     AccountStatus = 1
	AccountStatus_Disabled AccountStatus = 2
	AccountStatus_Invalid  AccountStatus = 3
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
	Status   AccountStatus
	lbapi.Key

	TelegramChat uint64          `gorm:"unique_index"`
	Deposit      decimal.Decimal `gorm:"type:decimal;index"`
	Note         string          `gorm:"text"`
}

type LBTransaction struct {
	ID        uint64
	Direction TransactionDirection
	lbapi.Transaction
}
