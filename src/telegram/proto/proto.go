package proto

import (
	"common/rabbit"
	core "core/proto"
)

type SendNotifyMessage struct {
	ChatID int64
	Text   string
}

var SendNotifyRoute = rabbit.Route{
	{
		Node: rabbit.Exchange{
			Name:    "telegram_notify",
			Kind:    "fanout",
			Durable: true,
		},
	},
	{
		Keys: []string{""},
		Node: rabbit.Queue{
			Name:    "telegram_notify",
			Durable: true,
		},
	},
}

type SendOfferRequest struct {
	ChatID int64
	Order  core.Order
}

var SendOffer = rabbit.RPC{
	Name:        "send_offer",
	HandlerType: (func(SendOfferRequest) (bool, error))(nil),
}
