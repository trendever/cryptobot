package main

import (
	"common/log"
	"common/stopper"
	"core/proto"
	"github.com/tucnak/telebot"
)

type Session struct {
	Operator proto.Operator
	State    State
	// per state context, clears on state change
	// @CHECK may map[string]string be better choice?
	context interface{}
	inbox   chan telebot.Message
	stopper *stopper.Stopper
}

func NewSession(chatID int64) *Session {
	s := &Session{
		Operator: proto.Operator{
			TelegramChat: chatID,
		},
		inbox:   make(chan telebot.Message, 4),
		State:   State_Start,
		stopper: stopper.NewStopper(),
	}
	global.waitGroup.Add(1)
	go s.loop()
	return s
}

func LoadSession(chatID int64) (*Session, error) {
	op, err := OperatorByTd(chatID)
	if err != nil {
		return NewSession(chatID), err
	}
	ses := &Session{
		Operator: op,
		State:    State_Start,
		inbox:    make(chan telebot.Message, 4),
		stopper:  stopper.NewStopper(),
	}
	ses.Operator.TelegramChat = chatID
	global.waitGroup.Add(1)
	go ses.loop()
	ses.StateFromStatus(op.Status)
	return ses, nil
}

func (s *Session) PushMessage(msg telebot.Message) {
	s.inbox <- msg
}

func (s *Session) Reload() error {
	op, err := OperatorByTd(s.Operator.TelegramChat)
	if err != nil {
		log.Errorf("failed to reload session for chat %v: %v", s.Operator.TelegramChat, err)
		return err
	}
	s.Operator = op
	s.StateFromStatus(op.Status)
	return nil
}

func (s *Session) StateFromStatus(status proto.OperatorStatus) {
	switch status {
	case proto.OperatorStatus_None, proto.OperatorStatus_Inactive:
		s.ChangeState(State_Start)
	case proto.OperatorStatus_Ready:
		// @TODO
	case proto.OperatorStatus_Busy:
		// @TODO
	case proto.OperatorStatus_Utility:
		s.ChangeState(State_InterruptedAction)
	default:
		log.Errorf("unknown operator status %v in StateFromStatus", status)
		s.ChangeState(State_Start)
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

func (s Session) Dest() chatDestination {
	return Dest(s.Operator.TelegramChat)
}

func (s *Session) Stop() {
	s.stopper.Stop()
}

func (s *Session) ChangeState(newState State) {
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
			s.ChangeState(State_Unavailable)
		}
		return
	}
	s.State = newState
	s.context = nil
	if actions.Enter != nil {
		actions.Enter(s)
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

func (s *Session) loop() {
	defer global.waitGroup.Done()
	for {
		select {
		case <-global.stopper.Chan():
			return
		case <-s.stopper.Chan():
			return
		case msg := <-s.inbox:
			actions, ok := states[s.State]
			if !ok {
				s.ChangeState(State_Unavailable)
			} else {
				actions.Message(s, &msg)
			}
		}
	}
}
