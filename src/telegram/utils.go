package main

import (
	"github.com/tucnak/telebot"
	"strconv"
)

// Returns full-size message for key string. In message is not defined, returns key itself.
// Correctly messages should be defined in service config.
func M(key string) string {
	msg, ok := conf.Messages[key]
	if ok {
		return msg
	}
	//log.Warn("message for key '%v' is undefined", key)
	return key
}

// Implements telebot.Recipient interface
type ChatDestination string

func (dest ChatDestination) Destination() string {
	return string(dest)
}

func DestinationForID(chatID int64) ChatDestination {
	return ChatDestination(strconv.FormatInt(chatID, 10))
}

// Returns *telebot.SendOptions with keyboard defined by passes keys
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
