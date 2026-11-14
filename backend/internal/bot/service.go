package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"confectioner-backend/internal/config"
	"confectioner-backend/internal/db"
)

// UserInfo is the minimal MAX user profile needed to register a confectioner.
type UserInfo struct {
	ID        int64
	FirstName string
	LastName  string
	Username  string
}

type Service struct {
	cfg    *config.Config
	client *Client
	store  *db.Store
	loc    *time.Location
}

func NewService(cfg *config.Config, client *Client, store *db.Store) *Service {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Printf("[Bot] Unknown APP_TIMEZONE %q (%v), using UTC", cfg.Timezone, err)
		loc = time.UTC
	}
	return &Service{cfg: cfg, client: client, store: store, loc: loc}
}

func (s *Service) Client() *Client        { return s.client }
func (s *Service) Loc() *time.Location    { return s.loc }

func (s *Service) BotName() string {
	if s.client.botUsername != "" {
		return s.client.botUsername
	}
	return s.cfg.MaxBotName
}

func (s *Service) AppLink(payload string) string {
	return fmt.Sprintf("https://max.ru/%s?startapp=%s", s.BotName(), payload)
}

func (s *Service) InviteLink(code string) string {
	return fmt.Sprintf("https://max.ru/%s?start=join_%s", s.BotName(), code)
}

// EnsureMember finds the confectioner or registers them. Status is one of:
// "existing", "created", "joined", "denied", "bad_code".
func (s *Service) EnsureMember(ctx context.Context, u UserInfo, chatID int64, joinCode string) (*db.Member, string, error) {
	m, err := s.store.GetMember(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	if m != nil {
		if chatID > 0 && m.ChatID != chatID {
			_ = s.store.UpdateMemberChat(ctx, u.ID, chatID)
			m.ChatID = chatID
		}
		return m, "existing", nil
	}

	base := db.Member{MaxUserID: u.ID, ChatID: chatID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username}

	if joinCode != "" {
		joined, err := s.store.JoinWorkspaceByCode(ctx, joinCode, base)
		if err != nil {
			return nil, "", err
		}
		if joined == nil {
			return nil, "bad_code", nil
		}
		return joined, "joined", nil
	}

	if !s.cfg.CanRegister(u.ID) {
		return nil, "denied", nil
	}
	name := strings.TrimSpace(u.FirstName)
	if name == "" {
		name = "Кондитерская"
	} else {
		name = "Кондитерская " + name
	}
	created, err := s.store.CreateWorkspaceWithOwner(ctx, base, name)
	if err != nil {
		return nil, "", err
	}
	return created, "created", nil
}

// ---------- Update loop ----------

func (s *Service) StartPolling(ctx context.Context) {
	if !s.client.IsConfigured() {
		log.Println("[MAX Bot] MAX_BOT_TOKEN is empty — bot is disabled, only the admin UI works")
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
		if upd.User == nil {
			return
		}
		s.handleStart(ctx, userFromRaw(upd.User), upd.ChatID, upd.Payload)
	case "message_created":
		s.handleMessage(ctx, upd)
	case "message_callback":
		s.handleCallback(ctx, upd)
	}
}

func userFromRaw(u *RawUser) UserInfo {
	first, last := u.FirstName, u.LastName
	if first == "" && last == "" {
		parts := strings.Fields(u.Name)
		if len(parts) > 0 {
			first = parts[0]
			last = strings.Join(parts[1:], " ")
		}
	}
	return UserInfo{ID: u.UserID, FirstName: first, LastName: last, Username: u.Username}
}

func (s *Service) handleMessage(ctx context.Context, upd RawUpdate) {
	msg := upd.Message
	if msg == nil || msg.Sender == nil {
		return
	}
	if s.client.botID != 0 && msg.Sender.UserID == s.client.botID {
		return
	}
	// Only private dialogs with the bot are served.
	if t := strings.ToLower(msg.Recipient.ChatType); t != "" && t != "dialog" {
		return
	}

	u := userFromRaw(msg.Sender)
	chatID := msg.Recipient.ChatID
	text := strings.TrimSpace(msg.Body.Text)

	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "/join ") || strings.HasPrefix(lower, "join ") {
		code := strings.TrimSpace(text[strings.Index(text, " "):])
		s.handleStart(ctx, u, chatID, "join_"+code)
		return
	}

	if sc := extractSharedContact(msg); sc != nil {
		m, status, err := s.EnsureMember(ctx, u, chatID, "")
		if err != nil {
			log.Printf("[MAX Bot] EnsureMember error: %v", err)
			return
		}
		if status == "denied" || m == nil {
			s.sendText(ctx, chatID, u.ID, "🔒 Этот бот — закрытая система учёта для кондитеров.")
			return
		}
		s.handleSharedContact(ctx, m, sc)
		return
	}

	s.handleStart(ctx, u, chatID, "")
}

