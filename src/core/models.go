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

	// @TODO extra consistency checks in db?
	CurrentOrder uint64 `gorm:"index"`
}

type LBTransaction struct {
	ID        uint64
	Direction TransactionDirection
	lbapi.Transaction
}

type Order struct {
	db.Model
	ClientName string
	// Bitshares address
	Destination   string
	PaymentMethod string
	Currency      string
	// In currency above
	FiatAmount        decimal.Decimal `gorm:"type:decimal"`
	PaymentRequisites string          `gorm:"text"`
	LBContactID       uint64
	// Value of lb contract in bitcoins
	LBAmount    decimal.Decimal `gorm:"type:decimal"`
	LBFee       decimal.Decimal `gorm:"type:decimal"`
	OperatorFee decimal.Decimal `gorm:"type:decimal"`
	BotFee      decimal.Decimal `gorm:"type:decimal"`
	Status      proto.OrderStatus
	OperatorID  uint64
}

func (order Order) Encode() proto.Order {
	return proto.Order{
		ID:          order.ID,
		ClientName:  order.ClientName,
		Destination: order.Destination,
		Currency:    order.Currency,
		FiatAmount:  order.FiatAmount,
		LBAmount:    order.LBAmount,
		Status:      order.Status,
		OperatorID:  order.OperatorID,
	}
}
