package main

import (
	"fmt"
	"log"

	"github.com/gen2brain/beeep"
)

type Notifier struct{}

func NewNotifier() *Notifier {
	return &Notifier{}
}

func (n *Notifier) Notify(msg *EmailMessage) {
	title := fmt.Sprintf("새 이메일: %s", msg.From)
	body := msg.Subject
	if msg.Snippet != "" {
		body = fmt.Sprintf("%s\n%s", msg.Subject, truncate(msg.Snippet, 100))
	}

	if err := beeep.Notify(title, body, ""); err != nil {
		log.Printf("알림 전송 실패: %v", err)
	}

	log.Printf("[알림] From: %s | Subject: %s", msg.From, msg.Subject)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
