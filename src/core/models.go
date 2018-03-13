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

func (t TransactionDirection) String() string {
	if t == TransactionDirection_To {
		return "to"
	}
	return "from"
}

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

func (op Operator) Encode() proto.Operator {
	p, s := op.Key.IsValid()
	return proto.Operator{
		ID:           op.ID,
		Username:     op.Username,
		TelegramChat: op.TelegramChat,
		HasValidKey:  p && s,
		Status:       op.Status,
		CurrentOrder: op.CurrentOrder,
		Deposit:      op.Deposit,
	}
}

// Loads operator and lock for transaction lifetime using current data as conditions
func (op *Operator) LockLoad(tx *gorm.DB) error {
	return tx.Set("gorm:query_option", "FOR UPDATE").Where(op).First(op).Error
}

func LockLoadOperatorByID(tx *gorm.DB, id uint64) (Operator, error) {
	op := Operator{Model: db.Model{ID: id}}
	err := op.LockLoad(tx)
	return op, err
}

// Saves everything except deposit
func (op *Operator) Save(tx *gorm.DB) error {
	err := tx.Omit("deposit").Save(op).Error
	return err
}

func (op Operator) ChangeDeposit(tx *gorm.DB, amount decimal.Decimal) error {
	return tx.Model(&op).Update("deposit", gorm.Expr("deposit + ?", amount)).Error
}

type LBTransaction struct {
	ID uint64
	// username of lb account from which transaction was fetched
	Account   string
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

func (order *Order) LockLoad(tx *gorm.DB) error {
	return tx.Set("gorm:query_option", "FOR UPDATE").Where(order).First(order).Error
}

func LockLoadOrderByID(tx *gorm.DB, id uint64) (Order, error) {
	order := Order{Model: db.Model{ID: id}}
	err := order.LockLoad(tx)
	return order, err
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

func (order Order) OutletAmount() decimal.Decimal {
	return order.LBAmount.Sub(order.LBFee).Sub(order.OperatorFee).Sub(order.BotFee)
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
