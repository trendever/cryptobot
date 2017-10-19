package main

import (
	"common/cli"
	"common/config"
	"common/log"
	"github.com/tucnak/telebot"
	"time"
)

type service struct{}

var conf struct {
	Token string

	Debug     bool
	SentryDSN string
}

func main() {
	cli.Main(service{})
}

func (srv service) Load() {
	log.Fatal(config.LoadStruct("telegram", &conf))
	log.Init(conf.Debug, "telegram", conf.SentryDSN)
}

func (srv service) Migrate(bool) {
	srv.Load()
	log.Info("nothing to migrate here, really")
}

func (srv service) Start() {
	srv.Load()
	bot, err := telebot.NewBot(conf.Token)
	log.Fatal(err)
	Listen(bot)
}

func (srv service) Cleanup() {}

func Listen(bot *telebot.Bot) {
	messages := make(chan telebot.Message, 100)
	bot.Listen(messages, 1*time.Second)

	for message := range messages {
		log.Debug(
			"got message from chat %v(%v):\n%v",
			message.Chat.ID, message.Chat.Destination(),
			message.Text,
		)
		if !message.IsPersonal() {
			continue
		}
	}
}
