package capture

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type PayloadCompression string

const (
	PayloadCompressionNone PayloadCompression = ""
	PayloadCompressionGzip PayloadCompression = "gzip"
)

func ParsePayloadCompression(value string) (PayloadCompression, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none":
		return PayloadCompressionNone, nil
	case string(PayloadCompressionGzip):
		return PayloadCompressionGzip, nil
	default:
		return "", fmt.Errorf("unsupported relay capture compression: %s", value)
	}
}

func encryptPart(body []byte, compression PayloadCompression) (string, error) {
	encoded, err := compressPart(body, compression)
	if err != nil {
		return "", err
	}
	return common.EncryptSecret(string(encoded), "relay-capture")
}

func decryptPart(ciphertext []byte, compression PayloadCompression) ([]byte, error) {
	plaintext, err := common.DecryptSecret(string(ciphertext), "relay-capture")
	if err != nil {
		return nil, err
	}
	return decompressPart([]byte(plaintext), compression)
}

func compressPart(body []byte, compression PayloadCompression) ([]byte, error) {
	switch compression {
	case PayloadCompressionNone:
		return body, nil
	case PayloadCompressionGzip:
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(body); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		return compressed.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported relay capture compression: %s", compression)
	}
}

func decompressPart(body []byte, compression PayloadCompression) ([]byte, error) {
	switch compression {
	case PayloadCompressionNone:
		return body, nil
	case PayloadCompressionGzip:
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		decompressed, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return decompressed, nil
	default:
		return nil, fmt.Errorf("unsupported relay capture compression: %s", compression)
	}
}