type SharedContact struct {
	MaxUserID int64
	Name      string
	Username  string
	Phone     string
}

func extractSharedContact(msg *RawMessage) *SharedContact {
	if msg == nil {
		return nil
	}
	allAtts := append([]RawAttachment{}, msg.Body.Attachments...)
	if msg.Link != nil {
		allAtts = append(allAtts, msg.Link.Message.Attachments...)
	}
	for _, att := range allAtts {
		if strings.ToLower(att.Type) != "contact" {
			continue
		}
		sc := &SharedContact{
			Phone: ExtractPhoneFromVCF(att.Payload.VCFInfo),
			Name:  ExtractNameFromVCF(att.Payload.VCFInfo),
		}
		if att.Payload.MaxInfo != nil {
			u := userFromRaw(att.Payload.MaxInfo)
			sc.MaxUserID = u.ID
			sc.Username = strings.TrimPrefix(strings.TrimSpace(u.Username), "@")
			if sc.Name == "" {
				sc.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
			}
		}
		if sc.MaxUserID > 0 || sc.Phone != "" || sc.Username != "" || sc.Name != "" {
			return sc
		}
	}

	// Also support forwarded messages from a client (msg.Link with sender).
	if msg.Link != nil && msg.Link.Sender != nil {
		u := userFromRaw(msg.Link.Sender)
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if u.ID > 0 || u.Username != "" || name != "" {
			return &SharedContact{
				MaxUserID: u.ID,
				Name:      name,
				Username:  strings.TrimPrefix(strings.TrimSpace(u.Username), "@"),
			}
		}
	}
	return nil
}

func formatClientContactDetails(c *db.Client) string {
	var lines []string
	if c.Phone != "" {
		lines = append(lines, fmt.Sprintf("📞 Телефон: `%s`", clean(c.Phone)))
	}
	if c.MaxUsername != "" {
		lines = append(lines, fmt.Sprintf("💬 MAX: @%s", clean(c.MaxUsername)))
	}
	if c.MaxUserID > 0 {
		lines = append(lines, fmt.Sprintf("🆔 MAX ID: `%d`", c.MaxUserID))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

func (s *Service) handleSharedContact(ctx context.Context, m *db.Member, sc *SharedContact) {
	// 1. If the confectioner requested linking to a specific client card from the admin panel:
	if m.PendingLinkClientID > 0 {
		targetID := m.PendingLinkClientID
		_ = s.store.SetMemberPendingLinkClient(ctx, m.MaxUserID, 0)
		linked, err := s.store.LinkClientMaxContact(ctx, m.WorkspaceID, targetID, sc.MaxUserID, sc.Username, sc.Phone, sc.Name)
		if err == nil && linked != nil {
			text := fmt.Sprintf("✅ **Контакт MAX привязан к клиенту «%s»!**%s",
				clean(linked.Name), formatClientContactDetails(linked))
			s.sendToMember(ctx, m, text, [][]ButtonReq{
				{{Type: "link", Text: "👤 Открыть карточку клиента", URL: s.AppLink(fmt.Sprintf("client_%d", linked.ID))}},
				{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}},
			})
			return
		}
	}

	// 2. Check if a client with this MAX ID, @username, or phone already exists in the workspace:
	existing, err := s.store.FindClientByMaxOrPhone(ctx, m.WorkspaceID, sc.MaxUserID, sc.Username, sc.Phone)
	if err != nil {
		log.Printf("[MAX Bot] FindClientByMaxOrPhone error: %v", err)
	}
	if existing != nil {
		updated, err := s.store.LinkClientMaxContact(ctx, m.WorkspaceID, existing.ID, sc.MaxUserID, sc.Username, sc.Phone, sc.Name)
		if err != nil || updated == nil {
			updated = existing
		}
		text := fmt.Sprintf("🔗 **Клиент «%s» найден в базе, привязка к MAX обновлена!**%s",
			clean(updated.Name), formatClientContactDetails(updated))
		s.sendToMember(ctx, m, text, [][]ButtonReq{
			{{Type: "link", Text: "👤 Открыть карточку в админке", URL: s.AppLink(fmt.Sprintf("client_%d", updated.ID))}},
			{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}},
		})
		return
	}

	// 3. Otherwise create a new client linked to this MAX contact:
	name := strings.TrimSpace(sc.Name)
	if name == "" && sc.Username != "" {
		name = "@" + sc.Username
	}
	if name == "" && sc.Phone != "" {
		name = sc.Phone
	}
	if name == "" {
		name = fmt.Sprintf("Клиент MAX #%d", sc.MaxUserID)
	}

	created, err := s.store.CreateClient(ctx, m.WorkspaceID, db.Client{
		Name:        name,
		Phone:       sc.Phone,
		MaxUserID:   sc.MaxUserID,
		MaxUsername: sc.Username,
		Tags:        []string{},
	})
	if err != nil {
		log.Printf("[MAX Bot] CreateClient from contact error: %v", err)
		s.sendToMember(ctx, m, "⚠️ Не удалось сохранить контакт клиента.", nil)
		return
	}

	text := fmt.Sprintf("➕ **Клиент «%s» добавлен и привязан к контакту MAX!**%s\n\nЕсли этот клиент уже был записан у вас под другим именем, нажмите «Привязать к существующему» ниже:",
		clean(created.Name), formatClientContactDetails(created))
	s.sendToMember(ctx, m, text, [][]ButtonReq{
		{{Type: "link", Text: "👤 Открыть карточку в админке", URL: s.AppLink(fmt.Sprintf("client_%d", created.ID))}},
		{{Type: "callback", Text: "🔗 Привязать к существующему клиенту", Payload: fmt.Sprintf("pick_merge_%d", created.ID)}},
		{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}},
	})
}

