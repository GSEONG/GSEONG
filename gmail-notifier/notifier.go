package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/ncruces/zenity"
	"google.golang.org/api/gmail/v1"
)

const maxConcurrentPopups = 5

type Notifier struct {
	activePopups atomic.Int32
	soundCfg     SoundConfig
	gmailSvc     *gmail.Service
}

func NewNotifier(soundCfg SoundConfig, svc *gmail.Service) *Notifier {
	return &Notifier{soundCfg: soundCfg, gmailSvc: svc}
}

func (n *Notifier) Notify(msg *EmailMessage) {
	log.Printf("[알림] 새 이메일 도착 (ID: %s)", msg.ID)

	trayMgr.SetLastMail(msg.From)
	dashServer.AddMail(msg)

	sysNotify(fmt.Sprintf("새 이메일: %s", msg.From), buildBody(msg))

	playSound(n.soundCfg)

	if n.activePopups.Load() >= maxConcurrentPopups {
		log.Printf("팝업 한도 초과로 팝업 생략")
		return
	}

	go n.showPopup(msg)
}

func (n *Notifier) showPopup(msg *EmailMessage) {
	n.activePopups.Add(1)
	defer n.activePopups.Add(-1)

	text := fmt.Sprintf("보낸 사람: %s\n제목: %s", msg.From, msg.Subject)
	if msg.Snippet != "" {
		text += fmt.Sprintf("\n\n%s", truncate(msg.Snippet, 200))
	}

	if len(msg.Attachments) == 0 {
		// 첨부파일 없음 — 확인 버튼만 표시
		err := zenity.Info(text,
			zenity.Title("새 이메일 도착"),
			zenity.OKLabel("확인"),
		)
		if err != nil && err != zenity.ErrCanceled {
			log.Printf("팝업 오류: %v", err)
		}
		return
	}

	// 첨부파일 목록 추가
	text += fmt.Sprintf("\n\n📎 첨부파일 %d개", len(msg.Attachments))
	for _, att := range msg.Attachments {
		text += fmt.Sprintf("\n  • %s (%s)", att.Filename, formatSize(att.Size))
	}

	// 다운로드 / 닫기 선택
	err := zenity.Question(text,
		zenity.Title("새 이메일 도착"),
		zenity.OKLabel("📥 다운로드"),
		zenity.CancelLabel("닫기"),
	)
	if err == nil {
		// 사용자가 "다운로드" 클릭
		go n.downloadAttachments(msg)
	}
}

func (n *Notifier) downloadAttachments(msg *EmailMessage) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("홈 디렉토리 조회 실패: %v", err)
		return
	}

	saveDir := filepath.Join(homeDir, "Downloads", "gmail-notifier")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		log.Printf("저장 폴더 생성 실패: %v", err)
		return
	}

	var saved []string
	for _, att := range msg.Attachments {
		path, err := n.downloadOne(msg.ID, att, saveDir)
		if err != nil {
			log.Printf("첨부파일 다운로드 실패 (%s): %v", att.Filename, err)
			continue
		}
		saved = append(saved, att.Filename)
		log.Printf("첨부파일 저장: %s", path)
	}

	if len(saved) == 0 {
		return
	}

	// 저장 완료 알림 + 폴더 열기
	sysNotify("다운로드 완료", fmt.Sprintf("%d개 파일이 저장되었습니다.\n%s", len(saved), saveDir))
	openFolder(saveDir)
}

func (n *Notifier) downloadOne(msgID string, att Attachment, saveDir string) (string, error) {
	res, err := n.gmailSvc.Users.Messages.Attachments.
		Get("me", msgID, att.AttachmentID).Do()
	if err != nil {
		return "", err
	}

	// Gmail은 URL-safe base64 사용. 패딩 없을 수 있어 RawURLEncoding으로 디코드.
	decoded, err := base64.RawURLEncoding.DecodeString(res.Data)
	if err != nil {
		// 패딩 있는 경우 재시도
		decoded, err = base64.URLEncoding.DecodeString(res.Data)
		if err != nil {
			return "", fmt.Errorf("base64 디코딩 실패: %w", err)
		}
	}

	path := uniquePath(saveDir, att.Filename)
	if err := os.WriteFile(path, decoded, 0o644); err != nil {
		return "", fmt.Errorf("파일 저장 실패: %w", err)
	}
	return path, nil
}

// uniquePath returns a non-conflicting file path (adds _1, _2, ... if needed).
func uniquePath(dir, filename string) string {
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func openFolder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func buildBody(msg *EmailMessage) string {
	body := msg.Subject
	if msg.Snippet != "" {
		body = fmt.Sprintf("%s\n%s", msg.Subject, truncate(msg.Snippet, 100))
	}
	if len(msg.Attachments) > 0 {
		body += fmt.Sprintf("\n📎 첨부파일 %d개", len(msg.Attachments))
	}
	return body
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	case bytes >= 1024:
		return fmt.Sprintf("%.0f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
