package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

// trayMgr is the global tray state shared across packages.
var trayMgr = &TrayManager{}

type TrayManager struct {
	mu           sync.Mutex
	mStatus      *systray.MenuItem
	mLastMail    *systray.MenuItem
	lastMailFrom string
	lastMailTime time.Time
}

// SetStatus updates the status menu item text.
func (t *TrayManager) SetStatus(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mStatus != nil {
		t.mStatus.SetTitle(text)
	}
}

// SetLastMail records the latest received email sender and updates the menu.
func (t *TrayManager) SetLastMail(from string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastMailFrom = from
	t.lastMailTime = time.Now()
	if t.mLastMail != nil {
		t.mLastMail.SetTitle("마지막 수신: 방금 전")
	}
}

func runTray(onReady func(), onExit func()) {
	systray.Run(
		func() { onTrayReady(onReady, onExit) },
		func() {},
	)
}

func onTrayReady(onReady func(), onExit func()) {
	systray.SetIcon(generateTrayIcon())
	systray.SetTooltip("Gmail 알리미")

	mTitle := systray.AddMenuItem("Gmail 알리미", "")
	mTitle.Disable()

	systray.AddSeparator()

	trayMgr.mu.Lock()
	trayMgr.mStatus = systray.AddMenuItem("⏳ 시작 중...", "")
	trayMgr.mStatus.Disable()
	trayMgr.mLastMail = systray.AddMenuItem("마지막 수신: 없음", "")
	trayMgr.mLastMail.Disable()
	trayMgr.mu.Unlock()

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("종료", "프로그램 종료")

	// Start application logic in background
	go onReady()

	// Periodically refresh elapsed time on "마지막 수신" item
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			trayMgr.mu.Lock()
			if !trayMgr.lastMailTime.IsZero() && trayMgr.mLastMail != nil {
				trayMgr.mLastMail.SetTitle(fmt.Sprintf("마지막 수신: %s", elapsedLabel(trayMgr.lastMailTime)))
			}
			trayMgr.mu.Unlock()
		}
	}()

	<-mQuit.ClickedCh
	onExit()
	systray.Quit()
}

func elapsedLabel(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "방금 전"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	}
}
