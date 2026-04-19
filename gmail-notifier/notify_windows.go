//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"sync"

	toast "git.sr.ht/~jackmordaunt/go-toast"
	"git.sr.ht/~jackmordaunt/go-toast/wintoast"
)

const notifyAppID = "GmailNotifier"

var notifyOnce sync.Once

func initNotify() {
	notifyOnce.Do(func() {
		if err := toast.SetAppData(toast.AppData{AppID: notifyAppID}); err != nil {
			log.Printf("알림 앱 등록 실패: %v", err)
		}
	})
}

// sysNotify sends a Windows toast notification using PowerShell directly,
// bypassing the WinRT COM path that fails with doc.LoadXml error 3222070623.
func sysNotify(title, body string) {
	initNotify()
	if err := wintoast.Push(toastXML(title, body), wintoast.PreferPowershell); err != nil {
		log.Printf("시스템 알림 실패: %v", err)
	}
}

// toastXML builds a minimal Windows toast XML string.
// Escapes ]]> to prevent premature CDATA section termination.
func toastXML(title, body string) string {
	title = escapeCDATA(title)
	body = escapeCDATA(body)
	return fmt.Sprintf(
		`<toast><visual><binding template="ToastGeneric">`+
			`<text><![CDATA[%s]]></text>`+
			`<text><![CDATA[%s]]></text>`+
			`</binding></visual><audio silent="true"/></toast>`,
		title, body,
	)
}

func escapeCDATA(s string) string {
	// ]]> closes a CDATA section; split into two adjacent sections to encode it literally.
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}
