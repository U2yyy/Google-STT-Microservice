package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func GetAesKey(timestamp int64) []byte {
	const sstSecret = "u2ysstkeyqwihuduheqwid"

	timeKey := timestamp / 1000 / 60 * 2
	strKey := fmt.Sprintf("%d_%s", timeKey, sstSecret)

	hashBytes := md5.Sum([]byte(strKey))
	hexStr := hex.EncodeToString(hashBytes[:])
	upperHexStr := strings.ToUpper(hexStr)
	finalKeyStr := upperHexStr[16:]

	return []byte(finalKeyStr)
}

// 解码函数
func AesDecrypt(ciphertextHex string, key []byte) ([]byte, error) {
	fullData, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex string")
	}

	// 长度校验
	// 最小长度 = IV长度(16) + 最小块长度(16) = 32
	if len(fullData) < aes.BlockSize*2 {
		return nil, errors.New("ciphertext too short")
	}

	// 前 16 字节是 IV
	iv := fullData[:aes.BlockSize]
	// 后面全是密文
	ciphertext := fullData[aes.BlockSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext is not a multiple of the block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)

	// 原地解密
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	plaintext, err = pkcs7Unpadding(plaintext)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// 辅助函数：去填充
func pkcs7Unpadding(data []byte) ([]byte, error) {
	length := len(data)
	if length == 0 {
		return nil, errors.New("pkcs7: data is empty")
	}
	// 获取最后一个字节的值（它是填充的数量）
	padding := int(data[length-1])

	if padding > length || padding == 0 {
		return nil, errors.New("pkcs7: invalid padding")
	}

	// 校验填充内容是否正确（可选，但推荐）
	// 例如 padding 是 3，那么最后 3 个字节都应该是 0x03
	for i := 0; i < padding; i++ {
		if data[length-1-i] != byte(padding) {
			return nil, errors.New("pkcs7: invalid padding bytes")
		}
	}

	return data[:length-padding], nil
}
