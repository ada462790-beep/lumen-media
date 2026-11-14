// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package api

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// schnittstellenArten fragt den Kern (rtnetlink), welche Art jede
// Schnittstelle ist: „veth" (virtuelles Kabel zu einer Container-Brücke),
// „macvlan"/„ipvlan" (eigene Adresse im echten Heimnetz), „bridge" …
// Schnittstellen ohne Art (echte Netzwerkkarten, lo) fehlen. nil, wenn
// der Kern nicht antwortet.
func schnittstellenArten() map[string]string {
	rib, err := syscall.NetlinkRIB(syscall.RTM_GETLINK, syscall.AF_UNSPEC)
	if err != nil {
		return nil
	}
	msgs, err := syscall.ParseNetlinkMessage(rib)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, m := range msgs {
		if m.Header.Type != syscall.RTM_NEWLINK {
			continue
		}
		attrs, err := syscall.ParseNetlinkRouteAttr(&m)
		if err != nil {
			continue
		}
		var name, art string
		for _, a := range attrs {
			switch a.Attr.Type {
			case syscall.IFLA_IFNAME:
				name = cString(a.Value)
			case iflaLinkinfo:
				art = linkInfoArt(a.Value)
			}
		}
		if name != "" && art != "" {
			out[name] = art
		}
	}
	return out
}

const (
	iflaLinkinfo = 18 // IFLA_LINKINFO
	iflaInfoKind = 1  // IFLA_INFO_KIND
)

// linkInfoArt liest IFLA_INFO_KIND aus den verschachtelten Angaben von
// IFLA_LINKINFO.
func linkInfoArt(b []byte) string {
	kopf := int(unsafe.Sizeof(syscall.RtAttr{}))
	for len(b) >= kopf {
		l := int(binary.NativeEndian.Uint16(b[0:2]))
		t := binary.NativeEndian.Uint16(b[2:4])
		if l < kopf || l > len(b) {
			return ""
		}
		if t == iflaInfoKind {
			return cString(b[kopf:l])
		}
		weiter := (l + syscall.RTA_ALIGNTO - 1) &^ (syscall.RTA_ALIGNTO - 1)
		if weiter > len(b) {
			return ""
		}
		b = b[weiter:]
	}
	return ""
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
