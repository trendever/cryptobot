package main

import (
	"common/log"
	"common/rabbit"
	"core/proto"
	"fmt"
	"github.com/tucnak/telebot"
	"lbapi"
	"strconv"
)

type State int

const (
	State_None State = iota
	State_Start
	State_Unavailable
	State_ChangeKey
	State_InterruptedAction
	State_WaitForOrders
	State_ServeOrder
)

var stateString = map[State]string{
	State_None:              "None",
	State_Start:             "Start",
	State_Unavailable:       "Unavailable",
	State_ChangeKey:         "ChangeKey",
	State_InterruptedAction: "InterruptedAction",
	State_WaitForOrders:     "WaitForOrders",
	State_ServeOrder:        "ServeOrder",
}

func (s State) String() string {
	str, ok := stateString[s]
	if ok {
		return str
	}
	return strconv.FormatInt(int64(s), 10)
}

type EnterHandler func(s *Session, loaded bool)

// Set of handlers for state
type StateActions struct {
	Enter EnterHandler
	// Every state should have message handler, all other handlers are optional
	Message MessageHandler
	Event   EventHandler
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
		Enter:   startStateEnter,
		Message: startStateMessage,
	},

	State_Unavailable: {
		Enter:   unavailableStateEnter,
		Message: unavailableStateMessage,
	},

	State_ChangeKey: {
		Enter:   changeKeyStateEnter,
		Message: changeKeyStateMessage,
	},

	State_InterruptedAction: {
		Message: func(s *Session, msg *telebot.Message) {
			log.Error(SendMessage(s.Dest(), M("session was interrupted"), nil))
			s.ChangeState(State_Start)
		},
	},

	State_WaitForOrders: {
		Enter:   waitForOrdersStateEnter,
		Message: waitForOrdersStateMessage,
		Event:   waitForOrdersStateEvent,
	},

	State_ServeOrder: {
		Enter:   serveOrderStateEnter,
		Message: serveOrderStateMessage,
		Event:   serveOrderStateEvent,
	},
}

func startStateEnter(s *Session, loaded bool) {
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
	if !loaded {
		log.Error(SendMessage(s.Dest(), M("start"), startKeyboard(s)))
	}
}

func startStateMessage(s *Session, msg *telebot.Message) {
	switch msg.Text {
	case M("set key"):
		s.ChangeState(State_ChangeKey)
		return

	case M("help"):
		helpHandler(s, msg)
		return

	case M("start serve"):
		s.ChangeState(State_WaitForOrders)
		return

	case M("show deposit"):
		depositHandler(s, msg)
		return
	}
	log.Error(SendMessage(s.Dest(), M("start"), startKeyboard(s)))
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
			M("show deposit"),
		)
	}
	return Keyboard(keys...)
}

func unavailableStateEnter(s *Session, loaded bool) {
	if loaded {
		return
	}
	log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), Keyboard(
		M("reload"),
	)))
}

func unavailableStateMessage(s *Session, msg *telebot.Message) {
	s.ClearInbox()
	// ignore any unexpected messages
	if msg.Text != M("reload") {
		log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("service unavailable")), Keyboard(
			M("reload"),
		)))
		return
	}
	reloadHandler(s, msg)
}

func changeKeyStateEnter(s *Session, loaded bool) {
	err := s.SetOperatorStatus(proto.OperatorStatus_Utility)
	if err != nil {
		s.ChangeState(State_Unavailable)
		return
	}
	log.Error(SendMessage(s.Dest(), M("input public key"), Keyboard(M("cancel"))))
}

func changeKeyStateMessage(s *Session, msg *telebot.Message) {
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
				log.Error(SendMessage(s.Dest(), M("invalid key"), nil))
				s.ChangeState(State_Start)
			} else {
				log.Errorf("got unexpected error from CheckKey rpc: %v", err)
				s.ChangeState(State_Unavailable)
			}
			return
		}

		log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("key belogs to %v"), op.Username), nil))

		if s.Operator.ID != 0 && op.ID != s.Operator.ID {
			log.Error(SendMessage(s.Dest(), fmt.Sprintf(M("previos account attached to this chat was %v"), s.Operator.Username), nil))
		}

		op, err = SetOperatorKey(proto.SetOperatorKeyRequest{
			ChatID: s.Operator.TelegramChat,
			Key:    key,
		})
		switch {
		// Everything went fine, refresh session data now
		case err == nil:
			s.Reload()
			return

		// Somehow this operator is busy with order now
		case err.Error() == proto.ForbiddenError:
			log.Error(SendMessage(s.Dest(), M("you are not allowed to change accout rigth now"), nil))
			// Reload for actual state
			s.Reload()
			return

		default:
			log.Errorf("failed to set lb key for chat %v: %v", s.Operator.TelegramChat, err)
			s.ChangeState(State_Unavailable)
			return
		}
	}
}

