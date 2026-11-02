package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// 参数随哈希保存；只接受支持的参数，避免异常参数导致过量内存分配。
const (
	memoryKiB   = 64 * 1024
	iterations  = 3
	parallelism = 2
	saltBytes   = 16
	keyBytes    = 32
)

var prefix = fmt.Sprintf(
	"$argon2id$v=%d$m=%d,t=%d,p=%d$",
	argon2.Version, memoryKiB, iterations, parallelism,
)

// Hash 每次生成独立随机盐，相同密码也会得到不同的编码结果。
func Hash(plain string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallelism, keyBytes)
	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedKey := base64.RawStdEncoding.EncodeToString(key)

	return prefix + encodedSalt + "$" + encodedKey, nil
}

// Verify 只接受当前支持的参数，避免异常哈希参数耗尽内存。
func Verify(plain, encoded string) (bool, error) {
	if !strings.HasPrefix(encoded, prefix) {
		return false, fmt.Errorf("unsupported password hash")
	}
	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != saltBytes {
		return false, fmt.Errorf("invalid password salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(want) != keyBytes {
		return false, fmt.Errorf("invalid password key")
	}
	got := argon2.IDKey([]byte(plain), salt, iterations, memoryKiB, parallelism, keyBytes)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
