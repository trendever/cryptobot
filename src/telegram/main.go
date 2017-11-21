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

const DiscardMessageTimeout = time.Hour * 24

var conf struct {
	Token  string
	Rabbit rabbit.Config

	Debug     bool
	SentryDSN string

	Messages map[string]string
}

type event struct {
	ChatID int64
	Data   interface{}
}

var global = struct {
	bot       *telebot.Bot
	stopper   *stopper.Stopper
	waitGroup sync.WaitGroup
	events    chan event
	sessions  map[int64]*Session
}{
	stopper:  stopper.NewStopper(),
	events:   make(chan event, 10),
	sessions: make(map[int64]*Session),
}

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

	// there will be no way to get message again later(telegram do not have such api) in case of any troubles or just a shutdown
	// @TODO save all messages or something else?
	for {
		select {
		case <-global.stopper.Chan():
			for _, s := range global.sessions {
				s.Stop()
			}
			global.waitGroup.Done()
			return
		case message := <-messages:
			if time.Now().Sub(message.Time()) > DiscardMessageTimeout {
				log.Info("message from chat %v discarded due expiration", message.ID)
				continue
			}
			log.Debug(
				"got message from chat %v(%v):\n%v",
				message.Chat.ID, message.Chat.Destination(),
				log.IndentEncode(message),
			)
			if !message.IsPersonal() {
				continue
			}

			session := getSession(message.Chat.ID, true)
			if session != nil {
				session.PushMessage(message)
			}
		case event := <-global.events:
			session := getSession(event.ChatID, true)
			if session != nil {
				session.PushEvent(event.Data)
			}
		}
	}
}

func getSession(chatID int64, notifyError bool) *Session {
	session, ok := global.sessions[chatID]
	if !ok {
		var err error
		session, err = LoadSession(chatID)
		if err != nil {
			log.Errorf("failed to load session for chat %v: %v", chatID, err)
			if notifyError {
				log.Error(SendMessage(Dest(chatID), M("service unavailable"), nil))
			}
			return nil
		}
		// @TODO unload session on timeout?
		global.sessions[chatID] = session
	}
	return session
}
