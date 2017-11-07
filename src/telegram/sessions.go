package main

import (
	"common/log"
	"common/stopper"
	"github.com/tucnak/telebot"
)

type Session struct {
	UserID uint64
	ChatID int64 `gorm:"primary_key"`
	State  State
	// per state context, clears on state change
	// @CHECK may map[string]string be better choice?
	context interface{}
	inbox   chan telebot.Message
	stopper *stopper.Stopper
	// @TODO waitgroup for graceful shutdown?
}

func NewSession(chatID int64) *Session {
	s := &Session{
		ChatID:  chatID,
		inbox:   make(chan telebot.Message, 4),
		State:   State_Start,
		stopper: stopper.NewStopper(),
	}
	global.waitGroup.Add(1)
	go s.loop()
	return s
}

func LoadSession(chatID int64) (*Session, error) {
	// @TODO
	return NewSession(chatID), nil
}

func (s *Session) PushMessage(msg telebot.Message) {
	s.inbox <- msg
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
		// hm?
		// @TODO do something about it
		log.Errorf("session %v tried to join unknown state %v", s.ChatID, newState)
		return
	}
	s.State = newState
	s.context = nil
	if actions.Enter != nil {
		actions.Enter(s)
	}
	// @TODO save state here?
}

func (s *Session) loop() {
	for {
		select {
		case <-global.stopper.Chan():
			global.waitGroup.Done()
			return
		case <-s.stopper.Chan():
			global.waitGroup.Done()
			return
		case msg := <-s.inbox:
			actions, ok := states[s.State]
			if !ok {
				// @TODO reload session?
				actions = states[State_Start]
			}
			actions.Message(s, &msg)
		}
	}
}
