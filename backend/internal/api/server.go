package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"confectioner-backend/internal/bot"
	"confectioner-backend/internal/config"
	"confectioner-backend/internal/db"
	"confectioner-backend/internal/storage"
)

type Server struct {
	cfg   *config.Config
	store *db.Store
	s3    *storage.S3Storage
	bot   *bot.Service
}

func NewServer(cfg *config.Config, store *db.Store, s3 *storage.S3Storage, botService *bot.Service) *Server {
	return &Server{cfg: cfg, store: store, s3: s3, bot: botService}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/media/", s.handleMedia)
	mux.HandleFunc("POST /api/webhook/max", s.handleMaxWebhook)

	// Authenticated endpoints (scoped to the confectioner's workspace)
	mux.HandleFunc("GET /api/me", s.auth(s.handleMe))
	mux.HandleFunc("GET /api/team", s.auth(s.handleTeam))
	mux.HandleFunc("DELETE /api/team/{userId}", s.auth(s.handleRemoveMember))

	mux.HandleFunc("GET /api/clients", s.auth(s.handleListClients))
	mux.HandleFunc("POST /api/clients", s.auth(s.handleCreateClient))
	mux.HandleFunc("GET /api/clients/{id}", s.auth(s.handleGetClient))
	mux.HandleFunc("PUT /api/clients/{id}", s.auth(s.handleUpdateClient))
	mux.HandleFunc("DELETE /api/clients/{id}", s.auth(s.handleDeleteClient))
	mux.HandleFunc("POST /api/clients/{id}/notes", s.auth(s.handleAddNote))
	mux.HandleFunc("DELETE /api/notes/{noteId}", s.auth(s.handleDeleteNote))
	mux.HandleFunc("POST /api/clients/{id}/orders", s.auth(s.handleSaveOrder))

	mux.HandleFunc("GET /api/orders/active", s.auth(s.handleActiveOrders))
	mux.HandleFunc("POST /api/orders/{orderId}/status", s.auth(s.handleUpdateOrderStatus))
	mux.HandleFunc("DELETE /api/orders/{orderId}", s.auth(s.handleDeleteOrder))
	mux.HandleFunc("POST /api/orders/{orderId}/photos", s.auth(s.handleAddOrderPhoto))
	mux.HandleFunc("DELETE /api/orders/{orderId}/photos/{photoId}", s.auth(s.handleDeleteOrderPhoto))

	mux.HandleFunc("GET /api/stats", s.auth(s.handleStats))

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Max-Init-Data, X-Dev-User-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- Public ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"bot_configured": s.bot.Client().IsConfigured(),
		"bot_name":       s.bot.BotName(),
		"dev_mode":       s.cfg.DevMode,
	})
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimLeft(strings.TrimPrefix(r.URL.Path, "/api/media/"), "/")
	if key == "" || strings.Contains(key, "..") {
		http.NotFound(w, r)
		return
	}
	obj, info, err := s.s3.GetObject(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer obj.Close()

	if info.ContentType != "" {
		w.Header().Set("Content-Type", info.ContentType)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, obj)
}

