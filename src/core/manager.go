package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"fmt"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	tg "telegram/proto"
	"time"
)

const AcceptTimeout = 3 * time.Minute

type orderManager struct {
	waiters   []Order
	orders    chan Order
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
	waiters:   make([]Order, 0),
	orders:    make(chan Order),
	operators: make(chan Operator),
	accepts:   make(chan accept),
}

func StartOrderManager() {
	go manager.loop()
}

func (man *orderManager) loop() {
	db.New().Find(&man.waiters, "status = ?", proto.OrderStatus_New)
	timer := time.NewTimer(time.Second)

	for {
		select {
		case <-timer.C:
			now := time.Now()
			for i, order := range man.waiters {
				if now.Sub(order.CreatedAt) < AcceptTimeout {
					man.waiters = man.waiters[i:]
				}
				err := RejectOrder(order)
				if err != nil {
					log.Errorf("failed to reject expired order: %v", err)
					man.waiters = man.waiters[i:]
					break
				}
			}
			if len(man.waiters) != 0 {
				timer.Reset(now.Sub(man.waiters[0].CreatedAt) + AcceptTimeout)
			}

		case order := <-man.orders:
			var ops []Operator
			err := db.New().Find(&ops, "status = ?", proto.OperatorStatus_Ready).Error
			if err != nil {
				log.Errorf("failed to load list of ready operators: %v", err)
				continue
			}
			for _, op := range ops {
				if op.Deposit.Cmp(order.LBAmount) >= 0 {
					err := OfferOrder(op, order)
					if err != nil {
						log.Errorf("failed to send offer to %v: %v", op.ID, err)
					}
				} else {
					go NotifyLackOfDeposit(op, order.LBAmount)
				}
			}
			man.waiters = append(man.waiters, order)
			if len(man.waiters) == 1 {
				timer.Stop()
				select {
				case <-timer.C:
				default:
				}
				timer.Reset(AcceptTimeout)
			}

		case op := <-man.operators:
			sended := false
			for _, order := range man.waiters {
				if op.Deposit.Cmp(order.LBAmount) > 0 {
					err := OfferOrder(op, order)
					if err != nil {
						log.Errorf("failed to send offer to %v: %v", op.ID, err)
					}
					sended = true
					break
				}
			}
			if !sended && len(man.waiters) != 0 {
				go NotifyLackOfDeposit(op, man.waiters[0].LBAmount)
			}

		case accept := <-man.accepts:
			man.acceptOrder(accept)
		}
	}
}

func (man *orderManager) PushOrder(order Order) {
	man.orders <- order
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
	var (
		i     int
		order Order
	)
	for i, order = range man.waiters {
		if order.ID == accept.orderID {
			break
		}
	}
	// Order was taken by someone else or rejected on timeout already
	if order.ID == 0 {
		accept.reply <- acceptReply{
			err: errors.New("order unaviable"),
		}
		return
	}

	var op Operator
	tx := db.NewTransaction()
	scope := tx.First(&op, "id = ?", accept.operatorID)
	if scope.RecordNotFound() {
		log.Errorf("unknown operator id %v in order accept", accept.operatorID)
		accept.reply <- acceptReply{
			err: errors.New("unknown operator"),
		}
		tx.Rollback()
		return
	}
	if scope.Error != nil {
		log.Errorf("failed to load operator %v: %v", accept.operatorID, scope.Error)
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		tx.Rollback()
		return
	}

	if op.Deposit.Cmp(order.LBAmount) < 0 {
		log.Errorf("operator %v tried to accept order %v but do not have enough on deposit", accept.operatorID, order.ID, op.Status)
		accept.reply <- acceptReply{
			err: errors.New("lack of deposit"),
		}
		tx.Rollback()
		return
	}

	if op.Status != proto.OperatorStatus_Proposal {
		log.Errorf("operator %v tried to accept order %v but had unexpected status %v", accept.operatorID, order.ID, op.Status)
		accept.reply <- acceptReply{
			err: errors.New("unexpected status"),
		}
		tx.Rollback()
		return
	}
	if op.CurrentOrder != accept.orderID {
		log.Errorf("operator %v tried to accept order %v while his current order was %v", accept.operatorID, order.ID, op.CurrentOrder)
		accept.reply <- acceptReply{
			err: errors.New("unexpected status"),
		}
		tx.Rollback()
		return
	}

	err := tx.Update(&op).Update("status", proto.OperatorStatus_Busy).Error
	if err != nil {
		log.Errorf("failed to save operator: %v", err)
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		tx.Rollback()
		return
	}

	order.OperatorID = op.ID
	order.Status = proto.OrderStatus_Accepted
	err = order.Save(tx)
	if err != nil {
		accept.reply <- acceptReply{
			err: errors.New("db error"),
		}
		tx.Rollback()
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
	man.waiters = append(man.waiters[:i], man.waiters[i+1:]...)
}

func OfferOrder(op Operator, order Order) error {
	_, err := SendOffer(tg.SendOfferRequest{
		ChatID: op.TelegramChat,
		Order:  order.Encode(),
	})
	if err != nil {
		return err
	}
	return db.New().Model(op).Updates(map[string]interface{}{
		"status":        proto.OperatorStatus_Proposal,
		"current_order": order.ID,
	}).Error
}

func RejectOrder(order Order) error {
	order.Status = proto.OrderStatus_Rejected
	tx := db.NewTransaction()
	err := order.Save(tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	var ops []Operator
	err = tx.Find(&ops, "current_order = ?", order.ID).Error
	if err != nil {
		tx.Rollback()
		return err
	}
	encoded := order.Encode()
	for _, op := range ops {
		err := tx.Model(&op).Updates(map[string]interface{}{
			"status":        proto.OperatorStatus_Ready,
			"current_order": 0,
		}).Error
		if err != nil {
			tx.Rollback()
			return err
		}
		go func() {
			_, err := OrderEvent(tg.OrderEventMessage{
				ChatID: op.TelegramChat,
				Order:  encoded,
			})
			if err != nil {
				log.Errorf("failed to perform cancel offer request: %v", err)
			}
		}()
	}
	return tx.Commit().Error
}

func NotifyLackOfDeposit(op Operator, required decimal.Decimal) {
	err := SendTelegramNotify(op.TelegramChat, fmt.Sprintf(
		M("order for an BTC amount %v was skipped due lack of your deposit(have %v)"), required, op.Deposit,
	))
	if err != nil {
		log.Errorf("failed to send lack of deposit notify: %v", err)
	}
}
