package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
)

func main() {
	configPath := flag.String("config", "config.yaml", "설정 파일 경로")
	flag.Parse()

	logPath, logFile := setupLogger()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("설정 파일 로드 실패: %v", err)
	}

	absConfig, err := filepath.Abs(*configPath)
	if err != nil {
		absConfig = *configPath
	}

	dashPort, err := dashServer.Start(logPath)
	if err != nil {
		log.Printf("대시보드 서버 시작 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	runTray(
		cfg,
		logPath,
		logFile,
		absConfig,
		dashPort,
		func() {
			trayMgr.SetStatus("🔗 Gmail 연결 중...")

			svc, err := newGmailService(ctx, cfg.CredentialsFile, cfg.TokenFile)
			if err != nil {
				trayMgr.SetStatus("❌ 연결 실패")
				log.Printf("Gmail 서비스 초기화 실패: %v", err)
				return
			}

			notifier := NewNotifier(cfg.Sound, svc)
			dashServer.SetGmailService(svc)

			watcher, err := NewWatcher(svc, cfg, notifier)
			if err != nil {
				trayMgr.SetStatus("❌ 초기화 실패")
				log.Printf("Watcher 초기화 실패: %v", err)
				return
			}

			go dashServer.LoadHistory(50)

			trayMgr.SetStatus("✅ 모니터링 중")
			watcher.Poll(ctx)
		},
		func() {
			log.Println("종료 중...")
			cancel()
		},
	)
}

// setupLogger redirects log output to gmail-notifier.log next to the executable.
// Returns the log file path and the open file handle.
func setupLogger() (string, *os.File) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil
	}
	logPath := filepath.Join(filepath.Dir(exe), "gmail-notifier.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return logPath, nil
	}
	log.SetOutput(f)
	log.SetFlags(log.Ldate | log.Ltime)
	return logPath, f
}
