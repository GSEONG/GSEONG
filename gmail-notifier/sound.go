package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"github.com/gen2brain/beeep"
)

func playSound(cfg SoundConfig) {
	if !cfg.Enabled {
		return
	}

	if cfg.File != "" {
		if err := playSoundFile(cfg.File); err != nil {
			log.Printf("커스텀 사운드 재생 실패 (%s): %v — 시스템 비프음으로 대체", cfg.File, err)
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
	switch runtime.GOOS {
	case "linux":
		return playLinux(path)
	case "darwin":
		return exec.Command("afplay", path).Run()
	case "windows":
		script := fmt.Sprintf(
			`(New-Object Media.SoundPlayer '%s').PlaySync()`,
			path,
		)
		return exec.Command("powershell", "-c", script).Run()
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
