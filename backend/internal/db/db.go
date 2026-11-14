package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = pgx.ErrNoRows

type Store struct {
	pool *pgxpool.Pool
}

type scanner interface {
	Scan(dest ...any) error
}

// ---------- Types ----------

type Member struct {
	MaxUserID     int64     `json:"max_user_id"`
	WorkspaceID   int64     `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	InviteCode    string    `json:"-"`
	ChatID        int64     `json:"-"`
	FirstName     string    `json:"first_name"`
	LastName      string    `json:"last_name"`
	Username      string    `json:"username"`
	Role          string    `json:"role"` // "owner" | "member"
	CreatedAt     time.Time `json:"created_at"`
}

func (m Member) DisplayName() string {
	full := strings.TrimSpace(m.FirstName + " " + m.LastName)
	if full != "" {
		return full
	}
	if m.Username != "" {
		return "@" + m.Username
	}
	return fmt.Sprintf("Кондитер #%d", m.MaxUserID)
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
	ID          int64             `json:"id"`
	WorkspaceID int64             `json:"workspace_id"`
	Name        string            `json:"name"`
	Phone       string            `json:"phone"`
	Address     string            `json:"address"`
	Description string            `json:"description"`
	MaxUsername string            `json:"max_username"`
	Tags        []string          `json:"tags"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ActiveOrder *ActiveOrderBrief `json:"active_order,omitempty"`
	OrdersCount int               `json:"orders_count"`
	TotalSpent  float64           `json:"total_spent"`
}