func waitForOrdersStateEnter(s *Session, loaded bool) {
	switch {
	case !loaded:
		err := s.SetOperatorStatus(proto.OperatorStatus_Ready)
		if err != nil {
			s.ChangeState(State_Unavailable)
			return
		}
		log.Error(SendMessage(s.Dest(), M("wait for orders"), Keyboard(M("cancel"))))

	case s.Operator.Status == proto.OperatorStatus_Proposal:
		order, err := GetOrder(s.Operator.CurrentOrder)
		if err != nil {
			log.Errorf("failed to load order %v: %v", s.Operator.CurrentOrder, err)
		}
		s.context = order

	default:
		// nothing
	}
}

func waitForOrdersStateMessage(s *Session, msg *telebot.Message) {
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
		order, err := AcceptOffer(proto.AcceptOfferRequest{
			OperatorID: s.Operator.ID,
			OrderID:    order.ID,
		})
		if err != nil {
			log.Error(SendMessage(s.Dest(), M(err.Error()), Keyboard(M("cancel"))))
			return
		}
		s.Operator.CurrentOrder = order.ID
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
	default:
		if s.context != nil {
			order, ok := s.context.(proto.Order)
			if !ok {
				log.Errorf("unexpected context type for state %v in session %v", s.State, s.Operator.TelegramChat)
				s.ChangeState(State_Unavailable)
				return
			}
			log.Error(SendMessage(
				s.Dest(),
				fmt.Sprintf(M("order %v from %v for an amount of %v %v"), order.ID, order.ClientName, order.FiatAmount, order.Currency),
				Keyboard(M("accept"), M("skip")),
			))
			return
		}
	}

	log.Error(SendMessage(s.Dest(), M("wait for orders"), Keyboard(M("cancel"))))
}

