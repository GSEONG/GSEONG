package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
)

type EmailMessage struct {
	ID      string
	From    string
	Subject string
	Snippet string
}

type Watcher struct {
	svc         *gmail.Service
	cfg         *Config
	lastHistory uint64
	notifier    *Notifier
}

func NewWatcher(svc *gmail.Service, cfg *Config, notifier *Notifier) (*Watcher, error) {
	w := &Watcher{
		svc:      svc,
		cfg:      cfg,
		notifier: notifier,
	}

	historyID, err := w.fetchLatestHistoryID()
	if err != nil {
		return nil, fmt.Errorf("초기 historyId 가져오기 실패: %w", err)
	}
	w.lastHistory = historyID
	log.Printf("모니터링 시작 (historyId: %d)", historyID)
	return w, nil
}

func (w *Watcher) fetchLatestHistoryID() (uint64, error) {
	res, err := w.svc.Users.Messages.List("me").MaxResults(1).LabelIds("INBOX").Do()
	if err != nil {
		return 0, err
	}
	if len(res.Messages) == 0 {
		profile, err := w.svc.Users.GetProfile("me").Do()
		if err != nil {
			return 0, err
		}
		return uint64(profile.HistoryId), nil
	}

	msg, err := w.svc.Users.Messages.Get("me", res.Messages[0].Id).Format("metadata").Do()
	if err != nil {
		return 0, err
	}
	return uint64(msg.HistoryId), nil
}

func (w *Watcher) Poll(ctx context.Context) {
	interval := time.Duration(w.cfg.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("폴링 간격: %v", interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkNewMails()
		}
	}
}

func (w *Watcher) checkNewMails() {
	res, err := w.svc.Users.History.List("me").
		StartHistoryId(w.lastHistory).
		HistoryTypes("messageAdded").
		LabelId("INBOX").
		Do()
	if err != nil {
		log.Printf("히스토리 조회 오류: %v", err)
		return
	}

	if res.HistoryId > 0 {
		w.lastHistory = res.HistoryId
	}

	for _, h := range res.History {
		for _, added := range h.MessagesAdded {
			msg, err := w.fetchMessage(added.Message.Id)
			if err != nil {
				log.Printf("메시지 조회 오류 (%s): %v", added.Message.Id, err)
				continue
			}
			if w.matchesFilter(msg) {
				w.notifier.Notify(msg)
			}
		}
	}
}

func (w *Watcher) fetchMessage(id string) (*EmailMessage, error) {
	raw, err := w.svc.Users.Messages.Get("me", id).Format("metadata").
		MetadataHeaders("From", "Subject").Do()
	if err != nil {
		return nil, err
	}

	msg := &EmailMessage{
		ID:      id,
		Snippet: raw.Snippet,
	}

	for _, h := range raw.Payload.Headers {
		switch h.Name {
		case "From":
			msg.From = h.Value
		case "Subject":
			msg.Subject = h.Value
		}
	}
	return msg, nil
}

func (w *Watcher) matchesFilter(msg *EmailMessage) bool {
	if len(w.cfg.Filters) == 0 {
		return true
	}

	for _, f := range w.cfg.Filters {
		fromMatch := f.From == "" || strings.Contains(strings.ToLower(msg.From), strings.ToLower(f.From))
		subjectMatch := f.Subject == "" || strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(f.Subject))
		if fromMatch && subjectMatch {
			return true
		}
	}
	return false
}