type Note struct {
	ID         int64     `json:"id"`
	ClientID   int64     `json:"client_id"`
	AuthorName string    `json:"author_name"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"created_at"`
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
	WorkspaceID      int64        `json:"workspace_id"`
	ClientID         int64        `json:"client_id"`
	ClientName       string       `json:"client_name"`
	Status           string       `json:"status"`
	CakeDescription  string       `json:"cake_description"`
	Filling          string       `json:"filling"`
	DecorInscription string       `json:"decor_inscription"`
	WeightKg         float64      `json:"weight_kg"`
	Price            float64      `json:"price"`
	Prepayment       float64      `json:"prepayment"`
	PaymentStatus    string       `json:"payment_status"`
	DeliveryType     string       `json:"delivery_type"`
	DeliveryAddress  string       `json:"delivery_address"`
	DeliveryDate     *time.Time   `json:"delivery_date,omitempty"`
	RemindBeforeMin  int          `json:"remind_before_min"`
	Photos           []OrderPhoto `json:"photos"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
}

type TopClient struct {
	ClientID int64   `json:"client_id"`
	Name     string  `json:"name"`
	Orders   int     `json:"orders"`
	Revenue  float64 `json:"revenue"`
}

type Stats struct {
	Month           string      `json:"month"`
	Revenue         float64     `json:"revenue"`
	OrdersCompleted int         `json:"orders_completed"`
	WeightKg        float64     `json:"weight_kg"`
	AvgCheck        float64     `json:"avg_check"`
	NewClients      int         `json:"new_clients"`
	ActiveOrders    int         `json:"active_orders"`
	Outstanding     float64     `json:"outstanding"`
	TopClients      []TopClient `json:"top_clients"`
}

type ReminderItem struct {
	OrderID         int64
	WorkspaceID     int64
	ClientName      string
	CakeDescription string
	WeightKg        float64
	DeliveryDate    time.Time
	DeliveryType    string
	DeliveryAddress string
}

// ---------- Setup ----------

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
	// The first (single-tenant, chat-based) version had no workspace_id. Keep that data
	// under legacy_* names instead of dropping it.
	var legacy bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'clients')
		   AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'clients' AND column_name = 'workspace_id')
	`).Scan(&legacy)
	if err != nil {
		return err
	}
	if legacy {
		if _, err := s.pool.Exec(ctx, `
			ALTER TABLE order_photos RENAME TO legacy_order_photos;
			ALTER TABLE orders RENAME TO legacy_orders;
			ALTER TABLE messages RENAME TO legacy_messages;
			ALTER TABLE clients RENAME TO legacy_clients;
		`); err != nil {
			return fmt.Errorf("archive legacy tables: %w", err)
		}
	}

	schema := `
CREATE TABLE IF NOT EXISTS workspaces (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	invite_code TEXT UNIQUE NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS members (
	max_user_id BIGINT PRIMARY KEY,
	workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	chat_id BIGINT NOT NULL DEFAULT 0,
	first_name TEXT NOT NULL DEFAULT '',
	last_name TEXT NOT NULL DEFAULT '',
	username TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'member',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_members_ws ON members(workspace_id);

CREATE TABLE IF NOT EXISTS clients (
	id BIGSERIAL PRIMARY KEY,
	workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	name TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	max_username TEXT NOT NULL DEFAULT '',
	tags TEXT[] NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_clients_ws ON clients(workspace_id);

CREATE TABLE IF NOT EXISTS client_notes (
	id BIGSERIAL PRIMARY KEY,
	client_id BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
	workspace_id BIGINT NOT NULL,
	author_id BIGINT NOT NULL DEFAULT 0,
	author_name TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notes_client ON client_notes(client_id, created_at DESC);

CREATE TABLE IF NOT EXISTS orders (
	id BIGSERIAL PRIMARY KEY,
	workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
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
	remind_before_min INT NOT NULL DEFAULT 120,
	reminded_at TIMESTAMPTZ NULL,
	created_by BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	completed_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_orders_ws_date ON orders(workspace_id, delivery_date);
CREATE INDEX IF NOT EXISTS idx_orders_client ON orders(client_id);

CREATE TABLE IF NOT EXISTS order_photos (
	id BIGSERIAL PRIMARY KEY,
	order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT 'reference',
	s3_key TEXT NOT NULL,
	url TEXT NOT NULL,
	caption TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_photos_order ON order_photos(order_id);

CREATE TABLE IF NOT EXISTS bot_state (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err = s.pool.Exec(ctx, schema)
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

// ---------- Workspaces & members ----------

func randomCode() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

const memberSelect = `m.max_user_id, m.workspace_id, w.name, w.invite_code, m.chat_id,
	m.first_name, m.last_name, m.username, m.role, m.created_at`

func scanMember(row scanner) (*Member, error) {
	var m Member
	if err := row.Scan(&m.MaxUserID, &m.WorkspaceID, &m.WorkspaceName, &m.InviteCode, &m.ChatID,
		&m.FirstName, &m.LastName, &m.Username, &m.Role, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMember returns nil, nil when the user is not registered.
func (s *Store) GetMember(ctx context.Context, maxUserID int64) (*Member, error) {
	m, err := scanMember(s.pool.QueryRow(ctx, `
		SELECT `+memberSelect+`
		FROM members m JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.max_user_id = $1`, maxUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

func (s *Store) ListMembers(ctx context.Context, workspaceID int64) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+memberSelect+`
		FROM members m JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.workspace_id = $1
		ORDER BY (m.role = 'owner') DESC, m.created_at ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *m)
	}
	return list, rows.Err()
}

func (s *Store) CreateWorkspaceWithOwner(ctx context.Context, m Member, workspaceName string) (*Member, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var wsID int64
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces (name, invite_code) VALUES ($1, $2) RETURNING id`,
		workspaceName, randomCode()).Scan(&wsID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO members (max_user_id, workspace_id, chat_id, first_name, last_name, username, role)
		VALUES ($1, $2, $3, $4, $5, $6, 'owner')
		ON CONFLICT (max_user_id) DO NOTHING`,
		m.MaxUserID, wsID, m.ChatID, m.FirstName, m.LastName, m.Username); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetMember(ctx, m.MaxUserID)
}

// JoinWorkspaceByCode returns nil, nil when the invite code is unknown.
func (s *Store) JoinWorkspaceByCode(ctx context.Context, code string, m Member) (*Member, error) {
	var wsID int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE invite_code = $1`, strings.TrimSpace(strings.ToLower(code))).Scan(&wsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO members (max_user_id, workspace_id, chat_id, first_name, last_name, username, role)
		VALUES ($1, $2, $3, $4, $5, $6, 'member')
		ON CONFLICT (max_user_id) DO NOTHING`,
		m.MaxUserID, wsID, m.ChatID, m.FirstName, m.LastName, m.Username); err != nil {
		return nil, err
	}
	return s.GetMember(ctx, m.MaxUserID)
}

