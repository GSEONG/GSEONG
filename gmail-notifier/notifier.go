package main

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/gen2brain/beeep"
	"github.com/ncruces/zenity"
)

const maxConcurrentPopups = 5

type Notifier struct {
	activePopups atomic.Int32
	soundCfg     SoundConfig
}

func NewNotifier(soundCfg SoundConfig) *Notifier {
	return &Notifier{soundCfg: soundCfg}
}

func (n *Notifier) Notify(msg *EmailMessage) {
	log.Printf("[알림] 새 이메일 도착 (ID: %s)", msg.ID)

	// 트레이 상태 갱신
	trayMgr.SetLastMail(msg.From)

	// 시스템 트레이 알림
	title := fmt.Sprintf("새 이메일: %s", msg.From)
	body := buildBody(msg)
	if err := beeep.Notify(title, body, ""); err != nil {
		log.Printf("시스템 알림 실패: %v", err)
	}

	// 소리 재생
	playSound(n.soundCfg)

	// 동시에 열 수 있는 팝업 수 제한
	if n.activePopups.Load() >= maxConcurrentPopups {
		log.Printf("팝업 한도 초과로 팝업 생략: %s", msg.Subject)
		return
	}

	go n.showPopup(msg)
}

func (n *Notifier) showPopup(msg *EmailMessage) {
	n.activePopups.Add(1)
	defer n.activePopups.Add(-1)

	text := fmt.Sprintf("보낸 사람: %s\n제목: %s",
		msg.From, msg.Subject)
	if msg.Snippet != "" {
		text += fmt.Sprintf("\n\n%s", truncate(msg.Snippet, 200))
	}

	err := zenity.Info(
		text,
		zenity.Title("새 이메일 도착"),
		zenity.OKLabel("확인"),
	)
	if err != nil && err != zenity.ErrCanceled {
		log.Printf("팝업 오류: %v", err)
	}
}

func buildBody(msg *EmailMessage) string {
	if msg.Snippet != "" {
		return fmt.Sprintf("%s\n%s", msg.Subject, truncate(msg.Snippet, 100))
	}
	return msg.Subject
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
