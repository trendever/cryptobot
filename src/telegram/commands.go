package main

import (
	"common/log"
	"github.com/tucnak/telebot"
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
}

func helpHandler(s *Session, _ *telebot.Message) {
	log.Error(SendMessage(s.Dest(), M("help text"), nil))
}
