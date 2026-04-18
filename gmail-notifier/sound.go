package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gen2brain/beeep"
)

var allowedAudioExts = map[string]bool{
	".wav": true, ".mp3": true, ".ogg": true, ".flac": true,
}

func playSound(cfg SoundConfig) {
	if !cfg.Enabled {
		return
	}

	if cfg.File != "" {
		if err := playSoundFile(cfg.File); err != nil {
			log.Printf("커스텀 사운드 재생 실패: %v — 시스템 비프음으로 대체", err)
			playBeep(cfg)
		}
		return
	}

	playBeep(cfg)
}

func playBeep(cfg SoundConfig) {
	if err := beeep.Beep(cfg.BeepFrequency, cfg.BeepDurationMs); err != nil {
		log.Printf("비프음 재생 실패: %v", err)
	}
}

// playSoundFile plays an audio file using OS-native tools.
// Linux: paplay → aplay → ffplay → mpg123
// macOS: afplay
// Windows: PowerShell Media.SoundPlayer
func playSoundFile(path string) error {
	// 확장자 검증
	ext := strings.ToLower(filepath.Ext(path))
	if !allowedAudioExts[ext] {
		return fmt.Errorf("지원하지 않는 파일 형식 (%s). 허용: wav, mp3, ogg, flac", ext)
	}

	// 파일 존재 확인
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("사운드 파일을 찾을 수 없습니다: %w", err)
	}

	// 절대 경로로 변환 (경로 조작 방지)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("파일 경로 해석 실패: %w", err)
	}

	switch runtime.GOOS {
	case "linux":
		return playLinux(absPath)
	case "darwin":
		return exec.Command("afplay", absPath).Run()
	case "windows":
		return playWindows(absPath)
	default:
		return fmt.Errorf("지원하지 않는 OS: %s", runtime.GOOS)
	}
}

func playLinux(path string) error {
	players := [][]string{
		{"paplay", path},
		{"aplay", path},
		{"ffplay", "-nodisp", "-autoexit", path},
		{"mpg123", "-q", path},
	}

	for _, args := range players {
		if bin, err := exec.LookPath(args[0]); err == nil {
			if err := exec.Command(bin, args[1:]...).Run(); err != nil {
				return fmt.Errorf("%s 재생 실패: %w", args[0], err)
			}
			return nil
		}
	}
	return fmt.Errorf("사용 가능한 오디오 플레이어를 찾을 수 없습니다 (paplay/aplay/ffplay/mpg123)")
}

func playWindows(path string) error {
	// PowerShell 인젝션 방지: single quote 이스케이프 후 변수로 분리 전달
	escaped := strings.ReplaceAll(path, "'", "''")
	script := fmt.Sprintf(
		`$p = New-Object Media.SoundPlayer '%s'; $p.PlaySync()`,
		escaped,
	)
	return exec.Command("powershell", "-NonInteractive", "-c", script).Run()
}
