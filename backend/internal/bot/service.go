package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"confectioner-backend/internal/config"
	"confectioner-backend/internal/db"
	"confectioner-backend/internal/storage"
)

type Broadcaster interface {
	Broadcast(eventType string, payload any)
}

type Service struct {
	cfg    *config.Config
	client *Client
	store  *db.Store
	s3     *storage.S3Storage
	hub    Broadcaster
}

func NewService(cfg *config.Config, client *Client, store *db.Store, s3 *storage.S3Storage, hub Broadcaster) *Service {
	return &Service{
		cfg:    cfg,
		client: client,
		store:  store,
		s3:     s3,
		hub:    hub,
	}
}

func (s *Service) Client() *Client {
	return s.client
}

func (s *Service) StartPolling(ctx context.Context) {
	if !s.client.IsConfigured() {
		log.Println("[MAX Bot] MAX_BOT_TOKEN is empty — running in offline/UI-only mode (set MAX_BOT_TOKEN in .env to connect to MAX)")
		return
	}

	s.client.FetchBotInfo(ctx)

	if s.cfg.MaxUpdateMode == "webhook" {
		log.Println("[MAX Bot] Running in webhook mode (listening on /api/webhook/max)")
		return
	}

	marker, err := s.store.GetBotMarker(ctx)
	if err != nil {
		log.Printf("[MAX Bot] Failed to read polling marker: %v", err)
	}
	log.Printf("[MAX Bot] Starting Long Polling from marker=%d...", marker)

	for {
		select {
		case <-ctx.Done():
			log.Println("[MAX Bot] Long Polling stopped")
			return
		default:
		}

		updates, nextMarker, err := s.client.GetUpdates(ctx, marker)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[MAX Bot] GetUpdates error: %v (retrying in 3s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}

		if nextMarker != marker && nextMarker > 0 {
			marker = nextMarker
			_ = s.store.SetBotMarker(ctx, marker)
		}

		for _, upd := range updates {
			s.HandleUpdate(ctx, upd)
		}

		// Respect 300ms minimum interval between GetUpdates calls per MAX docs
		select {
		case <-ctx.Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
	}
}

func (s *Service) HandleUpdate(ctx context.Context, upd RawUpdate) {
	switch upd.UpdateType {
	case "bot_started":
		s.handleBotStarted(ctx, upd)
	case "message_created":
		s.handleMessageCreated(ctx, upd)
	case "message_callback":
		s.handleMessageCallback(ctx, upd)
	}
}

func (s *Service) handleBotStarted(ctx context.Context, upd RawUpdate) {
	if upd.User == nil {
		return
	}
	firstName, lastName := splitUserName(upd.User)
	client, err := s.store.UpsertClientFromBot(ctx, upd.User.UserID, upd.ChatID, firstName, lastName, upd.User.Username, upd.User.AvatarURL)
	if err != nil {
		log.Printf("[MAX Bot] UpsertClientFromBot error: %v", err)
		return
	}

	s.sendWelcomeMenu(ctx, client)
	s.hub.Broadcast("client_updated", client)
}

func (s *Service) handleMessageCallback(ctx context.Context, upd RawUpdate) {
	if upd.Callback == nil {
		return
	}
	cb := upd.Callback
	userID := cb.User.UserID
	var chatID int64
	if upd.Message != nil {
		chatID = upd.Message.Recipient.ChatID
	}

	client, err := s.store.GetClientByMaxUserID(ctx, userID)
	if err != nil && userID > 0 {
		firstName, lastName := splitUserName(&cb.User)
		client, _ = s.store.UpsertClientFromBot(ctx, userID, chatID, firstName, lastName, cb.User.Username, cb.User.AvatarURL)
	}

	if cb.Payload == "my_active_order" && client != nil {
		_ = s.client.AnswerCallback(ctx, client.MaxChatID, cb.CallbackID, "Загружаем информацию о заказе...")
		order, err := s.store.GetActiveOrder(ctx, client.ID)
		if err != nil || order == nil {
			text := "🎂 **У вас пока нет активного заказа.**\n\nНапишите в этот чат, какой торт вы хотели бы заказать (повод, дату, примерный вес или фото-референс), и кондитер скоро ответит вам!"
			_, _ = s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, text, s.buildClientButtons(client.MaxUserID))
			return
		}
		text := FormatOrderCardForClient(order)
		_, _ = s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, text, s.buildClientButtons(client.MaxUserID))
		return
	}

	_ = s.client.AnswerCallback(ctx, chatID, cb.CallbackID, "")
}

