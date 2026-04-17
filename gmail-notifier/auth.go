package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

func newGmailService(ctx context.Context, credentialsFile, tokenFile string) (*gmail.Service, error) {
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
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("토큰 저장 실패 (%s): %w", path, err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(tok)
}

func fetchTokenFromWeb(ctx context.Context, cfg *oauth2.Config, tokenFile string) (*oauth2.Token, error) {
	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("\n브라우저에서 아래 URL을 열어 Gmail 접근 권한을 허용하세요:\n\n%s\n\n", authURL)
	fmt.Print("인증 코드를 입력하세요: ")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("인증 코드 읽기 실패: %w", err)
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("인증 코드 교환 실패: %w", err)
	}

	if err := saveToken(tokenFile, tok); err != nil {
		fmt.Printf("경고: 토큰 저장 실패 - %v\n", err)
	}
	return tok, nil
}
