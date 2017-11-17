package main

import (
	"common/log"
	"common/rabbit"
	"fmt"
	"github.com/tucnak/telebot"
	"lbapi"
	"strconv"
)

type State int

const (
	State_Start State = iota
	State_ChangeKey
)

type StateActions struct {
	Enter       func(s *Session)
	Message     func(s *Session, msg *telebot.Message)
	Exit        func(s *Session)
	Recoverable bool
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
			log.Error(SendMessage(s.Dest(), M("start"), Keyboard(M("set key"))))
		},
		Message: func(s *Session, msg *telebot.Message) {
			switch msg.Text {
			case M("set key"):
				s.ChangeState(State_ChangeKey)
				return
			}
			log.Error(SendMessage(s.Dest(), M("start"), Keyboard(M("set key"))))
		},
		Recoverable: true,
	},
	State_ChangeKey: {
		Enter: func(s *Session) {
			log.Error(SendMessage(s.Dest(), M("input public key"), Keyboard(M("cancel"))))
		},
		Message: changeKey,
	},
}

func changeKey(s *Session, msg *telebot.Message) {
	if msg.Text == M("cancel") {
		s.ChangeState(State_Start)
		return
	}
	if s.context == nil {
		key := lbapi.Key{
			Public: msg.Text,
		}
		ok, _ := key.IsValid()
		if !ok {
			log.Error(SendMessage(s.Dest(), M("invalid key"), Keyboard(M("cancel"))))
			return
		}
		s.context = key
		log.Error(SendMessage(s.Dest(), M("input secret key"), Keyboard(M("cancel"))))
	} else { // We have public key already, so it's secret part now.
		key := s.context.(lbapi.Key)
		key.Secret = msg.Text
		_, ok := key.IsValid()
		if !ok {
			log.Error(SendMessage(s.Dest(), M("invalid key"), Keyboard(M("cancel"))))
			return
		}
		op, err := CheckKey(key)
		if err != nil {
			rpcErr := err.(rabbit.RPCError)
			if rpcErr.Description == "HMAC authentication key and signature was given, but they are invalid." {
				log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("invalid key"), err), nil))
			} else {
				log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), nil))
			}
			s.ChangeState(State_Start)
			return
		}

		log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("check key: %v"), op), nil))
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
	return ret
}