func (s *Store) UpdateMemberChat(ctx context.Context, maxUserID, chatID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE members SET chat_id = $2 WHERE max_user_id = $1`, maxUserID, chatID)
	return err
}

// RemoveMember removes a non-owner member of the workspace.
func (s *Store) RemoveMember(ctx context.Context, workspaceID, maxUserID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM members WHERE workspace_id = $1 AND max_user_id = $2 AND role <> 'owner'`,
		workspaceID, maxUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Clients ----------

const clientSelect = `
	SELECT c.id, c.workspace_id, c.name, c.phone, c.address, c.description, c.max_username, c.tags,
	       c.created_at, c.updated_at,
	       ao.id, ao.status, ao.cake_description, ao.weight_kg, ao.price, ao.delivery_date,
	       COALESCE(st.cnt, 0), COALESCE(st.total, 0)
	FROM clients c
	LEFT JOIN LATERAL (
		SELECT id, status, cake_description, weight_kg, price, delivery_date
		FROM orders o
		WHERE o.client_id = c.id AND o.status NOT IN ('completed', 'cancelled')
		ORDER BY o.created_at DESC
		LIMIT 1
	) ao ON TRUE
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS cnt, COALESCE(SUM(price), 0) AS total
		FROM orders o
		WHERE o.client_id = c.id AND o.status = 'completed'
	) st ON TRUE
`

func scanClient(row scanner) (Client, error) {
	var c Client
	var aoID *int64
	var aoStatus, aoDesc *string
	var aoWeight, aoPrice *float64
	var aoDate *time.Time
	if err := row.Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Phone, &c.Address, &c.Description, &c.MaxUsername, &c.Tags,
		&c.CreatedAt, &c.UpdatedAt,
		&aoID, &aoStatus, &aoDesc, &aoWeight, &aoPrice, &aoDate,
		&c.OrdersCount, &c.TotalSpent,
	); err != nil {
		return c, err
	}
	if c.Tags == nil {
		c.Tags = []string{}
	}
	if aoID != nil && aoStatus != nil {
		c.ActiveOrder = &ActiveOrderBrief{ID: *aoID, Status: *aoStatus, DeliveryDate: aoDate}
		if aoDesc != nil {
			c.ActiveOrder.CakeDescription = *aoDesc
		}
		if aoWeight != nil {
			c.ActiveOrder.WeightKg = *aoWeight
		}
		if aoPrice != nil {
			c.ActiveOrder.Price = *aoPrice
		}
	}
	return c, nil
}

func (s *Store) ListClients(ctx context.Context, workspaceID int64, search, tag string) ([]Client, error) {
	query := clientSelect + ` WHERE c.workspace_id = $1`
	args := []any{workspaceID}

	if q := strings.TrimSpace(search); q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		query += fmt.Sprintf(` AND (c.name ILIKE $%[1]d OR c.phone ILIKE $%[1]d OR c.address ILIKE $%[1]d
			OR c.description ILIKE $%[1]d OR c.max_username ILIKE $%[1]d OR array_to_string(c.tags, ' ') ILIKE $%[1]d)`, n)
	}
	if t := strings.TrimSpace(tag); t != "" {
		args = append(args, t)
		query += fmt.Sprintf(` AND $%d = ANY(c.tags)`, len(args))
	}
	query += ` ORDER BY lower(c.name) ASC, c.id ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Client{}
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetClient(ctx context.Context, workspaceID, id int64) (*Client, error) {
	c, err := scanClient(s.pool.QueryRow(ctx, clientSelect+` WHERE c.workspace_id = $1 AND c.id = $2`, workspaceID, id))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateClient(ctx context.Context, workspaceID int64, c Client) (*Client, error) {
	if c.Tags == nil {
		c.Tags = []string{}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO clients (workspace_id, name, phone, address, description, max_username, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		workspaceID, c.Name, c.Phone, c.Address, c.Description, c.MaxUsername, c.Tags).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetClient(ctx, workspaceID, id)
}

func (s *Store) UpdateClient(ctx context.Context, workspaceID, id int64, c Client) (*Client, error) {
	if c.Tags == nil {
		c.Tags = []string{}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE clients SET name = $3, phone = $4, address = $5, description = $6,
		       max_username = $7, tags = $8, updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id, c.Name, c.Phone, c.Address, c.Description, c.MaxUsername, c.Tags)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetClient(ctx, workspaceID, id)
}

func (s *Store) DeleteClient(ctx context.Context, workspaceID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM clients WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Notes ----------

func (s *Store) ListNotes(ctx context.Context, workspaceID, clientID int64) ([]Note, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, client_id, author_name, text, created_at
		FROM client_notes
		WHERE workspace_id = $1 AND client_id = $2
		ORDER BY created_at DESC, id DESC`, workspaceID, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.ClientID, &n.AuthorName, &n.Text, &n.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

