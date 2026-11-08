package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"confectioner-backend/internal/storage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type Attachment struct {
	Type     string `json:"type"`
	S3Key    string `json:"s3_key"`
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
}

type ActiveOrderBrief struct {
	ID              int64      `json:"id"`
	Status          string     `json:"status"`
	CakeDescription string     `json:"cake_description"`
	WeightKg        float64    `json:"weight_kg"`
	Price           float64    `json:"price"`
	DeliveryDate    *time.Time `json:"delivery_date,omitempty"`
}

type Client struct {
	ID              int64             `json:"id"`
	MaxUserID       int64             `json:"max_user_id"`
	MaxChatID       int64             `json:"max_chat_id"`
	FirstName       string            `json:"first_name"`
	LastName        string            `json:"last_name"`
	Username        string            `json:"username"`
	AvatarURL       string            `json:"avatar_url"`
	CustomName      string            `json:"custom_name"`
	Phone           string            `json:"phone"`
	Address         string            `json:"address"`
	Description     string            `json:"description"`
	UnreadCount     int               `json:"unread_count"`
	LastMessageText string            `json:"last_message_text"`
	LastMessageAt   time.Time         `json:"last_message_at"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	ActiveOrder     *ActiveOrderBrief `json:"active_order,omitempty"`
}

func (c Client) DisplayName() string {
	if strings.TrimSpace(c.CustomName) != "" {
		return strings.TrimSpace(c.CustomName)
	}
	full := strings.TrimSpace(c.FirstName + " " + c.LastName)
	if full != "" {
		return full
	}
	if c.Username != "" {
		return "@" + c.Username
	}
	return fmt.Sprintf("Клиент #%d", c.ID)
}

type Message struct {
	ID          int64        `json:"id"`
	ClientID    int64        `json:"client_id"`
	MaxMID      string       `json:"max_mid"`
	SenderType  string       `json:"sender_type"` // "client" | "operator" | "system"
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
	CreatedAt   time.Time    `json:"created_at"`
}

type OrderPhoto struct {
	ID        int64     `json:"id"`
	OrderID   int64     `json:"order_id"`
	Kind      string    `json:"kind"` // "reference" | "result"
	S3Key     string    `json:"s3_key"`
	URL       string    `json:"url"`
	Caption   string    `json:"caption"`
	CreatedAt time.Time `json:"created_at"`
}

type Order struct {
	ID               int64        `json:"id"`
	ClientID         int64        `json:"client_id"`
	Status           string       `json:"status"` // "new" | "awaiting_prepayment" | "in_progress" | "ready" | "completed" | "cancelled"
	CakeDescription  string       `json:"cake_description"`
	Filling          string       `json:"filling"`
	DecorInscription string       `json:"decor_inscription"`
	WeightKg         float64      `json:"weight_kg"`
	Price            float64      `json:"price"`
	Prepayment       float64      `json:"prepayment"`
	PaymentStatus    string       `json:"payment_status"` // "unpaid" | "prepaid" | "paid"
	DeliveryType     string       `json:"delivery_type"`  // "pickup" | "delivery"
	DeliveryAddress  string       `json:"delivery_address"`
	DeliveryDate     *time.Time   `json:"delivery_date,omitempty"`
	Photos           []OrderPhoto `json:"photos"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}
	return s, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS clients (
	id BIGSERIAL PRIMARY KEY,
	max_user_id BIGINT UNIQUE NOT NULL,
	max_chat_id BIGINT NOT NULL,
	first_name TEXT NOT NULL DEFAULT '',
	last_name TEXT NOT NULL DEFAULT '',
	username TEXT NOT NULL DEFAULT '',
	avatar_url TEXT NOT NULL DEFAULT '',
	custom_name TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	unread_count INT NOT NULL DEFAULT 0,
	last_message_text TEXT NOT NULL DEFAULT '',
	last_message_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_clients_max_chat_id ON clients(max_chat_id);
CREATE INDEX IF NOT EXISTS idx_clients_last_message_at ON clients(last_message_at DESC);

CREATE TABLE IF NOT EXISTS messages (
	id BIGSERIAL PRIMARY KEY,
	client_id BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	max_mid TEXT NOT NULL DEFAULT '',
	sender_type TEXT NOT NULL,
	text TEXT NOT NULL DEFAULT '',
	attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_client_id_created ON messages(client_id, created_at ASC);

CREATE TABLE IF NOT EXISTS orders (
	id BIGSERIAL PRIMARY KEY,
	client_id BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'new',
	cake_description TEXT NOT NULL DEFAULT '',
	filling TEXT NOT NULL DEFAULT '',
	decor_inscription TEXT NOT NULL DEFAULT '',
	weight_kg NUMERIC(6,2) NOT NULL DEFAULT 0,
	price NUMERIC(12,2) NOT NULL DEFAULT 0,
	prepayment NUMERIC(12,2) NOT NULL DEFAULT 0,
	payment_status TEXT NOT NULL DEFAULT 'unpaid',
	delivery_type TEXT NOT NULL DEFAULT 'pickup',
	delivery_address TEXT NOT NULL DEFAULT '',
	delivery_date TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_orders_client_id ON orders(client_id);

CREATE TABLE IF NOT EXISTS order_photos (
	id BIGSERIAL PRIMARY KEY,
	order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT 'reference',
	s3_key TEXT NOT NULL,
	url TEXT NOT NULL,
	caption TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_order_photos_order_id ON order_photos(order_id);

CREATE TABLE IF NOT EXISTS bot_state (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err := s.pool.Exec(ctx, schema)
	return err
}

func (s *Store) GetBotMarker(ctx context.Context) (int64, error) {
	var val string
	err := s.pool.QueryRow(ctx, `SELECT value FROM bot_state WHERE key = 'polling_marker'`).Scan(&val)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var marker int64
	_, _ = fmt.Sscanf(val, "%d", &marker)
	return marker, nil
}

func (s *Store) SetBotMarker(ctx context.Context, marker int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bot_state (key, value, updated_at)
		VALUES ('polling_marker', $1, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, fmt.Sprintf("%d", marker))
	return err
}

func (s *Store) UpsertClientFromBot(ctx context.Context, maxUserID, maxChatID int64, firstName, lastName, username, avatarURL string) (*Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx, `
		INSERT INTO clients (max_user_id, max_chat_id, first_name, last_name, username, avatar_url, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (max_user_id) DO UPDATE SET
			max_chat_id = CASE WHEN EXCLUDED.max_chat_id != 0 THEN EXCLUDED.max_chat_id ELSE clients.max_chat_id END,
			first_name = CASE WHEN EXCLUDED.first_name != '' THEN EXCLUDED.first_name ELSE clients.first_name END,
			last_name = CASE WHEN EXCLUDED.last_name != '' THEN EXCLUDED.last_name ELSE clients.last_name END,
			username = CASE WHEN EXCLUDED.username != '' THEN EXCLUDED.username ELSE clients.username END,
			avatar_url = CASE WHEN EXCLUDED.avatar_url != '' THEN EXCLUDED.avatar_url ELSE clients.avatar_url END,
			updated_at = NOW()
		RETURNING id, max_user_id, max_chat_id, first_name, last_name, username, avatar_url,
		          custom_name, phone, address, description, unread_count, last_message_text,
		          last_message_at, created_at, updated_at
	`, maxUserID, maxChatID, firstName, lastName, username, avatarURL).Scan(
		&c.ID, &c.MaxUserID, &c.MaxChatID, &c.FirstName, &c.LastName, &c.Username, &c.AvatarURL,
		&c.CustomName, &c.Phone, &c.Address, &c.Description, &c.UnreadCount, &c.LastMessageText,
		&c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetClient(ctx context.Context, id int64) (*Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx, `
		SELECT id, max_user_id, max_chat_id, first_name, last_name, username, avatar_url,
		       custom_name, phone, address, description, unread_count, last_message_text,
		       last_message_at, created_at, updated_at
		FROM clients WHERE id = $1
	`, id).Scan(
		&c.ID, &c.MaxUserID, &c.MaxChatID, &c.FirstName, &c.LastName, &c.Username, &c.AvatarURL,
		&c.CustomName, &c.Phone, &c.Address, &c.Description, &c.UnreadCount, &c.LastMessageText,
		&c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	active, _ := s.GetActiveOrder(ctx, c.ID)
	if active != nil {
		c.ActiveOrder = &ActiveOrderBrief{
			ID:              active.ID,
			Status:          active.Status,
			CakeDescription: active.CakeDescription,
			WeightKg:        active.WeightKg,
			Price:           active.Price,
			DeliveryDate:    active.DeliveryDate,
		}
	}
	return &c, nil
}

func (s *Store) GetClientByMaxUserID(ctx context.Context, maxUserID int64) (*Client, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM clients WHERE max_user_id = $1`, maxUserID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetClient(ctx, id)
}

func (s *Store) ListClients(ctx context.Context, search, filter string) ([]Client, error) {
	query := `
		SELECT c.id, c.max_user_id, c.max_chat_id, c.first_name, c.last_name, c.username, c.avatar_url,
		       c.custom_name, c.phone, c.address, c.description, c.unread_count, c.last_message_text,
		       c.last_message_at, c.created_at, c.updated_at,
		       ao.id, ao.status, ao.cake_description, ao.weight_kg, ao.price, ao.delivery_date
		FROM clients c
		LEFT JOIN LATERAL (
			SELECT id, status, cake_description, weight_kg, price, delivery_date
			FROM orders o
			WHERE o.client_id = c.id AND o.status NOT IN ('completed', 'cancelled')
			ORDER BY o.created_at DESC
			LIMIT 1
		) ao ON TRUE
		WHERE 1=1
	`
	var args []any
	argIdx := 1

	if strings.TrimSpace(search) != "" {
		query += fmt.Sprintf(` AND (
			c.first_name ILIKE $%d OR
			c.last_name ILIKE $%d OR
			c.custom_name ILIKE $%d OR
			c.username ILIKE $%d OR
			c.phone ILIKE $%d OR
			c.description ILIKE $%d OR
			c.address ILIKE $%d
		)`, argIdx, argIdx, argIdx, argIdx, argIdx, argIdx, argIdx)
		args = append(args, "%"+strings.TrimSpace(search)+"%")
		argIdx++
	}

	switch filter {
	case "unread":
		query += ` AND c.unread_count > 0`
	case "active_order":
		query += ` AND ao.id IS NOT NULL`
	}

	query += ` ORDER BY c.last_message_at DESC, c.id DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Client
	for rows.Next() {
		var c Client
		var aoID *int64
		var aoStatus, aoDesc *string
		var aoWeight, aoPrice *float64
		var aoDate *time.Time

		if err := rows.Scan(
			&c.ID, &c.MaxUserID, &c.MaxChatID, &c.FirstName, &c.LastName, &c.Username, &c.AvatarURL,
			&c.CustomName, &c.Phone, &c.Address, &c.Description, &c.UnreadCount, &c.LastMessageText,
			&c.LastMessageAt, &c.CreatedAt, &c.UpdatedAt,
			&aoID, &aoStatus, &aoDesc, &aoWeight, &aoPrice, &aoDate,
		); err != nil {
			return nil, err
		}
		if aoID != nil && aoStatus != nil {
			c.ActiveOrder = &ActiveOrderBrief{
				ID:              *aoID,
				Status:          *aoStatus,
				CakeDescription: derefStr(aoDesc),
				WeightKg:        derefFloat(aoWeight),
				Price:           derefFloat(aoPrice),
				DeliveryDate:    aoDate,
			}
		}
		result = append(result, c)
	}
	if result == nil {
		result = []Client{}
	}
	return result, nil
}

func (s *Store) UpdateClientProfile(ctx context.Context, id int64, customName, phone, address, description string) (*Client, error) {
	_, err := s.pool.Exec(ctx, `
		UPDATE clients
		SET custom_name = $2,
		    phone = $3,
		    address = $4,
		    description = $5,
		    updated_at = NOW()
		WHERE id = $1
	`, id, strings.TrimSpace(customName), strings.TrimSpace(phone), strings.TrimSpace(address), strings.TrimSpace(description))
	if err != nil {
		return nil, err
	}
	return s.GetClient(ctx, id)
}

func (s *Store) UpdateClientPhone(ctx context.Context, id int64, phone string) (*Client, error) {
	_, err := s.pool.Exec(ctx, `
		UPDATE clients SET phone = $2, updated_at = NOW() WHERE id = $1
	`, id, strings.TrimSpace(phone))
	if err != nil {
		return nil, err
	}
	return s.GetClient(ctx, id)
}

func (s *Store) ResetUnreadCount(ctx context.Context, clientID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE clients SET unread_count = 0 WHERE id = $1`, clientID)
	return err
}

func (s *Store) CreateMessage(ctx context.Context, clientID int64, maxMID, senderType, text string, attachments []Attachment, incrementUnread bool) (*Message, error) {
	if attachments == nil {
		attachments = []Attachment{}
	}
	attBytes, err := json.Marshal(attachments)
	if err != nil {
		return nil, err
	}

	var m Message
	var rawAtt []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO messages (client_id, max_mid, sender_type, text, attachments, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, NOW())
		RETURNING id, client_id, max_mid, sender_type, text, attachments, created_at
	`, clientID, maxMID, senderType, text, string(attBytes)).Scan(
		&m.ID, &m.ClientID, &m.MaxMID, &m.SenderType, &m.Text, &rawAtt, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rawAtt, &m.Attachments)
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}

	preview := strings.TrimSpace(text)
	if preview == "" && len(attachments) > 0 {
		preview = "📷 Фотография"
	}
	if len([]rune(preview)) > 120 {
		preview = string([]rune(preview)[:120]) + "…"
	}

	if incrementUnread {
		_, _ = s.pool.Exec(ctx, `
			UPDATE clients
			SET last_message_text = $2,
			    last_message_at = $3,
			    unread_count = unread_count + 1,
			    updated_at = NOW()
			WHERE id = $1
		`, clientID, preview, m.CreatedAt)
	} else {
		_, _ = s.pool.Exec(ctx, `
			UPDATE clients
			SET last_message_text = $2,
			    last_message_at = $3,
			    updated_at = NOW()
			WHERE id = $1
		`, clientID, preview, m.CreatedAt)
	}

	return &m, nil
}

func (s *Store) ListMessages(ctx context.Context, clientID int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, client_id, max_mid, sender_type, text, attachments, created_at
		FROM messages
		WHERE client_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2
	`, clientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Message
	for rows.Next() {
		var m Message
		var rawAtt []byte
		if err := rows.Scan(&m.ID, &m.ClientID, &m.MaxMID, &m.SenderType, &m.Text, &rawAtt, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawAtt, &m.Attachments)
		if m.Attachments == nil {
			m.Attachments = []Attachment{}
		}
		list = append(list, m)
	}
	if list == nil {
		list = []Message{}
	}
	return list, nil
}

func (s *Store) GetOrderByID(ctx context.Context, orderID int64) (*Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx, `
		SELECT id, client_id, status, cake_description, filling, decor_inscription,
		       weight_kg, price, prepayment, payment_status, delivery_type,
		       delivery_address, delivery_date, created_at, updated_at, completed_at
		FROM orders WHERE id = $1
	`, orderID).Scan(
		&o.ID, &o.ClientID, &o.Status, &o.CakeDescription, &o.Filling, &o.DecorInscription,
		&o.WeightKg, &o.Price, &o.Prepayment, &o.PaymentStatus, &o.DeliveryType,
		&o.DeliveryAddress, &o.DeliveryDate, &o.CreatedAt, &o.UpdatedAt, &o.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	photos, err := s.listOrderPhotos(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Photos = photos
	return &o, nil
}

func (s *Store) GetActiveOrder(ctx context.Context, clientID int64) (*Order, error) {
	var orderID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM orders
		WHERE client_id = $1 AND status NOT IN ('completed', 'cancelled')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, clientID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetOrderByID(ctx, orderID)
}

func (s *Store) ListOrderHistory(ctx context.Context, clientID int64) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, client_id, status, cake_description, filling, decor_inscription,
		       weight_kg, price, prepayment, payment_status, delivery_type,
		       delivery_address, delivery_date, created_at, updated_at, completed_at
		FROM orders
		WHERE client_id = $1 AND status IN ('completed', 'cancelled')
		ORDER BY COALESCE(completed_at, updated_at) DESC, id DESC
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(
			&o.ID, &o.ClientID, &o.Status, &o.CakeDescription, &o.Filling, &o.DecorInscription,
			&o.WeightKg, &o.Price, &o.Prepayment, &o.PaymentStatus, &o.DeliveryType,
			&o.DeliveryAddress, &o.DeliveryDate, &o.CreatedAt, &o.UpdatedAt, &o.CompletedAt,
		); err != nil {
			return nil, err
		}
		o.Photos, _ = s.listOrderPhotos(ctx, o.ID)
		orders = append(orders, o)
	}
	if orders == nil {
		orders = []Order{}
	}
	return orders, nil
}

func (s *Store) SaveOrder(ctx context.Context, in Order) (*Order, error) {
	if in.Status == "" {
		in.Status = "new"
	}
	if in.PaymentStatus == "" {
		in.PaymentStatus = "unpaid"
	}
	if in.DeliveryType == "" {
		in.DeliveryType = "pickup"
	}

	var completedAt *time.Time
	if in.Status == "completed" || in.Status == "cancelled" {
		now := time.Now()
		completedAt = &now
	}

	if in.ID == 0 {
		var newID int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO orders (
				client_id, status, cake_description, filling, decor_inscription,
				weight_kg, price, prepayment, payment_status, delivery_type,
				delivery_address, delivery_date, completed_at, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),NOW())
			RETURNING id
		`,
			in.ClientID, in.Status, in.CakeDescription, in.Filling, in.DecorInscription,
			in.WeightKg, in.Price, in.Prepayment, in.PaymentStatus, in.DeliveryType,
			in.DeliveryAddress, in.DeliveryDate, completedAt,
		).Scan(&newID)
		if err != nil {
			return nil, err
		}
		return s.GetOrderByID(ctx, newID)
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			status = $2,
			cake_description = $3,
			filling = $4,
			decor_inscription = $5,
			weight_kg = $6,
			price = $7,
			prepayment = $8,
			payment_status = $9,
			delivery_type = $10,
			delivery_address = $11,
			delivery_date = $12,
			completed_at = COALESCE($13, completed_at),
			updated_at = NOW()
		WHERE id = $1
	`,
		in.ID, in.Status, in.CakeDescription, in.Filling, in.DecorInscription,
		in.WeightKg, in.Price, in.Prepayment, in.PaymentStatus, in.DeliveryType,
		in.DeliveryAddress, in.DeliveryDate, completedAt,
	)
	if err != nil {
		return nil, err
	}
	return s.GetOrderByID(ctx, in.ID)
}

func (s *Store) UpdateOrderStatus(ctx context.Context, orderID int64, status string) (*Order, error) {
	var completedAt *time.Time
	if status == "completed" || status == "cancelled" {
		now := time.Now()
		completedAt = &now
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE orders
		SET status = $2,
		    completed_at = CASE WHEN $3::timestamptz IS NOT NULL THEN $3::timestamptz ELSE completed_at END,
		    updated_at = NOW()
		WHERE id = $1
	`, orderID, status, completedAt)
	if err != nil {
		return nil, err
	}
	return s.GetOrderByID(ctx, orderID)
}

func (s *Store) AddOrderPhoto(ctx context.Context, orderID int64, kind, s3Key, url, caption string) (*OrderPhoto, error) {
	if kind != "result" {
		kind = "reference"
	}
	var p OrderPhoto
	err := s.pool.QueryRow(ctx, `
		INSERT INTO order_photos (order_id, kind, s3_key, url, caption, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		RETURNING id, order_id, kind, s3_key, url, caption, created_at
	`, orderID, kind, s3Key, url, caption).Scan(
		&p.ID, &p.OrderID, &p.Kind, &p.S3Key, &p.URL, &p.Caption, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) DeleteOrderPhoto(ctx context.Context, photoID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM order_photos WHERE id = $1`, photoID)
	return err
}

func (s *Store) listOrderPhotos(ctx context.Context, orderID int64) ([]OrderPhoto, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, kind, s3_key, url, caption, created_at
		FROM order_photos
		WHERE order_id = $1
		ORDER BY created_at ASC, id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []OrderPhoto
	for rows.Next() {
		var p OrderPhoto
		if err := rows.Scan(&p.ID, &p.OrderID, &p.Kind, &p.S3Key, &p.URL, &p.Caption, &p.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	if list == nil {
		list = []OrderPhoto{}
	}
	return list, nil
}

func (s *Store) SeedDemoData(ctx context.Context, s3 *storage.S3Storage) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM clients`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Upload sample SVG cake illustrations to S3 so demo orders have real S3 objects
	berryCakeSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="600" height="450" viewBox="0 0 600 450">
		<defs>
			<linearGradient id="bg1" x1="0" y1="0" x2="1" y2="1">
				<stop offset="0%" stop-color="#fff0f5"/>
				<stop offset="100%" stop-color="#ffe4e9"/>
			</linearGradient>
		</defs>
		<rect width="600" height="450" fill="url(#bg1)"/>
		<ellipse cx="300" cy="360" rx="190" ry="30" fill="#e2c4c8"/>
		<rect x="150" y="210" width="300" height="140" rx="18" fill="#fffdfc" stroke="#f3c6d3" stroke-width="4"/>
		<path d="M150 235 Q175 265 200 235 T250 235 T300 235 T350 235 T400 235 T450 235" fill="none" stroke="#e11d48" stroke-width="10" stroke-linecap="round"/>
		<circle cx="210" cy="198" r="16" fill="#be123c"/>
		<circle cx="255" cy="195" r="18" fill="#e11d48"/>
		<circle cx="300" cy="192" r="19" fill="#9f1239"/>
		<circle cx="345" cy="195" r="18" fill="#e11d48"/>
		<circle cx="390" cy="198" r="16" fill="#be123c"/>
		<text x="300" y="305" font-family="sans-serif" font-size="22" font-weight="bold" fill="#881337" text-anchor="middle">Ягодный велюр 2.5 кг</text>
		<text x="300" y="415" font-family="sans-serif" font-size="16" fill="#9f1239" text-anchor="middle">Фото готового торта (S3 MinIO)</text>
	</svg>`)

	bentoCakeSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="600" height="450" viewBox="0 0 600 450">
		<rect width="600" height="450" fill="#fef9c3"/>
		<ellipse cx="300" cy="350" rx="170" ry="26" fill="#fde047"/>
		<rect x="170" y="220" width="260" height="120" rx="24" fill="#ffffff" stroke="#facc15" stroke-width="4"/>
		<text x="300" y="280" font-family="sans-serif" font-size="24" font-weight="bold" fill="#854d0e" text-anchor="middle">С Днём Рождения! ✨</text>
		<text x="300" y="312" font-family="sans-serif" font-size="15" fill="#a16207" text-anchor="middle">Бенто Манго-Маракуйя</text>
		<text x="300" y="410" font-family="sans-serif" font-size="16" fill="#854d0e" text-anchor="middle">Референс клиента (S3 MinIO)</text>
	</svg>`)

	chocoCakeSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="600" height="450" viewBox="0 0 600 450">
		<rect width="600" height="450" fill="#f5f5f4"/>
		<ellipse cx="300" cy="365" rx="185" ry="28" fill="#d6d3d1"/>
		<rect x="155" y="185" width="290" height="170" rx="16" fill="#451a03" stroke="#78350f" stroke-width="4"/>
		<path d="M155 215 Q185 255 210 215 T265 215 T320 215 T375 215 T445 215" fill="none" stroke="#fbbf24" stroke-width="8" stroke-linecap="round"/>
		<text x="300" y="285" font-family="sans-serif" font-size="22" font-weight="bold" fill="#fef3c7" text-anchor="middle">Три шоколада • Золото</text>
		<text x="300" y="415" font-family="sans-serif" font-size="16" fill="#57534e" text-anchor="middle">Архив заказов (S3 MinIO)</text>
	</svg>`)

	berryURL, _ := s3.PutStaticKey(ctx, "demo/berry_velvet_cake.svg", "image/svg+xml", berryCakeSVG)
	bentoURL, _ := s3.PutStaticKey(ctx, "demo/bento_reference.svg", "image/svg+xml", bentoCakeSVG)
	chocoURL, _ := s3.PutStaticKey(ctx, "demo/choco_gold_cake.svg", "image/svg+xml", chocoCakeSVG)

	// Client 1: Анна Смирнова
	c1, err := s.UpsertClientFromBot(ctx, 100001, 200001, "Анна", "Смирнова", "anna_sweet", "")
	if err != nil {
		return err
	}
	_, _ = s.UpdateClientProfile(ctx, c1.ID, "Анна Смирнова", "+7 (999) 123-45-67", "г. Москва, ул. Лесная, д. 12, кв. 45", "Постоянная клиентка. Любит нежные ягодные начинки, не слишком сладкий крем. Аллергия на арахис и фундук!")

	// Completed historical order for Анна
	pastDate := time.Now().AddDate(0, -1, -5)
	histOrder, err := s.SaveOrder(ctx, Order{
		ClientID:         c1.ID,
		Status:           "completed",
		CakeDescription:  "Двухъярусный торт в розовом велюре со свежей голубикой и малиной",
		Filling:          "Клубничный пломбир с фисташковым бисквитом",
		DecorInscription: "С 5-летием, София!",
		WeightKg:         2.5,
		Price:            7500,
		Prepayment:       7500,
		PaymentStatus:    "paid",
		DeliveryType:     "delivery",
		DeliveryAddress:  "г. Москва, ул. Лесная, д. 12, кв. 45",
		DeliveryDate:     &pastDate,
	})
	if err == nil && histOrder != nil {
		_, _ = s.AddOrderPhoto(ctx, histOrder.ID, "result", "demo/berry_velvet_cake.svg", berryURL, "Готовый торт перед отправкой")
	}

	// Active order for Анна
	upcoming := time.Now().AddDate(0, 0, 3).Truncate(time.Hour)
	active1, err := s.SaveOrder(ctx, Order{
		ClientID:         c1.ID,
		Status:           "in_progress",
		CakeDescription:  "Праздничный торт с живыми ягодами и акриловым топпером",
		Filling:          "Манго-маракуйя со сливочным крем-чизом",
		DecorInscription: "Любимой маме в День Рождения!",
		WeightKg:         2.0,
		Price:            6200,
		Prepayment:       3000,
		PaymentStatus:    "prepaid",
		DeliveryType:     "delivery",
		DeliveryAddress:  "г. Москва, ул. Лесная, д. 12, кв. 45",
		DeliveryDate:     &upcoming,
	})
	if err == nil && active1 != nil {
		_, _ = s.AddOrderPhoto(ctx, active1.ID, "reference", "demo/bento_reference.svg", bentoURL, "Пожелание по оформлению от Анны")
	}

	_, _ = s.CreateMessage(ctx, c1.ID, "demo_m1", "client", "Здравствуйте! Хочу заказать торт маме на юбилей к субботе на 2 кг 🎂", nil, false)
	_, _ = s.CreateMessage(ctx, c1.ID, "demo_m2", "operator", "Здравствуйте, Анна! С радостью сделаем. Начинку Манго-Маракуйя без орехов, как вы любите?", nil, false)
	_, _ = s.CreateMessage(ctx, c1.ID, "demo_m3", "client", "Да! Вот такой декор нравится, прикрепляю фото-референс:", []Attachment{
		{Type: "image", S3Key: "demo/bento_reference.svg", URL: bentoURL, Filename: "reference.svg"},
	}, true)

	// Client 2: Алексей Воронов
	c2, err := s.UpsertClientFromBot(ctx, 100002, 200002, "Алексей", "Воронов", "alex_voronov", "")
	if err == nil && c2 != nil {
		_, _ = s.UpdateClientProfile(ctx, c2.ID, "Алексей Воронов", "+7 (916) 555-88-99", "Самовывоз из кондитерской", "Предпочитает шоколадные начинки. Заказывает на корпоративы и семейные праздники.")
		past2 := time.Now().AddDate(0, -2, 0)
		h2, _ := s.SaveOrder(ctx, Order{
			ClientID:         c2.ID,
			Status:           "completed",
			CakeDescription:  "Шоколадный муссовый торт с золотым кандурином",
			Filling:          "Три бельгийских шоколада",
			DecorInscription: "Happy Birthday!",
			WeightKg:         3.0,
			Price:            8900,
			Prepayment:       8900,
			PaymentStatus:    "paid",
			DeliveryType:     "pickup",
			DeliveryAddress:  "Самовывоз",
			DeliveryDate:     &past2,
		})
		if h2 != nil {
			_, _ = s.AddOrderPhoto(ctx, h2.ID, "result", "demo/choco_gold_cake.svg", chocoURL, "Три шоколада 3 кг")
		}
		_, _ = s.CreateMessage(ctx, c2.ID, "demo_m4", "client", "Добрый день! Подскажите, можно ли на пятницу сделать торт «Сникерс» на 2.5 кг?", nil, true)
	}

	return nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

