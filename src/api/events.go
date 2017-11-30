package main

import (
	"common/log"
	"common/rabbit"
	"common/soso"
	"core/proto"
)

func init() {
	rabbit.Subscribe(rabbit.Subscription{
		Name:           "order_event",
		Routes:         []rabbit.Route{proto.OrderEventRoute},
		AutoAck:        true,
		Prefetch:       10,
		DecodedHandler: OrderEventHandler,
	})
}

func OrderEventHandler(order proto.Order) bool {
	log.Debug("order event: %+v", order)
	ctx := soso.NewRemoteContext("order", "event", map[string]interface{}{
		"order": order,
	})
	sess := soso.Sessions.Get(IDForAddress(order.Destination))
	for _, ses := range sess {
		ctx.Session = ses
		ctx.SendResponse()
	}
	return true
}
