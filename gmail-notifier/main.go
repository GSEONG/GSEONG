package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.yaml", "설정 파일 경로")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("설정 파일 로드 실패: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := newGmailService(ctx, cfg.CredentialsFile, cfg.TokenFile)
	if err != nil {
		log.Fatalf("Gmail 서비스 초기화 실패: %v", err)
	}

	notifier := NewNotifier()

	watcher, err := NewWatcher(svc, cfg, notifier)
	if err != nil {
		log.Fatalf("Watcher 초기화 실패: %v", err)
	}

	go watcher.Poll(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("종료 중...")
	cancel()
}
