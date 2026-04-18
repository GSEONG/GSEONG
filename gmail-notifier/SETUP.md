# Google Cloud Console 및 Gmail API 설정 가이드

## 목차

1. [Google Cloud 프로젝트 생성](#1-google-cloud-프로젝트-생성)
2. [Gmail API 활성화](#2-gmail-api-활성화)
3. [OAuth 동의 화면 설정](#3-oauth-동의-화면-설정)
4. [OAuth 2.0 클라이언트 ID 생성](#4-oauth-20-클라이언트-id-생성)
5. [credentials.json 저장](#5-credentialsjson-저장)
6. [첫 실행 및 계정 인증](#6-첫-실행-및-계정-인증)
7. [보안 주의사항](#7-보안-주의사항)

---

## 1. Google Cloud 프로젝트 생성

1. 브라우저에서 [Google Cloud Console](https://console.cloud.google.com/) 접속
2. 상단의 프로젝트 선택 드롭다운 클릭 → **새 프로젝트**
3. 프로젝트 이름 입력 (예: `gmail-notifier`) → **만들기**
4. 생성 후 방금 만든 프로젝트가 선택되어 있는지 확인

---

## 2. Gmail API 활성화

1. 왼쪽 메뉴 → **API 및 서비스** → **라이브러리**
2. 검색창에 `Gmail API` 입력
3. **Gmail API** 클릭 → **사용 설정** 버튼 클릭

---

## 3. OAuth 동의 화면 설정

API 키를 발급받기 전에 동의 화면을 먼저 구성해야 합니다.

1. **API 및 서비스** → **OAuth 동의 화면**
2. User Type 선택:
   - **외부**: 본인의 Gmail 계정만 사용할 경우에도 선택 가능
   - **내부**: Google Workspace 조직 계정인 경우
3. 앱 정보 입력:
   - **앱 이름**: `Gmail Notifier` (아무 이름 가능)
   - **사용자 지원 이메일**: 본인 이메일
   - **개발자 연락처**: 본인 이메일
4. **저장 후 계속** 클릭
5. **범위(Scopes)** 화면: 별도 추가 없이 **저장 후 계속**
6. **테스트 사용자** 화면:
   - **사용자 추가** → 본인의 Gmail 주소 입력
   - 이 단계가 중요합니다. 여기 등록된 계정만 인증이 가능합니다.
7. **저장 후 계속** → 완료

> **참고**: 앱이 '게시되지 않음(테스트)' 상태면 refresh token이 7일마다 만료될 수 있습니다.  
> 장기 사용 시 **앱 게시** → 프로덕션 상태로 변경하거나, 만료 시 재인증하면 됩니다.

---

## 4. OAuth 2.0 클라이언트 ID 생성

1. **API 및 서비스** → **사용자 인증 정보**
2. **사용자 인증 정보 만들기** → **OAuth 클라이언트 ID**
3. 애플리케이션 유형: **데스크톱 앱** 선택
4. 이름: `gmail-notifier-client` (아무 이름 가능)
5. **만들기** 클릭
6. 생성된 클라이언트 ID 팝업에서 **JSON 다운로드** 클릭

---

## 5. credentials.json 저장

다운로드한 파일을 프로그램 실행 디렉토리에 `credentials.json` 이름으로 저장합니다.

```bash
mv ~/Downloads/client_secret_*.json ./credentials.json

# 파일 권한을 소유자만 읽을 수 있도록 설정 (중요)
chmod 600 credentials.json
```

파일 내용은 아래와 같은 구조입니다:

```json
{
  "installed": {
    "client_id": "123456789-abc.apps.googleusercontent.com",
    "client_secret": "GOCSPX-...",
    "redirect_uris": ["http://localhost"],
    ...
  }
}
```

> **주의**: `client_secret` 값은 절대 외부에 공유하거나 Git에 커밋하지 마세요.

---

## 6. 첫 실행 및 계정 인증

### 빌드

```bash
go build -o gmail-notifier .
```

### 설정 파일 준비

```bash
cp config.yaml.example config.yaml
# config.yaml 을 편집해 원하는 필터 설정
```

### 실행

```bash
./gmail-notifier
```

최초 실행 시 아래와 같은 메시지가 출력됩니다:

```
브라우저에서 아래 URL을 열어 Gmail 접근 권한을 허용하세요:

https://accounts.google.com/o/oauth2/auth?...

인증 코드를 입력하세요:
```

1. URL을 복사해 브라우저에 붙여넣기
2. Google 계정으로 로그인 → **Gmail 읽기 전용** 권한 허용
3. 브라우저에 표시된 인증 코드를 터미널에 붙여넣기 → Enter

인증 성공 시 `token.json` 파일이 자동으로 생성되고, 이후 실행에서는 재인증 없이 바로 실행됩니다.

```bash
# token.json 도 권한 제한 (프로그램이 자동으로 설정하지만 확인)
chmod 600 token.json
```

---

## 7. 보안 주의사항

### credentials.json / token.json 관리

| 파일 | 내용 | 위험도 | 조치 |
|------|------|--------|------|
| `credentials.json` | OAuth Client ID/Secret | 높음 | `chmod 600`, Git 제외 |
| `token.json` | Refresh Token (계정 접근 키) | 매우 높음 | `chmod 600`, Git 제외 |

`token.json`이 유출되면 타인이 **귀하의 Gmail을 읽기 전용으로 접근**할 수 있습니다.  
유출이 의심되면 즉시 아래에서 권한을 취소하세요:

> [Google 계정 보안 → 앱 접근 권한](https://myaccount.google.com/permissions)  
> → `gmail-notifier` (또는 설정한 앱 이름) → **접근 권한 삭제**

### .gitignore 확인

이 프로젝트의 `.gitignore`에는 아래 파일들이 이미 포함되어 있습니다:

```
credentials.json
token.json
config.yaml
```

Git에 절대 커밋되지 않도록 주의하세요.

### API 권한 범위

이 프로그램은 `gmail.readonly` 스코프만 사용합니다.  
이메일 **읽기**만 가능하며, 전송/삭제/수정은 불가능합니다.

### token.json 만료 및 재발급

- Refresh Token은 **6개월 미사용** 또는 **앱이 테스트 상태에서 7일** 경과 시 만료됩니다.
- 만료 시 `token.json`을 삭제하고 프로그램을 재실행하면 재인증 절차가 진행됩니다.

```bash
rm token.json
./gmail-notifier
```
