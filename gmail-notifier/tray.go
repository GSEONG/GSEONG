package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/ncruces/zenity"
)

// trayMgr is the global tray state shared across packages.
var trayMgr = &TrayManager{}

type TrayManager struct {
	mu           sync.Mutex
	mStatus      *systray.MenuItem
	mLastMail    *systray.MenuItem
	lastMailFrom string
	lastMailTime time.Time
	cfg          *Config
	logPath      string
	logFile      *os.File
	configPath   string
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

func runTray(cfg *Config, logPath string, logFile *os.File, configPath string, onReady func(), onExit func()) {
	trayMgr.cfg = cfg
	trayMgr.logPath = logPath
	trayMgr.logFile = logFile
	trayMgr.configPath = configPath

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
	mLog       := systray.AddMenuItem("📋 로그 보기", "로그 파일 열기")
	mClearLog  := systray.AddMenuItem("🗑️  로그 초기화", "로그 파일 비우기")

	systray.AddSeparator()
	mConfig    := systray.AddMenuItem("⚙️  설정 보기", "현재 설정 확인")
	mDownloads := systray.AddMenuItem("📂 다운로드 폴더 열기", "첨부파일 저장 폴더 열기")

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("종료", "프로그램 종료")

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

	// Menu click handlers
	for {
		select {
		case <-mLog.ClickedCh:
			if err := openInEditor(trayMgr.logPath); err != nil {
				log.Printf("로그 파일 열기 실패: %v", err)
			}
		case <-mClearLog.ClickedCh:
			clearLogFile(trayMgr.logPath, trayMgr.logFile)
		case <-mConfig.ClickedCh:
			showConfigDialog(trayMgr.cfg, trayMgr.configPath)
		case <-mDownloads.ClickedCh:
			openDownloadsFolder()
		case <-mQuit.ClickedCh:
			onExit()
			systray.Quit()
			return
		}
	}
}

// openInEditor opens a text file with the OS default text editor.
func openInEditor(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad", path)
	case "darwin":
		cmd = exec.Command("open", "-t", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// showConfigDialog displays current config values in a popup dialog.
func showConfigDialog(cfg *Config, configPath string) {
	if cfg == nil {
		return
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("설정 파일: %s\n", configPath))
	sb.WriteString(fmt.Sprintf("폴링 간격: %d초\n", cfg.PollIntervalSeconds))
	sb.WriteString(fmt.Sprintf("Credentials: %s\n", cfg.CredentialsFile))
	sb.WriteString(fmt.Sprintf("Token: %s\n", cfg.TokenFile))

	sb.WriteString("\n[ 알림 소리 ]\n")
	if !cfg.Sound.Enabled {
		sb.WriteString("  비활성화\n")
	} else if cfg.Sound.File != "" {
		sb.WriteString(fmt.Sprintf("  파일: %s\n", cfg.Sound.File))
	} else {
		sb.WriteString(fmt.Sprintf("  비프음: %.0f Hz / %d ms\n",
			cfg.Sound.BeepFrequency, cfg.Sound.BeepDurationMs))
	}

	sb.WriteString(fmt.Sprintf("\n[ 필터 — 총 %d개 ]\n", len(cfg.Filters)))
	if len(cfg.Filters) == 0 {
		sb.WriteString("  (없음 — 모든 이메일 알림)\n")
	}
	for i, f := range cfg.Filters {
		mode := "일반"
		if f.Regex {
			mode = "정규식"
		}
		fromStr := "(모두)"
		if len(f.From) > 0 {
			fromStr = strings.Join(f.From, ", ")
		}
		subjectStr := "(모두)"
		if len(f.Subject) > 0 {
			subjectStr = strings.Join(f.Subject, ", ")
		}
		sb.WriteString(fmt.Sprintf("  [%d] (%s) 발신: %s / 제목: %s\n",
			i+1, mode, fromStr, subjectStr))
	}

	err := zenity.Info(
		sb.String(),
		zenity.Title("현재 설정"),
		zenity.OKLabel("닫기"),
	)
	if err != nil && err != zenity.ErrCanceled {
		log.Printf("설정 다이얼로그 오류: %v", err)
	}
}

// clearLogFile prompts for confirmation then truncates the log file.
func clearLogFile(logPath string, logFile *os.File) {
	err := zenity.Question(
		"로그 파일을 초기화하시겠습니까?\n\n이 작업은 되돌릴 수 없습니다.",
		zenity.Title("로그 초기화"),
		zenity.OKLabel("초기화"),
		zenity.CancelLabel("취소"),
		zenity.WarningIcon,
	)
	if err != nil {
		return // 취소 또는 창 닫음
	}

	if err := os.Truncate(logPath, 0); err != nil {
		log.Printf("로그 초기화 실패: %v", err)
		return
	}
	// 기존 파일 핸들의 쓰기 위치를 파일 시작으로 되돌림
	if logFile != nil {
		logFile.Seek(0, 0)
	}
	log.Printf("로그 파일 초기화 완료")
}

// openDownloadsFolder opens the gmail-notifier downloads directory in the file explorer.
func openDownloadsFolder() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("홈 디렉토리 조회 실패: %v", err)
		return
	}
	dir := filepath.Join(homeDir, "Downloads", "gmail-notifier")

	// 폴더가 없으면 미리 생성
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("다운로드 폴더 생성 실패: %v", err)
		return
	}

	if err := openFolder(dir); err != nil {
		log.Printf("다운로드 폴더 열기 실패: %v", err)
	}
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