func (s *Service) SendLinkPrompt(ctx context.Context, m *db.Member, c *db.Client) {
	text := fmt.Sprintf("🔗 **Привязка контакта MAX к клиенту «%s»**\n\nОтправьте в этот чат **Контакт** (скрепка → Контакт) или **перешлите любое сообщение** от этого клиента.\n\nСледующий присланный контакт будет автоматически привязан к карточке «%s».",
		clean(c.Name), clean(c.Name))
	s.sendToMember(ctx, m, text, [][]ButtonReq{
		{{Type: "callback", Text: "✖️ Отменить привязку", Payload: "cancel_link"}},
	})
}

// handleStart registers/looks up the confectioner and shows the menu.
func (s *Service) handleStart(ctx context.Context, u UserInfo, chatID int64, payload string) {
	code := ""
	if strings.HasPrefix(payload, "join_") {
		code = strings.TrimPrefix(payload, "join_")
	}

	m, status, err := s.EnsureMember(ctx, u, chatID, code)
	if err != nil {
		log.Printf("[MAX Bot] EnsureMember error: %v", err)
		return
	}

	switch status {
	case "denied":
		s.sendText(ctx, chatID, u.ID, "🔒 Этот бот — закрытая система учёта для кондитеров. Если вам нужен доступ, попросите владельца кондитерской прислать ссылку-приглашение.")
		return
	case "bad_code":
		s.sendText(ctx, chatID, u.ID, "⚠️ Код приглашения недействителен. Попросите владельца прислать актуальную ссылку.")
		return
	}

	prefix := ""
	switch status {
	case "created":
		prefix = fmt.Sprintf("👋 Здравствуйте, **%s**! Я создал для вас рабочее пространство «%s».\nЗдесь вы ведёте клиентов и заказы. Пригласить помощников можно кнопкой «Пригласить».\n\n", clean(m.DisplayName()), clean(m.WorkspaceName))
	case "joined":
		prefix = fmt.Sprintf("✅ Вы присоединились к «%s». Теперь у вас общая база клиентов и заказов.\n\n", clean(m.WorkspaceName))
	case "existing":
		if code != "" {
			prefix = fmt.Sprintf("ℹ️ Вы уже состоите в «%s».\n\n", clean(m.WorkspaceName))
		}
	}
	s.sendMenu(ctx, m, prefix)
}