func (s *Service) handleMessageCreated(ctx context.Context, upd RawUpdate) {
	if upd.Message == nil || upd.Message.Sender == nil {
		return
	}
	sender := upd.Message.Sender
	// Ignore messages sent by the bot itself
	if s.client.botID != 0 && sender.UserID == s.client.botID {
		return
	}

	chatID := upd.Message.Recipient.ChatID
	firstName, lastName := splitUserName(sender)
	client, err := s.store.UpsertClientFromBot(ctx, sender.UserID, chatID, firstName, lastName, sender.Username, sender.AvatarURL)
	if err != nil {
		log.Printf("[MAX Bot] UpsertClientFromBot error: %v", err)
		return
	}

	text := strings.TrimSpace(upd.Message.Body.Text)
	if text == "/start" {
		s.sendWelcomeMenu(ctx, client)
		return
	}

	var savedAttachments []db.Attachment
	var extractedPhone string

	for _, att := range upd.Message.Body.Attachments {
		switch att.Type {
		case "contact":
			if att.Payload.VCFInfo != "" && s.client.VerifyContactHash(att.Payload.VCFInfo, att.Payload.Hash) {
				if phone := ExtractPhoneFromVCF(att.Payload.VCFInfo); phone != "" {
					extractedPhone = phone
				}
			}
		case "image":
			if att.Payload.URL != "" {
				s3Key, mediaURL, _, _, err := s.s3.UploadFromURL(ctx, fmt.Sprintf("chat/client_%d", client.ID), att.Payload.URL)
				if err != nil {
					log.Printf("[MAX Bot] Failed to save chat image to S3: %v", err)
					savedAttachments = append(savedAttachments, db.Attachment{
						Type:  "image",
						S3Key: "",
						URL:   att.Payload.URL,
					})
				} else {
					savedAttachments = append(savedAttachments, db.Attachment{
						Type:  "image",
						S3Key: s3Key,
						URL:   mediaURL,
					})
				}
			}
		}
	}

	if extractedPhone != "" {
		updatedClient, err := s.store.UpdateClientPhone(ctx, client.ID, extractedPhone)
		if err == nil {
			client = updatedClient
		}
		if text == "" {
			text = fmt.Sprintf("📱 Клиент поделился номером телефона: %s", extractedPhone)
		}
		confirmText := fmt.Sprintf("✅ Спасибо! Ваш номер телефона **%s** сохранён.", extractedPhone)
		_, _ = s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, confirmText, s.buildClientButtons(client.MaxUserID))
	}

	msg, err := s.store.CreateMessage(ctx, client.ID, upd.Message.Body.MID, "client", text, savedAttachments, true)
	if err != nil {
		log.Printf("[MAX Bot] CreateMessage error: %v", err)
		return
	}

	refreshedClient, _ := s.store.GetClient(ctx, client.ID)
	s.hub.Broadcast("new_message", map[string]any{
		"client":  refreshedClient,
		"message": msg,
	})

	// Notify operators in MAX with a deep link button into this client's chat
	s.notifyOperatorsNewMessage(ctx, refreshedClient, msg)
}

func (s *Service) sendWelcomeMenu(ctx context.Context, client *db.Client) {
	greeting := fmt.Sprintf(
		"👋 Здравствуйте, **%s**!\n\nДобро пожаловать в кондитерскую! Напишите в этот чат ваши пожелания по торту или десерту, прикрепите фото-примеры, и мы с радостью обсудим детали заказа.\n\nВы также можете поделиться номером телефона для связи или проверить статус своего заказа по кнопкам ниже:",
		client.DisplayName(),
	)
	_, _ = s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, greeting, s.buildClientButtons(client.MaxUserID))
}