func (s *Store) AddNote(ctx context.Context, workspaceID, clientID, authorID int64, authorName, text string) (*Note, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM clients WHERE id = $1 AND workspace_id = $2)`,
		clientID, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	var n Note
	err := s.pool.QueryRow(ctx, `
		INSERT INTO client_notes (client_id, workspace_id, author_id, author_name, text)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, client_id, author_name, text, created_at`,
		clientID, workspaceID, authorID, authorName, text).Scan(&n.ID, &n.ClientID, &n.AuthorName, &n.Text, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) DeleteNote(ctx context.Context, workspaceID, noteID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM client_notes WHERE workspace_id = $1 AND id = $2`, workspaceID, noteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Orders ----------

const orderSelect = `
	SELECT o.id, o.workspace_id, o.client_id, c.name, o.status, o.cake_description, o.filling, o.decor_inscription,
	       o.weight_kg, o.price, o.prepayment, o.payment_status, o.delivery_type, o.delivery_address, o.delivery_date,
	       o.remind_before_min, o.created_at, o.updated_at, o.completed_at
	FROM orders o
	JOIN clients c ON c.id = o.client_id
`

func scanOrder(row scanner) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.WorkspaceID, &o.ClientID, &o.ClientName, &o.Status, &o.CakeDescription, &o.Filling,
		&o.DecorInscription, &o.WeightKg, &o.Price, &o.Prepayment, &o.PaymentStatus, &o.DeliveryType,
		&o.DeliveryAddress, &o.DeliveryDate, &o.RemindBeforeMin, &o.CreatedAt, &o.UpdatedAt, &o.CompletedAt)
	o.Photos = []OrderPhoto{}
	return o, err
}

