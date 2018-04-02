package main

import (
	"common/db"
	"common/log"
	"common/rabbit"
	"core/proto"
	"fmt"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"strconv"
	tg "telegram/proto"
	"time"
)

func init() {
	rabbit.AddPublishers(rabbit.Publisher{
		Name:    "offer_event",
		Routes:  []rabbit.Route{tg.OfferEventRoute},
		Confirm: true,
	})
}

type orderManager struct {
	orders    chan orderPush
	operators chan opPush
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

type opPush struct {
	id uint64
	// recheck already skipped orders as well
	anew bool
}

type orderPush struct {
	id uint64
	// notify lack of deposit
	notify bool
}

var manager = orderManager{
	orders:    make(chan orderPush),
	operators: make(chan opPush),
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
			case manager.orders <- orderPush{
				id:     order.ID,
				notify: false,
			}:

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

		case push := <-man.orders:
			man.onOrderReceive(push)

		case push := <-man.operators:
			man.onOperatorReceive(push)

		case accept := <-man.accepts:
			man.acceptOrder(accept)
		}
	}
}

func (man *orderManager) onOperatorReceive(push opPush) {
	requeue := func(push opPush) {
		timer := time.NewTimer(conf.OrderTimeouts.Accept)
		time.Sleep(5 * time.Second)
		select {
		case <-timer.C:
			return
		case manager.operators <- push:

		}
	}

	tx := db.NewTransaction()
	op, err := LockLoadOperatorByID(tx, push.id)
	switch {
	case err == nil:

	case err.Error() == "record not found":
		return

	default:
		tx.Rollback()
		log.Errorf("failed to load operator: %v", err)
		go requeue(push)
		return
	}

	var orders []Order
	scope := tx.Order("id").Where("status = ?", proto.OrderStatus_New)
	if !push.anew {
		scope = scope.Where("id > ?", op.CurrentOrder)
	}
	err = scope.Find(&orders).Error
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to load list of orders: %v", err)
		go requeue(push)
		return
	}

	if len(orders) == 0 {
		tx.Commit()
		return
	}

	for _, order := range orders {
		if op.Deposit.Cmp(order.LBAmount) > 0 {
			op.CurrentOrder = order.ID
			op.Status = proto.OperatorStatus_Proposal
			err := op.Save(tx)
			if err != nil {
				tx.Rollback()
				log.Errorf("failed to save operator: %v", err)
				go requeue(push)
				return
			}

			err = rabbit.Publish("offer_event", "", tg.OfferEvent{
				Chats: []int64{op.TelegramChat},
				Order: order.Encode(),
			})
			if err != nil {
				tx.Rollback()
				log.Errorf("failed to send offer to %v: %v", op.ID, err)
				go requeue(push)
				return
			}

			err = tx.Commit().Error
			if err != nil {
				log.Errorf("failed to commit in onOperatorReceive: %v", err)
				go requeue(push)
				return
			}
			return
		} else {
			// @TODO it can be kinda spammy. Combine notifies?
			go NotifyLackOfDeposit(op.TelegramChat, order.LBAmount)
		}
	}

	op.CurrentOrder = orders[len(orders)-1].ID
	err = op.Save(tx)
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to save operator: %v", err)
		go requeue(push)
		return
	}

	tx.Commit()
}

func (man *orderManager) onOrderReceive(push orderPush) {
	requeue := func(id uint64, notify bool) {
		timer := time.NewTimer(conf.OrderTimeouts.Accept)
		time.Sleep(5 * time.Second)
		select {
		case <-timer.C:
			return
		case manager.orders <- orderPush{
			id:     id,
			notify: notify,
		}:

		}
	}

	tx := db.NewTransaction()

	order, err := LockLoadOrderByID(tx, push.id)
	switch {
	case err == nil:

	case err.Error() == "record not found":
		tx.Rollback()
		return

	default:
		tx.Rollback()
		log.Errorf("failed to load order: %v", err)
		go requeue(push.id, true)
		return
	}

	if order.Status != proto.OrderStatus_New {
		tx.Rollback()
		return
	}

	var ops []Operator
	err = tx.Set("gorm:query_option", "FOR UPDATE").
		Find(&ops, "status = ? AND current_order < ?", proto.OperatorStatus_Ready, push.id).Error
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to load list of ready operators: %v", err)
		go requeue(push.id, true)
		return
	}

	var offer_ids, lack_ids []uint64
	var offer_chats, lack_chats []int64
	for _, op := range ops {
		if op.Deposit.Cmp(order.LBAmount) >= 0 {
			offer_ids = append(offer_ids, op.ID)
			offer_chats = append(offer_chats, op.TelegramChat)
		} else if push.notify {
			lack_ids = append(lack_ids, op.ID)
			lack_chats = append(lack_chats, op.TelegramChat)
		}
	}

	err = tx.Model(&Operator{}).Where("id in (?)", offer_ids).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Proposal,
		"current_order": order.ID,
	}).Error
	if err != nil {
		tx.Rollback()
		log.Errorf("failed update operators: %v", err)
		go requeue(push.id, true)
		return
	}

	err = tx.Model(&Operator{}).Where("id in (?)", lack_ids).Updates(map[string]interface{}{
		"current_order": order.ID,
	}).Error
	if err != nil {
		tx.Rollback()
		log.Errorf("failed update operators: %v", err)
		go requeue(push.id, true)
		return
	}

	if len(offer_ids) == 0 {
		tx.Commit()
		go requeue(push.id, false)
		go func() {
			for _, chat := range lack_chats {
				NotifyLackOfDeposit(chat, order.LBAmount)
			}
		}()
		return
	}

	err = rabbit.Publish("offer_event", "", tg.OfferEvent{
		Chats: offer_chats,
		Order: order.Encode(),
	})
	if err != nil {
		tx.Rollback()
		log.Errorf("failed to send offer event: %v", err)
		go requeue(push.id, false)
		return
	}

	err = tx.Commit().Error
	if err != nil {
		log.Errorf("failed update operators: %v", err)
		go requeue(push.id, false)
		return
	}
}

