package main

import (
	"common/cli"
	"common/config"
	"common/log"
	"common/rabbit"
	"common/stopper"
	"github.com/tucnak/telebot"
	"sync"
	"time"
)

type service struct{}

var conf struct {
	Token  string
	Rabbit rabbit.Config

	Debug     bool
	SentryDSN string

	Messages map[string]string
}

var global = struct {
	bot       *telebot.Bot
	stopper   *stopper.Stopper
	waitGroup sync.WaitGroup
}{stopper: stopper.NewStopper()}

var SendMessage func(recipient telebot.Recipient, message string, options *telebot.SendOptions) error

func main() {
	cli.Main(service{})
}

func (srv service) Load() {
	log.Fatal(config.LoadStruct("telegram", &conf))
	log.Init(conf.Debug, "telegram", conf.SentryDSN)
	log.Debug("config:\n%v", log.IndentEncode(conf))
}

func (srv service) Migrate(bool) {
	srv.Load()
	log.Info("nothing to migrate here, really")
}

func (srv service) Start() {
	srv.Load()
	var err error
	global.bot, err = telebot.NewBot(conf.Token)
	log.Fatal(err)
	SendMessage = global.bot.SendMessage
	rabbit.Start(&conf.Rabbit)
	global.waitGroup.Add(1)
	go Listen()
}

func (srv service) Cleanup() {
	global.stopper.Stop()
	rabbit.Stop()
	log.Info("Shuting service down...")
	global.waitGroup.Wait()
	log.Info("Service is stopped")
}

func Listen() {
	messages := make(chan telebot.Message, 20)
	global.bot.Listen(messages, 1*time.Second)

	sessions := map[int64]*Session{}

	// there will be no way to get message again later(telegram do not have such api) in case of any troubles or just a shutdown
	// @TODO save all messages or something else?
	for {
		select {
		case <-global.stopper.Chan():
			global.waitGroup.Done()
			return
		case message := <-messages:
			log.Debug(
				"got message from chat %v(%v):\n%v",
				message.Chat.ID, message.Chat.Destination(),
				log.IndentEncode(message),
			)
			if !message.IsPersonal() {
				continue
			}

			session, ok := sessions[message.Chat.ID]
			if !ok {
				var err error
				session, err = LoadSession(message.Chat.ID)
				if err != nil {
					log.Errorf("failed to load session for chat %v: %v", message.Chat.ID, err)
					log.Error(SendMessage(Dest(message.Chat.ID), M("service unavailable"), nil))
					continue
				}
				// @TODO unload session on timeout?
				sessions[message.Chat.ID] = session
			}
			session.PushMessage(message)
		}
	}
}
