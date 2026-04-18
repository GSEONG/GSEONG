package main

import (
	"bufio"
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

var dashServer = &DashboardServer{}

type attEntry struct {
	MsgID    string `json:"msgId"`
	AttID    string `json:"attId"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type mailEntry struct {
	Time    string     `json:"time"`
	From    string     `json:"from"`
	Subject string     `json:"subject"`
	Atts    []attEntry `json:"atts"`
}

type DashboardServer struct {
	mu          sync.Mutex
	port        int
	hub         *sseHub
	status      string
	recentMails []mailEntry
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
	d.hub = newSSEHub()
	d.status = "⏳ 시작 중..."
	d.startTime = time.Now()
	d.logPath = logPath
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
			now := time.Now()
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
		Time:    time.Now().Format("15:04:05"),
		From:    msg.From,
		Subject: msg.Subject,
		Atts:    atts,
	}
	d.mu.Lock()
	d.recentMails = append([]mailEntry{entry}, d.recentMails...)
	if len(d.recentMails) > 50 {
		d.recentMails = d.recentMails[:50]
	}
	d.todayCount++
	d.totalCount++
	d.lastMailAt = time.Now()
	d.mu.Unlock()
	d.pushState()
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
	msgID := r.URL.Query().Get("msgId")
	attID := r.URL.Query().Get("attId")
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
	lines := tailLog(d.logPath, 50)
	if b, err := json.Marshal(lines); err == nil {
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
	fmt.Fprint(w, dashboardHTML)
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

const dashboardHTML = `<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Gmail 알리미 대시보드</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f5f5f5;color:#333}
header{background:#4285F4;color:#fff;padding:16px 24px;display:flex;align-items:center;gap:12px;box-shadow:0 2px 4px rgba(0,0,0,.2)}
header svg{width:28px;height:28px;fill:#fff}
header h1{font-size:1.25rem;font-weight:600}
#status-badge{margin-left:auto;font-size:.85rem;background:rgba(255,255,255,.2);padding:4px 12px;border-radius:12px;white-space:nowrap}
#notif-btn{margin-left:8px;font-size:.8rem;background:rgba(255,255,255,.15);border:1px solid rgba(255,255,255,.4);color:#fff;padding:4px 10px;border-radius:8px;cursor:pointer}
#notif-btn:hover{background:rgba(255,255,255,.25)}
.cards{display:flex;gap:16px;padding:20px 24px;flex-wrap:wrap}
.card{background:#fff;border-radius:10px;padding:18px 22px;flex:1;min-width:160px;box-shadow:0 1px 3px rgba(0,0,0,.1)}
.card .label{font-size:.78rem;color:#888;text-transform:uppercase;letter-spacing:.05em}
.card .value{font-size:2rem;font-weight:700;color:#4285F4;margin-top:4px}
.card .sub{font-size:.78rem;color:#aaa;margin-top:2px}
section{margin:0 24px 20px;background:#fff;border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.1);overflow:hidden}
section h2{font-size:.9rem;font-weight:600;padding:14px 18px;border-bottom:1px solid #eee;background:#fafafa;color:#555}
table{width:100%;border-collapse:collapse;font-size:.88rem}
th{text-align:left;padding:10px 14px;background:#f9f9f9;color:#777;font-weight:500;border-bottom:1px solid #eee}
td{padding:9px 14px;border-bottom:1px solid #f0f0f0;vertical-align:top}
tr:last-child td{border-bottom:none}
tr:hover td{background:#fafcff}
.att-list{display:flex;flex-direction:column;gap:4px;margin-top:2px}
.att-link{display:inline-flex;align-items:center;gap:4px;background:#e8f0fe;color:#1a73e8;border-radius:4px;padding:2px 8px;font-size:.78rem;text-decoration:none;white-space:nowrap}
.att-link:hover{background:#d2e3fc}
.att-link .sz{color:#5f6368;font-size:.72rem}
#log-box{font-family:'Courier New',monospace;font-size:.8rem;background:#1e1e1e;color:#d4d4d4;padding:14px 18px;max-height:260px;overflow-y:auto;white-space:pre-wrap;word-break:break-all}
.no-data{color:#bbb;font-size:.85rem;padding:18px;text-align:center}
#reconnect{display:none;background:#fff3cd;color:#856404;padding:8px 16px;text-align:center;font-size:.85rem}
</style>
</head>
<body>
<header>
<svg viewBox="0 0 24 24"><path d="M20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4-8 5-8-5V6l8 5 8-5v2z"/></svg>
<h1>Gmail 알리미 대시보드</h1>
<span id="status-badge">연결 중...</span>
<button id="notif-btn" onclick="requestNotifPerm()">🔔 알림 허용</button>
</header>
<div id="reconnect">서버와 연결이 끊겼습니다. 재연결 중...</div>
<div class="cards">
  <div class="card"><div class="label">오늘 수신</div><div class="value" id="cnt-today">0</div><div class="sub">이메일</div></div>
  <div class="card"><div class="label">총 수신</div><div class="value" id="cnt-total">0</div><div class="sub">이메일</div></div>
  <div class="card"><div class="label">마지막 수신</div><div class="value" style="font-size:1.1rem;padding-top:8px" id="last-mail">-</div><div class="sub" id="uptime-sub"></div></div>
</div>
<section>
  <h2>최근 이메일 (최대 50건)</h2>
  <div id="mail-wrap"><div class="no-data">수신된 이메일이 없습니다.</div></div>
</section>
<section>
  <h2>로그 (최근 50줄)</h2>
  <div id="log-box">로그를 불러오는 중...</div>
</section>
<script>
let es, prevTotal=0;

// ── Notification permission ──────────────────────────────
function requestNotifPerm(){
  if(!('Notification' in window)){alert('이 브라우저는 알림을 지원하지 않습니다.');return;}
  Notification.requestPermission().then(updateNotifBtn);
}
function updateNotifBtn(){
  const btn=document.getElementById('notif-btn');
  if(!('Notification' in window)){btn.style.display='none';return;}
  if(Notification.permission==='granted'){btn.textContent='🔔 알림 켜짐';btn.style.opacity='.6';btn.style.cursor='default';}
  else if(Notification.permission==='denied'){btn.textContent='🔕 알림 차단됨';btn.style.opacity='.6';}
}

// ── AudioContext beep ────────────────────────────────────
function playBeep(){
  try{
    const ctx=new(window.AudioContext||window.webkitAudioContext)();
    const osc=ctx.createOscillator();
    const gain=ctx.createGain();
    osc.connect(gain);gain.connect(ctx.destination);
    osc.type='sine';osc.frequency.value=880;
    gain.gain.setValueAtTime(0.3,ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001,ctx.currentTime+0.8);
    osc.start(ctx.currentTime);osc.stop(ctx.currentTime+0.8);
    setTimeout(()=>ctx.close(),1200);
  }catch(e){}
}

// ── Desktop notification ─────────────────────────────────
function showDesktopNotif(mail){
  if(Notification.permission!=='granted')return;
  const n=new Notification('새 이메일: '+mail.from,{
    body:mail.subject+(mail.atts&&mail.atts.length?' 📎 '+mail.atts.length+'개':''),
    icon:'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect fill="%234285F4" width="24" height="24" rx="4"/><path fill="white" d="M20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4-8 5-8-5V6l8 5 8-5v2z"/></svg>'
  });
  setTimeout(()=>n.close(),6000);
}

// ── SSE connection ───────────────────────────────────────
function connect(){
  es=new EventSource('/events');
  document.getElementById('reconnect').style.display='none';

  es.addEventListener('state',e=>{
    const d=JSON.parse(e.data);
    document.getElementById('status-badge').textContent=d.status;
    document.getElementById('cnt-today').textContent=d.todayCount;
    document.getElementById('cnt-total').textContent=d.totalCount;
    document.getElementById('last-mail').textContent=d.lastMailAt||'-';
    const h=Math.floor(d.uptimeSec/3600),m=Math.floor((d.uptimeSec%3600)/60),s=d.uptimeSec%60;
    document.getElementById('uptime-sub').textContent='가동: '+h+'h '+m+'m '+s+'s';

    if(prevTotal>0 && d.totalCount>prevTotal && d.mails && d.mails.length>0){
      playBeep();
      showDesktopNotif(d.mails[0]);
    }
    prevTotal=d.totalCount;
    renderMails(d.mails);
  });

  es.addEventListener('logs',e=>{
    const lines=JSON.parse(e.data);
    const box=document.getElementById('log-box');
    box.textContent=lines.join('\n');
    box.scrollTop=box.scrollHeight;
  });

  es.onerror=()=>{
    document.getElementById('reconnect').style.display='block';
    es.close();
    setTimeout(connect,3000);
  };
}

// ── Render mail table ────────────────────────────────────
function fmtSize(b){
  if(b>=1048576)return (b/1048576).toFixed(1)+' MB';
  if(b>=1024)return Math.round(b/1024)+' KB';
  return b+' B';
}
function renderMails(mails){
  const wrap=document.getElementById('mail-wrap');
  if(!mails||mails.length===0){wrap.innerHTML='<div class="no-data">수신된 이메일이 없습니다.</div>';return;}
  let html='<table><thead><tr><th>시간</th><th>발신자</th><th>제목</th><th>첨부파일</th></tr></thead><tbody>';
  mails.forEach(m=>{
    let attHtml='';
    if(m.atts&&m.atts.length>0){
      attHtml='<div class="att-list">';
      m.atts.forEach(a=>{
        const url='/download?msgId='+encodeURIComponent(a.msgId)+'&attId='+encodeURIComponent(a.attId)+'&filename='+encodeURIComponent(a.filename);
        attHtml+=` + "`" + `<a class="att-link" href="${url}" download="${esc(a.filename)}">📎 ${esc(a.filename)} <span class="sz">${fmtSize(a.size)}</span></a>` + "`" + `;
      });
      attHtml+='</div>';
    }
    html+=` + "`" + `<tr><td>${esc(m.time)}</td><td>${esc(m.from)}</td><td>${esc(m.subject)}</td><td>${attHtml}</td></tr>` + "`" + `;
  });
  html+='</tbody></table>';
  wrap.innerHTML=html;
}

function esc(s){
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// ── Init ─────────────────────────────────────────────────
updateNotifBtn();
connect();
setInterval(()=>{
  if(es&&es.readyState===1){
    fetch('/state').then(r=>r.json()).then(d=>{
      const h=Math.floor(d.uptimeSec/3600),m=Math.floor((d.uptimeSec%3600)/60),s=d.uptimeSec%60;
      document.getElementById('uptime-sub').textContent='가동: '+h+'h '+m+'m '+s+'s';
    }).catch(()=>{});
  }
},10000);
</script>
</body>
</html>`
