package main

import (
	"common/db"
	"common/log"
	"common/rabbit"
	"core/proto"
	"errors"
	"github.com/shopspring/decimal"
	"lbapi"
)

func init() {
	rabbit.ServeRPC(proto.CheckKey, CheckKey)
	rabbit.ServeRPC(proto.OperatorByTg, OperatorByTg)
	rabbit.ServeRPC(proto.SetOperatorStatus, SetOperatorStatus)
	rabbit.ServeRPC(proto.SetOperatorKey, SetOperatorKey)
	rabbit.ServeRPC(proto.CreateOrder, CreateOrder)
	rabbit.ServeRPC(proto.GetOrder, GetOrder)
	rabbit.ServeRPC(proto.AcceptOffer, AcceptOffer)
	rabbit.ServeRPC(proto.SkipOffer, SkipOffer)
}

func CheckKey(key lbapi.Key) (proto.Operator, error) {
	p, s := key.IsValid()
	if !p || !s {
		return proto.Operator{}, errors.New("invalid key")
	}
	acc, err := key.Self()
	if err != nil {
		return proto.Operator{}, err
	}

	var op Operator
	scope := db.New().First(&op, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		return proto.Operator{
			Username: acc.Username,
		}, nil
	case scope.Error != nil:
		return proto.Operator{}, scope.Error
	}

	s, p = op.Key.IsValid()
	return proto.Operator{
		ID:           op.ID,
		Username:     acc.Username,
		TelegramChat: op.TelegramChat,
		Status:       op.Status,
		HasValidKey:  s && p,
		CurrentOrder: op.CurrentOrder,
	}, nil
}

func OperatorByTg(chatID int64) (proto.Operator, error) {
	var op Operator
	scope := db.New().First(&op, "telegram_chat = ?", chatID)
	switch {
	case scope.RecordNotFound():
		return proto.Operator{
			Username: op.Username,
		}, nil

	case scope.Error != nil:
		return proto.Operator{}, scope.Error
	}

	s, p := op.Key.IsValid()
	// It's fine to return empty value.
	return proto.Operator{
		ID:           op.ID,
		Username:     op.Username,
		TelegramChat: chatID,
		Status:       op.Status,
		HasValidKey:  s && p,
		CurrentOrder: op.CurrentOrder,
	}, nil
}

func SetOperatorStatus(req proto.SetOperatorStatusRequest) (bool, error) {
	var op Operator
	err := db.New().First(&op, "telegram_chat = ?", req.ChatID).Error
	if err != nil {
		return false, err
	}
	if op.Status == proto.OperatorStatus_Busy {
		return false, errors.New("operator is busy")
	}
	if op.Status == proto.OperatorStatus_Proposal {
		op.CurrentOrder = 0
	}
	op.Status = req.Status
	err = db.New().Save(&op).Error
	if err != nil {
		return false, err
	}
	if req.Status == proto.OperatorStatus_Ready {
		manager.PushOperator(op)
	}
	return true, nil
}

func SetOperatorKey(req proto.SetOperatorKeyRequest) (proto.Operator, error) {
	p, s := req.Key.IsValid()
	if !p || !s {
		return proto.Operator{}, errors.New("invalid key")
	}
	acc, err := req.Key.Self()
	if err != nil {
		return proto.Operator{}, err
	}

	var op Operator
	scope := db.New().First(&op, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		op.Username = acc.Username
		op.Deposit = decimal.Zero

	case scope.Error != nil:
		return proto.Operator{}, scope.Error

	default:
		if op.TelegramChat != req.ChatID {
			// @TODO send something to old chat and ensure unique chatID
		}
	}

	op.TelegramChat = req.ChatID
	op.Status = proto.OperatorStatus_Inactive
	op.Key = req.Key

	err = db.New().Save(&op).Error
	if err != nil {
		return proto.Operator{}, err
	}

	return proto.Operator{
		ID:           op.ID,
		Username:     acc.Username,
		TelegramChat: op.TelegramChat,
		Status:       op.Status,
		HasValidKey:  true,
		CurrentOrder: op.CurrentOrder,
	}, nil
}

func CreateOrder(req proto.Order) (proto.Order, error) {
	if req.ClientName == "" {
		return proto.Order{}, errors.New("empty client name")
	}
	if req.FiatAmount.Sign() <= 0 {
		return proto.Order{}, errors.New("invalid fiat amount")
	}
	valid := false
	for _, cur := range CurrencyList {
		if cur == req.Currency {
			valid = true
			break
		}
	}
	if !valid {
		return proto.Order{}, errors.New("unknown currency")
	}

	node, err := GetExchangeRate(req.Currency)
	if err != nil {
		return proto.Order{}, errors.New("failed to determine exchange rate")
	}

	// @TODO Check payment method
	// @TODO Check destination
	// @TODO Lock something on bitshares buffer? May be on later step
	order := Order{
		ClientName:    req.ClientName,
		PaymentMethod: req.PaymentMethod,
		Currency:      req.Currency,
		FiatAmount:    req.FiatAmount,
		Status:        proto.OrderStatus_New,
		// At this point it only determines required deposit. So we will refer to the best offer.
		LBAmount: req.FiatAmount.Div(node.Minimal),
	}
	err = db.New().Save(&order).Error
	if err != nil {
		log.Errorf("failed to save new order: %v", err)
		return proto.Order{}, errors.New("db error")
	}

	manager.PushOrder(order)

	return order.Encode(), nil
}

func GetOrder(id uint64) (proto.Order, error) {
	var order Order
	scope := db.New().First(&order, "id = ?", id)
	if scope.RecordNotFound() {
		return proto.Order{}, errors.New("record not found")
	}
	if scope.Error != nil {
		log.Errorf("failed to load order %v: %v", id, scope.Error)
		return proto.Order{}, errors.New("db error")
	}
	return order.Encode(), nil
}

func AcceptOffer(req proto.AcceptOfferRequest) (bool, error) {
	return true, manager.AcceptOffer(req.OperatorID, req.OrderID)
}

func SkipOffer(req proto.SkipOfferRequest) (bool, error) {
	// @TODO
	return false, errors.New("unimlemented")
}