func (s *Service) handleCallback(ctx context.Context, upd RawUpdate) {
	cb := upd.Callback
	if cb == nil {
		return
	}
	var chatID int64
	if upd.Message != nil {
		chatID = upd.Message.Recipient.ChatID
	}

	m, err := s.store.GetMember(ctx, cb.User.UserID)
	if err != nil {
		log.Printf("[MAX Bot] GetMember error: %v", err)
		return
	}
	if m == nil {
		_ = s.client.AnswerCallback(ctx, chatID, cb.CallbackID, "Сначала нажмите «Старт» в чате с ботом")
		return
	}
	if chatID == 0 {
		chatID = m.ChatID
	}
	_ = s.client.AnswerCallback(ctx, chatID, cb.CallbackID, "")

	switch {
	case cb.Payload == "menu":
		s.sendMenu(ctx, m, "")
	case cb.Payload == "today":
		s.sendDayOrders(ctx, m, 0)
	case cb.Payload == "tomorrow":
		s.sendDayOrders(ctx, m, 1)
	case cb.Payload == "stats":
		s.sendStats(ctx, m)
	case cb.Payload == "invite":
		s.sendInvite(ctx, m)
	case cb.Payload == "cancel_link":
		_ = s.store.SetMemberPendingLinkClient(ctx, m.MaxUserID, 0)
		s.sendMenu(ctx, m, "✖️ Привязка контакта отменена.\n\n")
	case strings.HasPrefix(cb.Payload, "pick_merge_"):
		srcID, err := strconv.ParseInt(strings.TrimPrefix(cb.Payload, "pick_merge_"), 10, 64)
		if err == nil && srcID > 0 {
			s.sendMergePicker(ctx, m, srcID)
		}
	case strings.HasPrefix(cb.Payload, "merge_"):
		parts := strings.Split(strings.TrimPrefix(cb.Payload, "merge_"), "_")
		if len(parts) == 2 {
			srcID, err1 := strconv.ParseInt(parts[0], 10, 64)
			dstID, err2 := strconv.ParseInt(parts[1], 10, 64)
			if err1 == nil && err2 == nil && srcID > 0 && dstID > 0 {
				merged, err := s.store.MergeClientContactInto(ctx, m.WorkspaceID, srcID, dstID)
				if err != nil {
					s.sendToMember(ctx, m, "⚠️ Не удалось объединить карточки клиента.", [][]ButtonReq{{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}}})
					return
				}
				text := fmt.Sprintf("✅ **Контакт MAX привязан к клиенту «%s»!**%s",
					clean(merged.Name), formatClientContactDetails(merged))
				s.sendToMember(ctx, m, text, [][]ButtonReq{
					{{Type: "link", Text: "👤 Открыть карточку в админке", URL: s.AppLink(fmt.Sprintf("client_%d", merged.ID))}},
					{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}},
				})
			}
		}
	}
}

func (s *Service) sendMergePicker(ctx context.Context, m *db.Member, sourceClientID int64) {
	clients, err := s.store.ListClients(ctx, m.WorkspaceID, "", "")
	if err != nil {
		s.sendToMember(ctx, m, "⚠️ Ошибка загрузки списка клиентов.", nil)
		return
	}
	var rows [][]ButtonReq
	for _, c := range clients {
		if c.ID == sourceClientID {
			continue
		}
		label := c.Name
		if c.Phone != "" {
			label += " (" + c.Phone + ")"
		}
		if r := []rune(label); len(r) > 45 {
			label = string(r[:45]) + "…"
		}
		rows = append(rows, []ButtonReq{{
			Type:    "callback",
			Text:    "👤 " + label,
			Payload: fmt.Sprintf("merge_%d_%d", sourceClientID, c.ID),
		}})
		if len(rows) >= 10 {
			break
		}
	}
	if len(rows) == 0 {
		s.sendToMember(ctx, m, "ℹ️ В вашей базе пока нет других клиентов для объединения.", [][]ButtonReq{
			{{Type: "link", Text: "👤 Открыть карточку", URL: s.AppLink(fmt.Sprintf("client_%d", sourceClientID))}},
			{{Type: "callback", Text: "⬅️ В меню", Payload: "menu"}},
		})
		return
	}
	rows = append(rows, []ButtonReq{{Type: "callback", Text: "⬅️ Оставить как нового клиента", Payload: "menu"}})
	s.sendToMember(ctx, m, "🔗 **Выберите существующего клиента**, к которому нужно привязать этот контакт MAX:", rows)
}

