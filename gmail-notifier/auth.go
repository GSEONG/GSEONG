package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

func newGmailService(ctx context.Context, credentialsFile, tokenFile string) (*gmail.Service, error) {
	warnIfPermissive(credentialsFile)

	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("credentials 파일을 읽을 수 없습니다 (%s): %w\n"+
			"Google Cloud Console에서 OAuth 2.0 클라이언트 ID를 생성하고 credentials.json으로 저장하세요.", credentialsFile, err)
	}

	oauthCfg, err := google.ConfigFromJSON(data, gmail.GmailReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("OAuth 설정 파싱 실패: %w", err)
	}

	client, err := getHTTPClient(ctx, oauthCfg, tokenFile)
	if err != nil {
		return nil, err
	}

	svc, err := gmail.New(client)
	if err != nil {
		return nil, fmt.Errorf("Gmail 서비스 생성 실패: %w", err)
	}
	return svc, nil
}

// warnIfPermissive logs a warning if the file is readable by group or others.
// Windows uses a different permission model, so the check is skipped there.
func warnIfPermissive(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		fmt.Printf("경고: %s 의 파일 권한이 너무 개방적입니다 (%o). 보안을 위해 chmod 600 %s 를 실행하세요.\n",
			path, info.Mode().Perm(), path)
	}
}

func getHTTPClient(ctx context.Context, cfg *oauth2.Config, tokenFile string) (*http.Client, error) {
	tok, err := loadToken(tokenFile)
	if err != nil {
		tok, err = fetchTokenFromWeb(ctx, cfg, tokenFile)
		if err != nil {
			return nil, err
		}
	}
	return cfg.Client(ctx, tok), nil
}

func loadToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tok := &oauth2.Token{}
	if err := json.NewDecoder(f).Decode(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func saveToken(path string, tok *oauth2.Token) error {
	// 0600: 소유자만 읽기/쓰기 가능 — refresh token 유출 방지
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("토큰 저장 실패 (%s): %w", path, err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(tok)
}

func fetchTokenFromWeb(ctx context.Context, cfg *oauth2.Config, tokenFile string) (*oauth2.Token, error) {
	// 빈 포트로 로컬 서버 시작 — OS가 사용 가능한 포트를 자동 배정
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("로컬 인증 서버 시작 실패: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	cfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	trayMgr.SetStatus("🔐 Gmail 인증 필요")
	log.Printf("Gmail 인증 URL: %s", authURL)
	sysNotify("Gmail 알리미 — 인증 필요", "브라우저에서 Gmail 접근 권한을 허용해주세요.")
	openBrowser(authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			fmt.Fprintf(w, "<h2>인증 실패</h2><p>%s</p><p>터미널을 확인하세요.</p>", errMsg)
			errCh <- fmt.Errorf("인증 거부 또는 오류: %s", errMsg)
			return
		}
		fmt.Fprintf(w, "<h2>인증 완료!</h2><p>이 창을 닫고 터미널로 돌아가세요.</p>")
		codeCh <- code
	})

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("인증 서버 오류: %w", err)
		}
	}()

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		srv.Shutdown(context.Background())
		return nil, err
	case <-ctx.Done():
		srv.Shutdown(context.Background())
		return nil, ctx.Err()
	}
	srv.Shutdown(context.Background())

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("인증 코드 교환 실패: %w", err)
	}

	if err := saveToken(tokenFile, tok); err != nil {
		fmt.Printf("경고: 토큰 저장 실패 - %v\n", err)
	}
	trayMgr.SetStatus("✅ 모니터링 중")
	log.Println("인증 성공: token.json 저장 완료")
	return tok, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// cmd /c start 는 & 를 명령어 구분자로 해석해 URL을 잘라냄.
		// PowerShell Start-Process 로 URL 전체를 문자열로 전달한다.
		cmd = exec.Command("powershell", "-NoProfile", "-c",
			fmt.Sprintf(`Start-Process "%s"`, url))
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
