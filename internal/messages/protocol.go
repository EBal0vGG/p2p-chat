package messages

import (
	"time"

	"github.com/google/uuid"
)

type Message struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	From string `json:"from,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
	TS   int64  `json:"ts,omitempty"`
}

func NewChat(text, from, name string) Message {
	return Message{
		Type: "msg",
		ID:   uuid.NewString(),
		From: from,
		Name: name,
		Text: text,
		TS:   time.Now().Unix(),
	}
}

func NewHello(from, name string) Message {
	return Message{
		Type: "hello",
		From: from,
		Name: name,
		TS:   time.Now().Unix(),
	}
}
