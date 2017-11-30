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
	rabbit.ServeRPC(proto.OperatorByID, OperatorByID)
	rabbit.ServeRPC(proto.OperatorByTg, OperatorByTg)
	rabbit.ServeRPC(proto.SetOperatorStatus, SetOperatorStatus)
	rabbit.ServeRPC(proto.SetOperatorKey, SetOperatorKey)
	rabbit.ServeRPC(proto.CreateOrder, CreateOrder)
	rabbit.ServeRPC(proto.GetOrder, GetOrder)
	rabbit.ServeRPC(proto.AcceptOffer, AcceptOffer)
	rabbit.ServeRPC(proto.SkipOffer, SkipOffer)
	rabbit.ServeRPC(proto.DropOrder, DropOrder)
	rabbit.ServeRPC(proto.LinkLBContact, LinkLBContract)
	rabbit.ServeRPC(proto.RequestPayment, RequestPayment)
	rabbit.ServeRPC(proto.CancelOrder, CancelOrder)
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
		log.Errorf("failed to load operator '%v': %v", acc.Username, scope.Error)
		return proto.Operator{}, errors.New(proto.DBError)
	}

	// Just for encoding, do not save yet
	op.Username = acc.Username
	op.Key = key
	return op.Encode(), nil
}

func OperatorByTg(chatID int64) (proto.Operator, error) {
	var op Operator
	scope := db.New().First(&op, "telegram_chat = ?", chatID)
	switch {
	case scope.RecordNotFound():
		log.Debug("operator with telegram chat %v not found", chatID)
		return proto.Operator{}, nil

	case scope.Error != nil:
		log.Errorf("failed to load operator for chat %v: %v", chatID, scope.Error)
		return proto.Operator{}, errors.New(proto.DBError)
	}

	return op.Encode(), nil
}

func OperatorByID(operatopID uint64) (proto.Operator, error) {
	var op Operator
	scope := db.New().First(&op, "id = ?", operatopID)
	switch {
	case scope.RecordNotFound():
		log.Debug("operator %v not found", operatopID)
		return proto.Operator{}, nil

	case scope.Error != nil:
		log.Errorf("failed to load operator %v: %v", operatopID, scope.Error)
		return proto.Operator{}, errors.New(proto.DBError)
	}

	return op.Encode(), nil
}

func SetOperatorStatus(req proto.SetOperatorStatusRequest) (bool, error) {
	var op Operator
	err := db.New().First(&op, "telegram_chat = ?", req.ChatID).Error
	if err != nil {
		log.Errorf("failed to load operator for chat %v: %v", req.ChatID, err)
		return false, errors.New(proto.DBError)
	}
	if op.Status == proto.OperatorStatus_Busy {
		return false, errors.New("operator is busy")
	}
	var updMap = map[string]interface{}{
		"status": req.Status,
	}
	if op.Status == proto.OperatorStatus_Proposal {
		updMap["current_order"] = 0
	}
	err = db.New().Model(&op).Updates(updMap).Error
	if err != nil {
		log.Errorf("failed to update operator status: %v", err)
		return false, errors.New(proto.DBError)
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
		op.TelegramChat = req.ChatID
		op.Status = proto.OperatorStatus_Inactive
		op.Key = req.Key
		err = db.New().Create(&op).Error

	case scope.Error != nil:
		log.Errorf("failed to load operator '%v': %v", acc.Username, scope.Error)
		return proto.Operator{}, errors.New(proto.DBError)

	default:
		if op.TelegramChat != req.ChatID {
			// @TODO send something to old chat and ensure unique chatID
		}
		err = db.New().Model(&op).Updates(map[string]interface{}{
			"telegram_chat": req.ChatID,
			"status":        proto.OperatorStatus_Inactive,
			"lb_key":        req.Key.Public,
			"lb_secret":     req.Key.Secret,
		}).Error
	}

	if err != nil {
		log.Errorf("failed to save operator %v: %v", op.ID, err)
		return proto.Operator{}, errors.New(proto.DBError)
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
	err = order.Save(db.New())
	if err != nil {
		return proto.Order{}, errors.New(proto.DBError)
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
		return proto.Order{}, errors.New(proto.DBError)
	}
	return order.Encode(), nil
}

func AcceptOffer(req proto.AcceptOfferRequest) (proto.Order, error) {
	order, err := manager.AcceptOffer(req.OperatorID, req.OrderID)
	return order.Encode(), err
}

func SkipOffer(req proto.SkipOfferRequest) (bool, error) {
	var op Operator
	err := db.New().First(&op, "id = ?", req.OperatorID).Error
	if err != nil {
		log.Errorf("failed to load operator %v: %v", req.OperatorID, err)
		return false, errors.New(proto.DBError)
	}
	if op.Status != proto.OperatorStatus_Proposal || op.CurrentOrder != req.OrderID {
		log.Debug("operator %v tried to skip offer %v while his current status was %v, order %v",
			req.OperatorID, req.OrderID, op.Status, op.CurrentOrder)
		return false, errors.New("unexpected status")
	}
	err = db.New().Model(&op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Ready,
		"current_order": 0,
	}).Error
	if err != nil {
		log.Errorf("failed to save operator %v: %v", op.ID, err)
		return false, errors.New(proto.DBError)
	}
	manager.PushOperator(op)
	return true, nil
}

