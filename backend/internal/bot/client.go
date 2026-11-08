package bot

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"confectioner-backend/internal/config"
	"confectioner-backend/internal/ratelimit"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

const MaxBaseURL = "https://platform-api2.max.ru"

type Client struct {
	cfg           *config.Config
	sdk           *maxbot.Api
	httpClient    *http.Client
	globalLimiter *ratelimit.Limiter
	answerLimiter *ratelimit.ChatLimiterMap
	botID         int64
	botUsername   string
}

func NewClient(cfg *config.Config) (*Client, error) {
	httpClient := buildTrustedHTTPClient(cfg.MaxRootCAPath, 45*time.Second)

	var sdk *maxbot.Api
	if cfg.MaxBotToken != "" {
		var err error
		sdk, err = maxbot.NewApi(
			cfg.MaxBotToken,
			maxbot.WithHTTPClient(httpClient),
			maxbot.WithBaseURL(MaxBaseURL),
			maxbot.WithPollingTimeout(25*time.Second),
		)
		if err != nil {
			return nil, fmt.Errorf("init maxbot sdk: %w", err)
		}
	}

	return &Client{
		cfg:           cfg,
		sdk:           sdk,
		httpClient:    httpClient,
		globalLimiter: ratelimit.New(30),
		answerLimiter: ratelimit.NewChatLimiterMap(2),
		botUsername:   cfg.MaxBotName,
	}, nil
}

