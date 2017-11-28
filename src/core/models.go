package main

import (
	"common/db"
	"common/log"
	"common/rabbit"
	"core/proto"
	"github.com/jinzhu/gorm"
	"github.com/shopspring/decimal"
	"lbapi"
)

func init() {
	rabbit.AddPublishers(rabbit.Publisher{
		Name:   "order_event",
		Routes: []rabbit.Route{proto.OrderEventRoute},
		// Most of listenrs will have autodelete queues, maybe there is no need in confirm
		Confirm: true,
	})
}

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

func (order *Order) Save(db *gorm.DB) error {
	err := db.Save(order).Error
	if err != nil {
		log.Errorf("failed to save order %v: %v", order.ID, err)
		return err
	}
	err = rabbit.Publish("order_event", "", order)
	if err != nil {
		log.Errorf("failed to send order event: %v", err)
		// Still saved, so it kind of success
		// In other hand transaction could be passed here...
		// return err
	}
	return nil
}

func (order Order) Encode() proto.Order {
	return proto.Order{
		ID:                order.ID,
		ClientName:        order.ClientName,
		Destination:       order.Destination,
		PaymentMethod:     order.PaymentMethod,
		Currency:          order.Currency,
		FiatAmount:        order.FiatAmount,
		PaymentRequisites: order.PaymentRequisites,
		LBContractID:      order.LBContactID,
		LBAmount:          order.LBAmount,
		LBFee:             order.LBFee,
		OperatorFee:       order.OperatorFee,
		BotFee:            order.BotFee,
		Status:            order.Status,
		OperatorID:        order.OperatorID,
	}
}
