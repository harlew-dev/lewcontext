package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"slices"
	"strings"
	"time"
)

// Alert webhooks and heartbeats (docs/api/monitoring.md): receivers an
// operator manages in the dashboard. A webhook gets the signals and event
// kinds it was set to, signed with a key of its own; a heartbeat is GET by
// the alert leader after each good evaluation. URLs and keys are sealed with
// APP_KEY and never leave through a list, an event, a log or an error.

// Limits on how many receivers there are, and how often one is posted to.
const (
	MaxAlertWebhooks     = 20
	MaxAlertHeartbeats   = 5
	MaxWebhookPostsHour  = 12 // per signal, per webhook
	AlertKeyOverlap      = 15 * time.Minute
	alertWebhookNameMax  = 80
	alertWebhookSignals  = 200
	alertWebhookKindHook = "webhook"
	alertWebhookKindBeat = "heartbeat"
)

var (
	// ErrAlertWebhookConflict: the receiver changed since it was read (a stale
	// revision), or a key rotation is still overlapping.
	ErrAlertWebhookConflict = errors.New("this receiver changed; reload before saving")
	// ErrAlertWebhookName: another receiver already has the name.
	ErrAlertWebhookName = errors.New("a receiver with that name exists")
	// ErrAlertWebhookLimit: the most webhooks or heartbeats exist.
	ErrAlertWebhookLimit = errors.New("too many receivers")
	// ErrAlertWebhookInvalid: the input breaks a rule the HTTP layer should have caught.
	ErrAlertWebhookInvalid = errors.New("check the receiver")
)

type alertWebhookRecord struct {
	bun.BaseModel    `bun:"table:alert_webhooks,alias:whk"`
	ID               int64  `bun:",pk,autoincrement"`
	PublicID         string `bun:",nullzero"`
	Kind             string
	Name             string
	SealedURL        []byte
	Format           string
	SealedKey        []byte `bun:",nullzero"`
	KeyID            string
	SealedOverlapKey []byte `bun:",nullzero"`
	OverlapKeyID     string
	OverlapUntil     *time.Time
	Signals          []string `bun:",array"`
	Events           []string `bun:",array"`
	Enabled          bool
	DeliveryRevision int64
	Revision         int64
	PostTimes        json.RawMessage `bun:"post_times,type:jsonb,nullzero,default:'{}'"`
	CreatedBy        *int64
	UpdatedBy        *int64
	CreatedAt        time.Time `bun:",nullzero,default:current_timestamp"`
	UpdatedAt        time.Time `bun:",nullzero,default:current_timestamp"`
}

// AlertWebhookDelivery is the outcome of a receiver's latest delivery.
type AlertWebhookDelivery struct {
	Status string     `json:"status"` // delivered, failed or pending
	At     *time.Time `json:"at"`
}

