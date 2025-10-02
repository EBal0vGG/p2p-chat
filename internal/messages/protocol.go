package messages

import "time"

type Message struct {
    Type string `json:"type"`
    ID   string `json:"id,omitempty"`
    From string `json:"from,omitempty"`
    Name string `json:"name,omitempty"`
    Text string `json:"text,omitempty"`
    TS   int64  `json:"ts,omitempty"`
}

func NewChat(text, from, name string) Message {
    return Message{Type: "msg", Text: text, From: from, Name: name, TS: time.Now().Unix()}
}