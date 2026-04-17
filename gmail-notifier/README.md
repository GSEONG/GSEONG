# Gmail Desktop Notifier

특정 Gmail을 수신했을 때 데스크탑 알림을 띄워주는 Go 프로그램입니다.

## 설치 및 실행

### 1. Google Cloud Console 설정

1. [Google Cloud Console](https://console.cloud.google.com/)에서 프로젝트 생성
2. **Gmail API** 활성화
3. **OAuth 2.0 클라이언트 ID** 생성 (애플리케이션 유형: **데스크톱 앱**)
4. `credentials.json` 다운로드 후 프로그램과 같은 디렉토리에 저장

### 2. 설정 파일

```bash
cp config.yaml.example config.yaml
```

`config.yaml`을 편집해 원하는 필터를 설정합니다:

```yaml
poll_interval_seconds: 30

credentials_file: credentials.json
token_file: token.json

filters:
  - from: "github.com"     # GitHub 알림 이메일
    subject: ""
  - from: ""
    subject: "긴급"         # 제목에 '긴급'이 포함된 이메일
```

### 3. 빌드 및 실행

```bash
go build -o gmail-notifier .
./gmail-notifier -config config.yaml
```

최초 실행 시 브라우저 인증 URL이 출력됩니다. URL을 열어 Gmail 접근 권한을 허용하고 인증 코드를 붙여넣으세요.

## 필터 규칙

| 조건 | 설명 |
|------|------|
| `from` | 발신자 이메일에 포함된 문자열 (부분 일치, 대소문자 무관) |
| `subject` | 제목에 포함된 문자열 (부분 일치, 대소문자 무관) |

- 한 필터 내에서 `from`과 `subject`는 **AND** 조건
- 여러 필터 사이는 **OR** 조건
- `filters`가 비어있으면 **모든 수신 이메일**에 알림

## 의존성

- [Gmail API](https://pkg.go.dev/google.golang.org/api/gmail/v1)
- [beeep](https://github.com/gen2brain/beeep) — 크로스플랫폼 데스크탑 알림
- [oauth2](https://pkg.go.dev/golang.org/x/oauth2)
