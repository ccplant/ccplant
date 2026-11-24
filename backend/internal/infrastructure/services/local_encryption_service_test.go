package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 32 バイトのランダムキーを生成
func generateTestKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	bytesRead, err := rand.Read(key)
	require.NoError(t, err)
	require.Equal(t, len(key), bytesRead)
	return key
}

func TestNewLocalEncryptionService_FromFile(t *testing.T) {
	// テスト用のキーファイルを作成
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "encryption.key")
	testKey := generateTestKey(t)
	err := os.WriteFile(keyPath, testKey, 0600)
	require.NoError(t, err)

	// サービスを作成
	service, err := NewLocalEncryptionService(keyPath, "")
	require.NoError(t, err)
	require.NotNil(t, service)

	assert.Equal(t, "aes-256-gcm", service.Algorithm())
	assert.Contains(t, service.KeyID(), "sha256:")
}

func TestNewLocalEncryptionService_FromEnv(t *testing.T) {
	// テスト用のキーを環境変数に設定
	testKey := generateTestKey(t)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64)

	// サービスを作成
	service, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)
	require.NotNil(t, service)

	assert.Equal(t, "aes-256-gcm", service.Algorithm())
	assert.Contains(t, service.KeyID(), "sha256:")
}

func TestNewLocalEncryptionService_InvalidKeySize(t *testing.T) {
	// 不正なサイズのキーファイルを作成
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "encryption.key")
	invalidKey := make([]byte, 16) // 16 バイト（AES-128）は NG
	err := os.WriteFile(keyPath, invalidKey, 0600)
	require.NoError(t, err)

	// サービス作成はエラーになるはず
	service, err := NewLocalEncryptionService(keyPath, "")
	assert.Error(t, err)
	assert.Nil(t, service)
	assert.Contains(t, err.Error(), "must be 32 bytes")
}

func TestNewLocalEncryptionService_NoKey(t *testing.T) {
	// キーが設定されていない場合
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", "")

	service, err := NewLocalEncryptionService("", "")
	assert.Error(t, err)
	assert.Nil(t, service)
	assert.Contains(t, err.Error(), "encryption key not found")
}

func TestLocalEncryptionService_Encrypt(t *testing.T) {
	testKey := generateTestKey(t)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64)

	service, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	ctx := context.Background()
	plaintext := "test-plaintext"

	encrypted, err := service.Encrypt(ctx, plaintext)
	require.NoError(t, err)
	require.NotNil(t, encrypted)

	// 暗号化された値は元の値と異なるはず
	assert.NotEqual(t, plaintext, encrypted.EncryptedValue)
	assert.Equal(t, "aes-256-gcm", encrypted.Metadata.Algorithm)
	assert.Contains(t, encrypted.Metadata.KeyID, "sha256:")
	assert.Equal(t, "v1", encrypted.Metadata.Version)
	assert.False(t, encrypted.Metadata.EncryptedAt.IsZero())
}

func TestLocalEncryptionService_Decrypt(t *testing.T) {
	testKey := generateTestKey(t)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64)

	service, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	ctx := context.Background()
	plaintext := "test-plaintext"

	// 暗号化
	encrypted, err := service.Encrypt(ctx, plaintext)
	require.NoError(t, err)

	// 復号
	decrypted, err := service.Decrypt(ctx, encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestLocalEncryptionService_RoundTrip(t *testing.T) {
	testKey := generateTestKey(t)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64)

	service, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	ctx := context.Background()

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"unicode", "こんにちは世界"},
		{"special chars", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
		{"long text", "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
		{"json", `{"key": "value", "number": 123, "nested": {"array": [1, 2, 3]}}`},
		{"multiline", "line1\nline2\nline3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 暗号化
			encrypted, err := service.Encrypt(ctx, tc.plaintext)
			require.NoError(t, err)

			// 復号
			decrypted, err := service.Decrypt(ctx, encrypted)
			require.NoError(t, err)

			// 元の値と一致することを確認
			assert.Equal(t, tc.plaintext, decrypted)
		})
	}
}

func TestLocalEncryptionService_DifferentKeys(t *testing.T) {
	ctx := context.Background()

	// サービス1 を作成
	key1 := generateTestKey(t)
	key1B64 := base64.StdEncoding.EncodeToString(key1)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", key1B64)
	service1, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	// サービス2 を作成（異なるキー）
	key2 := generateTestKey(t)
	key2B64 := base64.StdEncoding.EncodeToString(key2)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", key2B64)
	service2, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	// サービス1 で暗号化
	plaintext := "test-plaintext"
	encrypted, err := service1.Encrypt(ctx, plaintext)
	require.NoError(t, err)

	// サービス2 で復号を試みる（失敗するはず）
	decrypted, err := service2.Decrypt(ctx, encrypted)
	assert.Error(t, err)
	assert.Empty(t, decrypted)
	assert.Contains(t, err.Error(), "failed to decrypt")
}

func TestLocalEncryptionService_InvalidCiphertext(t *testing.T) {
	testKey := generateTestKey(t)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	t.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64)

	service, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	ctx := context.Background()

	// 不正な Base64
	encrypted, err := service.Encrypt(ctx, "test")
	require.NoError(t, err)
	encrypted.EncryptedValue = "not-valid-base64!@#$"
	decrypted, err := service.Decrypt(ctx, encrypted)
	assert.Error(t, err)
	assert.Empty(t, decrypted)

	// 短すぎる暗号文
	encrypted.EncryptedValue = base64.StdEncoding.EncodeToString([]byte("short"))
	decrypted, err = service.Decrypt(ctx, encrypted)
	assert.Error(t, err)
	assert.Empty(t, decrypted)
	assert.Contains(t, err.Error(), "ciphertext too short")

	// 改ざんされた暗号文
	validEncrypted, err := service.Encrypt(ctx, "test")
	require.NoError(t, err)
	ciphertext, err := base64.StdEncoding.DecodeString(validEncrypted.EncryptedValue)
	require.NoError(t, err)
	ciphertext[len(ciphertext)-1] ^= 0xFF // 最後のバイトを反転
	validEncrypted.EncryptedValue = base64.StdEncoding.EncodeToString(ciphertext)
	decrypted, err = service.Decrypt(ctx, validEncrypted)
	assert.Error(t, err)
	assert.Empty(t, decrypted)
}