func (s *Service) buildClientButtons(userID int64) [][]ButtonReq {
	rows := [][]ButtonReq{
		{
			{
				Type:    "callback",
				Text:    "🎂 Мой активный заказ",
				Payload: "my_active_order",
			},
			{
				Type: "request_contact",
				Text: "📱 Поделиться телефоном",
			},
		},
	}

	if s.cfg.IsOperator(userID) && len(s.cfg.OperatorIDs) > 0 {
		botName := s.client.botUsername
		if botName == "" {
			botName = s.cfg.MaxBotName
		}
		rows = append(rows, []ButtonReq{
			{
				Type:   "link",
				Text:   "👩‍🍳 Открыть панель кондитера",
				URL:    fmt.Sprintf("https://max.ru/%s?startapp=operator", botName),
			},
		})
	}
	return rows
}

func (s *Service) notifyOperatorsNewMessage(ctx context.Context, client *db.Client, msg *db.Message) {
	if client == nil || msg == nil || len(s.cfg.OperatorIDs) == 0 {
		return
	}
	// Don't send push notification if the operator themselves wrote
	if s.cfg.OperatorIDs[client.MaxUserID] {
		return
	}

	preview := msg.Text
	if preview == "" && len(msg.Attachments) > 0 {
		preview = "📷 [Прислал(а) фотографию]"
	}

	botName := s.client.botUsername
	if botName == "" {
		botName = s.cfg.MaxBotName
	}

	notifText := fmt.Sprintf(
		"📩 **Новое сообщение от клиента: %s**\n%s",
		client.DisplayName(),
		preview,
	)

	buttons := [][]ButtonReq{
		{
			{
				Type: "link",
				Text: fmt.Sprintf("↗️ Открыть чат (%s)", client.DisplayName()),
				URL:  fmt.Sprintf("https://max.ru/%s?startapp=client_%d", botName, client.ID),
			},
		},
	}

	for opUserID := range s.cfg.OperatorIDs {
		_, _ = s.client.SendMessageWithKeyboard(ctx, 0, opUserID, notifText, buttons)
	}
}

func (s *Service) SendOperatorMessage(ctx context.Context, client *db.Client, text string, attachments []db.Attachment, rawImageBytes []byte, rawImageName string) (*db.Message, error) {
	var maxMID string
	if s.client.IsConfigured() {
		var err error
		if len(rawImageBytes) > 0 {
			maxMID, err = s.client.SendMessageWithImage(ctx, client.MaxChatID, client.MaxUserID, text, rawImageBytes, rawImageName)
		} else if strings.TrimSpace(text) != "" {
			maxMID, err = s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, text, nil)
		}
		if err != nil {
			log.Printf("[MAX Bot] Warning: failed to deliver message to MAX chat %d: %v", client.MaxChatID, err)
		}
	}

	msg, err := s.store.CreateMessage(ctx, client.ID, maxMID, "operator", text, attachments, false)
	if err != nil {
		return nil, err
	}
	_ = s.store.ResetUnreadCount(ctx, client.ID)

	refreshedClient, _ := s.store.GetClient(ctx, client.ID)
	s.hub.Broadcast("new_message", map[string]any{
		"client":  refreshedClient,
		"message": msg,
	})
	return msg, nil
}

