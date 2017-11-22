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
	State_WaitForOrders
	State_ServeOrder
)

const ReloadTimeout = 3 * time.Second

type StateActions struct {
	Enter   func(s *Session)
	Message func(s *Session, msg *telebot.Message)
	Event   func(s *Session, event interface{})
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
					s.ChangeState(State_Unavailable)
					return
				}
			}
			log.Error(SendMessage(s.Dest(), M("start"), startKeyboard(s)))
		},

		Message: func(s *Session, msg *telebot.Message) {
			// @TODO show deposit balance
			switch msg.Text {
			case M("set key"):
				s.ChangeState(State_ChangeKey)
				return

			case M("help"):
				log.Error(SendMessage(s.Dest(), M("help text"), nil))
				return

			case M("start serve"):
				s.ChangeState(State_WaitForOrders)
				return
			}
			log.Error(SendMessage(s.Dest(), M("start"), startKeyboard(s)))
		},
	},

	State_Unavailable: {
		Enter: func(s *Session) {
			log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), Keyboard(
				M("reload"),
			)))
		},
		Message: func(s *Session, msg *telebot.Message) {
			s.ClearInbox()
			// ignore any unexpected messages
			if msg.Text != M("reload") {
				log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), Keyboard(
					M("reload"),
				)))
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

	State_WaitForOrders: {
		Enter: func(s *Session) {
			err := s.SetOperatorStatus(proto.OperatorStatus_Ready)
			if err != nil {
				s.ChangeState(State_Unavailable)
				return
			}
			log.Error(SendMessage(s.Dest(), M("wait for orders"), Keyboard(M("cancel"))))
		},

		Message: func(s *Session, msg *telebot.Message) {
			switch msg.Text {
			case M("cancel"):
				s.ChangeState(State_Start)
				return
			case M("accept"):
				order, ok := s.context.(proto.Order)
				if !ok {
					log.Error(SendMessage(s.Dest(), M("there was no active offer"), Keyboard(M("cancel"))))
					return
				}
				_, err := AcceptOffer(proto.AcceptOfferRequest{
					OperatorID: s.Operator.ID,
					OrderID:    order.ID,
				})
				if err != nil {
					log.Error(SendMessage(s.Dest(), M(err.Error()), Keyboard(M("cancel"))))
					return
				}
				s.ChangeState(State_ServeOrder)
				return

			case M("skip"):
				order, ok := s.context.(proto.Order)
				if !ok {
					log.Error(SendMessage(s.Dest(), M("there was no active offer"), Keyboard(M("cancel"))))
					return
				}
				_, err := SkipOffer(proto.SkipOfferRequest{
					OperatorID: s.Operator.ID,
					OrderID:    order.ID,
				})
				if err != nil {
					log.Error(SendMessage(s.Dest(), M(err.Error()), Keyboard(M("cancel"))))
					return
				}
			}

			log.Error(SendMessage(s.Dest(), M("wait for orders"), Keyboard(M("cancel"))))
		},

		Event: func(s *Session, event interface{}) {
			order, ok := event.(proto.Order)
			if !ok {
				return
			}
			switch order.Status {
			case proto.OrderStatus_New:
				log.Error(SendMessage(
					s.Dest(),
					fmt.Sprintf(M("new order %v from %v for an amount of %v %v"), order.ID, order.ClientName, order.FiatAmount, order.Currency),
					Keyboard(M("accept"), M("skip")),
				))
				s.context = order
			case proto.OrderStatus_Accepted:
				log.Error(SendMessage(
					s.Dest(),
					fmt.Sprintf(M("order %v was taken by another operators"), order.ID),
					Keyboard(M("cancel")),
				))
				s.context = nil
			case proto.OrderStatus_Rejected:
				log.Error(SendMessage(
					s.Dest(),
					fmt.Sprintf(M("order %v was rejected on timeout"), order.ID),
					Keyboard(M("cancel")),
				))
				s.context = nil
			case proto.OrderStatus_Canceled:
				log.Error(SendMessage(
					s.Dest(),
					fmt.Sprintf(M("order %v was canceled by client"), order.ID),
					Keyboard(M("cancel")),
				))
				s.context = nil
			default:
				log.Warn("got order %v with unxepected status %v in WaitForOrders", order.ID, order.Status)
				if s.context == nil {
					return
				}
				ctx, ok := s.context.(proto.Order)
				if !ok || ctx.ID != order.ID {
					return
				}
				log.Error(SendMessage(
					s.Dest(),
					fmt.Sprintf(M("order %v entered unexped state"), order.ID),
					Keyboard(M("cancel")),
				))
				s.context = nil
			}
		},
	},

	State_ServeOrder: {
		Enter: func(s *Session) {
			log.Error(SendMessage(s.Dest(), M("ok"), nil))
		},
		Message: func(s *Session, msg *telebot.Message) {
			log.Error(SendMessage(s.Dest(), M("eh"), Keyboard(M("nothing"))))
		},
	},
}

func startKeyboard(s *Session) *telebot.SendOptions {
	keys := []string{
		M("set key"),
		M("help"),
	}
	if s.Operator.HasValidKey {
		keys = append(
			keys,
			M("start serve"),
		)
	}
	return Keyboard(keys...)
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

		log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("key belogs to %v"), op.Username), nil))

		if s.Operator.ID != 0 && op.ID != s.Operator.ID {
			log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("previos account tat was attached to this chat is %v"), s.Operator.Username), nil))
		}

		op, err = SetOperatorKey(proto.SetOperatorKeyRequest{
			ChatID: s.Operator.TelegramChat,
			Key:    key,
		})
		if err != nil {
			log.Errorf("failed to set lb key for chat %v: %v", s.Operator.TelegramChat, err)
			s.ChangeState(State_Unavailable)
			return
		}

		s.Operator = op
		s.ChangeState(State_Start)
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
