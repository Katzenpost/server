// debug.go - Katzenpost server debug bits and peices.
// Copyright (C) 2017  Yawning Angel.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package server

import (
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/katzenpost/core/crypto/ecdh"
	"github.com/katzenpost/core/sphinx/constants"
)

func nodeIDToPrintString(id *[constants.NodeIDLength]byte) string {
	return strings.ToUpper(hex.EncodeToString(id[:]))
}

func ecdhToPrintString(pk *ecdh.PublicKey) string {
	return strings.ToUpper(hex.EncodeToString(pk.Bytes()))
}

func unsafeByteToPrintString(ad []byte) string {
	r := make([]byte, 0, len(ad))

	// This should *never* be used in production, since it attempts to give a
	// printable representation of a byte sequence for debug logging, and it's
	// slow.
	for _, v := range ad {
		if unicode.IsPrint(rune(v)) {
			r = append(r, v)
		} else {
			r = append(r, '*') // At least I didn't pick `:poop:`.
		}
	}
	return string(r)
}
