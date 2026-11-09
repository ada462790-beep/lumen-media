// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

// Package totp rechnet Einmalcodes nach RFC 6238, so wie Google
// Authenticator, Microsoft Authenticator, Aegis oder 1Password sie
// erzeugen: sechs Ziffern, alle 30 Sekunden neu, HMAC-SHA1. Lumen
// schickt dafür nichts über E-Mail oder SMS — das Geheimnis wandert
// einmal per QR-Code in die App, danach reicht die Uhr.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// NeuesGeheimnis erzeugt ein Geheimnis (20 Byte, Base32 ohne Füllzeichen —
// so, wie die Apps es erwarten).
func NeuesGeheimnis() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// Code rechnet den Code für den Zeitpunkt t.
func Code(geheimnis string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.ReplaceAll(geheimnis, " ", "")))
	if err != nil {
		return "", fmt.Errorf("geheimnis unlesbar: %w", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(t.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000), nil
}

// Stimmt prüft einen Code — mit einem Schritt Spiel nach beiden Seiten,
// weil Uhren nie ganz gleich gehen.
func Stimmt(geheimnis, code string, jetzt time.Time) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return false
	}
	ok := false
	for _, d := range []time.Duration{0, -30 * time.Second, 30 * time.Second} {
		want, err := Code(geheimnis, jetzt.Add(d))
		if err != nil {
			return false
		}
		// Kein frühes Ende — alle drei vergleichen, gleich lang.
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			ok = true
		}
	}
	return ok
}

// Adresse ist die otpauth-URL, die die App aus dem QR-Code liest.
func Adresse(geheimnis, konto string) string {
	return "otpauth://totp/" + url.PathEscape("Lumen:"+konto) +
		"?secret=" + geheimnis + "&issuer=Lumen&algorithm=SHA1&digits=6&period=30"
}

// QR zeichnet die Adresse als PNG.
func QR(adresse string) ([]byte, error) {
	return qrcode.Encode(adresse, qrcode.Medium, 256)
}
