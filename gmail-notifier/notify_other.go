//go:build !windows

package main

import (
	"log"

	"github.com/gen2brain/beeep"
)

func sysNotify(title, body string) {
	if err := beeep.Notify(title, body, ""); err != nil {
		log.Printf("시스템 알림 실패: %v", err)
	}
}
