package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/api/gmail/v1"
)

//go:embed dashboard.html
var dashboardHTMLBytes []byte

var dashServer = &DashboardServer{}

type attEntry struct {
	MsgID    string `json:"msgId"`
	AttID    string `json:"attId"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type mailEntry struct {
	ID      string     `json:"id"`
	Time    string     `json:"time"`
	From    string     `json:"from"`
	Subject string     `json:"subject"`
	Atts    []attEntry `json:"atts"`
	IsHist  bool       `json:"isHist"` // true = loaded from Gmail history (pre-server-start)
}

type DashboardServer struct {
	mu          sync.Mutex
	port        int
	hub         *sseHub
	status      string
	recentMails []mailEntry
	seen        map[string]struct{} // dedup by Gmail message ID
	todayCount  int
	totalCount  int
	startTime   time.Time
	lastMailAt  time.Time
	logPath     string
	gmailSvc    *gmail.Service
}

type sseHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan string]struct{})}
}

func (h *sseHub) subscribe() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *sseHub) broadcast(event, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (d *DashboardServer) Start(logPath string) (int, error) {
	d.mu.Lock()
	d.hub       = newSSEHub()
	d.status    = "⏳ 시작 중..."
	d.startTime = time.Now()
	d.logPath   = logPath
	d.seen      = make(map[string]struct{})
	d.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	d.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/events", d.handleSSE)
	mux.HandleFunc("/state", d.handleState)
	mux.HandleFunc("/logs", d.handleLogs)
	mux.HandleFunc("/download", d.handleDownload)

	go func() {
		if err := http.Serve(ln, mux); err != nil {
			log.Printf("dashboard server error: %v", err)
		}
	}()

	// Periodic log push every 5 s
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			lines := tailLog(d.logPath, 50)
			if b, err := json.Marshal(lines); err == nil {
				d.hub.broadcast("logs", string(b))
			}
		}
	}()

	// Midnight reset of today count
	go func() {
		for {
			now  := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(next))
			d.mu.Lock()
			d.todayCount = 0
			d.mu.Unlock()
			d.pushState()
		}
	}()

	return d.port, nil
}

func (d *DashboardServer) SetGmailService(svc *gmail.Service) {
	d.mu.Lock()
	d.gmailSvc = svc
	d.mu.Unlock()
}

func (d *DashboardServer) SetStatus(s string) {
	d.mu.Lock()
	d.status = s
	d.mu.Unlock()
	d.pushState()
}

func (d *DashboardServer) AddMail(msg *EmailMessage) {
	atts := make([]attEntry, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		atts = append(atts, attEntry{
			MsgID:    msg.ID,
			AttID:    a.AttachmentID,
			Filename: a.Filename,
			Size:     a.Size,
		})
	}
	entry := mailEntry{
		ID:      msg.ID,
		Time:    time.Now().Format("15:04:05"),
		From:    msg.From,
		Subject: msg.Subject,
		Atts:    atts,
	}
	d.mu.Lock()
	d.seen[msg.ID] = struct{}{}
	d.recentMails = append([]mailEntry{entry}, d.recentMails...)
	if len(d.recentMails) > 200 {
		d.recentMails = d.recentMails[:200]
	}
	d.todayCount++
	d.totalCount++
	d.lastMailAt = time.Now()
	d.mu.Unlock()
	d.pushState()
}

// LoadHistory fetches the n most recent inbox messages from Gmail that arrived
// before the server started, and appends them to recentMails as historical entries.
// Should be called in a goroutine after SetGmailService.
func (d *DashboardServer) LoadHistory(n int) {
	d.mu.Lock()
	svc := d.gmailSvc
	d.mu.Unlock()
	if svc == nil {
		return
	}

	log.Printf("[대시보드] 히스토리 로드 시작 (최대 %d건)", n)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := svc.Users.Messages.List("me").
		Context(ctx).
		MaxResults(int64(n)).
		LabelIds("INBOX").
		Do()
	if err != nil {
		log.Printf("[대시보드] 히스토리 목록 조회 실패: %v", err)
		return
	}
	if len(res.Messages) == 0 {
		return
	}

	// Fetch metadata concurrently (up to 10 at a time)
	type result struct {
		entry mailEntry
		ok    bool
	}

	sem     := make(chan struct{}, 10)
	results := make([]result, len(res.Messages))
	var wg sync.WaitGroup

	for i, m := range res.Messages {
		id := m.Id

		d.mu.Lock()
		_, exists := d.seen[id]
		d.mu.Unlock()
		if exists {
			continue
		}

		wg.Add(1)
		go func(idx int, msgID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Format("full") is required to get payload.parts with body.attachmentId.
			// Format("metadata") omits parts, so attachments cannot be detected.
			raw, err := svc.Users.Messages.Get("me", msgID).
				Context(ctx).
				Format("full").
				Do()
			if err != nil {
				return
			}

			entry := mailEntry{
				ID:     msgID,
				IsHist: true,
				Time:   time.UnixMilli(raw.InternalDate).Local().Format("01-02 15:04"),
				Atts:   []attEntry{},
			}
			if raw.Payload != nil {
				for _, h := range raw.Payload.Headers {
					switch h.Name {
					case "From":
						entry.From = h.Value
					case "Subject":
						entry.Subject = h.Value
					}
				}
				for _, a := range extractAttachments(raw.Payload.Parts) {
					entry.Atts = append(entry.Atts, attEntry{
						MsgID:    msgID,
						AttID:    a.AttachmentID,
						Filename: a.Filename,
						Size:     a.Size,
					})
				}
			}
			results[idx] = result{entry: entry, ok: true}
		}(i, id)
	}
	wg.Wait()

	var entries []mailEntry
	for _, r := range results {
		if r.ok {
			entries = append(entries, r.entry)
		}
	}
	if len(entries) == 0 {
		log.Printf("[대시보드] 히스토리: 신규 항목 없음 (모두 중복)")
		return
	}

	d.mu.Lock()
	for _, e := range entries {
		d.seen[e.ID] = struct{}{}
	}
	// Historical entries go after live entries (live = newest first, hist = below)
	d.recentMails = append(d.recentMails, entries...)
	if len(d.recentMails) > 200 {
		d.recentMails = d.recentMails[:200]
	}
	d.mu.Unlock()

	d.pushState()
	log.Printf("[대시보드] 히스토리 로드 완료: %d건", len(entries))
}

func (d *DashboardServer) pushState() {
	b, err := json.Marshal(d.stateSnapshot())
	if err != nil {
		return
	}
	d.hub.broadcast("state", string(b))
}

type stateSnapshot struct {
	Status     string      `json:"status"`
	TodayCount int         `json:"todayCount"`
	TotalCount int         `json:"totalCount"`
	UptimeSec  int         `json:"uptimeSec"`
	LastMailAt string      `json:"lastMailAt"`
	Mails      []mailEntry `json:"mails"`
}

func (d *DashboardServer) stateSnapshot() stateSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	lastStr := ""
	if !d.lastMailAt.IsZero() {
		lastStr = d.lastMailAt.Format("01-02 15:04:05")
	}
	return stateSnapshot{
		Status:     d.status,
		TodayCount: d.todayCount,
		TotalCount: d.totalCount,
		UptimeSec:  int(time.Since(d.startTime).Seconds()),
		LastMailAt: lastStr,
		Mails:      d.recentMails,
	}
}

func (d *DashboardServer) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d.stateSnapshot())
}

func (d *DashboardServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tailLog(d.logPath, 50))
}

func (d *DashboardServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	msgID    := r.URL.Query().Get("msgId")
	attID    := r.URL.Query().Get("attId")
	filename := filepath.Base(r.URL.Query().Get("filename"))

	if msgID == "" || attID == "" || filename == "" || filename == "." {
		http.Error(w, "invalid params", http.StatusBadRequest)
		return
	}

	d.mu.Lock()
	svc := d.gmailSvc
	d.mu.Unlock()

	if svc == nil {
		http.Error(w, "Gmail service not ready", http.StatusServiceUnavailable)
		return
	}

	res, err := svc.Users.Messages.Attachments.Get("me", msgID, attID).Do()
	if err != nil {
		log.Printf("[대시보드] 첨부파일 가져오기 실패 (%s): %v", filename, err)
		http.Error(w, "failed to fetch attachment", http.StatusInternalServerError)
		return
	}

	data, err := base64.RawURLEncoding.DecodeString(res.Data)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(res.Data)
		if err != nil {
			http.Error(w, "decode error", http.StatusInternalServerError)
			return
		}
	}

	log.Printf("[대시보드] 첨부파일 다운로드: %s (%d bytes, 메시지: %s)", filename, len(data), msgID)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

func (d *DashboardServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := d.hub.subscribe()
	defer d.hub.unsubscribe(ch)

	if b, err := json.Marshal(d.stateSnapshot()); err == nil {
		fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
	}
	if b, err := json.Marshal(tailLog(d.logPath, 50)); err == nil {
		fmt.Fprintf(w, "event: logs\ndata: %s\n\n", b)
	}
	flusher.Flush()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (d *DashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTMLBytes)
}

func tailLog(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
