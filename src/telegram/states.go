package main

import (
	"common/log"
	"common/rabbit"
	"core/proto"
	"fmt"
	"github.com/tucnak/telebot"
	"lbapi"
	"strconv"
	"time"
)

type State int

const (
	State_Start State = iota
	State_Unavailable
	State_ChangeKey
	State_InterruptedAction
)

const ReloadTimeout = 3 * time.Second

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
			if s.Operator.ID != 0 {
				status := proto.OperatorStatus_None
				if s.Operator.HasValidKey {
					status = proto.OperatorStatus_Inactive
				}
				err := s.SetOperatorStatus(status)
				if err != nil {

				}
			}
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
	},
	State_Unavailable: {
		Enter: func(s *Session) {
			log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), Keyboard(
				M("reload"),
			)))
		},
		Message: func(s *Session, msg *telebot.Message) {
			// ignore any unexpected messages
			if msg.Text != M("reload") {
				return
			}
			now := time.Now()
			if s.context != nil {
				lastTry := s.context.(time.Time)
				if now.Sub(lastTry) < ReloadTimeout {
					return
				}
			}
			err := s.Reload()
			if err != nil {
				s.context = now
			}
		},
	},
	State_ChangeKey: {
		Enter: func(s *Session) {
			err := s.SetOperatorStatus(proto.OperatorStatus_Utility)
			if err != nil {
				s.ChangeState(State_Unavailable)
				return
			}
			log.Error(SendMessage(s.Dest(), M("input public key"), Keyboard(M("cancel"))))
		},
		Message: changeKey,
	},
	State_InterruptedAction: {
		Message: func(s *Session, msg *telebot.Message) {
			log.Error(SendMessage(s.Dest(), M("session was interrupted"), nil))
			s.ChangeState(State_Start)
		},
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
				s.ChangeState(State_Start)
			} else {
				log.Errorf("got unexpected error from CheckKey rpc: %v", err)
				s.ChangeState(State_Unavailable)
			}
			return
		}

		s.ChangeState(State_Unavailable)
		return

		if s.Operator.ID != 0 && op.ID != s.Operator.ID {

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
