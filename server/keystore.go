package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	encryptionKeyKVKey = "encryption_key"
	apiKeyPrefix       = "apikey:"
)

var errAPIKeyNotFound = errors.New("cursor API key is not configured")

type keyValueStore interface {
	KVGet(key string) ([]byte, *model.AppError)
	KVSet(key string, value []byte) *model.AppError
	KVCompareAndSet(key string, oldValue, newValue []byte) (bool, *model.AppError)
	KVDelete(key string) *model.AppError
}

type encryptedKeyRecord struct {
	Email      string `json:"email"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

type KeyStore struct {
	kv   keyValueStore
	aead cipher.AEAD
}

func NewKeyStore(kv keyValueStore) (*KeyStore, error) {
	key, err := loadOrCreateEncryptionKey(kv)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize API key cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize API key encryption: %w", err)
	}
	return &KeyStore{kv: kv, aead: aead}, nil
}

func loadOrCreateEncryptionKey(kv keyValueStore) ([]byte, error) {
	stored, appErr := kv.KVGet(encryptionKeyKVKey)
	if appErr != nil {
		return nil, fmt.Errorf("get encryption key: %w", appErr)
	}
	if len(stored) > 0 {
		if len(stored) != 32 {
			return nil, fmt.Errorf("stored encryption key has invalid length %d", len(stored))
		}
		return stored, nil
	}

	generated := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, generated); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	written, appErr := kv.KVCompareAndSet(encryptionKeyKVKey, nil, generated)
	if appErr != nil {
		return nil, fmt.Errorf("store encryption key: %w", appErr)
	}
	if written {
		return generated, nil
	}

	stored, appErr = kv.KVGet(encryptionKeyKVKey)
	if appErr != nil {
		return nil, fmt.Errorf("reload encryption key: %w", appErr)
	}
	if len(stored) != 32 {
		return nil, errors.New("encryption key initialization raced but no valid key was stored")
	}
	return stored, nil
}

func (s *KeyStore) Set(userID, apiKey, email string) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, readErr := io.ReadFull(rand.Reader, nonce); readErr != nil {
		return fmt.Errorf("generate API key nonce: %w", readErr)
	}
	record := encryptedKeyRecord{
		Email:      email,
		Nonce:      nonce,
		Ciphertext: s.aead.Seal(nil, nonce, []byte(apiKey), []byte(userID)),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode encrypted API key: %w", err)
	}
	if appErr := s.kv.KVSet(apiKeyPrefix+userID, encoded); appErr != nil {
		return fmt.Errorf("store encrypted API key: %w", appErr)
	}
	return nil
}

func (s *KeyStore) Get(userID string) (string, string, error) {
	record, err := s.getRecord(userID)
	if err != nil {
		return "", "", err
	}
	plaintext, err := s.aead.Open(nil, record.Nonce, record.Ciphertext, []byte(userID))
	if err != nil {
		return "", "", fmt.Errorf("decrypt Cursor API key: %w", err)
	}
	return string(plaintext), record.Email, nil
}

func (s *KeyStore) Info(userID string) (bool, string, error) {
	record, err := s.getRecord(userID)
	if errors.Is(err, errAPIKeyNotFound) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, record.Email, nil
}

func (s *KeyStore) Delete(userID string) error {
	if appErr := s.kv.KVDelete(apiKeyPrefix + userID); appErr != nil {
		return fmt.Errorf("delete Cursor API key: %w", appErr)
	}
	return nil
}

func (s *KeyStore) getRecord(userID string) (encryptedKeyRecord, error) {
	encoded, appErr := s.kv.KVGet(apiKeyPrefix + userID)
	if appErr != nil {
		return encryptedKeyRecord{}, fmt.Errorf("get encrypted API key: %w", appErr)
	}
	if len(encoded) == 0 {
		return encryptedKeyRecord{}, errAPIKeyNotFound
	}
	var record encryptedKeyRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return encryptedKeyRecord{}, fmt.Errorf("decode encrypted API key: %w", err)
	}
	return record, nil
}
