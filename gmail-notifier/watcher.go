package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"google.golang.org/api/gmail/v1"
)

const (
	maxRegexPatternLen = 500
	apiCallTimeout     = 15 * time.Second
)

type EmailMessage struct {
	ID      string
	From    string
	Subject string
	Snippet string
}

// compiledFilter holds precompiled patterns for one filter entry.
// Empty slice means "match anything" (wildcard).
type compiledFilter struct {
	from    []*regexp.Regexp
	subject []*regexp.Regexp
}

type Watcher struct {
	svc         *gmail.Service
	cfg         *Config
	filters     []compiledFilter
	lastHistory uint64
	historyMu   sync.Mutex
	notifier    *Notifier
}

func NewWatcher(svc *gmail.Service, cfg *Config, notifier *Notifier) (*Watcher, error) {
	filters, err := compileFilters(cfg.Filters)
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		svc:      svc,
		cfg:      cfg,
		filters:  filters,
		notifier: notifier,
	}

	historyID, err := w.fetchLatestHistoryID()
	if err != nil {
		return nil, fmt.Errorf("초기 historyId 가져오기 실패: %w", err)
	}
	w.lastHistory = historyID
	log.Printf("모니터링 시작 (historyId: %d, 필터 수: %d)", historyID, len(filters))
	return w, nil
}

func compileFilters(filters []FilterConfig) ([]compiledFilter, error) {
	result := make([]compiledFilter, len(filters))
	for i, f := range filters {
		cf := compiledFilter{}

		for _, kw := range f.From {
			if kw == "" {
				continue
			}
			re, err := compilePattern(kw, f.Regex, i, "from")
			if err != nil {
				return nil, err
			}
			cf.from = append(cf.from, re)
		}

		for _, kw := range f.Subject {
			if kw == "" {
				continue
			}
			re, err := compilePattern(kw, f.Regex, i, "subject")
			if err != nil {
				return nil, err
			}
			cf.subject = append(cf.subject, re)
		}

		result[i] = cf
	}
	return result, nil
}

func compilePattern(kw string, isRegex bool, idx int, field string) (*regexp.Regexp, error) {
	if isRegex && len(kw) > maxRegexPatternLen {
		return nil, fmt.Errorf("필터[%d] %s 패턴이 너무 깁니다 (최대 %d자): %q",
			idx, field, maxRegexPatternLen, kw)
	}
	re, err := regexp.Compile(toPattern(kw, isRegex))
	if err != nil {
		return nil, fmt.Errorf("필터[%d] %s 패턴 오류 (%q): %w", idx, field, kw, err)
	}
	return re, nil
}

// toPattern converts a keyword to a regex string.
// Non-regex keywords are escaped and wrapped with case-insensitive flag.
func toPattern(keyword string, isRegex bool) string {
	if isRegex {
		return keyword
	}
	return `(?i)` + regexp.QuoteMeta(keyword)
}

func (w *Watcher) fetchLatestHistoryID() (uint64, error) {
	res, err := w.svc.Users.Messages.List("me").MaxResults(1).LabelIds("INBOX").Do()
	if err != nil {
		return 0, err
	}
	if len(res.Messages) == 0 {
		profile, err := w.svc.Users.GetProfile("me").Do()
		if err != nil {
			return 0, err
		}
		return uint64(profile.HistoryId), nil
	}

	msg, err := w.svc.Users.Messages.Get("me", res.Messages[0].Id).Format("metadata").Do()
	if err != nil {
		return 0, err
	}
	return uint64(msg.HistoryId), nil
}

func (w *Watcher) Poll(ctx context.Context) {
	interval := time.Duration(w.cfg.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("폴링 간격: %v", interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkNewMails(ctx)
		}
	}
}

func (w *Watcher) checkNewMails(ctx context.Context) {
	callCtx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()

	w.historyMu.Lock()
	startID := w.lastHistory
	w.historyMu.Unlock()

	res, err := w.svc.Users.History.List("me").
		Context(callCtx).
		StartHistoryId(startID).
		HistoryTypes("messageAdded").
		LabelId("INBOX").
		Do()
	if err != nil {
		log.Printf("히스토리 조회 오류: %v", err)
		return
	}

	if res.HistoryId > 0 {
		w.historyMu.Lock()
		w.lastHistory = res.HistoryId
		w.historyMu.Unlock()
	}

	for _, h := range res.History {
		for _, added := range h.MessagesAdded {
			msg, err := w.fetchMessage(callCtx, added.Message.Id)
			if err != nil {
				log.Printf("메시지 조회 오류: %v", err)
				continue
			}
			if w.matchesFilter(msg) {
				w.notifier.Notify(msg)
			}
		}
	}
}

func (w *Watcher) fetchMessage(ctx context.Context, id string) (*EmailMessage, error) {
	raw, err := w.svc.Users.Messages.Get("me", id).
		Context(ctx).
		Format("metadata").
		MetadataHeaders("From", "Subject").Do()
	if err != nil {
		return nil, err
	}

	msg := &EmailMessage{
		ID:      id,
		Snippet: raw.Snippet,
	}

	for _, h := range raw.Payload.Headers {
		switch h.Name {
		case "From":
			msg.From = h.Value
		case "Subject":
			msg.Subject = h.Value
		}
	}
	return msg, nil
}

func (w *Watcher) matchesFilter(msg *EmailMessage) bool {
	if len(w.filters) == 0 {
		return true
	}

	for _, f := range w.filters {
		if matchesPatterns(msg.From, f.from) && matchesPatterns(msg.Subject, f.subject) {
			return true
		}
	}
	return false
}

// matchesPatterns returns true if patterns is empty (wildcard) or any pattern matches value.
func matchesPatterns(value string, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, re := range patterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}
