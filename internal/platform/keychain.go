package platform

import (
	"context"
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	keychainService = "memo-app"
	keychainUser    = "notion-pat"
)

// Keychain stores the Notion personal access token in the OS credential store.
// The token is never written to SQLite, logs or the frontend.
type Keychain struct{}

// NotionToken reads the PAT. A missing entry is not an error.
func (Keychain) NotionToken(context.Context) (string, error) {
	token, err := keyring.Get(keychainService, keychainUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return token, err
}

// SetNotionToken writes the PAT.
func (Keychain) SetNotionToken(_ context.Context, token string) error {
	if token == "" {
		return keyring.Delete(keychainService, keychainUser)
	}
	return keyring.Set(keychainService, keychainUser, token)
}

// ClearNotionToken removes the PAT.
func (Keychain) ClearNotionToken(context.Context) error {
	err := keyring.Delete(keychainService, keychainUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
