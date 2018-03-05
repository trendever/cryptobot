package main

import (
	"common/db"
	"common/log"
	"common/rabbit"
	"core/proto"
	"errors"
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/shopspring/decimal"
	"lbapi"
	"strconv"
)

func init() {
	rabbit.ServeRPC(proto.CheckKey, CheckKey)
	rabbit.ServeRPC(proto.OperatorByID, OperatorByID)
	rabbit.ServeRPC(proto.OperatorByTg, OperatorByTg)
	rabbit.ServeRPC(proto.SetOperatorStatus, SetOperatorStatus)
	rabbit.ServeRPC(proto.SetOperatorKey, SetOperatorKey)
	rabbit.ServeRPC(proto.GetDepositRefillAddress, GetDepositRefillAddress)
	rabbit.ServeRPC(proto.CreateOrder, CreateOrder)
	rabbit.ServeRPC(proto.GetOrder, GetOrder)
	rabbit.ServeRPC(proto.AcceptOffer, AcceptOffer)
	rabbit.ServeRPC(proto.SkipOffer, SkipOffer)
	rabbit.ServeRPC(proto.DropOrder, DropOrder)
	rabbit.ServeRPC(proto.LinkLBContact, LinkLBContract)
	rabbit.ServeRPC(proto.RequestPayment, RequestPayment)
	rabbit.ServeRPC(proto.CancelOrder, CancelOrder)
	rabbit.ServeRPC(proto.MarkPayed, MarkPayed)
	rabbit.ServeRPC(proto.ConfirmPayment, ConfirmPayment)
}

func GetDepositRefillAddress(operatorID uint64) (string, error) {
	return ReceivingAddress, nil
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
		return proto.Operator{
			TelegramChat: chatID,
		}, nil

	case scope.Error != nil:
		log.Errorf("failed to load operator for chat %v: %v", chatID, scope.Error)
		return proto.Operator{}, errors.New(proto.DBError)
	}

	return op.Encode(), nil
}

