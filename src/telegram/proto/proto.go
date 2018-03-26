package proto

import (
	"common/rabbit"
	core "core/proto"
)

type SendNotifyMessage struct {
	Destination string
	Text        string
	// If true message will be resend later in case of any errors
	Reliable bool
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

type OfferEvent struct {
	Chats []int64
	Order core.Order
}

var OfferEventRoute = rabbit.Route{
	{
		Node: rabbit.Exchange{
			Name:    "offer_event",
			Kind:    "fanout",
			Durable: true,
		},
	},
	{
		Keys: []string{""},
		Node: rabbit.Queue{
			Name:    "offer_event",
			Durable: true,
		},
	},
}