func (s *Server) handleMaxWebhook(w http.ResponseWriter, r *http.Request) {
	if !VerifyWebhookSecret(s.cfg, r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var upd bot.RawUpdate
	if err := json.Unmarshal(body, &upd); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.bot.HandleUpdate(context.Background(), upd)
}

// ---------- Me & team ----------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, m *db.Member) {
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleTeam(w http.ResponseWriter, r *http.Request, m *db.Member) {
	members, err := s.store.ListMembers(r.Context(), m.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{
		"workspace_name": m.WorkspaceName,
		"is_owner":       m.Role == "owner",
		"members":        members,
		"me":             m.MaxUserID,
	}
	if m.Role == "owner" {
		resp["invite_code"] = m.InviteCode
		resp["invite_link"] = s.bot.InviteLink(m.InviteCode)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, m *db.Member) {
	if m.Role != "owner" {
		writeError(w, http.StatusForbidden, "Только владелец может удалять кондитеров")
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := s.store.RemoveMember(r.Context(), m.WorkspaceID, userID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- Clients ----------

type clientRequest struct {
	Name        string   `json:"name"`
	Phone       string   `json:"phone"`
	Address     string   `json:"address"`
	Description string   `json:"description"`
	MaxUsername string   `json:"max_username"`
	Tags        []string `json:"tags"`
}

func (req clientRequest) toClient() db.Client {
	seen := map[string]bool{}
	tags := []string{}
	for _, t := range req.Tags {
		t = strings.TrimSpace(t)
		if r := []rune(t); len(r) > 30 {
			t = string(r[:30])
		}
		if t == "" || seen[strings.ToLower(t)] || len(tags) >= 10 {
			continue
		}
		seen[strings.ToLower(t)] = true
		tags = append(tags, t)
	}
	return db.Client{
		Name:        strings.TrimSpace(req.Name),
		Phone:       strings.TrimSpace(req.Phone),
		Address:     strings.TrimSpace(req.Address),
		Description: strings.TrimSpace(req.Description),
		MaxUsername: strings.TrimPrefix(strings.TrimSpace(req.MaxUsername), "@"),
		Tags:        tags,
	}
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request, m *db.Member) {
	clients, err := s.store.ListClients(r.Context(), m.WorkspaceID, r.URL.Query().Get("search"), r.URL.Query().Get("tag"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request, m *db.Member) {
	var req clientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	c := req.toClient()
	if c.Name == "" {
		writeError(w, http.StatusBadRequest, "Укажите имя клиента")
		return
	}
	created, err := s.store.CreateClient(r.Context(), m.WorkspaceID, c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request, m *db.Member) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	client, err := s.store.GetClient(ctx, m.WorkspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	active, err := s.store.GetActiveOrder(ctx, m.WorkspaceID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := s.store.ListOrderHistory(ctx, m.WorkspaceID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	notes, err := s.store.ListNotes(ctx, m.WorkspaceID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client":       client,
		"active_order": active,
		"history":      history,
		"notes":        notes,
	})
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request, m *db.Member) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req clientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	c := req.toClient()
	if c.Name == "" {
		writeError(w, http.StatusBadRequest, "Укажите имя клиента")
		return
	}
	updated, err := s.store.UpdateClient(r.Context(), m.WorkspaceID, id, c)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request, m *db.Member) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.DeleteClient(r.Context(), m.WorkspaceID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- Notes ----------

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request, m *db.Member) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "Введите текст заметки")
		return
	}
	note, err := s.store.AddNote(r.Context(), m.WorkspaceID, id, m.MaxUserID, m.DisplayName(), strings.TrimSpace(req.Text))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request, m *db.Member) {
	id, ok := pathID(w, r, "noteId")
	if !ok {
		return
	}
	if err := s.store.DeleteNote(r.Context(), m.WorkspaceID, id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- Orders ----------

var (
	validStatuses = map[string]bool{"new": true, "awaiting_prepayment": true, "in_progress": true, "ready": true, "completed": true, "cancelled": true}
	validPayments = map[string]bool{"unpaid": true, "prepaid": true, "paid": true}
	validDelivery = map[string]bool{"pickup": true, "delivery": true}
)

func (s *Server) handleSaveOrder(w http.ResponseWriter, r *http.Request, m *db.Member) {
	clientID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		ID               int64   `json:"id"`
		Status           string  `json:"status"`
		CakeDescription  string  `json:"cake_description"`
		Filling          string  `json:"filling"`
		DecorInscription string  `json:"decor_inscription"`
		WeightKg         float64 `json:"weight_kg"`
		Price            float64 `json:"price"`
		Prepayment       float64 `json:"prepayment"`
		PaymentStatus    string  `json:"payment_status"`
		DeliveryType     string  `json:"delivery_type"`
		DeliveryAddress  string  `json:"delivery_address"`
		DeliveryDate     string  `json:"delivery_date"`
		RemindBeforeMin  int     `json:"remind_before_min"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Status == "" {
		req.Status = "new"
	}
	if req.PaymentStatus == "" {
		req.PaymentStatus = "unpaid"
	}
	if req.DeliveryType == "" {
		req.DeliveryType = "pickup"
	}
	if !validStatuses[req.Status] || !validPayments[req.PaymentStatus] || !validDelivery[req.DeliveryType] {
		writeError(w, http.StatusBadRequest, "invalid status, payment_status or delivery_type")
		return
	}
	if req.WeightKg < 0 || req.Price < 0 || req.Prepayment < 0 || req.RemindBeforeMin < 0 {
		writeError(w, http.StatusBadRequest, "values must not be negative")
		return
	}

	var date *time.Time
	if v := strings.TrimSpace(req.DeliveryDate); v != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, v, s.bot.Loc()); err == nil {
				date = &t
				break
			}
		}
		if date == nil {
			writeError(w, http.StatusBadRequest, "invalid delivery_date")
			return
		}
	}

	saved, err := s.store.SaveOrder(r.Context(), m.WorkspaceID, m.MaxUserID, db.Order{
		ID:               req.ID,
		ClientID:         clientID,
		Status:           req.Status,
		CakeDescription:  strings.TrimSpace(req.CakeDescription),
		Filling:          strings.TrimSpace(req.Filling),
		DecorInscription: strings.TrimSpace(req.DecorInscription),
		WeightKg:         req.WeightKg,
		Price:            req.Price,
		Prepayment:       req.Prepayment,
		PaymentStatus:    req.PaymentStatus,
		DeliveryType:     req.DeliveryType,
		DeliveryAddress:  strings.TrimSpace(req.DeliveryAddress),
		DeliveryDate:     date,
		RemindBeforeMin:  req.RemindBeforeMin,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleActiveOrders(w http.ResponseWriter, r *http.Request, m *db.Member) {
	orders, err := s.store.ListActiveOrders(r.Context(), m.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *Server) handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request, m *db.Member) {
	orderID, ok := pathID(w, r, "orderId")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validStatuses[req.Status] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	updated, err := s.store.UpdateOrderStatus(r.Context(), m.WorkspaceID, orderID, req.Status)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteOrder(w http.ResponseWriter, r *http.Request, m *db.Member) {
	orderID, ok := pathID(w, r, "orderId")
	if !ok {
		return
	}
	if err := s.store.DeleteOrder(r.Context(), m.WorkspaceID, orderID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleAddOrderPhoto(w http.ResponseWriter, r *http.Request, m *db.Member) {
	orderID, ok := pathID(w, r, "orderId")
	if !ok {
		return
	}
	// Make sure the order belongs to this workspace before storing anything.
	if _, err := s.store.GetOrder(r.Context(), m.WorkspaceID, orderID); err != nil {
		writeStoreError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 26<<20)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 25<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		writeError(w, http.StatusBadRequest, "Можно загружать только изображения")
		return
	}

	key, url, err := s.s3.Upload(r.Context(), fmt.Sprintf("ws_%d/order_%d", m.WorkspaceID, orderID),
		header.Filename, contentType, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "S3 upload error: "+err.Error())
		return
	}
	photo, err := s.store.AddOrderPhoto(r.Context(), m.WorkspaceID, orderID, r.FormValue("kind"), key, url,
		strings.TrimSpace(r.FormValue("caption")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, photo)
}

func (s *Server) handleDeleteOrderPhoto(w http.ResponseWriter, r *http.Request, m *db.Member) {
	orderID, ok := pathID(w, r, "orderId")
	if !ok {
		return
	}
	photoID, ok := pathID(w, r, "photoId")
	if !ok {
		return
	}
	if err := s.store.DeleteOrderPhoto(r.Context(), m.WorkspaceID, orderID, photoID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---------- Stats ----------

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, m *db.Member) {
	loc := s.bot.Loc()
	month := r.URL.Query().Get("month")
	var from time.Time
	if t, err := time.ParseInLocation("2006-01", month, loc); err == nil {
		from = t
	} else {
		now := time.Now().In(loc)
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	}
	st, err := s.store.Stats(r.Context(), m.WorkspaceID, from, from.AddDate(0, 1, 0))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	st.Month = from.Format("2006-01")
	writeJSON(w, http.StatusOK, st)
}

// ---------- Helpers ----------

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[API] encode json error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