func (s *Store) queryOrders(ctx context.Context, where string, args ...any) ([]Order, error) {
	rows, err := s.pool.Query(ctx, orderSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return list, s.attachPhotos(ctx, list)
}

func (s *Store) attachPhotos(ctx context.Context, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(orders))
	index := make(map[int64]int, len(orders))
	for i, o := range orders {
		ids = append(ids, o.ID)
		index[o.ID] = i
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, kind, s3_key, url, caption, created_at
		FROM order_photos WHERE order_id = ANY($1)
		ORDER BY created_at ASC, id ASC`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p OrderPhoto
		if err := rows.Scan(&p.ID, &p.OrderID, &p.Kind, &p.S3Key, &p.URL, &p.Caption, &p.CreatedAt); err != nil {
			return err
		}
		if i, ok := index[p.OrderID]; ok {
			orders[i].Photos = append(orders[i].Photos, p)
		}
	}
	return rows.Err()
}

func (s *Store) GetOrder(ctx context.Context, workspaceID, orderID int64) (*Order, error) {
	list, err := s.queryOrders(ctx, ` WHERE o.workspace_id = $1 AND o.id = $2`, workspaceID, orderID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// GetActiveOrder returns nil, nil when the client has no active order.
func (s *Store) GetActiveOrder(ctx context.Context, workspaceID, clientID int64) (*Order, error) {
	list, err := s.queryOrders(ctx, `
		WHERE o.workspace_id = $1 AND o.client_id = $2 AND o.status NOT IN ('completed', 'cancelled')
		ORDER BY o.created_at DESC, o.id DESC LIMIT 1`, workspaceID, clientID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (s *Store) ListOrderHistory(ctx context.Context, workspaceID, clientID int64) ([]Order, error) {
	return s.queryOrders(ctx, `
		WHERE o.workspace_id = $1 AND o.client_id = $2 AND o.status IN ('completed', 'cancelled')
		ORDER BY COALESCE(o.delivery_date, o.completed_at, o.updated_at) DESC, o.id DESC`, workspaceID, clientID)
}

// ListActiveOrders returns every non-finished order of the workspace, soonest first, undated last.
func (s *Store) ListActiveOrders(ctx context.Context, workspaceID int64) ([]Order, error) {
	return s.queryOrders(ctx, `
		WHERE o.workspace_id = $1 AND o.status NOT IN ('completed', 'cancelled')
		ORDER BY o.delivery_date ASC NULLS LAST, o.id ASC`, workspaceID)
}

// ListOrdersBetween returns active orders whose delivery date is within [from, to).
func (s *Store) ListOrdersBetween(ctx context.Context, workspaceID int64, from, to time.Time) ([]Order, error) {
	return s.queryOrders(ctx, `
		WHERE o.workspace_id = $1 AND o.status NOT IN ('completed', 'cancelled')
		  AND o.delivery_date >= $2 AND o.delivery_date < $3
		ORDER BY o.delivery_date ASC, o.id ASC`, workspaceID, from, to)
}

func (s *Store) SaveOrder(ctx context.Context, workspaceID, userID int64, in Order) (*Order, error) {
	if in.Status == "" {
		in.Status = "new"
	}
	if in.PaymentStatus == "" {
		in.PaymentStatus = "unpaid"
	}
	if in.DeliveryType == "" {
		in.DeliveryType = "pickup"
	}

	if in.ID == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM clients WHERE id = $1 AND workspace_id = $2)`,
			in.ClientID, workspaceID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO orders (
				workspace_id, client_id, status, cake_description, filling, decor_inscription,
				weight_kg, price, prepayment, payment_status, delivery_type, delivery_address,
				delivery_date, remind_before_min, created_by, completed_at
			) VALUES ($1,$2,$3::text,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
				CASE WHEN $3::text IN ('completed','cancelled') THEN NOW() ELSE NULL END)
			RETURNING id`,
			workspaceID, in.ClientID, in.Status, in.CakeDescription, in.Filling, in.DecorInscription,
			in.WeightKg, in.Price, in.Prepayment, in.PaymentStatus, in.DeliveryType, in.DeliveryAddress,
			in.DeliveryDate, in.RemindBeforeMin, userID).Scan(&id)
		if err != nil {
			return nil, err
		}
		return s.GetOrder(ctx, workspaceID, id)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			status = $3::text,
			cake_description = $4,
			filling = $5,
			decor_inscription = $6,
			weight_kg = $7,
			price = $8,
			prepayment = $9,
			payment_status = $10,
			delivery_type = $11,
			delivery_address = $12,
			reminded_at = CASE
				WHEN delivery_date IS DISTINCT FROM $13::timestamptz OR remind_before_min IS DISTINCT FROM $14::int THEN NULL
				ELSE reminded_at END,
			delivery_date = $13::timestamptz,
			remind_before_min = $14::int,
			completed_at = CASE WHEN $3::text IN ('completed','cancelled') THEN COALESCE(completed_at, NOW()) ELSE NULL END,
			updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2`,
		workspaceID, in.ID, in.Status, in.CakeDescription, in.Filling, in.DecorInscription,
		in.WeightKg, in.Price, in.Prepayment, in.PaymentStatus, in.DeliveryType, in.DeliveryAddress,
		in.DeliveryDate, in.RemindBeforeMin)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetOrder(ctx, workspaceID, in.ID)
}

