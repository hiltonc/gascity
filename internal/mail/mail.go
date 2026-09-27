// Package mail defines the pluggable mail provider interface for Gas City.
// The primary extension point is the exec script protocol (see
// internal/mail/exec); the Go interface exists for code organization and
// testability.
package mail //nolint:revive // internal package, always imported qualified

import (
	"errors"
	"time"
)

// ErrAlreadyArchived is returned by [Provider.Archive] when the message has
// already been archived or deleted. CLI code uses this to print a distinct
// message.
var ErrAlreadyArchived = errors.New("already archived")

// ErrNotFound is returned when a message ID does not exist.
var ErrNotFound = errors.New("message not found")

// ErrNotArchived is returned by [Provider.Unarchive] when the message is
// already open, so callers can skip announcing a reopen that did not happen.
var ErrNotArchived = errors.New("not archived")

const (
	// StatusOpen marks a message that is live in the recipient's mailbox.
	StatusOpen = "open"
	// StatusClosed marks an archived message: still readable by ID and listed
	// by [Provider.Archived], but absent from inbox views.
	StatusClosed = "closed"
)

const (
	// AutoHandoffLabel marks mail created by gc handoff --auto for provider
	// context-cycle delivery.
	AutoHandoffLabel = "gc:auto-handoff"
	// ArchiveAfterInjectLabel marks system mail that should be archived after
	// successful hook injection.
	ArchiveAfterInjectLabel = "gc:archive-after-inject"
	// FromSessionIDMetadataKey stores the stable session bead ID used for
	// reply routing when a message's display sender may later be renamed.
	FromSessionIDMetadataKey = "mail.from_session_id"
	// FromDisplayMetadataKey stores the human-readable sender captured when
	// the message was created.
	FromDisplayMetadataKey = "mail.from_display"
	// ToSessionIDMetadataKey stores the stable recipient session bead ID used
	// for routing replies while keeping the public To field human-readable.
	ToSessionIDMetadataKey = "mail.to_session_id"
	// ToDisplayMetadataKey stores the human-readable recipient captured when
	// the message was created.
	ToDisplayMetadataKey = "mail.to_display"
	// ReadMetadataKey mirrors the "read" label as a queryable metadata flag
	// ("true"/"false"), set alongside the label by MarkRead/MarkUnread. Retention
	// sweeps query it directly (the label-based query is recipient-scoped).
	ReadMetadataKey = "mail.read"
)

// Message represents a mail message between agents or humans.
type Message struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
	ThreadID  string    `json:"thread_id,omitempty"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	Priority  int       `json:"priority,omitempty"`
	CC        []string  `json:"cc,omitempty"`
	Rig       string    `json:"rig,omitempty"`
	// Status is [StatusOpen] or [StatusClosed]. An archived message is closed.
	Status string `json:"status" enum:"open,closed" doc:"Message state: open, or closed once archived."`
	// ClosedAt is when the message was closed; nil while it is open.
	ClosedAt *time.Time `json:"closed_at,omitempty" doc:"When the message was closed. Absent while open."`
}

// HandoffIntent is the domain-shaped request for handoff mail. It lets the
// gc handoff command express a message in mail terms — sender, recipient,
// subject, body — plus the two handoff-specific routing details that ordinary
// [Provider.Send] does not surface: an explicit thread ID (so a handoff thread
// is stable and addressable) and extra labels (the auto-handoff / archive-after-
// inject markers). The bead translation of these fields is confined to the
// backend implementation; callers never construct a message bead themselves.
type HandoffIntent struct {
	From        string
	To          string
	Subject     string
	Body        string
	ThreadID    string
	ExtraLabels []string
}

// ArchiveResult is one message's outcome in a batch [Provider.ArchiveMany] or
// [Provider.DeleteMany] call. Err is nil for a newly-archived/deleted message,
// [ErrAlreadyArchived] for an idempotent repeat, or a provider error.
type ArchiveResult struct {
	ID  string
	Err error
}

// Provider is the internal interface for mail backends and the canonical
// domain seam for mail: every mail caller speaks mail.Message (and
// [HandoffIntent]) here, never a raw message bead. The translation between mail
// and storage rows is confined to each backend — for the built-in default that
// edge is beadmail.Provider, where a message becomes a Type="message" bead.
// Implementations include beadmail (built-in default backed by beads.Store) and
// exec (user-supplied script via fork/exec).
type Provider interface {
	// Send creates a message. Subject is the summary line, body is the
	// full content. Returns the created message with assigned ID.
	Send(from, to, subject, body string) (Message, error)

	// Inbox returns unread messages for the recipient.
	Inbox(recipient string) ([]Message, error)

	// Get retrieves a message by ID without marking it read.
	Get(id string) (Message, error)

	// Read retrieves a message by ID and marks it as read.
	// The message remains in the store (not closed).
	Read(id string) (Message, error)

	// MarkRead marks a message as read (adds "read" label).
	MarkRead(id string) error

	// MarkUnread marks a message as unread (removes "read" label).
	MarkUnread(id string) error

	// Archive closes a message. It leaves the inbox views (Inbox, Check, All,
	// Count, Thread) but stays readable by ID and is listed by Archived.
	// Archiving a closed message returns [ErrAlreadyArchived].
	Archive(id string) error

	// Unarchive reopens an archived message, returning it to the inbox views.
	// It returns [ErrNotFound] for a message that does not exist and
	// [ErrNotArchived] for one that is already open.
	Unarchive(id string) error

	// Archived returns the closed (archived) messages for the recipient,
	// read and unread.
	Archived(recipient string) ([]Message, error)

	// ArchiveMany archives a batch of messages in one round-trip where the
	// backend supports it, returning per-id results in input order.
	// Implementations MUST preserve per-id error reporting.
	ArchiveMany(ids []string) ([]ArchiveResult, error)

	// Delete permanently removes a message, open or archived. Unlike Archive
	// it is destructive: a deleted message is no longer readable by ID.
	// Deleting a message that no longer exists returns [ErrAlreadyArchived].
	Delete(id string) error

	// DeleteMany deletes a batch of messages in one round-trip where the
	// backend supports it, returning per-id results in input order.
	// Implementations MUST preserve delete semantics and per-id error
	// reporting.
	DeleteMany(ids []string) ([]ArchiveResult, error)

	// Check returns unread messages without marking them read.
	Check(recipient string) ([]Message, error)

	// Reply creates a reply to an existing message. Inherits ThreadID
	// from the original, sets ReplyTo to the original's ID.
	Reply(id, from, subject, body string) (Message, error)

	// Thread returns all messages sharing a thread ID, ordered by time.
	// The id may be either the thread ID or any message ID in that thread.
	Thread(id string) ([]Message, error)

	// All returns all open messages (read and unread) for the recipient.
	All(recipient string) ([]Message, error)

	// Count returns (total, unread) message counts for a recipient.
	Count(recipient string) (total int, unread int, err error)
}

// MultiRecipientInboxer is an optional extension for providers that can return
// unread inbox messages for multiple recipients in one backend pass.
type MultiRecipientInboxer interface {
	InboxRecipients(recipients []string) ([]Message, error)
}
