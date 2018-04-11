package main

import (
	"common/log"
	"common/stopper"
	"core/proto"
	"errors"
	"github.com/tucnak/telebot"
)

type Session struct {
	Operator proto.Operator
	State    State
	// per state context, clears on state change
	// @CHECK may map[string]string be better choice?
	context interface{}
	inbox   chan telebot.Message
	events  chan interface{}
	stopper *stopper.Stopper
}

func NewSession(chatID int64) *Session {
	s := &Session{
		Operator: proto.Operator{
			TelegramChat: chatID,
		},
		inbox:   make(chan telebot.Message, 8),
		events:  make(chan interface{}, 8),
		State:   State_Start,
		stopper: stopper.NewStopper(),
	}
	global.waitGroup.Add(1)
	go s.loop()
	return s
}

func LoadSessionForOperator(operatorID uint64) (*Session, error) {
	op, err := OperatorByID(operatorID)
	if err != nil {
		return nil, err
	}
	if op.ID == 0 {
		return nil, errors.New("operator not found")
	}
	return makeSessionWithOperator(op), nil
}

func LoadSessionForChat(chatID int64) (*Session, error) {
	op, err := OperatorByTg(chatID)
	if err != nil {
		return NewSession(chatID), err
	}
	return makeSessionWithOperator(op), nil
}

func makeSessionWithOperator(op proto.Operator) *Session {
	ses := &Session{
		Operator: op,
		State:    State_Start,
		inbox:    make(chan telebot.Message, 8),
		events:   make(chan interface{}, 8),
		stopper:  stopper.NewStopper(),
	}
	global.waitGroup.Add(1)
	go ses.loop()
	ses.StateFromOpStatus(true)
	return ses
}

func (s *Session) PushMessage(msg telebot.Message) {
	s.inbox <- msg
}

func (s *Session) PushEvent(event interface{}) {
	s.events <- event
}

func (s *Session) Reload() error {
	op, err := OperatorByTg(s.Operator.TelegramChat)
	if err != nil {
		log.Errorf("failed to reload session for chat %v: %v", s.Operator.TelegramChat, err)
		return err
	}
	s.Operator = op
	s.StateFromOpStatus(false)
	return nil
}

func (s *Session) StateFromOpStatus(loaded bool) {
	switch s.Operator.Status {
	case proto.OperatorStatus_None, proto.OperatorStatus_Inactive:
		s.changeState(State_Start, loaded)
	case proto.OperatorStatus_Ready, proto.OperatorStatus_Proposal:
		s.changeState(State_WaitForOrders, loaded)
	case proto.OperatorStatus_Busy:
		s.changeState(State_ServeOrder, loaded)
	case proto.OperatorStatus_Utility:
		s.changeState(State_InterruptedAction, loaded)
	default:
		log.Errorf("unknown operator status %v in StateFromStatus", s.Operator.Status)
		s.changeState(State_Start, loaded)
	}
}

func (s *Session) SetOperatorStatus(status proto.OperatorStatus) error {
	if s.Operator.ID == 0 {
		return nil
	}
	_, err := SetOperatorStatus(proto.SetOperatorStatusRequest{
		ChatID: s.Operator.TelegramChat,
		Status: status,
	})
	if err == nil {
		s.Operator.Status = status
	}
	return err
}

func (s Session) Dest() ChatDestination {
	return DestinationForID(s.Operator.TelegramChat)
}

func (s *Session) Stop() {
	s.stopper.Stop()
}

func (s *Session) ChangeState(newState State) {
	s.changeState(newState, false)
}

func (s *Session) changeState(newState State, loaded bool) {
	if s.State == newState {
		return
	}
	actions, ok := states[s.State]
	if ok && actions.Exit != nil {
		actions.Exit(s)
	}
	actions, ok = states[newState]
	if !ok {
		log.Errorf("session %v tried to join unknown state %v", s.Operator.TelegramChat, newState)
		log.Error(SendMessage(s.Dest(), M("internal error"), nil))
		err := s.Reload()
		if err != nil {
			if newState == State_Unavailable {
				log.Fatalf("Unavailable state is not defined")
			}
			s.changeState(State_Unavailable, true)
		}
		return
	}
	if loaded {
		log.Debug("session %v(%v) loaded with state %v",
			s.Operator.TelegramChat, s.Operator.ID, newState)
	} else {
		log.Debug("session %v(%v) state changed: %v -> %v",
			s.Operator.TelegramChat, s.Operator.ID, s.State, newState)
	}
	s.State = newState
	s.context = nil
	s.ClearInbox()
	if actions.Enter != nil {
		actions.Enter(s, loaded)
	}
}

func (s *Session) ReceiveMessage() *telebot.Message {
	select {
	case <-global.stopper.Chan():
		return nil
	case <-s.stopper.Chan():
		return nil
	case msg := <-s.inbox:
		return &msg
	}
}

// Removes messages from inbox queue.
// Honestly it cleans internal buffer of session only and there can be more messages outside it still
func (s *Session) ClearInbox() {
	for {
		select {
		case <-s.inbox:
		default:
			return
		}
	}
}

func (s *Session) loop() {
	defer global.waitGroup.Done()
	for {
		select {
		case <-global.stopper.Chan():
			return
		case <-s.stopper.Chan():
			return
		case msg := <-s.inbox:
			// Check whether it is global command first
			if handler, ok := commands[msg.Text]; ok {
				handler(s, &msg)
				continue
			}
			// Go for state-defined handler
			actions, ok := states[s.State]
			if !ok {
				log.Errorf("state '%v' do not have messages handler", s.State)
				s.ChangeState(State_Unavailable)
			} else {
				actions.Message(s, &msg)
			}
		case event := <-s.events:
			actions, ok := states[s.State]
			switch {
			case !ok:
				s.ChangeState(State_Unavailable)
			case actions.Event == nil:
				// No events expected in this state, drop it
			default:
				actions.Event(s, event)

			}
		}
	}
}
