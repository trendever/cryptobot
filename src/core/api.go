package main

import (
	"common/rabbit"
	"telegram/proto"
)

func init() {
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