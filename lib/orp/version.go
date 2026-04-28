// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 BoanLab @ Dankook University

package orp

// IsSupportedVersion reports whether v can be processed by this build.
// Version negotiation today is trivial — only Version1 exists. When v2
// ships, this and HELLO/HELLO_ACK payloads gain real negotiation logic.
func IsSupportedVersion(v uint8) bool {
	return v == Version1
}
