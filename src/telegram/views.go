package main

import (
	"common/log"
	"common/rabbit"
	core "core/proto"
	"telegram/proto"
)

func init() {
	rabbit.ServeRPC(proto.SendOffer, SendOfferHandler)
	rabbit.Subscribe(rabbit.Subscription{
		Name:           "order_event",
		Routes:         []rabbit.Route{core.OrderEventRoute},
		AutoAck:        true,
		Prefetch:       10,
		DecodedHandler: OrderEventHandler,
	})

	rabbit.Subscribe(
		rabbit.Subscription{
			Name:           "telegram_notify",
			Routes:         []rabbit.Route{proto.SendNotifyRoute},
			AutoAck:        true,
			Prefetch:       10,
			DecodedHandler: SendNotifyHandler,
		},
	)
}

func SendNotifyHandler(notify proto.SendNotifyMessage) bool {
	log.Error(SendMessage(Dest(notify.ChatID), notify.Text, nil))
	return true
}

func SendOfferHandler(req proto.SendOfferRequest) (bool, error) {
	global.events <- event{
		ChatID: req.ChatID,
		Data:   req.Order,
	}
	return true, nil
}

func OrderEventHandler(order core.Order) bool {
	log.Debug("order event: %+v", order)
	global.events <- event{
		OperatorID: order.OperatorID,
		Data:       order,
	}
	return true
}