func waitForOrdersStateEvent(s *Session, event interface{}) {
	order, ok := event.(proto.Order)
	if !ok {
		return
	}
	curOrder, ok := s.context.(proto.Order)
	switch order.Status {
	case proto.OrderStatus_New:
		log.Error(SendMessage(
			s.Dest(),
			fmt.Sprintf(M("new order %v from %v for an amount of %v %v"), order.ID, order.ClientName, order.FiatAmount, order.Currency),
			Keyboard(M("accept"), M("skip")),
		))
		s.context = order

	case proto.OrderStatus_Accepted:
		if curOrder.ID != order.ID {
			return
		}
		log.Error(SendMessage(
			s.Dest(),
			fmt.Sprintf(M("order %v was taken by another operators"), order.ID),
			Keyboard(M("cancel")),
		))
		s.context = nil

	case proto.OrderStatus_Rejected:
		if curOrder.ID != order.ID {
			return
		}
		log.Error(SendMessage(
			s.Dest(),
			fmt.Sprintf(M("order %v was rejected on timeout"), order.ID),
			Keyboard(M("cancel")),
		))
		s.context = nil

	case proto.OrderStatus_Canceled:
		if curOrder.ID != order.ID {
			return
		}
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
}

func serveOrderStateEnter(s *Session, loaded bool) {
	order, err := GetOrder(s.Operator.CurrentOrder)
	if err != nil {
		log.Errorf("failed to load order %v: %v", s.Operator.CurrentOrder, err)
		s.ChangeState(State_Unavailable)
		return
	}

	s.context = order

	if loaded {
		return
	}

	switch order.Status {
	case proto.OrderStatus_Accepted:
		// @TODO (re-)send order info?
		log.Error(SendMessage(s.Dest(), M("create lb contact and input requisites here"), Keyboard(M("drop"))))

	case proto.OrderStatus_Linked:
		log.Error(SendMessage(s.Dest(), fmt.Sprintf(
			M("lb link: %v\ncontact amount: %v\nrequsites:\n%v"),
			fmt.Sprintf("https://localbitcoins.net/request/online_sell_buyer/%v", order.LBContractID),
			order.LBAmount, order.PaymentRequisites,
		), Keyboard(M("confirm"), M("drop"))))

	case proto.OrderStatus_Payment:
		log.Error(SendMessage(s.Dest(), "wait for payment", Keyboard("...")))

	case proto.OrderStatus_Confirmation:
		log.Error(SendMessage(s.Dest(), M("client marked order as payed"), Keyboard(M("confirm"))))

	case proto.OrderStatus_ConfirmationExtended:
		log.Error(SendMessage(s.Dest(),
			M("confirmation timeout is exceeded, you can drop order now"),
			Keyboard(M("confirm"), M("drop")),
		))
	}
}

func serveOrderStateEvent(s *Session, event interface{}) {
	order, ok := event.(proto.Order)
	if !ok {
		return
	}
	curOrder, ok := s.context.(proto.Order)
	if curOrder.ID != order.ID {
		// @TODO May it happen when new offer comes right after work with another was finished?
		log.Errorf(
			"operator %v got event for order %v while serving %v",
			s.Operator.ID, order.ID, curOrder.ID,
		)
		return
	}

	switch order.Status {
	case proto.OrderStatus_Accepted:
		// Does not matter, that is result of our accept actuality

	case proto.OrderStatus_Canceled:
		log.Error(SendMessage(
			s.Dest(),
			fmt.Sprintf(M("order %v was canceled by client"), order.ID),
			Keyboard(M("cancel")),
		))
		s.ChangeState(State_WaitForOrders)

	case proto.OrderStatus_Timeout:
		log.Error(SendMessage(
			s.Dest(),
			fmt.Sprintf(M("order %v was canceled on timeout"), order.ID),
			Keyboard(M("cancel")),
		))
		s.ChangeState(State_WaitForOrders)

	case proto.OrderStatus_Linked, proto.OrderStatus_Payment:
		// nothing need to be done here
		s.context = order

	case proto.OrderStatus_Confirmation:
		log.Error(SendMessage(s.Dest(), M("client marked order as payed"), Keyboard(M("confirm"))))
		s.context = order

	case proto.OrderStatus_ConfirmationExtended:
		log.Error(SendMessage(s.Dest(),
			M("confirmation timeout is exceeded, you can drop order now"),
			Keyboard(M("confirm"), M("drop")),
		))
		s.context = order

	case proto.OrderStatus_Unconfirmed:
		s.ChangeState(State_WaitForOrders)

	case proto.OrderStatus_Transfer, proto.OrderStatus_Finished:
		amount := order.LBAmount.Sub(order.LBFee).Sub(order.OperatorFee)
		log.Error(SendMessage(
			s.Dest(),
			M("order is finished")+"\n"+
				M(fmt.Sprintf("%v BTC was writed-off from you deposit", amount))+"\n"+
				M(fmt.Sprintf("your fee was %v", order.OperatorFee)),
			Keyboard(M("confirm")),
		))
		s.ChangeState(State_WaitForOrders)

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
		s.ChangeState(State_Unavailable)
	}
}

func serveOrderStateMessage(s *Session, msg *telebot.Message) {
	order, ok := s.context.(proto.Order)
	if !ok {
		s.ChangeState(State_Unavailable)
		return
	}

	if msg.Text == M("drop") {
		_, err := DropOrder(proto.DropOrderRequest{
			OperatorID: s.Operator.ID,
			OrderID:    order.ID,
		})
		if err != nil {
			log.Errorf("failed to drop order %v: %v", order.ID, err)
			s.ChangeState(State_Unavailable)
			return
		}
		log.Error(SendMessage(s.Dest(), M("order was dropped"), Keyboard("...")))
		s.ChangeState(State_WaitForOrders)
		return
	}
	switch order.Status {
	case proto.OrderStatus_Linked:
		if msg.Text == M("confirm") {
			order, err := RequestPayment(order.ID)
			if err != nil {
				s.ChangeState(State_Unavailable)
				return
			}
			s.context = order
			log.Error(SendMessage(s.Dest(), M("wait for payment"), Keyboard("...")))
			return
		}
		fallthrough

	case proto.OrderStatus_Accepted:
		ret, err := LinkLBContact(proto.LinkLBContractRequest{
			OrderID:    order.ID,
			Requisites: msg.Text,
		})
		switch {
		case err == nil:
			order = ret
			s.context = order
			log.Error(SendMessage(s.Dest(), fmt.Sprintf(
				M("lb link: %v\ncontact amount: %v\nrequsites:\n%v"),
				fmt.Sprintf("https://localbitcoins.net/request/online_sell_buyer/%v", order.LBContractID),
				order.LBAmount, order.PaymentRequisites,
			), Keyboard(M("confirm"), M("drop"))))

		// @TODO Do we need a way to exchange without contact?
		case err.Error() == proto.ContactNotFoundError:
			log.Error(SendMessage(s.Dest(), M("related lb contact not found"), Keyboard(M("drop"))))
		default:
			log.Errorf("failed to link lb contact for order %v: %v", order.ID, err)
			s.ChangeState(State_Unavailable)
		}

	case proto.OrderStatus_Payment:
		log.Error(SendMessage(s.Dest(), M("wait for payment"), Keyboard("...")))

	case proto.OrderStatus_Confirmation:
		if msg.Text == M("confirm") {
			log.Error(SendMessage(s.Dest(), M("wait for finish of transaction"), Keyboard("...")))
			_, err := ConfirmPayment(order.ID)
			if err != nil {
				s.ChangeState(State_Unavailable)
				return
			}
			return
		}
		log.Error(SendMessage(s.Dest(), M("client marked order as payed"), Keyboard(M("confirm"))))

	case proto.OrderStatus_ConfirmationExtended:
		switch msg.Text {
		case M("confirm"):
			_, err := ConfirmPayment(order.ID)
			if err != nil {
				s.ChangeState(State_Unavailable)
				return
			}
			log.Error(SendMessage(s.Dest(), M("wait for finish of transaction"), Keyboard("...")))
			return

		// handled above
		//case "drop":

		default:
			log.Error(SendMessage(s.Dest(),
				M("confirmation timeout is exceeded, you can drop order now"),
				Keyboard(M("confirm"), M("drop")),
			))
		}

	default:
		s.ChangeState(State_Unavailable)
	}

	return
}
