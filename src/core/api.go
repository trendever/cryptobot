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

func SendTelegramNotify(chatID int64, text string) error {
	return rabbit.Publish("telegram_notify", "", proto.SendNotifyMessage{
		ChatID: chatID,
		Text:   text,
	})
}