func (s *Store) UpdateOrderStatus(ctx context.Context, workspaceID, orderID int64, status string) (*Order, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET
			status = $3::text,
			completed_at = CASE WHEN $3::text IN ('completed','cancelled') THEN COALESCE(completed_at, NOW()) ELSE NULL END,
			updated_at = NOW()
		WHERE workspace_id = $1 AND id = $2`, workspaceID, orderID, status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetOrder(ctx, workspaceID, orderID)
}

func (s *Store) DeleteOrder(ctx context.Context, workspaceID, orderID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM orders WHERE workspace_id = $1 AND id = $2`, workspaceID, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddOrderPhoto(ctx context.Context, workspaceID, orderID int64, kind, s3Key, url, caption string) (*OrderPhoto, error) {
	if kind != "result" {
		kind = "reference"
	}
	var p OrderPhoto
	err := s.pool.QueryRow(ctx, `
		INSERT INTO order_photos (order_id, kind, s3_key, url, caption)
		SELECT o.id, $3, $4, $5, $6 FROM orders o WHERE o.id = $2 AND o.workspace_id = $1
		RETURNING id, order_id, kind, s3_key, url, caption, created_at`,
		workspaceID, orderID, kind, s3Key, url, caption).Scan(
		&p.ID, &p.OrderID, &p.Kind, &p.S3Key, &p.URL, &p.Caption, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) DeleteOrderPhoto(ctx context.Context, workspaceID, orderID, photoID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM order_photos p USING orders o
		WHERE p.id = $3 AND p.order_id = o.id AND o.id = $2 AND o.workspace_id = $1`,
		workspaceID, orderID, photoID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Reminders ----------

// ClaimDueReminders marks and returns orders whose reminder time has come.
func (s *Store) ClaimDueReminders(ctx context.Context) ([]ReminderItem, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			UPDATE orders o SET reminded_at = NOW()
			WHERE o.reminded_at IS NULL
			  AND o.remind_before_min > 0
			  AND o.status NOT IN ('completed', 'cancelled')
			  AND o.delivery_date IS NOT NULL
			  AND o.delivery_date - (o.remind_before_min * INTERVAL '1 minute') <= NOW()
			  AND o.delivery_date > NOW() - INTERVAL '2 hours'
			RETURNING o.id, o.workspace_id, o.client_id, o.cake_description, o.weight_kg,
			          o.delivery_date, o.delivery_type, o.delivery_address
		)
		SELECT d.id, d.workspace_id, c.name, d.cake_description, d.weight_kg,
		       d.delivery_date, d.delivery_type, d.delivery_address
		FROM due d JOIN clients c ON c.id = d.client_id
		ORDER BY d.delivery_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ReminderItem
	for rows.Next() {
		var it ReminderItem
		if err := rows.Scan(&it.OrderID, &it.WorkspaceID, &it.ClientName, &it.CakeDescription,
			&it.WeightKg, &it.DeliveryDate, &it.DeliveryType, &it.DeliveryAddress); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ---------- Stats ----------

func (s *Store) Stats(ctx context.Context, workspaceID int64, from, to time.Time) (*Stats, error) {
	st := &Stats{TopClients: []TopClient{}}

	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(price), 0), COALESCE(SUM(weight_kg), 0)
		FROM orders
		WHERE workspace_id = $1 AND status = 'completed'
		  AND COALESCE(delivery_date, completed_at) >= $2 AND COALESCE(delivery_date, completed_at) < $3`,
		workspaceID, from, to).Scan(&st.OrdersCompleted, &st.Revenue, &st.WeightKg)
	if err != nil {
		return nil, err
	}
	if st.OrdersCompleted > 0 {
		st.AvgCheck = st.Revenue / float64(st.OrdersCompleted)
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM clients WHERE workspace_id = $1 AND created_at >= $2 AND created_at < $3`,
		workspaceID, from, to).Scan(&st.NewClients); err != nil {
		return nil, err
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(GREATEST(price - prepayment, 0)) FILTER (WHERE payment_status <> 'paid'), 0)
		FROM orders
		WHERE workspace_id = $1 AND status NOT IN ('completed', 'cancelled')`,
		workspaceID).Scan(&st.ActiveOrders, &st.Outstanding); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, COUNT(*), COALESCE(SUM(o.price), 0)
		FROM orders o JOIN clients c ON c.id = o.client_id
		WHERE o.workspace_id = $1 AND o.status = 'completed'
		  AND COALESCE(o.delivery_date, o.completed_at) >= $2 AND COALESCE(o.delivery_date, o.completed_at) < $3
		GROUP BY c.id, c.name
		ORDER BY SUM(o.price) DESC, COUNT(*) DESC
		LIMIT 5`, workspaceID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t TopClient
		if err := rows.Scan(&t.ClientID, &t.Name, &t.Orders, &t.Revenue); err != nil {
			return nil, err
		}
		st.TopClients = append(st.TopClients, t)
	}
	return st, rows.Err()
}
