package main

import (
	"common/log"
	"core/proto"
	"fmt"
	"github.com/tucnak/telebot"
	"time"
)

type MessageHandler func(s *Session, msg *telebot.Message)
type EventHandler func(s *Session, event interface{})

// List of global commands which are not state-related
var commands = map[string]MessageHandler{}

func AddCommand(command string, handler MessageHandler) {
	_, ok := commands[command]
	if ok {
		log.Warn("commad '%v' is already registed, replacing")
	}
	commands[command] = handler
}

func init() {
	AddCommand("/help", helpHandler)
	AddCommand("/deposit", depositHandler)
	AddCommand("/reload", reloadHandler)
}

func helpHandler(s *Session, _ *telebot.Message) {
	log.Error(SendMessage(s.Dest(), M("help text"), nil))
}

func depositHandler(s *Session, _ *telebot.Message) {
	if s.Operator.ID == 0 {
		log.Error(SendMessage(s.Dest(), M("related account not fould"), nil))
		return
	}
	addr, err := GetDepositRefillAddress(s.Operator.ID)
	if err != nil {
		s.ChangeState(State_Unavailable)
	}
	op, err := OperatorByID(s.Operator.ID)
	if err != nil {
		s.ChangeState(State_Unavailable)
	}
	s.Operator = op
	log.Error(SendMessage(
		s.Dest(),
		fmt.Sprintf(
			"current deposit: %v\n"+
				"for replenishment send localbitcoin trasfer to address %v with comment '%v%v'",
			op.Deposit, addr, proto.DepositTransactionPrefix, op.ID),
		nil),
	)
}

// Timeout between actual attempts to reload session
const ReloadTimeout = 3 * time.Second

func reloadHandler(s *Session, _ *telebot.Message) {
	now := time.Now()
	if s.context != nil && s.State == State_Unavailable {
		lastTry := s.context.(time.Time)
		// there is no point to try too frequently
		if now.Sub(lastTry) < ReloadTimeout {
			// idk, do we need to send something?
			return
		}
	}
	err := s.Reload()
	if err != nil {
		s.ChangeState(State_Unavailable)
		s.context = now
	}
}
