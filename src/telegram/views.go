package main

import (
	"common/log"
	"common/rabbit"
	"telegram/proto"
)

func init() {
	rabbit.ServeRPC(proto.SendOffer, SendOfferHandler)
	rabbit.ServeRPC(proto.OrderEvent, OrderEventHandler)

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

func OrderEventHandler(req proto.OrderEventMessage) (bool, error) {
	global.events <- event{
		ChatID: req.ChatID,
		Data:   req.Order,
	}
	return true, nil
}