func (man *orderManager) tickUpdate() {
	var orders []Order
	now := time.Now()
	touts := conf.OrderTimeouts

	err := db.New().
		Or("status = ? and created_at < ?", proto.OrderStatus_New, now.Add(-touts.Accept)).
		Or("status = ? and payment_requested_at < ?", proto.OrderStatus_Payment, now.Add(-touts.Payment)).
		Or("status = ? and marked_payed_at < ?", proto.OrderStatus_Confirmation, now.Add(-touts.Confirm)).
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
	man.orders <- orderPush{
		id:     orderID,
		notify: true,
	}
}

func (man *orderManager) PushOperator(opID uint64, anew bool) {
	man.operators <- opPush{
		id:   opID,
		anew: anew,
	}
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
			err: errors.New(proto.DBError),
		}
		return
	}

	var ops []Operator
	err = tx.Set("gorm:query_option", "FOR UPDATE").
		Where("status = ? AND current_order = ? AND id != ?", proto.OperatorStatus_Proposal, order.ID, op.ID).
		Find(&ops).Error
	if err != nil {
		tx.Rollback()
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		log.Errorf("failed to load related operators: %v", err)
		return
	}
	var ids []uint64
	var chats []int64
	for _, op := range ops {
		ids = append(ids, op.ID)
		chats = append(chats, op.TelegramChat)
	}

	err = tx.Model(&Operator{}).Where("id in (?)", ids).Updates(map[string]interface{}{
		"status": proto.OperatorStatus_Ready,
	}).Error
	if err != nil {
		tx.Rollback()
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		log.Errorf("failed to withdraw order from operators: %v", err)
		return
	}

	order.OperatorID = op.ID
	order.Status = proto.OrderStatus_Accepted
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		return
	}

	err = rabbit.Publish("offer_event", "", tg.OfferEvent{
		Chats: chats,
		Order: order.Encode(),
	})
	if err != nil {
		log.Errorf("failed to send reject offer event: %v", err)
	}

	err = tx.Commit().Error
	if err != nil {
		log.Errorf("failed to commit: %v", err)
		accept.reply <- acceptReply{
			err: errors.New(proto.DBError),
		}
		return
	}

	accept.reply <- acceptReply{
		order: order,
	}
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
	err = tx.Set("gorm:query_option", "FOR UPDATE").
		Find(&ops, "status = ? AND current_order = ?", proto.OperatorStatus_Proposal, order.ID).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load related operators: %v", err)
	}

	var ids []uint64
	var chats []int64
	for _, op := range ops {
		ids = append(ids, op.ID)
		chats = append(chats, op.TelegramChat)
	}

	err = tx.Model(&Operator{}).Where("id in (?)", ids).Updates(map[string]interface{}{
		"status": proto.OperatorStatus_Ready,
	}).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to withdraw order from operators: %v", err)
	}

	order.Status = proto.OrderStatus_Rejected
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save order: %v", err)
	}

	err = rabbit.Publish("offer_event", "", tg.OfferEvent{
		Chats: chats,
		Order: order.Encode(),
	})
	if err != nil {
		log.Errorf("failed to send reject offer event: %v", err)
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
	err = order.Save(tx)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to save order: %v", err)
	}
	return tx.Commit().Error
}

func NotifyLackOfDeposit(chat int64, required decimal.Decimal) {
	err := SendTelegramNotify(strconv.FormatInt(chat, 10), fmt.Sprintf(
		M("order for an BTC amount %v was skipped due lack of your deposit"), required,
	), false)
	if err != nil {
		log.Errorf("failed to send lack of deposit notify: %v", err)
	}
}