// ---------- Messages ----------

func (s *Service) sendText(ctx context.Context, chatID, userID int64, text string) {
	if _, err := s.client.SendMessageWithKeyboard(ctx, chatID, userID, text, nil); err != nil {
		log.Printf("[MAX Bot] send error: %v", err)
	}
}

func (s *Service) sendToMember(ctx context.Context, m *db.Member, text string, buttons [][]ButtonReq) {
	chatID, userID := m.ChatID, int64(0)
	if chatID == 0 {
		userID = m.MaxUserID
	}
	if _, err := s.client.SendMessageWithKeyboard(ctx, chatID, userID, text, buttons); err != nil {
		log.Printf("[MAX Bot] send to member %d error: %v", m.MaxUserID, err)
	}
}

func (s *Service) menuButtons(m *db.Member) [][]ButtonReq {
	rows := [][]ButtonReq{
		{{Type: "link", Text: "🗂 Открыть админку", URL: s.AppLink("home")}},
		{
			{Type: "link", Text: "➕ Новый клиент", URL: s.AppLink("new_client")},
			{Type: "link", Text: "🎂 Новый заказ", URL: s.AppLink("new_order")},
		},
		{
			{Type: "callback", Text: "📅 Сегодня", Payload: "today"},
			{Type: "callback", Text: "📅 Завтра", Payload: "tomorrow"},
		},
		{{Type: "callback", Text: "📊 Статистика месяца", Payload: "stats"}},
	}
	if m.Role == "owner" {
		rows = append(rows, []ButtonReq{{Type: "callback", Text: "👥 Пригласить кондитера", Payload: "invite"}})
	}
	return rows
}

func (s *Service) dayBounds(offsetDays int) (time.Time, time.Time) {
	now := time.Now().In(s.loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, offsetDays)
	return start, start.AddDate(0, 0, 1)
}

func (s *Service) sendMenu(ctx context.Context, m *db.Member, prefix string) {
	var counts [2]int
	for offset := range counts {
		from, to := s.dayBounds(offset)
		if list, err := s.store.ListOrdersBetween(ctx, m.WorkspaceID, from, to); err == nil {
			counts[offset] = len(list)
		}
	}
	todayN, tomorrowN := counts[0], counts[1]

	text := fmt.Sprintf("%s🎂 **%s**\nСегодня заказов: **%d**, завтра: **%d**.\n\n💡 _Отправьте сюда Контакт или перешлите сообщение от клиента, чтобы добавить его или привязать к MAX._\n\nВыберите действие:",
		prefix, clean(m.WorkspaceName), todayN, tomorrowN)
	s.sendToMember(ctx, m, text, s.menuButtons(m))
}

func (s *Service) orderLine(o db.Order) string {
	when := "время не указано"
	if o.DeliveryDate != nil {
		when = o.DeliveryDate.In(s.loc).Format("15:04")
	}
	line := fmt.Sprintf("• **%s** — %s", when, clean(o.ClientName))
	if d := strings.TrimSpace(o.CakeDescription); d != "" {
		line += ": " + clean(d)
	}
	if o.WeightKg > 0 {
		line += fmt.Sprintf(", %.1f кг", o.WeightKg)
	}
	if o.DeliveryType == "delivery" {
		line += ", доставка"
		if a := strings.TrimSpace(o.DeliveryAddress); a != "" {
			line += " (" + clean(a) + ")"
		}
	} else {
		line += ", самовывоз"
	}
	line += " · " + HumanOrderStatus(o.Status)
	return line
}