func (s *Service) NotifyOrderStatusChanged(ctx context.Context, client *db.Client, order *db.Order) {
	if client == nil || order == nil {
		return
	}

	statusTitle := HumanOrderStatus(order.Status)
	var extraNote string
	switch order.Status {
	case "awaiting_prepayment":
		if order.Prepayment > 0 {
			extraNote = fmt.Sprintf("\n💳 Сумма предоплаты для бронирования даты: **%.0f ₽**.", order.Prepayment)
		}
	case "in_progress":
		extraNote = "\n👩‍🍳 Ваш заказ взят в работу! Мы готовим ваш торт с любовью."
	case "ready":
		extraNote = "\n✨ Ваш торт готов! Ждём вас или готовим к отправке."
	case "completed":
		extraNote = "\n🎉 Спасибо за заказ! Будем рады вашим отзывам и новым встречам!"
	case "cancelled":
		extraNote = "\nЕсли у вас возникли вопросы, напишите нам в этом чате."
	}

	notifText := fmt.Sprintf(
		"🔔 **Статус вашего заказа #%d обновлён:**\n👉 **%s**%s\n\n%s",
		order.ID,
		statusTitle,
		extraNote,
		FormatOrderCardForClient(order),
	)

	if s.client.IsConfigured() {
		_, err := s.client.SendMessageWithKeyboard(ctx, client.MaxChatID, client.MaxUserID, notifText, s.buildClientButtons(client.MaxUserID))
		if err != nil {
			log.Printf("[MAX Bot] Failed to send order status notification to client %d: %v", client.ID, err)
		}
	}

	// Also record a system message in the chat history so the operator sees the notification in the timeline
	sysMsg, err := s.store.CreateMessage(ctx, client.ID, "", "system", fmt.Sprintf("🔔 Статус заказа #%d изменён на «%s» (клиенту отправлено уведомление)", order.ID, statusTitle), nil, false)
	if err == nil {
		refreshedClient, _ := s.store.GetClient(ctx, client.ID)
		s.hub.Broadcast("new_message", map[string]any{
			"client":  refreshedClient,
			"message": sysMsg,
		})
	}
}

func HumanOrderStatus(status string) string {
	switch status {
	case "new":
		return "📝 Новый / Обсуждение"
	case "awaiting_prepayment":
		return "⏳ Ожидает предоплаты"
	case "in_progress":
		return "👩‍🍳 В работе"
	case "ready":
		return "🎂 Готов к выдаче / доставке"
	case "completed":
		return "✅ Выполнен"
	case "cancelled":
		return "❌ Отменён"
	default:
		return status
	}
}

func FormatOrderCardForClient(o *db.Order) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎂 **Заказ #%d** (%s)\n", o.ID, HumanOrderStatus(o.Status)))
	if o.CakeDescription != "" {
		sb.WriteString(fmt.Sprintf("• **Торт:** %s\n", o.CakeDescription))
	}
	if o.Filling != "" {
		sb.WriteString(fmt.Sprintf("• **Начинка:** %s\n", o.Filling))
	}
	if o.DecorInscription != "" {
		sb.WriteString(fmt.Sprintf("• **Декор / надпись:** %s\n", o.DecorInscription))
	}
	if o.WeightKg > 0 {
		sb.WriteString(fmt.Sprintf("• **Вес:** %.2f кг\n", o.WeightKg))
	}
	if o.DeliveryDate != nil {
		sb.WriteString(fmt.Sprintf("• **Дата готовности:** %s\n", o.DeliveryDate.Format("02.01.2006 15:04")))
	}
	if o.DeliveryType == "delivery" {
		addr := o.DeliveryAddress
		if addr == "" {
			addr = "по адресу"
		}
		sb.WriteString(fmt.Sprintf("• **Получение:** Доставка (%s)\n", addr))
	} else {
		sb.WriteString("• **Получение:** Самовывоз\n")
	}
	if o.Price > 0 {
		rem := o.Price - o.Prepayment
		if o.PaymentStatus == "paid" || rem < 0 {
			rem = 0
		}
		sb.WriteString(fmt.Sprintf("• **Стоимость:** %.0f ₽ (предоплата: %.0f ₽, к доплате: %.0f ₽)\n", o.Price, o.Prepayment, rem))
	}
	return strings.TrimSpace(sb.String())
}

func splitUserName(u *RawUser) (string, string) {
	if u == nil {
		return "", ""
	}
	if u.FirstName != "" || u.LastName != "" {
		return u.FirstName, u.LastName
	}
	parts := strings.Fields(u.Name)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