// buildTrustedHTTPClient loads the system cert pool and appends the Russian Trusted Root CA
// (Минцифры) if present at rootCAPath. Never sets InsecureSkipVerify.
func buildTrustedHTTPClient(rootCAPath string, timeout time.Duration) *http.Client {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if rootCAPath != "" {
		if pemBytes, err := os.ReadFile(rootCAPath); err == nil && len(pemBytes) > 0 {
			if ok := pool.AppendCertsFromPEM(pemBytes); ok {
				log.Printf("[TLS] Loaded Russian Trusted Root CA from %s", rootCAPath)
			}
		}
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

func (c *Client) IsConfigured() bool {
	return c.cfg.MaxBotToken != "" && c.sdk != nil
}

func (c *Client) FetchBotInfo(ctx context.Context) {
	if !c.IsConfigured() {
		return
	}
	if err := c.globalLimiter.Wait(ctx); err != nil {
		return
	}
	info, err := c.sdk.Bots.GetMyInfo(ctx)
	if err != nil {
		log.Printf("[MAX Bot] GetMyInfo warning: %v", err)
		return
	}
	c.botID = info.UserID
	if info.Username != "" {
		c.botUsername = info.Username
	}
	log.Printf("[MAX Bot] Connected as @%s (ID: %d)", c.botUsername, c.botID)
}

// Raw JSON structures for GET /updates so we capture every attachment field (contact vcf_info, image url, etc.)
type RawUpdateList struct {
	Updates []RawUpdate `json:"updates"`
	Marker  *int64      `json:"marker"`
}

type RawUpdate struct {
	UpdateType string       `json:"update_type"`
	Timestamp  int64        `json:"timestamp"`
	ChatID     int64        `json:"chat_id"`
	User       *RawUser     `json:"user"`
	Payload    string       `json:"payload"`
	Message    *RawMessage  `json:"message"`
	Callback   *RawCallback `json:"callback"`
}

type RawUser struct {
	UserID    int64  `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

type RawCallback struct {
	Timestamp  int64   `json:"timestamp"`
	CallbackID string  `json:"callback_id"`
	Payload    string  `json:"payload"`
	User       RawUser `json:"user"`
}

type RawMessage struct {
	Sender    *RawUser       `json:"sender"`
	Recipient RawRecipient   `json:"recipient"`
	Timestamp int64          `json:"timestamp"`
	Body      RawMessageBody `json:"body"`
}

type RawRecipient struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
}

type RawMessageBody struct {
	MID         string          `json:"mid"`
	Text        string          `json:"text"`
	Attachments []RawAttachment `json:"attachments"`
}

type RawAttachment struct {
	Type     string            `json:"type"`
	Payload  RawAttachPayload  `json:"payload"`
	Filename string            `json:"filename"`
	Size     int64             `json:"size"`
}

type RawAttachPayload struct {
	PhotoID int64    `json:"photo_id"`
	Token   string   `json:"token"`
	URL     string   `json:"url"`
	VCFInfo string   `json:"vcf_info"`
	Hash    string   `json:"hash"`
	MaxInfo *RawUser `json:"max_info"`
}

func (c *Client) GetUpdates(ctx context.Context, marker int64) ([]RawUpdate, int64, error) {
	if !c.IsConfigured() {
		return nil, marker, errors.New("bot token not configured")
	}
	if err := c.globalLimiter.Wait(ctx); err != nil {
		return nil, marker, err
	}

	q := url.Values{}
	q.Set("limit", "50")
	q.Set("timeout", "25")
	if marker > 0 {
		q.Set("marker", fmt.Sprintf("%d", marker))
	}

	reqURL := fmt.Sprintf("%s/updates?%s", MaxBaseURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, marker, err
	}
	req.Header.Set("Authorization", c.cfg.MaxBotToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, marker, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, marker, fmt.Errorf("GET /updates HTTP %d: %s", resp.StatusCode, string(body))
	}

	var list RawUpdateList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, marker, err
	}
	nextMarker := marker
	if list.Marker != nil {
		nextMarker = *list.Marker
	}
	return list.Updates, nextMarker, nil
}

// Button & Keyboard request structures matching schema.yaml
type ButtonReq struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Payload   string `json:"payload,omitempty"`
	URL       string `json:"url,omitempty"`
	WebApp    string `json:"web_app,omitempty"`
	ContactID int64  `json:"contact_id,omitempty"`
}

func (c *Client) SendMessageWithKeyboard(ctx context.Context, chatID, userID int64, text string, buttons [][]ButtonReq) (string, error) {
	if !c.IsConfigured() {
		return "", nil
	}

	parts := splitMessage(text, 4000)
	var lastMID string
	for i, part := range parts {
		var kb [][]ButtonReq
		if i == len(parts)-1 {
			kb = buttons
		}
		mid, err := c.sendSingleMessage(ctx, chatID, userID, part, kb, nil)
		if err != nil {
			return lastMID, err
		}
		lastMID = mid
	}
	return lastMID, nil
}

func (c *Client) SendMessageWithImage(ctx context.Context, chatID, userID int64, text string, imgBytes []byte, filename string) (string, error) {
	if !c.IsConfigured() {
		return "", nil
	}

	token, err := c.uploadImageWithRetry(ctx, imgBytes, filename)
	if err != nil {
		return "", fmt.Errorf("upload image to MAX: %w", err)
	}

	// Send with attachment.not.ready backoff retry
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		mid, err := c.sendSingleMessage(ctx, chatID, userID, text, nil, []map[string]any{
			{
				"type": "image",
				"payload": map[string]any{
					"token": token,
				},
			},
		})
		if err == nil {
			return mid, nil
		}
		lastErr = err
		if strings.Contains(err.Error(), "attachment.not.ready") {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		return "", err
	}
	return "", lastErr
}

func (c *Client) uploadImageWithRetry(ctx context.Context, data []byte, filename string) (string, error) {
	if filename == "" {
		filename = "photo.jpg"
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.globalLimiter.Wait(ctx); err != nil {
			return "", err
		}
		token, err := c.sdk.Upload.Upload(
			ctx,
			model.UploadImage,
			bytes.NewReader(data),
			filename,
			int64(len(data)),
		)
		if err == nil && token != "" {
			return token, nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return "", lastErr
}

func (c *Client) sendSingleMessage(ctx context.Context, chatID, userID int64, text string, buttons [][]ButtonReq, extraAttachments []map[string]any) (string, error) {
	if err := c.globalLimiter.Wait(ctx); err != nil {
		return "", err
	}

	q := url.Values{}
	if chatID > 0 {
		q.Set("chat_id", fmt.Sprintf("%d", chatID))
	} else if userID > 0 {
		q.Set("user_id", fmt.Sprintf("%d", userID))
	} else {
		return "", errors.New("either chatID or userID must be > 0")
	}

	var attachments []map[string]any
	attachments = append(attachments, extraAttachments...)
	if len(buttons) > 0 {
		attachments = append(attachments, map[string]any{
			"type": "inline_keyboard",
			"payload": map[string]any{
				"buttons": buttons,
			},
		})
	}

	bodyMap := map[string]any{
		"text":   text,
		"format": "markdown",
	}
	if len(attachments) > 0 {
		bodyMap["attachments"] = attachments
	}

	rawBody, err := json.Marshal(bodyMap)
	if err != nil {
		return "", err
	}

	reqURL := fmt.Sprintf("%s/messages?%s", MaxBaseURL, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(rawBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", c.cfg.MaxBotToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("POST /messages HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var res struct {
		Message struct {
			Body struct {
				MID string `json:"mid"`
			} `json:"body"`
		} `json:"message"`
	}
	_ = json.Unmarshal(respBytes, &res)
	return res.Message.Body.MID, nil
}

func (c *Client) AnswerCallback(ctx context.Context, chatID int64, callbackID, notification string) error {
	if !c.IsConfigured() || callbackID == "" {
		return nil
	}
	if err := c.answerLimiter.Wait(ctx, chatID); err != nil {
		return err
	}
	if err := c.globalLimiter.Wait(ctx); err != nil {
		return err
	}

	var ans model.CallbackAnswer
	if notification != "" {
		ans.Notification = &notification
	}
	_, err := c.sdk.Messages.AnswerOnCallback(ctx, callbackID, ans)
	return err
}

// VerifyContactHash checks the HMAC-SHA256 signature of vcf_info from a request_contact attachment.
func (c *Client) VerifyContactHash(vcfInfo, hash string) bool {
	if c.cfg.MaxBotToken == "" || hash == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(c.cfg.MaxBotToken))
	mac.Write([]byte(vcfInfo))
	computed := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

var vcfTelRegex = regexp.MustCompile(`(?i)TEL[^:]*:([+\d\s\-()]+)`)

func ExtractPhoneFromVCF(vcf string) string {
	matches := vcfTelRegex.FindStringSubmatch(vcf)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// splitMessage splits text into parts of at most limit runes (characters, not bytes).
func splitMessage(text string, limit int) []string {
	r := []rune(text)
	if len(r) == 0 {
		return []string{""}
	}
	var parts []string
	for len(r) > limit {
		cut := limit
		if i := lastRune(r[:limit], '\n'); i > limit/2 {
			cut = i
		} else if i := lastRune(r[:limit], ' '); i > limit/2 {
			cut = i
		}
		parts = append(parts, string(r[:cut]))
		r = r[cut:]
	}
	return append(parts, string(r))
}

func lastRune(r []rune, target rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == target {
			return i
		}
	}
	return -1
}