func (s *Service) sendDayOrders(ctx context.Context, m *db.Member, offset int) {
	from, to := s.dayBounds(offset)
	list, err := s.store.ListOrdersBetween(ctx, m.WorkspaceID, from, to)
	if err != nil {
		log.Printf("[MAX Bot] ListOrdersBetween error: %v", err)
		return
	}
	label := "Сегодня"
	if offset == 1 {
		label = "Завтра"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 **%s, %s**\n\n", label, from.Format("02.01")))
	if len(list) == 0 {
		sb.WriteString("Заказов на этот день нет 🎉")
	}
	for _, o := range list {
		sb.WriteString(s.orderLine(o) + "\n")
	}
	s.sendToMember(ctx, m, strings.TrimSpace(sb.String()), [][]ButtonReq{
		{{Type: "link", Text: "🗂 Открыть админку", URL: s.AppLink("orders_today")}},
	})
}

var monthNames = []string{"", "январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}

func (s *Service) sendStats(ctx context.Context, m *db.Member) {
	now := time.Now().In(s.loc)
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.loc)
	st, err := s.store.Stats(ctx, m.WorkspaceID, from, from.AddDate(0, 1, 0))
	if err != nil {
		log.Printf("[MAX Bot] Stats error: %v", err)
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 **Итоги: %s %d**\n\n", monthNames[now.Month()], now.Year()))
	sb.WriteString(fmt.Sprintf("• Выручка: **%.0f ₽**\n", st.Revenue))
	sb.WriteString(fmt.Sprintf("• Выполнено заказов: **%d** (%.1f кг)\n", st.OrdersCompleted, st.WeightKg))
	sb.WriteString(fmt.Sprintf("• Средний чек: **%.0f ₽**\n", st.AvgCheck))
	sb.WriteString(fmt.Sprintf("• Новых клиентов: **%d**\n", st.NewClients))
	sb.WriteString(fmt.Sprintf("• Заказов в работе: **%d**, к доплате: **%.0f ₽**\n", st.ActiveOrders, st.Outstanding))
	if len(st.TopClients) > 0 {
		sb.WriteString("\n🏆 **Топ клиентов:**\n")
		for i, t := range st.TopClients {
			sb.WriteString(fmt.Sprintf("%d. %s — %.0f ₽ (%d)\n", i+1, clean(t.Name), t.Revenue, t.Orders))
		}
	}
	s.sendToMember(ctx, m, strings.TrimSpace(sb.String()), nil)
}

func (s *Service) sendInvite(ctx context.Context, m *db.Member) {
	if m.Role != "owner" {
		s.sendText(ctx, m.ChatID, m.MaxUserID, "Приглашать кондитеров может только владелец пространства.")
		return
	}
	text := fmt.Sprintf("👥 **Приглашение в «%s»**\n\nОтправьте эту ссылку помощнику — он нажмёт «Старт» и получит доступ к общей базе клиентов и заказов:\n%s\n\nИли пусть напишет боту: /join %s",
		clean(m.WorkspaceName), s.InviteLink(m.InviteCode), m.InviteCode)
	s.sendToMember(ctx, m, text, nil)
}

// ---------- Reminders ----------

func (s *Service) StartReminders(ctx context.Context) {
	if !s.client.IsConfigured() {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runReminders(ctx)
		}
	}
}

func (s *Service) runReminders(ctx context.Context) {
	items, err := s.store.ClaimDueReminders(ctx)
	if err != nil {
		log.Printf("[Reminders] claim error: %v", err)
		return
	}
	for _, it := range items {
		members, err := s.store.ListMembers(ctx, it.WorkspaceID)
		if err != nil {
			continue
		}
		text := fmt.Sprintf("⏰ **Напоминание о заказе #%d**\n%s — %s", it.OrderID,
			it.DeliveryDate.In(s.loc).Format("02.01 в 15:04"), clean(it.ClientName))
		if d := strings.TrimSpace(it.CakeDescription); d != "" {
			text += "\n" + clean(d)
		}
		if it.WeightKg > 0 {
			text += fmt.Sprintf(" (%.1f кг)", it.WeightKg)
		}
		if it.DeliveryType == "delivery" {
			text += "\n🚗 Доставка"
			if a := strings.TrimSpace(it.DeliveryAddress); a != "" {
				text += ": " + clean(a)
			}
		} else {
			text += "\n🏪 Самовывоз"
		}
		for i := range members {
			s.sendToMember(ctx, &members[i], text, nil)
		}
	}
}

// ---------- Formatting helpers ----------

func HumanOrderStatus(status string) string {
	switch status {
	case "new":
		return "📝 Новый"
	case "awaiting_prepayment":
		return "⏳ Ждёт предоплаты"
	case "in_progress":
		return "👩‍🍳 В работе"
	case "ready":
		return "🎂 Готов"
	case "completed":
		return "✅ Выполнен"
	case "cancelled":
		return "❌ Отменён"
	default:
		return status
	}
}

// clean strips markdown control characters from user-provided text.
func clean(s string) string {
	return strings.NewReplacer("*", "", "_", " ", "`", "", "~", "", "[", "(", "]", ")").Replace(s)
}