func OperatorByID(operatorID uint64) (proto.Operator, error) {
	var op Operator
	scope := db.New().First(&op, "id = ?", operatorID)
	switch {
	case scope.RecordNotFound():
		log.Debug("operator %v not found", operatorID)
		return proto.Operator{}, errors.New("operator not found")

	case scope.Error != nil:
		log.Errorf("failed to load operator %v: %v", operatorID, scope.Error)
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

	tx := db.NewTransaction()

	var op Operator
	scope := tx.First(&op, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		op.Username = acc.Username
		op.Deposit = decimal.Zero
		op.TelegramChat = req.ChatID
		op.Status = proto.OperatorStatus_Inactive
		op.Key = req.Key
		err = db.New().Create(&op).Error
		if err != nil {
			log.Errorf("failed to save operator %v: %v", op.ID, err)
			tx.Rollback()
			return proto.Operator{}, errors.New(proto.DBError)
		}

	case scope.Error != nil:
		log.Errorf("failed to load operator '%v': %v", acc.Username, scope.Error)
		tx.Rollback()
		return proto.Operator{}, errors.New(proto.DBError)

	default:
		err = relinkAccount(tx, &op, &req)
		if err != nil {
			tx.Rollback()
			return proto.Operator{}, err
		}
	}

	err = tx.Commit().Error
	if err != nil {
		log.Errorf("failed to commit SetOperatorKey transaction: %v", err)
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

func relinkAccount(tx *gorm.DB, op *Operator, req *proto.SetOperatorKeyRequest) error {
	if req.ChatID == op.TelegramChat {
		// Just update key & done
		err := tx.Model(&op).Updates(map[string]interface{}{
			"lb_key":    req.Key.Public,
			"lb_secret": req.Key.Secret,
		}).Error
		if err != nil {
			log.Errorf("failed to update key: %v", err)
			return errors.New(proto.DBError)
		}
	}

	var oldOp Operator
	scope := tx.Where("telegram_chat = ? ", req.ChatID).Where("id <> ?", op.ID).First(&oldOp)

	switch {
	case scope.RecordNotFound():

	case scope.Error != nil:
		log.Errorf("failed to load old operator: %v", scope.Error)
		return errors.New(proto.DBError)

	default:
		if oldOp.Status == proto.OperatorStatus_Busy {
			log.Errorf("operator with chat %v(%v) tried to change lb account while was busy with order", req.ChatID, oldOp.Username)
			return errors.New("forbidden")
		}
		err := tx.Model(&oldOp).Updates(map[string]interface{}{
			"telegram_chat": req.ChatID,
			"status":        proto.OperatorStatus_None,
		})
		if err != nil {
			log.Errorf("failed o detach old account on relink: %v", err)
			return errors.New(proto.DBError)
		}
	}

	oldChat := op.TelegramChat
	err := db.New().Model(&op).Updates(map[string]interface{}{
		"telegram_chat": req.ChatID,
		"lb_key":        req.Key.Public,
		"lb_secret":     req.Key.Secret,
	}).Error

	if err != nil {
		log.Errorf("failed to update operator: %v", err)
		return errors.New(proto.DBError)
	}

	if oldChat != req.ChatID {
		go func() {
			err := SendTelegramNotify(strconv.FormatInt(op.TelegramChat, 10), fmt.Sprintf(
				M("account %v was relinked to another telegram"), op.Username,
			), false)
			if err != nil {
				log.Errorf("failed to notify old telegram about relinked account: %v", err)
			}
		}()
	}
	return nil
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
		Destination:   req.Destination,
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
		"status": proto.OperatorStatus_Ready,
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
		if contact.Data.Currency == order.Currency && contact.Data.Amount.Equal(order.FiatAmount) {
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

	tx := db.NewTransaction()

	if order.OperatorID != 0 {
		var op Operator
		err = db.New().First(&op, "id = ?", order.OperatorID).Error
		if err != nil {
			tx.Rollback()
			log.Errorf("failed to load operator %v: %v", order.OperatorID, err)
			return false, errors.New(proto.DBError)
		}

		err = tx.Model(&op).Updates(map[string]interface{}{
			"status":        proto.OperatorStatus_Inactive,
			"current_order": 0,
		}).Error
		if err != nil {
			tx.Rollback()
			return false, errors.New(proto.DBError)
		}
	}

	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}
	err = tx.Commit().Error
	if err != nil {
		return false, errors.New(proto.DBError)
	}
	return true, nil
}

func MarkPayed(orderID uint64) (bool, error) {
	var order Order
	err := db.New().First(&order, "id = ?", orderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", orderID, err)
		return false, errors.New(proto.DBError)
	}
	if order.Status != proto.OrderStatus_Payment && order.Status != proto.OrderStatus_Confirmation {
		return false, errors.New("unexpected status")
	}
	order.Status = proto.OrderStatus_Confirmation
	err = order.Save(db.New())
	if err != nil {
		return false, errors.New(proto.DBError)
	}
	return true, nil
}

func ConfirmPayment(orderID uint64) (bool, error) {
	var order Order
	err := db.New().First(&order, "id = ?", orderID).Error
	if err != nil {
		log.Errorf("failed to load order %v: %v", orderID, err)
		return false, errors.New(proto.DBError)
	}
	if order.Status != proto.OrderStatus_Confirmation {
		return false, errors.New("unexpected status")
	}

	var op Operator
	err = db.New().First(&op, "id = ?", order.OperatorID).Error
	if err != nil {
		log.Errorf("failed to load operator %v: %v", order.OperatorID, err)
		return false, errors.New(proto.DBError)
	}

	tx := db.NewTransaction()
	// Amount to write-off from op deposit: contact_sum - lb_fee - op_fee
	amount := order.LBAmount.Sub(order.LBFee).Sub(order.OperatorFee)
	err = tx.Model(&op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Inactive,
		"current_order": 0,
		"deposit":       gorm.Expr("deposit - ?", amount),
	}).Error
	if err != nil {
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}
	// @TODO Transfer coins to client from bs buffer
	order.Status = proto.OrderStatus_Transfer
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return false, errors.New(proto.DBError)
	}
	err = SendTelegramNotify(conf.TelegramChanel, fmt.Sprintf(
		"order %v reached transfer status\ndestination: %v\noutlet amount: %v",
		order.ID, order.Destination, order.OutletAmount(),
	), true)
	if err != nil {
		tx.Rollback()
		return false, errors.New("notify failed")
	}
	err = tx.Commit().Error
	if err != nil {
		return false, errors.New(proto.DBError)
	}
	return true, nil
}
