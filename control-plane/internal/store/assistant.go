package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LLMProvider is a language model someone added with their own API key.
// The key stays on the server: the API never returns it.
type LLMProvider struct {
	ID        string
	Name      string
	Kind      string
	Preset    string
	BaseURL   string
	APIKey    string
	Model     string
	Default   bool
	CreatedAt time.Time
}

const providerColumns = `id, name, kind, preset, base_url, api_key, model, is_default, created_at`

func scanProvider(row pgx.Row) (LLMProvider, error) {
	var p LLMProvider
	err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.Preset, &p.BaseURL, &p.APIKey, &p.Model, &p.Default, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) LLMProviders(ctx context.Context) ([]LLMProvider, error) {
	rows, err := s.db.Query(ctx, `select `+providerColumns+` from llm_providers order by created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMProvider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) LLMProvider(ctx context.Context, id string) (LLMProvider, error) {
	return scanProvider(s.db.QueryRow(ctx, `select `+providerColumns+` from llm_providers where id::text = $1`, id))
}

func (s *Store) DefaultLLMProvider(ctx context.Context) (LLMProvider, error) {
	return scanProvider(s.db.QueryRow(ctx, `select `+providerColumns+` from llm_providers where is_default`))
}

// CreateLLMProvider saves a provider; the first one becomes the default.
func (s *Store) CreateLLMProvider(ctx context.Context, p LLMProvider) (LLMProvider, error) {
	return scanProvider(s.db.QueryRow(ctx, `
		insert into llm_providers (name, kind, preset, base_url, api_key, model, is_default)
		values ($1, $2, $3, $4, $5, $6, not exists (select 1 from llm_providers where is_default))
		returning `+providerColumns,
		p.Name, p.Kind, p.Preset, p.BaseURL, p.APIKey, p.Model))
}

// UpdateLLMProvider changes the name and model; a new key replaces the old
// one only when given.
func (s *Store) UpdateLLMProvider(ctx context.Context, id, name, model, apiKey string) error {
	tag, err := s.db.Exec(ctx, `
		update llm_providers set name = $2, model = $3, api_key = case when $4 = '' then api_key else $4 end
		where id::text = $1`, id, name, model, apiKey)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) SetDefaultLLMProvider(ctx context.Context, id string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `update llm_providers set is_default = false where is_default`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update llm_providers set is_default = true where id::text = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// DeleteLLMProvider removes a provider; when it was the default, the oldest
// remaining one takes over.
func (s *Store) DeleteLLMProvider(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `delete from llm_providers where id::text = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = s.db.Exec(ctx, `
		update llm_providers set is_default = true
		where id = (select id from llm_providers order by created_at limit 1)
		  and not exists (select 1 from llm_providers where is_default)`)
	return err
}

func (s *Store) Setting(ctx context.Context, key, fallback string) (string, error) {
	var v string
	err := s.db.QueryRow(ctx, `select value from settings where key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return fallback, nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.Exec(ctx, `insert into settings (key, value) values ($1, $2)
		on conflict (key) do update set value = excluded.value`, key, value)
	return err
}

// Chat is a conversation with the assistant. Transcript is what the
// dashboard shows; State is the model-specific history and any tool calls
// waiting for approval. Both are JSON the api package owns.
type Chat struct {
	ID           string
	Title        string
	ProviderID   *string
	AutomationID *string
	Status       string
	Transcript   json.RawMessage
	State        json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const chatColumns = `id, title, provider_id::text, automation_id::text, status, transcript, state, created_at, updated_at`

func scanChat(row pgx.Row) (Chat, error) {
	var c Chat
	err := row.Scan(&c.ID, &c.Title, &c.ProviderID, &c.AutomationID, &c.Status, &c.Transcript, &c.State, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Chats lists recent conversations without their contents.
func (s *Store) Chats(ctx context.Context, limit int) ([]Chat, error) {
	rows, err := s.db.Query(ctx, `
		select id, title, provider_id::text, automation_id::text, status, '[]'::jsonb, '{}'::jsonb, created_at, updated_at
		from assistant_chats order by updated_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Chat(ctx context.Context, id string) (Chat, error) {
	return scanChat(s.db.QueryRow(ctx, `select `+chatColumns+` from assistant_chats where id::text = $1`, id))
}

func (s *Store) CreateChat(ctx context.Context, c Chat) (Chat, error) {
	return scanChat(s.db.QueryRow(ctx, `
		insert into assistant_chats (title, provider_id, automation_id, status, transcript, state)
		values ($1, $2::uuid, $3::uuid, $4, $5, $6) returning `+chatColumns,
		c.Title, c.ProviderID, c.AutomationID, c.Status, c.Transcript, c.State))
}

func (s *Store) SaveChat(ctx context.Context, c Chat) error {
	_, err := s.db.Exec(ctx, `
		update assistant_chats set status = $2, transcript = $3, state = $4, updated_at = now()
		where id::text = $1`, c.ID, c.Status, c.Transcript, c.State)
	return err
}

func (s *Store) RenameChat(ctx context.Context, id, title string) error {
	_, err := s.db.Exec(ctx, `update assistant_chats set title = $2 where id::text = $1`, id, title)
	return err
}

func (s *Store) DeleteChat(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `delete from assistant_chats where id::text = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// FailRunningChats marks conversations a restart interrupted.
func (s *Store) FailRunningChats(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `update assistant_chats set status = 'failed' where status = 'running'`)
	return err
}

// Automation runs the assistant on a prompt every hour, day or week.
type Automation struct {
	ID         string
	Name       string
	Prompt     string
	ProviderID *string
	Every      string
	Enabled    bool
	NextRunAt  time.Time
	LastRunAt  *time.Time
	LastChatID *string
	CreatedAt  time.Time
}

const automationColumns = `id, name, prompt, provider_id::text, every, enabled, next_run_at, last_run_at, last_chat_id::text, created_at`

func scanAutomation(row pgx.Row) (Automation, error) {
	var a Automation
	err := row.Scan(&a.ID, &a.Name, &a.Prompt, &a.ProviderID, &a.Every, &a.Enabled, &a.NextRunAt, &a.LastRunAt, &a.LastChatID, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) automations(ctx context.Context, where string, args ...any) ([]Automation, error) {
	rows, err := s.db.Query(ctx, `select `+automationColumns+` from automations `+where+` order by created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Automations(ctx context.Context) ([]Automation, error) {
	return s.automations(ctx, "")
}

// DueAutomations are enabled automations whose next run has come.
func (s *Store) DueAutomations(ctx context.Context) ([]Automation, error) {
	return s.automations(ctx, "where enabled and next_run_at <= now()")
}

func (s *Store) Automation(ctx context.Context, id string) (Automation, error) {
	return scanAutomation(s.db.QueryRow(ctx, `select `+automationColumns+` from automations where id::text = $1`, id))
}

func (s *Store) CreateAutomation(ctx context.Context, a Automation) (Automation, error) {
	return scanAutomation(s.db.QueryRow(ctx, `
		insert into automations (name, prompt, provider_id, every, enabled, next_run_at)
		values ($1, $2, $3::uuid, $4, $5, $6) returning `+automationColumns,
		a.Name, a.Prompt, a.ProviderID, a.Every, a.Enabled, a.NextRunAt))
}

func (s *Store) UpdateAutomation(ctx context.Context, a Automation) error {
	tag, err := s.db.Exec(ctx, `
		update automations set name = $2, prompt = $3, provider_id = $4::uuid, every = $5, enabled = $6, next_run_at = $7
		where id::text = $1`, a.ID, a.Name, a.Prompt, a.ProviderID, a.Every, a.Enabled, a.NextRunAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// AutomationRan records a run and when the next one is due.
func (s *Store) AutomationRan(ctx context.Context, id, chatID string, next time.Time) error {
	_, err := s.db.Exec(ctx, `
		update automations set last_run_at = now(), last_chat_id = nullif($2, '')::uuid, next_run_at = $3 where id::text = $1`,
		id, chatID, next)
	return err
}

func (s *Store) DeleteAutomation(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `delete from automations where id::text = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
