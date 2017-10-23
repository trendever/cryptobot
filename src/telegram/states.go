package main

import (
	"common/log"
	"github.com/tucnak/telebot"
	"strconv"
)

type State int

const (
	State_Start State = iota
	State_Unknown
	State_ChangeKey
)

type StateActions struct {
	Enter   func(s *Session)
	Message func(s *Session, msg *telebot.Message)
	Exit    func(s *Session)
}

var states map[State]StateActions

func init() {
	// trick around initialization loop
	states = statesInit
}

// @TODO real error handling
var statesInit = map[State]StateActions{
	State_Start: {
		Enter: func(s *Session) {
			log.Error(global.bot.SendMessage(Dest(s.ChatID), M("greetings"), Keyboard(M("set_key"))))
		},
		Message: func(s *Session, msg *telebot.Message) {
			switch msg.Text {
			case M("set_key"):
				s.ChangeState(State_ChangeKey)
				return
			}
			log.Error(global.bot.SendMessage(Dest(s.ChatID), M("greetings"), Keyboard(M("set_key"))))
		},
	},
	State_ChangeKey: {
		Enter: func(s *Session) {
			log.Error(global.bot.SendMessage(Dest(s.ChatID), M("input_public_key"), Keyboard(M("cancel"))))
		},
		Message: func(s *Session, msg *telebot.Message) {
			if msg.Text == M("cancel") {
				// @TODO what if operator already have valid key and stated change by mistake?
				s.ChangeState(State_Start)
			}
			// we already have public key, so it's secret part now
			if s.context != nil {
				// @TODO tell it to core and stuff
				log.Debug("p: '%v', s: '%v'", s.context, msg.Text)
			} else {
				s.context = msg.Text
				log.Error(global.bot.SendMessage(Dest(s.ChatID), M("input_secter_key"), Keyboard(M("cancel"))))
			}
		},
	},
}

func M(key string) string {
	msg, ok := conf.Messages[key]
	if ok {
		return msg
	}
	log.Warn("message for key '%v' is undefined", key)
	return key
}

type chatDestination string

func (dest chatDestination) Destination() string {
	return string(dest)
}

func Dest(chatID int64) chatDestination {
	return chatDestination(strconv.FormatInt(chatID, 10))
}

func Keyboard(keys ...string) *telebot.SendOptions {
	ret := &telebot.SendOptions{}
	for _, button := range keys {
		ret.ReplyMarkup.CustomKeyboard = append(
			ret.ReplyMarkup.CustomKeyboard,
			[]string{button},
		)
	}
	ret.ReplyMarkup.ResizeKeyboard = true
	ret.ReplyMarkup.OneTimeKeyboard = true
	return ret
}