// AlertWebhook is a receiver as the dashboard reads it: never a model, never
// its URL, host or key.
type AlertWebhook struct {
	ID           string                `json:"id"`
	Kind         string                `json:"kind"`
	Name         string                `json:"name"`
	Format       string                `json:"format"`
	Signals      []string              `json:"signals"`
	Events       []string              `json:"events"`
	Enabled      bool                  `json:"enabled"`
	Revision     int64                 `json:"revision"`
	KeyID        string                `json:"keyId"`
	OverlapUntil *time.Time            `json:"overlapUntil"`
	CreatedAt    time.Time             `json:"createdAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	LastDelivery *AlertWebhookDelivery `json:"lastDelivery"`
}

// AlertDestination is transient plaintext for one bounded network operation.
// Never marshal, log or return it.
type AlertDestination struct {
	ID           string
	URL          string
	Format       string
	Key          []byte
	KeyID        string
	OverlapKey   []byte
	OverlapKeyID string
	OverlapUntil *time.Time
}

// AlertWebhookInput is a create or an edit: a nil field is left as it is.
type AlertWebhookInput struct {
	Kind          string // creates only
	Revision      int64  // edits only
	Name          *string
	URL           *string
	Format        *string
	Signals       *[]string
	Events        *[]string
	Enabled       *bool
	RevokeOverlap bool
}

func webhookOf(r alertWebhookRecord, now time.Time) AlertWebhook {
	out := AlertWebhook{ID: r.PublicID, Kind: r.Kind, Name: r.Name, Format: r.Format, Signals: nonNil(r.Signals), Events: nonNil(r.Events),
		Enabled: r.Enabled, Revision: r.Revision, KeyID: r.KeyID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if r.OverlapUntil != nil && now.Before(*r.OverlapUntil) {
		out.OverlapUntil = r.OverlapUntil
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// IndexAlertWebhooks lists every receiver, webhooks first, by name, each with
// its latest delivery.
func (s *Store) IndexAlertWebhooks(ctx context.Context) ([]AlertWebhook, error) {
	var rows []alertWebhookRecord
	if err := s.DB.NewSelect().Model(&rows).OrderExpr("whk.kind DESC, lower(whk.name), whk.id").Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]AlertWebhook, len(rows))
	ids := make([]string, len(rows))
	for i, r := range rows {
		out[i], ids[i] = webhookOf(r, time.Now()), r.PublicID
	}
	if len(ids) == 0 {
		return out, nil
	}
	var last []deliveryRecord
	err := s.DB.NewSelect().Model(&last).DistinctOn("d.target").
		Where("d.target IN (?)", bun.In(ids)).OrderExpr("d.target, d.event_id DESC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	byTarget := map[string]AlertWebhookDelivery{}
	for _, d := range last {
		v := AlertWebhookDelivery{Status: "pending"}
		switch {
		case !d.DeliveredAt.IsZero():
			v.Status, v.At = "delivered", &d.DeliveredAt
		case !d.FailedAt.IsZero():
			v.Status, v.At = "failed", &d.FailedAt
		}
		byTarget[d.Target] = v
	}
	for i := range out {
		if v, ok := byTarget[out[i].ID]; ok {
			out[i].LastDelivery = &v
		}
	}
	return out, nil
}

// FindAlertWebhook finds one receiver; sql.ErrNoRows when there is none.
func (s *Store) FindAlertWebhook(ctx context.Context, publicID string) (AlertWebhook, error) {
	var r alertWebhookRecord
	if err := s.DB.NewSelect().Model(&r).Where("whk.public_id = ?", publicID).Scan(ctx); err != nil {
		return AlertWebhook{}, err
	}
	return webhookOf(r, time.Now()), nil
}

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newKeyID is a public, non-secret label for a signing key: key_ and 8 base62
// characters, so a receiver holding several keys knows which one signed.
func newKeyID() (string, error) {
	out := make([]byte, 0, 8)
	buf := make([]byte, 16)
	for len(out) < 8 {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < 248 && len(out) < 8 {
				out = append(out, base62[int(b)%62])
			}
		}
	}
	return "key_" + string(out), nil
}

// newSigningKey is a fresh random 32-byte key, encoded for the receiver, and
// its public ID.
func newSigningKey() (key []byte, keyID string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return nil, "", err
	}
	keyID, err = newKeyID()
	return []byte(base64.RawURLEncoding.EncodeToString(raw)), keyID, err
}

func uniqueName(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func checkWebhookInput(kind string, name string, signals, eventKinds []string, format string) error {
	if kind != alertWebhookKindHook && kind != alertWebhookKindBeat {
		return ErrAlertWebhookInvalid
	}
	if strings.TrimSpace(name) == "" || len([]rune(name)) > alertWebhookNameMax || len(signals) > alertWebhookSignals {
		return ErrAlertWebhookInvalid
	}
	if kind == alertWebhookKindBeat {
		if len(signals) > 0 || len(eventKinds) > 0 || format != "" {
			return ErrAlertWebhookInvalid
		}
		return nil
	}
	if format != "json" && format != "slack" {
		return ErrAlertWebhookInvalid
	}
	for _, e := range eventKinds {
		if !slices.Contains(events.AlertWebhookKinds, e) {
			return ErrAlertWebhookInvalid
		}
	}
	return nil
}

// CreateAlertWebhook adds a webhook or heartbeat, sealing its URL (and a
// webhook's new signing key) to its own identity, recording
// admin.alert_webhook.created. Validated by the caller: the URL's shape, the
// signals against the catalogue.
func (s *Store) CreateAlertWebhook(ctx context.Context, in AlertWebhookInput, sealer *auth.DestinationSealer, a AdminAction) (AlertWebhook, error) {
	name, url := derefString(in.Name), derefString(in.URL)
	signals, kinds := derefList(in.Signals), derefList(in.Events)
	format := derefString(in.Format)
	if err := checkWebhookInput(in.Kind, name, signals, kinds, format); err != nil || url == "" {
		return AlertWebhook{}, ErrAlertWebhookInvalid
	}
	var out AlertWebhook
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		max := MaxAlertWebhooks
		if in.Kind == alertWebhookKindBeat {
			max = MaxAlertHeartbeats
		}
		n, err := tx.NewSelect().Model((*alertWebhookRecord)(nil)).Where("kind = ?", in.Kind).Count(ctx)
		if err != nil {
			return err
		}
		if int(n) >= max {
			return ErrAlertWebhookLimit
		}
		enabled := in.Enabled == nil || *in.Enabled
		row := alertWebhookRecord{Kind: in.Kind, Name: strings.TrimSpace(name), Format: format, Signals: signals, Events: kinds, Enabled: enabled,
			DeliveryRevision: 1, Revision: 1, CreatedBy: &a.Admin.ID, UpdatedBy: &a.Admin.ID, SealedURL: []byte{0}}
		var key []byte
		if in.Kind == alertWebhookKindHook {
			key, row.KeyID, err = newSigningKey()
			if err != nil {
				return err
			}
			row.SealedKey = []byte{0}
		}
		// The row's public ID is a database default, so insert, then seal to it.
		if _, err := tx.NewInsert().Model(&row).Returning("*").Exec(ctx); err != nil {
			if uniqueName(err) {
				return ErrAlertWebhookName
			}
			return err
		}
		if row.SealedURL, err = sealer.Seal([]byte(url), row.Kind, row.PublicID, "url"); err != nil {
			return err
		}
		cols := []string{"sealed_url"}
		if key != nil {
			if row.SealedKey, err = sealer.Seal(key, row.Kind, row.PublicID, "key"); err != nil {
				return err
			}
			cols = append(cols, "sealed_key")
		}
		if _, err := tx.NewUpdate().Model(&row).Column(cols...).WherePK().Exec(ctx); err != nil {
			return err
		}
		out = webhookOf(row, time.Now())
		return s.record(ctx, tx, events.AdminAlertWebhookCreated{Admin: a.Admin.PublicID, Webhook: row.PublicID, Kind: row.Kind, Name: row.Name, Reason: a.Reason}, EventScope{ActorID: a.Admin.ID})
	})
	return out, err
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefList(p *[]string) []string {
	if p == nil {
		return []string{}
	}
	out := append([]string{}, *p...)
	slices.Sort(out)
	return slices.Compact(out)
}

// UpdateAlertWebhook edits a receiver under optimistic concurrency. Replacing
// the URL gives a webhook a new key (with no overlap to the old receiver) and,
// like a new format, makes its queued deliveries skip: they are never sent to
// a changed receiver. Renaming it or changing its signals, events or enabled
// flag does not cancel anything already queued. sql.ErrNoRows when unknown.
func (s *Store) UpdateAlertWebhook(ctx context.Context, publicID string, in AlertWebhookInput, sealer *auth.DestinationSealer, a AdminAction) (AlertWebhook, error) {
	var out AlertWebhook
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row alertWebhookRecord
		if err := tx.NewSelect().Model(&row).Where("whk.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if row.Revision != in.Revision {
			return ErrAlertWebhookConflict
		}
		fields := []string{}
		cols := []string{}
		if in.Name != nil && strings.TrimSpace(*in.Name) != row.Name {
			row.Name = strings.TrimSpace(*in.Name)
			fields, cols = append(fields, "name"), append(cols, "name")
		}
		if in.Signals != nil {
			if v := derefList(in.Signals); !slices.Equal(v, row.Signals) {
				row.Signals = v
				fields, cols = append(fields, "signals"), append(cols, "signals")
			}
		}
		if in.Events != nil {
			if v := derefList(in.Events); !slices.Equal(v, row.Events) {
				row.Events = v
				fields, cols = append(fields, "events"), append(cols, "events")
			}
		}
		if in.Enabled != nil && *in.Enabled != row.Enabled {
			row.Enabled = *in.Enabled
			fields, cols = append(fields, "enabled"), append(cols, "enabled")
		}
		if in.Format != nil && *in.Format != row.Format {
			row.Format, row.DeliveryRevision = *in.Format, row.DeliveryRevision+1
			fields, cols = append(fields, "format"), append(cols, "format", "delivery_revision")
		}
		if in.RevokeOverlap && row.SealedOverlapKey != nil {
			row.SealedOverlapKey, row.OverlapKeyID, row.OverlapUntil = nil, "", nil
			fields, cols = append(fields, "overlap"), append(cols, "sealed_overlap_key", "overlap_key_id", "overlap_until")
		}
		if in.URL != nil {
			var err error
			if row.SealedURL, err = sealer.Seal([]byte(*in.URL), row.Kind, row.PublicID, "url"); err != nil {
				return err
			}
			fields, cols = append(fields, "url"), append(cols, "sealed_url")
			if !slices.Contains(cols, "delivery_revision") {
				row.DeliveryRevision++
				cols = append(cols, "delivery_revision")
			}
			if row.Kind == alertWebhookKindHook {
				key, id, err := newSigningKey()
				if err != nil {
					return err
				}
				if row.SealedKey, err = sealer.Seal(key, row.Kind, row.PublicID, "key"); err != nil {
					return err
				}
				row.KeyID = id
				row.SealedOverlapKey, row.OverlapKeyID, row.OverlapUntil = nil, "", nil
				fields = append(fields, "key")
				cols = append(cols, "sealed_key", "key_id", "sealed_overlap_key", "overlap_key_id", "overlap_until")
			}
		}
		if row.Kind == alertWebhookKindBeat && (len(row.Signals) > 0 || len(row.Events) > 0 || row.Format != "") {
			return ErrAlertWebhookInvalid
		}
		if len(fields) == 0 {
			out = webhookOf(row, time.Now())
			return nil
		}
		row.Revision++
		row.UpdatedBy = &a.Admin.ID
		cols = append(cols, "revision", "updated_by")
		if _, err := tx.NewUpdate().Model(&row).Column(cols...).Set("updated_at = now()").WherePK().Exec(ctx); err != nil {
			if uniqueName(err) {
				return ErrAlertWebhookName
			}
			return err
		}
		out = webhookOf(row, time.Now())
		return s.record(ctx, tx, events.AdminAlertWebhookUpdated{Admin: a.Admin.PublicID, Webhook: row.PublicID, Operation: "update", Fields: fields, Revision: int(row.Revision), Reason: a.Reason}, EventScope{ActorID: a.Admin.ID})
	})
	return out, err
}

// UpdateAlertWebhookKey rotates a webhook's key: a new key signs from now,
// and for 15 minutes each post carries a second signature made with the
// old one. A rotation during an overlap is ErrAlertWebhookConflict.
func (s *Store) UpdateAlertWebhookKey(ctx context.Context, publicID string, revision int64, sealer *auth.DestinationSealer, a AdminAction) (AlertWebhook, error) {
	var out AlertWebhook
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row alertWebhookRecord
		if err := tx.NewSelect().Model(&row).Where("whk.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if row.Kind != alertWebhookKindHook {
			return ErrAlertWebhookInvalid
		}
		if row.Revision != revision || (row.OverlapUntil != nil && time.Now().Before(*row.OverlapUntil)) {
			return ErrAlertWebhookConflict
		}
		old, err := sealer.Open(row.SealedKey, row.Kind, row.PublicID, "key")
		if err != nil {
			return err
		}
		key, id, err := newSigningKey()
		if err != nil {
			return err
		}
		if row.SealedOverlapKey, err = sealer.Seal(old, row.Kind, row.PublicID, "overlap"); err != nil {
			return err
		}
		until := time.Now().Add(AlertKeyOverlap)
		row.OverlapKeyID, row.OverlapUntil = row.KeyID, &until
		if row.SealedKey, err = sealer.Seal(key, row.Kind, row.PublicID, "key"); err != nil {
			return err
		}
		row.KeyID = id
		row.Revision++
		row.UpdatedBy = &a.Admin.ID
		if _, err := tx.NewUpdate().Model(&row).Column("sealed_key", "key_id", "sealed_overlap_key", "overlap_key_id", "overlap_until", "revision", "updated_by").Set("updated_at = now()").WherePK().Exec(ctx); err != nil {
			return err
		}
		out = webhookOf(row, time.Now())
		return s.record(ctx, tx, events.AdminAlertWebhookUpdated{Admin: a.Admin.PublicID, Webhook: row.PublicID, Operation: "rotate_key", Fields: []string{"key"}, Revision: int(row.Revision), Reason: a.Reason}, EventScope{ActorID: a.Admin.ID})
	})
	return out, err
}

// FindAlertWebhookSigningKey opens a webhook's current key and its public ID
// for an operator to copy. sql.ErrNoRows when unknown or a heartbeat.
func (s *Store) FindAlertWebhookSigningKey(ctx context.Context, publicID string, sealer *auth.DestinationSealer) (key, keyID string, err error) {
	var row alertWebhookRecord
	if err = s.DB.NewSelect().Model(&row).Where("whk.public_id = ? AND whk.kind = ?", publicID, alertWebhookKindHook).Scan(ctx); err != nil {
		return "", "", err
	}
	plain, err := sealer.Open(row.SealedKey, row.Kind, row.PublicID, "key")
	return string(plain), row.KeyID, err
}

// DeleteAlertWebhook removes a receiver, recording
// admin.alert_webhook.deleted. Its queued deliveries find no receiver and
// skip. sql.ErrNoRows when unknown.
func (s *Store) DeleteAlertWebhook(ctx context.Context, publicID string, a AdminAction) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var row alertWebhookRecord
		if err := tx.NewSelect().Model(&row).Where("whk.public_id = ?", publicID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model(&row).WherePK().Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.AdminAlertWebhookDeleted{Admin: a.Admin.PublicID, Webhook: row.PublicID, Kind: row.Kind, Reason: a.Reason}, EventScope{ActorID: a.Admin.ID})
	})
}

func openDestination(sealer *auth.DestinationSealer, r alertWebhookRecord, now time.Time) (AlertDestination, error) {
	out := AlertDestination{ID: r.PublicID, Format: r.Format, KeyID: r.KeyID}
	plain, err := sealer.Open(r.SealedURL, r.Kind, r.PublicID, "url")
	if err != nil {
		return AlertDestination{}, err
	}
	out.URL = string(plain)
	if len(r.SealedKey) > 0 {
		if out.Key, err = sealer.Open(r.SealedKey, r.Kind, r.PublicID, "key"); err != nil {
			return AlertDestination{}, err
		}
	}
	// An expired overlap key is never used, even before it is erased.
	if len(r.SealedOverlapKey) > 0 && r.OverlapUntil != nil && now.Before(*r.OverlapUntil) {
		if out.OverlapKey, err = sealer.Open(r.SealedOverlapKey, r.Kind, r.PublicID, "overlap"); err != nil {
			return AlertDestination{}, err
		}
		out.OverlapKeyID, out.OverlapUntil = r.OverlapKeyID, r.OverlapUntil
	}
	return out, nil
}

// FindAlertWebhookTarget opens the webhook a delivery was queued for, just
// before it is sent. false when it was deleted or disabled since, or its URL,
// format or key was replaced after the delivery was queued: a completed
// no-send, never retargeted to the new receiver.
func (s *Store) FindAlertWebhookTarget(ctx context.Context, publicID string, revision int64, sealer *auth.DestinationSealer) (AlertDestination, bool, error) {
	var row alertWebhookRecord
	err := s.DB.NewSelect().Model(&row).Where("whk.public_id = ? AND whk.kind = ?", publicID, alertWebhookKindHook).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertDestination{}, false, nil
	}
	if err != nil || !row.Enabled || row.DeliveryRevision != revision {
		return AlertDestination{}, false, err
	}
	d, err := openDestination(sealer, row, time.Now())
	return d, err == nil, err
}

// IndexAlertHeartbeats opens every enabled heartbeat, for the leader to ping
// after an evaluation that worked.
func (s *Store) IndexAlertHeartbeats(ctx context.Context, sealer *auth.DestinationSealer) ([]AlertDestination, error) {
	var rows []alertWebhookRecord
	if err := s.DB.NewSelect().Model(&rows).Where("whk.kind = ? AND whk.enabled", alertWebhookKindBeat).OrderExpr("whk.id").Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]AlertDestination, 0, len(rows))
	for _, r := range rows {
		d, err := openDestination(sealer, r, time.Now())
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// queueAlertWebhooks queues one delivery of an alert event for each enabled
// webhook that chose its signal and kind, within each webhook's cap of 12
// posts an hour per signal. An empty selection matches nothing. It runs in
// the transition's transaction, under the alert write lock.
func (s *Store) queueAlertWebhooks(ctx context.Context, db bun.IDB, eventID int64, listener, signal, kind string, at time.Time) error {
	var rows []alertWebhookRecord
	err := db.NewSelect().Model(&rows).
		Where("whk.kind = ? AND whk.enabled AND ? = ANY(whk.signals) AND ? = ANY(whk.events)", alertWebhookKindHook, signal, kind).
		OrderExpr("whk.id").For("UPDATE").Scan(ctx)
	if err != nil || len(rows) == 0 {
		return err
	}
	var queue []deliveryRecord
	for _, r := range rows {
		times := map[string][]string{}
		if len(r.PostTimes) > 0 {
			if err := json.Unmarshal(r.PostTimes, &times); err != nil {
				return err
			}
		}
		cutoff := at.Add(-time.Hour)
		for sig, list := range times {
			kept := list[:0]
			for _, t := range list {
				if p, err := time.Parse(time.RFC3339Nano, t); err == nil && p.After(cutoff) {
					kept = append(kept, t)
				}
			}
			if len(kept) == 0 {
				delete(times, sig)
			} else {
				times[sig] = kept
			}
		}
		if len(times[signal]) >= MaxWebhookPostsHour {
			continue
		}
		times[signal] = append(times[signal], at.UTC().Format(time.RFC3339Nano))
		raw, err := json.Marshal(times)
		if err != nil {
			return err
		}
		if _, err := db.NewUpdate().Model((*alertWebhookRecord)(nil)).Set("post_times = ?::jsonb", string(raw)).Where("id = ?", r.ID).Exec(ctx); err != nil {
			return err
		}
		queue = append(queue, deliveryRecord{EventID: eventID, Listener: listener, Target: r.PublicID, TargetRevision: r.DeliveryRevision})
	}
	if len(queue) == 0 {
		return nil
	}
	_, err = db.NewInsert().Model(&queue).Exec(ctx)
	return err
}
