package main

import (
	"common/rabbit"
	"telegram/proto"
)

var SendOffer func(proto.SendOfferRequest) (bool, error)

func init() {
	rabbit.DeclareRPC(proto.SendOffer, &SendOffer)

	rabbit.AddPublishers(
		rabbit.Publisher{
			Name:       "telegram_notify",
			Routes:     []rabbit.Route{proto.SendNotifyRoute},
			Persistent: true,
			Confirm:    true,
		},
	)
}

func SendTelegramNotify(dest string, text string, reliable bool) error {
	return rabbit.Publish("telegram_notify", "", proto.SendNotifyMessage{
		Destination: dest,
		Text:        text,
		Reliable:    reliable,
	})
}