func DropOrder(req proto.DropOrderRequest) (bool, error) {
	var op Operator
	err := db.New().First(&op, "id = ?", req.OperatorID).Error
	if err != nil {
		log.Errorf("failed to load operator %v: %v", req.OperatorID, err)
		return false, errors.New(proto.DBError)
	}
	if op.Status != proto.OperatorStatus_Busy || op.CurrentOrder != req.OrderID {
		log.Debug("operator %v tried to drop order %v while his current status was %v, order %v",
			req.OperatorID, req.OrderID, op.Status, op.CurrentOrder)
		return false, errors.New("unexpected status")
	}
	var order Order
	err = db.New().First(&order, "id = ?", req.OrderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", req.OrderID, err)
		return false, errors.New(proto.DBError)
	}
	if order.Status != proto.OrderStatus_Accepted && order.Status != proto.OrderStatus_Linked {
		log.Debug("operator %v tried to drop order %v while order had status %v",
			req.OperatorID, req.OrderID, order.Status)
		return false, errors.New("unexpected status")
	}

	tx := db.NewTransaction()
	order.Status = proto.OrderStatus_Dropped
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}
	err = tx.Model(&op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Inactive,
		"current_order": 0,
	}).Error
	if err != nil {
		log.Errorf("failed to save operator %v: %v", op.ID, err)
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}

	err = tx.Commit().Error
	if err != nil {
		log.Errorf("failed to commit: %v", err)
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}
	return true, nil
}

func LinkLBContract(req proto.LinkLBContractRequest) (proto.Order, error) {
	if req.Requisites == "" {
		return proto.Order{}, errors.New("empty requisites")
	}
	var order Order
	err := db.New().First(&order, "id = ?", req.OrderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", req.OrderID, err)
		return proto.Order{}, errors.New(proto.DBError)
	}

	if order.Status != proto.OrderStatus_Accepted && order.Status != proto.OrderStatus_Linked {
		return proto.Order{}, errors.New("unexpected status")
	}

	var op Operator
	err = db.New().First(&op, "id = ?", order.OperatorID).Error
	if err != nil {
		log.Errorf("failed to load operator %v: %v", order.OperatorID, err)
		return proto.Order{}, errors.New(proto.DBError)
	}

	contacts, err := op.Key.ActiveContacts()
	found := false
	var contact lbapi.Contact
	for _, contact = range contacts {
		if contact.Data.Currency == order.Currency && contact.Data.Amount == order.FiatAmount {
			found = true
			break
		}
	}
	if !found {
		return order.Encode(), errors.New(proto.ContactNotFoundError)
	}

	order.LBContactID = contact.Data.ContactID
	order.LBAmount = contact.Data.AmountBTC
	order.LBFee = contact.Data.FeeBTC
	order.OperatorFee = order.LBAmount.Mul(decimal.NewFromFloat(conf.OperatorFee))
	order.BotFee = order.LBAmount.Mul(decimal.NewFromFloat(conf.BotFee))
	order.Status = proto.OrderStatus_Linked
	order.PaymentRequisites = req.Requisites

	err = order.Save(db.New())
	if err != nil {
		return proto.Order{}, errors.New(proto.DBError)
	}

	return order.Encode(), nil
}

func RequestPayment(orderID uint64) (proto.Order, error) {
	var order Order
	err := db.New().First(&order, "id = ?", orderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", orderID, err)
		return proto.Order{}, errors.New(proto.DBError)
	}
	order.Status = proto.OrderStatus_Payment
	err = order.Save(db.New())
	if err != nil {
		return proto.Order{}, errors.New(proto.DBError)
	}

	return order.Encode(), nil
}

func CancelOrder(orderID uint64) (bool, error) {
	var order Order
	err := db.New().First(&order, "id = ?", orderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", orderID, err)
		return false, errors.New(proto.DBError)
	}
	switch order.Status {
	case proto.OrderStatus_New, proto.OrderStatus_Accepted,
		proto.OrderStatus_Linked, proto.OrderStatus_Payment,
		proto.OrderStatus_Confirmation:
	default:
		return false, errors.New("unexpected status")
	}
	order.Status = proto.OrderStatus_Canceled
	err = order.Save(db.New())
	if err != nil {
		return false, errors.New(proto.DBError)
	}
	return true, nil
}
