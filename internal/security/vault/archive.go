// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package vault

import "context"

// WrapArchiveKey seals a random archive data key under the active Master KEK and
// returns the key identifier needed by a future restore/verify operation.
func (s *Store) WrapArchiveKey(ctx context.Context, plaintextKey []byte) (string, []byte, []byte, error) {
	keyID, err := s.kek.ActiveKeyID(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	nonce, ciphertext, err := s.kek.WrapArchiveKey(ctx, keyID, plaintextKey)
	if err != nil {
		return "", nil, nil, err
	}
	return keyID, nonce, ciphertext, nil
}

// UnwrapArchiveKey opens an archive data key under its recorded Master KEK ID.
func (s *Store) UnwrapArchiveKey(ctx context.Context, keyID string, nonce, ciphertext []byte) ([]byte, error) {
	return s.kek.UnwrapArchiveKey(ctx, keyID, nonce, ciphertext)
}
