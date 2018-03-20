package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"fmt"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"strconv"
	tg "telegram/proto"
	"time"
)

type orderManager struct {
	orders    chan uint64
	operators chan Operator
	accepts   chan accept
}

type acceptReply struct {
	order Order
	err   error
}

type accept struct {
	orderID    uint64
	operatorID uint64
	reply      chan acceptReply
}

var manager = orderManager{
	orders:    make(chan uint64),
	operators: make(chan Operator),
	accepts:   make(chan accept),
}

func StartOrderManager() {
	// load list of new orders and push it to manager
	go func() {
		var orders []Order
		err := db.New().Find(&orders, "status = ?", proto.OrderStatus_New).Error
		if err != nil {
			log.Errorf("failed to load list of orders: %v", err)
			return
		}
		timer := time.NewTimer(conf.OrderTimeouts.Accept)
		for _, order := range orders {
			select {
			case <-timer.C:
				return
			case manager.orders <- order.ID:

			}
		}
	}()
	go manager.loop()
}

func (man *orderManager) loop() {
	ticker := time.NewTicker(conf.OrdersUpdateTick)

	for {
		select {
		case <-ticker.C:
			man.tickUpdate()

		case orderID := <-man.orders:
			man.onOrderReceive(orderID)

		case op := <-man.operators:
			var orders []Order
			err := db.New().Find(&orders, "status = ?", proto.OrderStatus_New).Error
			if err != nil {
				log.Errorf("failed to load list of orders: %v", err)
				continue
			}
			var lacked decimal.Decimal
			for _, order := range orders {
				if op.Deposit.Cmp(order.LBAmount) > 0 {
					// This order was skipped by operator already
					if op.CurrentOrder >= order.ID {
						continue
					}
					err := OfferOrder(op, order)
					if err != nil {
						log.Errorf("failed to send offer to %v: %v", op.ID, err)
					}
					break
				} else {
					lacked = order.LBAmount
				}
			}
			if lacked.Sign() > 0 {
				go NotifyLackOfDeposit(op, lacked)
			}

		case accept := <-man.accepts:
			man.acceptOrder(accept)
		}
	}
}

func (man *orderManager) onOrderReceive(orderID uint64) {
	requeue := func(orderID uint64) {
		timer := time.NewTimer(conf.OrderTimeouts.Accept)
		time.Sleep(5 * time.Second)
		select {
		case <-timer.C:
			return
		case manager.orders <- orderID:

		}
	}

	var order Order
	err := db.New().First(&order, orderID).Error
	switch {
	case err == nil:

	case err.Error() == "record not found":
		return

	default:
		log.Errorf("failed to load order: %v", err)
		go requeue(orderID)
	}

	if order.Status != proto.OrderStatus_New {
		return
	}

	var ops []Operator
	err = db.New().Find(&ops, "status = ?", proto.OperatorStatus_Ready).Error
	if err != nil {
		log.Errorf("failed to load list of ready operators: %v", err)
		return
	}

	for _, op := range ops {
		if op.Deposit.Cmp(order.LBAmount) >= 0 {
			err := OfferOrder(op, order)
			if err != nil {
				log.Errorf("failed to send offer to %v: %v", op.ID, err)
				go requeue(orderID)
			}
		} else {
			go NotifyLackOfDeposit(op, order.LBAmount)
		}
	}
}

func (man *orderManager) tickUpdate() {
	var orders []Order
	now := time.Now()
	touts := conf.OrderTimeouts

	err := db.New().
		Or("status = ? and created_at < ?", proto.OrderStatus_New, now.Truncate(touts.Accept)).
		Or("status = ? and payment_requested_at < ?", proto.OrderStatus_Payment, now.Truncate(touts.Payment)).
		Or("status = ? and marked_payed_at < ?", proto.OrderStatus_Confirmation, now.Truncate(touts.Confirm)).
		Find(&orders).Error

	if err != nil {
		log.Errorf("failed to find orders for update: %v", err)
		return
	}

	for _, order := range orders {
		switch order.Status {
		case proto.OrderStatus_New:
			err := rejectOrder(order.ID)
			if err != nil {
				log.Errorf("failed to reject order %v: %v", order.ID, err)
			}

		case proto.OrderStatus_Payment:
			err := timeoutOrder(order.ID)
			if err != nil {
				log.Errorf("failed to set order %v status to timeout: %v", order.ID, err)
			}

		case proto.OrderStatus_Confirmation:
			err := extendConfirmation(order.ID)
			if err != nil {
				log.Errorf("failed to set order %v status to confirmation extended: %v", order.ID, err)
			}
		default:
			log.Fatalf("unreachable point")
		}
	}
}

func (man *orderManager) PushOrder(orderID uint64) {
	man.orders <- orderID
}

func (man *orderManager) PushOperator(op Operator) {
	man.operators <- op
}

func (man *orderManager) AcceptOffer(operatorID, orderID uint64) (Order, error) {
	reply := make(chan acceptReply)
	man.accepts <- accept{
		operatorID: operatorID,
		orderID:    orderID,
		reply:      reply,
	}
	ret := <-reply
	return ret.order, ret.err
}

