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

	"confectioner-backend/internal/config"
)

type OperatorUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// ValidateMaxInitData implements the single-URL-decode HMAC-SHA256 validation
// for window.WebApp.initData per MAX Bridge security guidelines.
func ValidateMaxInitData(initData, botToken string, maxAge time.Duration) (*OperatorUser, string, error) {
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

	var user OperatorUser
	if rawUser := values.Get("user"); rawUser != "" {
		_ = json.Unmarshal([]byte(rawUser), &user)
	}
	startParam := values.Get("start_param")
	return &user, startParam, nil
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow CORS preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		initData := strings.TrimSpace(r.Header.Get("X-Max-Init-Data"))
		if initData == "" {
			initData = strings.TrimSpace(r.URL.Query().Get("init_data"))
		}

		// When opened in a standard browser (outside MAX WebView), initData is empty.
		// Allow local/browser access unless strict MAX WebApp auth is enforced.
		if initData == "" {
			next(w, r)
			return
		}

		if s.cfg.MaxBotToken == "" {
			next(w, r)
			return
		}

		user, _, err := ValidateMaxInitData(initData, s.cfg.MaxBotToken, 24*time.Hour)
		if err != nil {
			if s.cfg.DevMode {
				next(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "Неверная подпись MAX initData: "+err.Error())
			return
		}

		if user != nil && !s.cfg.IsOperator(user.ID) {
			writeError(w, http.StatusForbidden, fmt.Sprintf("Доступ запрещён для пользователя ID=%d (добавьте ID в OPERATOR_IDS)", user.ID))
			return
		}

		next(w, r)
	}
}

func VerifyWebhookSecret(cfg *config.Config, r *http.Request) bool {
	if cfg.MaxWebhookSecret == "" {
		return true
	}
	got := r.Header.Get("X-Max-Bot-Api-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(cfg.MaxWebhookSecret)) == 1
}

