package api

import (
	"bytes"
	"encoding/json"
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
	cfg        *config.Config
	store      *db.Store
	s3         *storage.S3Storage
	botService *bot.Service
	hub        *SSEHub
}

func NewServer(cfg *config.Config, store *db.Store, s3 *storage.S3Storage, botService *bot.Service, hub *SSEHub) *Server {
	return &Server{
		cfg:        cfg,
		store:      store,
		s3:         s3,
		botService: botService,
		hub:        hub,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public / system endpoints
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/media/", s.handleMedia)
	mux.HandleFunc("POST /api/webhook/max", s.handleMaxWebhook)

	// Authenticated Mini-App endpoints
	mux.HandleFunc("GET /api/events", s.authMiddleware(s.handleSSE))
	mux.HandleFunc("GET /api/clients", s.authMiddleware(s.handleListClients))
	mux.HandleFunc("POST /api/clients", s.authMiddleware(s.handleCreateClient))
	mux.HandleFunc("GET /api/clients/{id}", s.authMiddleware(s.handleGetClient))
	mux.HandleFunc("PUT /api/clients/{id}", s.authMiddleware(s.handleUpdateClient))
	mux.HandleFunc("GET /api/clients/{id}/messages", s.authMiddleware(s.handleListMessages))
	mux.HandleFunc("POST /api/clients/{id}/messages", s.authMiddleware(s.handleSendOperatorMessage))
	mux.HandleFunc("POST /api/clients/{id}/simulate", s.authMiddleware(s.handleSimulateClientMessage))
	mux.HandleFunc("GET /api/clients/{id}/orders", s.authMiddleware(s.handleGetClientOrders))
	mux.HandleFunc("POST /api/clients/{id}/orders", s.authMiddleware(s.handleSaveOrder))
	mux.HandleFunc("POST /api/orders/{orderId}/status", s.authMiddleware(s.handleUpdateOrderStatus))
	mux.HandleFunc("POST /api/orders/{orderId}/photos", s.authMiddleware(s.handleAddOrderPhoto))
	mux.HandleFunc("DELETE /api/orders/{orderId}/photos/{photoId}", s.authMiddleware(s.handleDeleteOrderPhoto))

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Max-Init-Data")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"bot_configured": s.botService.Client().IsConfigured(),
		"bot_name":       s.cfg.MaxBotName,
		"dev_mode":       s.cfg.DevMode,
	})
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/media/")
	key = strings.TrimLeft(key, "/")
	if key == "" {
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
	// Fast ack per MAX Webhook best practices
	w.WriteHeader(http.StatusOK)
	go func() {
		ctx := r.Context()
		s.botService.HandleUpdate(ctx, upd)
	}()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)

	// Initial ping
	_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"type":"connected"}`)
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(msg))
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	filter := r.URL.Query().Get("filter")
	clients, err := s.store.ListClients(r.Context(), search, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomName  string `json:"custom_name"`
		Phone       string `json:"phone"`
		Address     string `json:"address"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.CustomName) == "" {
		writeError(w, http.StatusBadRequest, "Укажите имя клиента")
		return
	}
	fakeMaxID := time.Now().UnixNano() / 1e6
	c, err := s.store.UpsertClientFromBot(r.Context(), fakeMaxID, fakeMaxID, req.CustomName, "", "", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c, err = s.store.UpdateClientProfile(r.Context(), c.ID, req.CustomName, req.Phone, req.Address, req.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("client_updated", c)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}
	activeOrder, _ := s.store.GetActiveOrder(r.Context(), id)
	history, _ := s.store.ListOrderHistory(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]any{
		"client":       client,
		"active_order": activeOrder,
		"history":      history,
	})
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	var req struct {
		CustomName  string `json:"custom_name"`
		Phone       string `json:"phone"`
		Address     string `json:"address"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	updated, err := s.store.UpdateClientProfile(r.Context(), id, req.CustomName, req.Phone, req.Address, req.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("client_updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	_ = s.store.ResetUnreadCount(r.Context(), id)
	msgs, err := s.store.ListMessages(r.Context(), id, 300)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updatedClient, err := s.store.GetClient(r.Context(), id); err == nil {
		s.hub.Broadcast("client_updated", updatedClient)
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleSendOperatorMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}

	var text string
	var attachments []db.Attachment
	var rawImgBytes []byte
	var rawImgName string

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(25 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form")
			return
		}
		text = strings.TrimSpace(r.FormValue("text"))
		file, header, err := r.FormFile("file")
		if err == nil && file != nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, 25<<20))
			if err != nil {
				writeError(w, http.StatusBadRequest, "read file error")
				return
			}
			ct := header.Header.Get("Content-Type")
			if ct == "" {
				ct = http.DetectContentType(data)
			}
			s3Key, mediaURL, err := s.s3.Upload(r.Context(), fmt.Sprintf("chat/client_%d", client.ID), header.Filename, ct, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "S3 upload failed: "+err.Error())
				return
			}
			attachments = append(attachments, db.Attachment{
				Type:     "image",
				S3Key:    s3Key,
				URL:      mediaURL,
				Filename: header.Filename,
			})
			rawImgBytes = data
			rawImgName = header.Filename
		}
	} else {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		text = strings.TrimSpace(req.Text)
	}

	if text == "" && len(attachments) == 0 {
		writeError(w, http.StatusBadRequest, "message cannot be empty")
		return
	}

	msg, err := s.botService.SendOperatorMessage(r.Context(), client, text, attachments, rawImgBytes, rawImgName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleSimulateClientMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "client not found")
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.TrimSpace(req.Text) == "" {
		req.Text = "Здравствуйте! Подскажите, можно добавить свежую голубику сверху на торт?"
	}

	msg, err := s.store.CreateMessage(r.Context(), client.ID, fmt.Sprintf("sim_%d", time.Now().UnixNano()), "client", req.Text, nil, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refreshed, _ := s.store.GetClient(r.Context(), client.ID)
	s.hub.Broadcast("new_message", map[string]any{
		"client":  refreshed,
		"message": msg,
	})
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleGetClientOrders(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
		return
	}
	active, err := s.store.GetActiveOrder(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := s.store.ListOrderHistory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active_order": active,
		"history":      history,
	})
}

func (s *Server) handleSaveOrder(w http.ResponseWriter, r *http.Request) {
	clientID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client id")
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	var parsedDate *time.Time
	if strings.TrimSpace(req.DeliveryDate) != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
			if t, err := time.Parse(layout, req.DeliveryDate); err == nil {
				parsedDate = &t
				break
			}
		}
	}

	var prevStatus string
	if req.ID > 0 {
		if existing, err := s.store.GetOrderByID(r.Context(), req.ID); err == nil && existing != nil {
			prevStatus = existing.Status
		}
	}

	saved, err := s.store.SaveOrder(r.Context(), db.Order{
		ID:               req.ID,
		ClientID:         clientID,
		Status:           req.Status,
		CakeDescription:  req.CakeDescription,
		Filling:          req.Filling,
		DecorInscription: req.DecorInscription,
		WeightKg:         req.WeightKg,
		Price:            req.Price,
		Prepayment:       req.Prepayment,
		PaymentStatus:    req.PaymentStatus,
		DeliveryType:     req.DeliveryType,
		DeliveryAddress:  req.DeliveryAddress,
		DeliveryDate:     parsedDate,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	client, _ := s.store.GetClient(r.Context(), clientID)
	s.hub.Broadcast("client_updated", client)
	s.hub.Broadcast("order_updated", saved)

	if prevStatus != "" && prevStatus != saved.Status && client != nil {
		go s.botService.NotifyOrderStatusChanged(r.Context(), client, saved)
	}

	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(r.PathValue("orderId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}

	existing, err := s.store.GetOrderByID(r.Context(), orderID)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}

	updated, err := s.store.UpdateOrderStatus(r.Context(), orderID, req.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	client, _ := s.store.GetClient(r.Context(), updated.ClientID)
	s.hub.Broadcast("client_updated", client)
	s.hub.Broadcast("order_updated", updated)

	if existing.Status != updated.Status && client != nil {
		s.botService.NotifyOrderStatusChanged(r.Context(), client, updated)
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAddOrderPhoto(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(r.PathValue("orderId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}

	order, err := s.store.GetOrderByID(r.Context(), orderID)
	if err != nil || order == nil {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(25 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form")
			return
		}
		kind := r.FormValue("kind")
		caption := strings.TrimSpace(r.FormValue("caption"))
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
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = http.DetectContentType(data)
		}

		s3Key, mediaURL, err := s.s3.Upload(r.Context(), fmt.Sprintf("orders/order_%d", order.ID), header.Filename, ct, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "S3 upload error: "+err.Error())
			return
		}

		photo, err := s.store.AddOrderPhoto(r.Context(), order.ID, kind, s3Key, mediaURL, caption)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		refreshedOrder, _ := s.store.GetOrderByID(r.Context(), order.ID)
		s.hub.Broadcast("order_updated", refreshedOrder)
		writeJSON(w, http.StatusCreated, photo)
		return
	}

	// JSON mode: attach existing photo from chat (by s3_key and url) to the order
	var req struct {
		Kind    string `json:"kind"`
		S3Key   string `json:"s3_key"`
		URL     string `json:"url"`
		Caption string `json:"caption"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.URL == "" && req.S3Key != "" {
		req.URL = s.s3.MediaURL(req.S3Key)
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url or s3_key is required")
		return
	}

	photo, err := s.store.AddOrderPhoto(r.Context(), order.ID, req.Kind, req.S3Key, req.URL, req.Caption)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refreshedOrder, _ := s.store.GetOrderByID(r.Context(), order.ID)
	s.hub.Broadcast("order_updated", refreshedOrder)
	writeJSON(w, http.StatusCreated, photo)
}

func (s *Server) handleDeleteOrderPhoto(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.ParseInt(r.PathValue("orderId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}
	photoID, err := strconv.ParseInt(r.PathValue("photoId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid photo id")
		return
	}
	if err := s.store.DeleteOrderPhoto(r.Context(), photoID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refreshedOrder, _ := s.store.GetOrderByID(r.Context(), orderID)
	s.hub.Broadcast("order_updated", refreshedOrder)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
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