func (man *orderManager) acceptOrder(accept accept) {
	tx := db.NewTransaction()
	order, err := LockLoadOrderByID(tx, accept.orderID)
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to load order: %v", err)
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		return
	}

	if order.Status != proto.OrderStatus_New {
		tx.Rollback()
		accept.reply <- acceptReply{
			err: errors.New("order unaviable"),
		}
		return
	}

	op, err := LockLoadOperatorByID(tx, accept.operatorID)
	switch {
	case err == nil:

	case err.Error() == "record not found":
		tx.Rollback()
		log.Errorf("unknown operator id %v in order accept", accept.operatorID)
		accept.reply <- acceptReply{
			err: errors.New("unknown operator"),
		}
		return

	default:
		tx.Rollback()
		log.Errorf("failed to load operator %v: %v", accept.operatorID, err)
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		return
	}

	if op.Deposit.Cmp(order.LBAmount) < 0 {
		tx.Rollback()
		log.Errorf("operator %v tried to accept order %v but do not have enough on deposit", accept.operatorID, order.ID)
		accept.reply <- acceptReply{
			err: errors.New("lack of deposit"),
		}
		return
	}

	if op.Status != proto.OperatorStatus_Proposal {
		tx.Rollback()
		log.Errorf("operator %v tried to accept order %v but had unexpected status %v", accept.operatorID, order.ID, op.Status)
		accept.reply <- acceptReply{
			err: errors.New("unexpected status"),
		}
		return
	}

	if op.CurrentOrder != accept.orderID {
		tx.Rollback()
		log.Errorf("operator %v tried to accept order %v while his current order was %v", accept.operatorID, order.ID, op.CurrentOrder)
		accept.reply <- acceptReply{
			err: errors.New("unexpected status"),
		}
		return
	}

	err = tx.Model(&op).Update("status", proto.OperatorStatus_Busy).Error
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to save operator: %v", err)
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		return
	}

	order.OperatorID = op.ID
	order.Status = proto.OrderStatus_Accepted
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		return
	}

	err = tx.Commit().Error
	if err != nil {
		log.Errorf("failed to commit: %v", err)
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		return
	}

	accept.reply <- acceptReply{
		order: order,
	}
}

func OfferOrder(op Operator, order Order) error {
	tx := db.NewTransaction()
	err := op.LockLoad(tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	if op.Status != proto.OperatorStatus_Ready {
		tx.Rollback()
		return nil
	}

	_, err = SendOffer(tg.SendOfferRequest{
		ChatID: op.TelegramChat,
		Order:  order.Encode(),
	})
	if err != nil {
		tx.Rollback()
		return err
	}
	err = tx.Model(op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Proposal,
		"current_order": order.ID,
	}).Error
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

func rejectOrder(orderID uint64) error {
	tx := db.NewTransaction()

	order, err := LockLoadOrderByID(tx, orderID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load order: %v", err)
	}
	if order.Status != proto.OrderStatus_New {
		tx.Commit()
		return nil
	}

	var ops []Operator
	err = tx.Set("gorm:query_option", "FOR UPDATE").Find(&ops, "current_order = ?", order.ID).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load related operators: %v", err)
	}
	for _, op := range ops {
		err := tx.Model(&op).Updates(map[string]interface{}{
			"status":        proto.OperatorStatus_Ready,
			"current_order": 0,
		}).Error
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to withdraw order from operator: %v", err)
		}
	}

	order.Status = proto.OrderStatus_Rejected
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save order: %v", err)
	}
	return tx.Commit().Error
}

func timeoutOrder(orderID uint64) error {
	tx := db.NewTransaction()

	order, err := LockLoadOrderByID(tx, orderID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load order: %v", err)
	}
	if order.Status != proto.OrderStatus_Payment {
		tx.Commit()
		return nil
	}

	op, err := LockLoadOperatorByID(tx, order.OperatorID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load operator: %v", err)
	}
	err = tx.Model(&op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Ready,
		"current_order": 0,
	}).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to withdraw order from operator: %v", err)
	}

	order.Status = proto.OrderStatus_Timeout
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save order: %v", err)
	}
	return tx.Commit().Error
}

func extendConfirmation(orderID uint64) error {
	tx := db.NewTransaction()

	order, err := LockLoadOrderByID(tx, orderID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load order: %v", err)
	}
	if order.Status != proto.OrderStatus_Confirmation {
		tx.Commit()
		return nil
	}

	order.Status = proto.OrderStatus_ConfirmationExtended
	order.Status = proto.OrderStatus_Timeout
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save order: %v", err)
	}
	return tx.Commit().Error
}

func NotifyLackOfDeposit(op Operator, required decimal.Decimal) {
	err := SendTelegramNotify(strconv.FormatInt(op.TelegramChat, 10), fmt.Sprintf(
		M("order for an BTC amount %v was skipped due lack of your deposit(have %v)"), required, op.Deposit,
	), false)
	if err != nil {
		log.Errorf("failed to send lack of deposit notify: %v", err)
	}
}
