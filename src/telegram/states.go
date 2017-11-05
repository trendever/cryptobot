package main

import (
	"common/log"
	"fmt"
	"github.com/tucnak/telebot"
	"lbapi"
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
			log.Error(SendMessage(Dest(s.ChatID), M("greetings"), Keyboard(M("set_key"))))
		},
		Message: func(s *Session, msg *telebot.Message) {
			switch msg.Text {
			case M("set_key"):
				s.ChangeState(State_ChangeKey)
				return
			}
			log.Error(SendMessage(Dest(s.ChatID), M("greetings"), Keyboard(M("set_key"))))
		},
	},
	State_ChangeKey: {
		Enter: func(s *Session) {
			log.Error(SendMessage(Dest(s.ChatID), M("input_public_key"), Keyboard(M("cancel"))))
		},
		Message: changeKey,
	},
}

func changeKey(s *Session, msg *telebot.Message) {
	if msg.Text == M("cancel") {
		// @TODO what if operator already have valid key and stated change by mistake?
		s.ChangeState(State_Start)
		return
	}
	if s.context == nil {
		key := lbapi.Key{
			Public: msg.Text,
		}
		ok, _ := key.IsValid()
		if !ok {
			log.Error(SendMessage(Dest(s.ChatID), M("invalid_key"), Keyboard(M("cancel"))))
			return
		}
		s.context = key
		log.Error(SendMessage(Dest(s.ChatID), M("input_secret_key"), Keyboard(M("cancel"))))
	} else { // We have public key already, so it's secret part now.
		key := s.context.(lbapi.Key)
		key.Secret = msg.Text
		_, ok := key.IsValid()
		if !ok {
			log.Error(SendMessage(Dest(s.ChatID), M("invalid_key"), Keyboard(M("cancel"))))
			return
		}
		op, err := CheckKey(key)
		// @TODO Kind of error
		if err != nil {
			log.Error(SendMessage(Dest(s.ChatID), fmt.Sprintf(M("check_key_falied: %v"), err), nil))
			s.ChangeState(State_Start)
			return
		}
		log.Error(SendMessage(Dest(s.ChatID), fmt.Sprintf(M("check_key: %v"), op), nil))
	}
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
