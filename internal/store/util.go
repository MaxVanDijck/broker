package store

import "strings"

func escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
