package main

import (
	"common/log"
	"common/rabbit"
	core "core/proto"
	"telegram/proto"
	"time"
)

func init() {
	rabbit.Subscribe(rabbit.Subscription{
		Name:           "order_event",
		Routes:         []rabbit.Route{core.OrderEventRoute},
		AutoAck:        true,
		Prefetch:       10,
		DecodedHandler: OrderEventHandler,
	})

	rabbit.Subscribe(rabbit.Subscription{
		Name:           "offer_event",
		Routes:         []rabbit.Route{proto.OfferEventRoute},
		AutoAck:        true,
		Prefetch:       10,
		DecodedHandler: OfferEventHandler,
	})

	rabbit.Subscribe(
		rabbit.Subscription{
			Name:           "telegram_notify",
			Routes:         []rabbit.Route{proto.SendNotifyRoute},
			AutoAck:        false,
			Prefetch:       5,
			DecodedHandler: SendNotifyHandler,
		},
	)
}

func SendNotifyHandler(notify proto.SendNotifyMessage) bool {
	err := SendMessage(ChatDestination(notify.Destination), notify.Text, nil)
	if err != nil {
		log.Errorf("failed to send notify to %v: %v", notify.Destination, err)
		if notify.Reliable {
			time.Sleep(time.Second)
		}
		return !notify.Reliable
	}
	return true
}

func OfferEventHandler(e proto.OfferEvent) bool {
	log.Debug("offer event: %+v", e)
	for _, chat := range e.Chats {
		global.events <- event{
			ChatID: chat,
			Data:   e.Order,
		}
	}
	return true
}

func OrderEventHandler(order core.Order) bool {
	log.Debug("order event: %+v", order)
	if order.OperatorID == 0 {
		// Ignore such events
		return true
	}
	global.events <- event{
		OperatorID: order.OperatorID,
		Data:       order,
	}
	return true
}
