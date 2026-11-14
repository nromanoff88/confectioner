package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"confectioner-backend/internal/bot"
	"confectioner-backend/internal/config"
	"confectioner-backend/internal/db"
)

type initDataUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// ValidateMaxInitData implements the single-URL-decode HMAC-SHA256 validation
// for window.WebApp.initData per MAX Bridge security guidelines.
func ValidateMaxInitData(initData, botToken string, maxAge time.Duration) (*initDataUser, string, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, "", fmt.Errorf("parse initData: %w", err)
	}

	hashValues, ok := values["hash"]
	if !ok || len(hashValues) != 1 {
		return nil, "", fmt.Errorf("hash parameter must be present")
	}
	receivedHash := hashValues[0]
	values.Del("hash")

	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("parse auth_date: %w", err)
	}
	if maxAge > 0 && time.Since(time.Unix(authDate, 0)) > maxAge {
		return nil, "", fmt.Errorf("auth_date is expired")
	}

	pairs := make([]string, 0, len(values))
	for key, vals := range values {
		for _, v := range vals {
			pairs = append(pairs, key+"="+v)
		}
	}
	sort.Strings(pairs)
	dataCheckString := strings.Join(pairs, "\n")

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(botToken))
	secretKey := secretMAC.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(dataCheckString))
	computedHash := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(receivedHash), []byte(computedHash)) != 1 {
		return nil, "", fmt.Errorf("initData HMAC signature mismatch")
	}

	var user initDataUser
	if rawUser := values.Get("user"); rawUser != "" {
		_ = json.Unmarshal([]byte(rawUser), &user)
	}
	return &user, values.Get("start_param"), nil
}

type authedHandler func(w http.ResponseWriter, r *http.Request, m *db.Member)

// auth identifies the confectioner from the signed MAX initData and scopes the request
// to their workspace. Outside DEV_MODE a valid signature is always required.
func (s *Server) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, status, msg := s.authenticate(r)
		if m == nil {
			writeError(w, status, msg)
			return
		}
		next(w, r, m)
	}
}

func (s *Server) authenticate(r *http.Request) (*db.Member, int, string) {
	initData := strings.TrimSpace(r.Header.Get("X-Max-Init-Data"))
	if initData == "" {
		initData = strings.TrimSpace(r.URL.Query().Get("init_data"))
	}

	var info bot.UserInfo
	if initData == "" {
		if !s.cfg.DevMode {
			return nil, http.StatusUnauthorized, "Откройте админку через бота в MAX"
		}
		id := int64(999000001)
		raw := r.Header.Get("X-Dev-User-Id")
		if raw == "" {
			raw = r.URL.Query().Get("dev_user")
		}
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			id = v
		}
		info = bot.UserInfo{ID: id, FirstName: "Dev"}
	} else {
		if s.cfg.MaxBotToken == "" {
			return nil, http.StatusServiceUnavailable, "MAX_BOT_TOKEN не настроен"
		}
		u, _, err := ValidateMaxInitData(initData, s.cfg.MaxBotToken, 24*time.Hour)
		if err != nil || u.ID == 0 {
			return nil, http.StatusUnauthorized, "Неверная подпись MAX initData"
		}
		info = bot.UserInfo{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username}
	}

	m, status, err := s.bot.EnsureMember(r.Context(), info, 0, "")
	if err != nil {
		return nil, http.StatusInternalServerError, "Ошибка базы данных"
	}
	if status == "denied" || m == nil {
		return nil, http.StatusForbidden, "Доступ закрыт. Попросите владельца кондитерской прислать ссылку-приглашение."
	}
	return m, 0, ""
}

func VerifyWebhookSecret(cfg *config.Config, r *http.Request) bool {
	if cfg.MaxWebhookSecret == "" {
		return true
	}
	got := r.Header.Get("X-Max-Bot-Api-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(cfg.MaxWebhookSecret)) == 1
}
