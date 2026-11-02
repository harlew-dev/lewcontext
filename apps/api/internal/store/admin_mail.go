package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"time"
)

// The admin dashboard's Mail page: the delivery queue (event_deliveries),
// how much mail went out and to whom it was asked for. Every count is
// bounded by a window on the events' created_at, or reads the pending
// deliveries' partial index.

// Delivery statuses, as the dashboard shows and filters them.
const (
	DeliveryPending   = "pending"   // waiting, never failed
	DeliveryRetrying  = "retrying"  // waiting, failed at least once
	DeliveryFailed    = "failed"    // given up on
	DeliveryDelivered = "delivered" // done
)

// waiting is a delivery not yet done or given up on (the pending index's
// predicate).
const waiting = "d.delivered_at IS NULL AND d.failed_at IS NULL"

// deliveryStatuses is the status filter's fixed SQL.
var deliveryStatuses = map[string]string{
	DeliveryPending:   waiting + " AND d.last_error = ''",
	DeliveryRetrying:  waiting + " AND d.last_error <> ''",
	DeliveryFailed:    "d.failed_at IS NOT NULL",
	DeliveryDelivered: "d.delivered_at IS NOT NULL",
}

// DeliveryStatus reports whether status is one the list filters by.
func DeliveryStatus(status string) bool {
	_, ok := deliveryStatuses[status]
	return ok
}

// adminDelivery is a delivery as the dashboard shows it, with its status;
// the next attempt only while it's waiting.
func adminDelivery(r deliveryRecord) AdminDelivery {
	d := AdminDelivery{Listener: r.Listener, Attempts: r.Attempts, LastError: r.LastError}
	switch {
	case !r.DeliveredAt.IsZero():
		d.Status, d.DeliveredAt = DeliveryDelivered, &r.DeliveredAt
	case !r.FailedAt.IsZero():
		d.Status, d.FailedAt = DeliveryFailed, &r.FailedAt
	case r.LastError != "":
		d.Status, d.NextAttemptAt = DeliveryRetrying, &r.NextAttemptAt
	default:
		d.Status, d.NextAttemptAt = DeliveryPending, &r.NextAttemptAt
	}
	return d
}

// AdminDeliveryFilter narrows the deliveries list; zero values don't filter.
// Before is a cursor: the event's evt_ ID and the listener.
type AdminDeliveryFilter struct {
	Status         string
	Listener       string
	From, To       *time.Time
	BeforeEvent    string
	BeforeListener string
}

