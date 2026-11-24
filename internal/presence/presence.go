// Package presence owns whether a memo's body and attachment bytes are on this
// device. Readers ask Ensure, retention asks Evict, and pull asks MergeRemote;
// no other module compares LocalState or deletes attachment bytes for eviction.
package presence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

// ErrUnavailable reports a body that cannot be brought back to this device.
var ErrUnavailable = errors.New("memo body is not available locally")

// FileRemover deletes an attachment's local bytes. attachments.Service is the
// production adapter; sync tests pass the importer port.
type FileRemover interface {
	DeleteFiles(ctx context.Context, a domain.Attachment) error
}

// Keeper is the body-presence module. restore materialises one body from the
// archive; it is injected so this package does not depend on sync.
type Keeper struct {
	store   *store.Store
	files   FileRemover
	restore func(context.Context, string) error
	clock   func() time.Time
	emit    func(string, any)
}

// New builds the keeper. clock defaults to time.Now.
func New(st *store.Store, files FileRemover, restore func(context.Context, string) error,
	clock func() time.Time, emit func(string, any)) *Keeper {
	if clock == nil {
		clock = time.Now
	}
	return &Keeper{store: st, files: files, restore: restore, clock: clock, emit: emit}
}

// Evicted reports whether the body is absent from this device.
func Evicted(m domain.Memo) bool {
	return m.LocalState == domain.LocalEvicted
}

// Evicted reports whether the body is absent from this device.
func (k *Keeper) Evicted(m domain.Memo) bool {
	return Evicted(m)
}

// Ensure returns the memo with its body on this device, restoring it from the
// archive when it was evicted. On a failed restore it returns the metadata
// together with ErrUnavailable, so a caller can still show the placeholder.
func (k *Keeper) Ensure(ctx context.Context, id string) (*domain.Memo, error) {
	memo, err := k.store.GetMemo(ctx, id)
	if err != nil {
		return nil, err
	}
	if !Evicted(*memo) {
		return memo, nil
	}
	if k.restore == nil {
		return memo, fmt.Errorf("%w: no restorer is configured", ErrUnavailable)
	}
	if err := k.restore(ctx, id); err != nil {
		return memo, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	memo, err = k.store.GetMemo(ctx, id)
	if err != nil {
		return nil, err
	}
	if Evicted(*memo) {
		return memo, fmt.Errorf("%w: the body is still not available locally", ErrUnavailable)
	}
	return memo, nil
}

// Evict removes the body and attachment bytes from this device in one step.
// Metadata, tags, derived text and the search row survive. Evicting twice is a
// no-op.
func (k *Keeper) Evict(ctx context.Context, id string) error {
	memo, err := k.store.GetMemo(ctx, id)
	if err != nil {
		return err
	}
	if Evicted(*memo) {
		return nil
	}
	now := k.clock().UTC()
	if err := k.store.EvictBody(ctx, id, now); err != nil {
		return err
	}
	if k.files != nil {
		for _, a := range memo.Attachments {
			_ = k.files.DeleteFiles(ctx, a)
		}
	}
	_ = k.store.Log(ctx, "info", "presence", "evicted memo "+id)
	k.notify("memos:changed", map[string]string{"id": id})
	return nil
}

// MergeRemote applies a pulled page. When the local body is evicted the remote
// text still feeds search and derived fields, but the body stays absent; the
// second result reports whether the caller may wire attachment bytes.
func (k *Keeper) MergeRemote(ctx context.Context, r store.RemoteMemo) (*domain.Memo, bool, error) {
	preserveAbsent := false
	if local, err := k.store.GetMemo(ctx, r.ID); err == nil {
		preserveAbsent = Evicted(*local)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, false, err
	}
	memo, err := k.store.ApplyRemote(ctx, r, preserveAbsent)
	if err != nil {
		return nil, false, err
	}
	return memo, !preserveAbsent, nil
}

func (k *Keeper) notify(name string, data any) {
	if k.emit != nil {
		k.emit(name, data)
	}
}
