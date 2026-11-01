package utils

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

const windowsQueueScheme = "winspool"

// WindowsQueueURI encodes a local Windows queue name as a printer URI.
func WindowsQueueURI(name string) string {
	return windowsQueueScheme + "://queue/" + base64.RawURLEncoding.EncodeToString([]byte(name))
}

func WindowsQueueName(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != windowsQueueScheme || u.Host != "queue" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("alamat antrean Windows tidak valid")
	}
	encoded, ok := strings.CutPrefix(u.EscapedPath(), "/")
	if !ok || encoded == "" || strings.Contains(encoded, "/") {
		return "", errors.New("alamat antrean Windows tidak valid")
	}
	nameBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", errors.New("alamat antrean Windows tidak valid")
	}
	name := string(nameBytes)
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, '\x00') || WindowsQueueURI(name) != raw {
		return "", errors.New("alamat antrean Windows tidak valid")
	}
	return name, nil
}
