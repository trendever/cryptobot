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

const DiscardMessageTimeout = time.Hour * 24

var conf struct {
	Token  string
	Rabbit rabbit.Config

	Debug     bool
	SentryDSN string

	Messages map[string]string
}

type event struct {
	ChatID     int64
	OperatorID uint64
	Data       interface{}
}

var global = struct {
	bot       *telebot.Bot
	stopper   *stopper.Stopper
	waitGroup sync.WaitGroup
	events    chan event
	sessions  map[int64]*Session
	// Operator id -> chat id for loaded sessions
	opMap map[uint64]int64
}{
	stopper:  stopper.NewStopper(),
	events:   make(chan event, 10),
	sessions: make(map[int64]*Session),
	opMap:    make(map[uint64]int64),
}

var SendMessage func(recipient telebot.Recipient, message string, options *telebot.SendOptions) error

func main() {
	cli.Main(service{})
}

type service struct{}

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
				"got message from chat %v(%v):\n%+v",
				message.Chat.ID, message.Chat.Destination(),
				message,
			)
			if !message.IsPersonal() {
				continue
			}

			session := getSession(message.Chat.ID, true)
			if session != nil {
				session.PushMessage(message)
			}
		case event := <-global.events:
			var session *Session
			switch {
			case event.OperatorID != 0:
				session = sessionByOp(event.OperatorID, false)
			case event.ChatID != 0:
				session = getSession(event.ChatID, true)
			default:
				log.Errorf("got event with operator and chat id both zero")
			}
			if session != nil {
				session.PushEvent(event.Data)
			}
		}
	}
}

func sessionByOp(operatorID uint64, notifyError bool) *Session {
	chatID, ok := global.opMap[operatorID]
	if ok {
		return getSession(chatID, notifyError)
	}
	session, err := LoadSessionForOperator(operatorID)
	if err != nil {
		log.Errorf("failed to load session for operator %v: %v", operatorID, err)
		return nil
	}
	chatID = session.Operator.TelegramChat
	global.sessions[chatID] = session
	if session.Operator.ID != 0 {
		global.opMap[session.Operator.ID] = chatID
	}
	return session
}

func getSession(chatID int64, notifyError bool) *Session {
	session, ok := global.sessions[chatID]
	if !ok {
		var err error
		session, err = LoadSessionForChat(chatID)
		if err != nil {
			log.Errorf("failed to load session for chat %v: %v", chatID, err)
			if notifyError {
				log.Error(SendMessage(Dest(chatID), M("service unavailable"), nil))
			}
			return nil
		}
		// @TODO unload session on timeout?
		global.sessions[chatID] = session
		if session.Operator.ID != 0 {
			global.opMap[session.Operator.ID] = chatID
		}
	}
	return session
}