// AdminEventSummary names a delivery's event.
type AdminEventSummary struct {
	PublicID  string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// AdminDeliveryRow is a delivery as the list shows it. To is the payload's
// email, where the event has one.
type AdminDeliveryRow struct {
	Event AdminEventSummary `json:"event"`
	To    *string           `json:"to"`
	AdminDelivery
}

// AdminDeliveryPage is one page of deliveries and the cursor for the next
// (evt_…:listener, nil at the end).
type AdminDeliveryPage struct {
	Data []AdminDeliveryRow `json:"data"`
	Next *string            `json:"next"`
}

// deliveryOrder is newest event first, then listener; every key is unique
// together, so the cursor never repeats or skips a row.
const deliveryOrder = "e.created_at DESC, e.id DESC, d.listener DESC"

// IndexAdminDeliveries returns up to perPage deliveries matching f, and the
// cursor for the next page. ErrNoCursor when the cursor's event is unknown.
func (s *Store) IndexAdminDeliveries(ctx context.Context, f AdminDeliveryFilter, perPage int) (AdminDeliveryPage, error) {
	q := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("d.*").
		ColumnExpr("e.public_id AS event_public_id, e.name AS event_name, e.created_at AS event_created_at").
		ColumnExpr("CASE WHEN d.listener = ? THEN e.payload->>'previousEmail' ELSE e.payload->>'email' END AS email", "mail.email_changed_previous").
		Join("JOIN events AS e ON e.id = d.event_id").
		OrderExpr(deliveryOrder).
		Limit(int64(perPage + 1))
	if f.Status != "" {
		q = q.Where(deliveryStatuses[f.Status])
	}
	if f.Listener != "" {
		q = q.Where("d.listener = ?", f.Listener)
	}
	if f.From != nil {
		q = q.Where("e.created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("e.created_at < ?", *f.To)
	}
	if f.BeforeEvent != "" {
		var cursor eventRecord
		err := s.DB.NewSelect().Model(&cursor).Column("e.id", "e.created_at").Where("e.public_id = ?", f.BeforeEvent).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return AdminDeliveryPage{}, ErrNoCursor
		}
		if err != nil {
			return AdminDeliveryPage{}, err
		}
		q = q.Where("(e.created_at, e.id, d.listener) < (?, ?, ?)", cursor.CreatedAt, cursor.ID, f.BeforeListener)
	}
	var rows []struct {
		deliveryRecord `bun:",extend"`
		EventPublicID  string         `bun:"event_public_id"`
		EventName      string         `bun:"event_name"`
		EventCreatedAt time.Time      `bun:"event_created_at"`
		Email          sql.NullString `bun:"email"`
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return AdminDeliveryPage{}, err
	}
	page := AdminDeliveryPage{Data: []AdminDeliveryRow{}}
	if len(rows) > perPage {
		rows = rows[:perPage]
		last := rows[perPage-1]
		next := last.EventPublicID + ":" + last.Listener
		page.Next = &next
	}
	for _, r := range rows {
		row := AdminDeliveryRow{
			Event:         AdminEventSummary{PublicID: r.EventPublicID, Name: r.EventName, CreatedAt: r.EventCreatedAt},
			AdminDelivery: adminDelivery(r.deliveryRecord),
		}
		if r.Email.Valid {
			row.To = &r.Email.String
		}
		page.Data = append(page.Data, row)
	}
	return page, nil
}

// AdminMailSummary is the Mail page's summary for the window since Since.
type AdminMailSummary struct {
	Queue         []AdminQueue     `json:"queue"`
	Volume        []AdminVolume    `json:"volume"`
	Requests      []AdminRequests  `json:"requests"`
	TopRecipients []AdminRecipient `json:"topRecipients"`
	LastDelivered *time.Time       `json:"lastDeliveredAt"`
	LastFailure   *AdminFailure    `json:"lastFailure"`
}

// AdminQueue is one listener's queue: waiting deliveries now (due, retrying,
// and when the oldest waiting one's event happened, the worker's lag), and
// those failed and delivered of events in the window.
type AdminQueue struct {
	Listener        string     `bun:"listener" json:"listener"`
	Due             int        `bun:"due" json:"due"`
	Retrying        int        `bun:"retrying" json:"retrying"`
	OldestPendingAt *time.Time `bun:"oldest_pending_at" json:"oldestPendingAt"`
	Failed          int        `bun:"failed" json:"failed"`
	Delivered       int        `bun:"delivered" json:"delivered"`
}

// Mail kinds, for the volume per template and the requests.
const (
	MailPasswordReset        = "password_reset"
	MailInvitation           = "invitation"
	MailRegistrationCode     = "registration_code"
	MailAccountExists        = "account_exists"
	MailEmailVerification    = "email_verification"
	MailEmailChange          = "email_change"
	MailEmailChangeCode      = "email_change_code"
	MailEmailChangeExists    = "email_change_exists"
	MailEmailChangedPrevious = "email_changed_previous"
	MailEmailChangedCurrent  = "email_changed_current"
)

// AdminVolume is how many emails of Kind were sent in the hour starting at
// Hour.
type AdminVolume struct {
	Hour  time.Time `bun:"hour" json:"hour"`
	Kind  string    `bun:"kind" json:"kind"`
	Count int       `bun:"count" json:"count"`
}

// AdminRequests compares emails asked for with emails sent, for one kind of
// request: password resets, sign-ups or email verification.
type AdminRequests struct {
	Kind      string `json:"kind"`
	Requested int    `json:"requested"`
	Sent      int    `json:"sent"`
}

// AdminRecipient is an address asked for at least minRecipientRequests
// times in the window, with the requests per kind.
type AdminRecipient struct {
	Email         string `bun:"email" json:"email"`
	Total         int    `bun:"total" json:"total"`
	Resets        int    `bun:"resets" json:"resets"`
	Invitations   int    `bun:"invitations" json:"invitations"`
	Registrations int    `bun:"registrations" json:"registrations"`
	EmailChanges  int    `bun:"email_changes" json:"emailChanges"`
}

// AdminFailure is the newest delivery given up on in the window.
type AdminFailure struct {
	Listener string    `bun:"listener" json:"listener"`
	FailedAt time.Time `bun:"failed_at" json:"failedAt"`
	Error    string    `bun:"last_error" json:"error"`
}

// minRecipientRequests is how often an address must be asked for to be
// listed: enough to spot flooding one inbox, not a log of who got mail.
const minRecipientRequests = 3

// maxRecipients bounds the top recipients listed.
const maxRecipients = 20

// requestPairs are the events asking for an email and the events saying it
// went out, per kind.
var requestPairs = []struct {
	kind            string
	requested, sent events.Event
}{
	{MailPasswordReset, events.PasswordResetRequested{}, events.PasswordResetSent{}},
	{"registration", events.RegistrationRequested{}, events.RegistrationSent{}},
	{MailEmailVerification, events.EmailVerificationRequested{}, events.EmailVerificationSent{}},
	{MailEmailChange, events.EmailChangeRequested{}, events.EmailChangeSent{}},
}

// FindAdminMailSummary reads the Mail page's summary for the window since
// since, with a queue row for each of listeners (even an idle one).
func (s *Store) FindAdminMailSummary(ctx context.Context, since time.Time, listeners []string) (AdminMailSummary, error) {
	var m AdminMailSummary
	var err error
	if m.Queue, err = s.mailQueue(ctx, since, listeners); err != nil {
		return m, err
	}
	if m.Volume, err = s.mailVolume(ctx, since); err != nil {
		return m, err
	}
	if m.Requests, err = s.mailRequests(ctx, since); err != nil {
		return m, err
	}
	if m.TopRecipients, err = s.topRecipients(ctx, since); err != nil {
		return m, err
	}
	m.LastDelivered, m.LastFailure, err = s.lastDeliveries(ctx, since)
	return m, err
}

// mailQueue counts waiting deliveries through the pending index, and those
// finished for events in the window, per listener.
func (s *Store) mailQueue(ctx context.Context, since time.Time, listeners []string) ([]AdminQueue, error) {
	var waitingRows, windowRows []AdminQueue
	err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Column("d.listener").
		ColumnExpr("count(*) FILTER (WHERE d.next_attempt_at <= now()) AS due").
		ColumnExpr("count(*) FILTER (WHERE d.last_error <> '') AS retrying").
		ColumnExpr("min(e.created_at) AS oldest_pending_at").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where(waiting).
		Group("d.listener").
		Scan(ctx, &waitingRows)
	if err != nil {
		return nil, err
	}
	err = s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Column("d.listener").
		ColumnExpr("count(*) FILTER (WHERE d.failed_at IS NOT NULL) AS failed").
		ColumnExpr("count(*) FILTER (WHERE d.delivered_at IS NOT NULL) AS delivered").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("e.created_at >= ?", since).
		Group("d.listener").
		Scan(ctx, &windowRows)
	if err != nil {
		return nil, err
	}
	// The registered listeners first, then any that isn't any more (renamed)
	// but still has deliveries.
	queue := []AdminQueue{}
	index := map[string]int{}
	row := func(name string) *AdminQueue {
		i, ok := index[name]
		if !ok {
			i, index[name] = len(queue), len(queue)
			queue = append(queue, AdminQueue{Listener: name})
		}
		return &queue[i]
	}
	for _, name := range listeners {
		row(name)
	}
	for _, w := range waitingRows {
		q := row(w.Listener)
		q.Due, q.Retrying, q.OldestPendingAt = w.Due, w.Retrying, w.OldestPendingAt
	}
	for _, w := range windowRows {
		q := row(w.Listener)
		q.Failed, q.Delivered = w.Failed, w.Delivered
	}
	return queue, nil
}

// mailVolume counts the emails sent per hour and kind, from the listeners'
// sent events.
func (s *Store) mailVolume(ctx context.Context, since time.Time) ([]AdminVolume, error) {
	reset, invitation := events.PasswordResetSent{}.EventName(), events.WorkspaceInvitationSent{}.EventName()
	registration, verification := events.RegistrationSent{}.EventName(), events.EmailVerificationSent{}.EventName()
	change := events.EmailChangeSent{}.EventName()
	kind := `CASE e.name WHEN ? THEN ? WHEN ? THEN ? WHEN ? THEN ? ELSE
		CASE WHEN e.name = ? THEN CASE e.payload->>'kind' WHEN ? THEN ? WHEN ? THEN ?
		ELSE CASE WHEN (e.payload->>'existingAccount')::boolean THEN ? ELSE ? END END
		ELSE CASE WHEN (e.payload->>'existingAccount')::boolean THEN ? ELSE ? END END END`
	rows := []AdminVolume{}
	err := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("date_trunc('hour', e.created_at) AS hour").
		ColumnExpr(kind+" AS kind", reset, MailPasswordReset, invitation, MailInvitation, verification, MailEmailVerification, change,
			events.EmailChangeMailPrevious, MailEmailChangedPrevious, events.EmailChangeMailCurrent, MailEmailChangedCurrent,
			MailEmailChangeExists, MailEmailChangeCode, MailAccountExists, MailRegistrationCode).
		ColumnExpr("count(*) AS count").
		Where("e.name IN (?)", bun.In([]string{reset, invitation, registration, verification, change})).
		Where("e.created_at >= ?", since).
		GroupExpr("1, 2").
		OrderExpr("1, 2").
		Scan(ctx, &rows)
	return rows, err
}

// mailRequests compares requests with emails sent, per kind.
func (s *Store) mailRequests(ctx context.Context, since time.Time) ([]AdminRequests, error) {
	var names []string
	for _, p := range requestPairs {
		names = append(names, p.requested.EventName(), p.sent.EventName())
	}
	var counts []struct {
		Name  string `bun:"name"`
		Count int    `bun:"count"`
	}
	err := s.DB.NewSelect().Model((*eventRecord)(nil)).
		Column("e.name").
		ColumnExpr("count(*) AS count").
		Where("e.name IN (?)", bun.In(names)).
		Where("e.name <> ? OR coalesce(e.payload->>'kind', '') NOT IN (?)", events.EmailChangeSent{}.EventName(), bun.In([]string{events.EmailChangeMailPrevious, events.EmailChangeMailCurrent})).
		Where("e.created_at >= ?", since).
		Group("e.name").
		Scan(ctx, &counts)
	byName := map[string]int{}
	for _, c := range counts {
		byName[c.Name] = c.Count
	}
	out := make([]AdminRequests, len(requestPairs))
	for i, p := range requestPairs {
		out[i] = AdminRequests{Kind: p.kind, Requested: byName[p.requested.EventName()], Sent: byName[p.sent.EventName()]}
	}
	return out, err
}

// topRecipients lists the addresses asked for most often in the window,
// those asked for at least minRecipientRequests times.
func (s *Store) topRecipients(ctx context.Context, since time.Time) ([]AdminRecipient, error) {
	reset := events.PasswordResetRequested{}.EventName()
	invitation := events.WorkspaceInvitationCreated{}.EventName()
	registration := events.RegistrationRequested{}.EventName()
	change := events.EmailChangeRequested{}.EventName()
	rows := []AdminRecipient{}
	err := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("lower(e.payload->>'email') AS email").
		ColumnExpr("count(*) AS total").
		ColumnExpr("count(*) FILTER (WHERE e.name = ?) AS resets", reset).
		ColumnExpr("count(*) FILTER (WHERE e.name = ?) AS invitations", invitation).
		ColumnExpr("count(*) FILTER (WHERE e.name = ?) AS registrations", registration).
		ColumnExpr("count(*) FILTER (WHERE e.name = ?) AS email_changes", change).
		Where("e.name IN (?)", bun.In([]string{reset, invitation, registration, change})).
		Where("e.created_at >= ?", since).
		GroupExpr("1").
		Having("count(*) >= ?", minRecipientRequests).
		OrderExpr("total DESC, email").
		Limit(maxRecipients).
		Scan(ctx, &rows)
	return rows, err
}

// lastDeliveries finds the newest delivery done, and the newest given up
// on, among events in the window.
func (s *Store) lastDeliveries(ctx context.Context, since time.Time) (*time.Time, *AdminFailure, error) {
	var delivered sql.NullTime
	err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("max(d.delivered_at)").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("e.created_at >= ?", since).
		Scan(ctx, &delivered)
	if err != nil {
		return nil, nil, err
	}
	var failure AdminFailure
	err = s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Column("d.listener", "d.failed_at", "d.last_error").
		Join("JOIN events AS e ON e.id = d.event_id").
		Where("e.created_at >= ? AND d.failed_at IS NOT NULL", since).
		OrderExpr("d.failed_at DESC").
		Limit(1).
		Scan(ctx, &failure)
	var last *time.Time
	if delivered.Valid {
		last = &delivered.Time
	}
	if errors.Is(err, sql.ErrNoRows) {
		return last, nil, nil
	}
	return last, &failure, err
}
