package bot

import (
	"context"
	"fmt"
	"log"
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
	s.handleStart(ctx, u, chatID, "")
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

	switch cb.Payload {
	case "menu":
		s.sendMenu(ctx, m, "")
	case "today":
		s.sendDayOrders(ctx, m, 0)
	case "tomorrow":
		s.sendDayOrders(ctx, m, 1)
	case "stats":
		s.sendStats(ctx, m)
	case "invite":
		s.sendInvite(ctx, m)
	}
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

	text := fmt.Sprintf("%s🎂 **%s**\nСегодня заказов: **%d**, завтра: **%d**.\n\nВыберите действие:",
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
