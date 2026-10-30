package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"ourtaiko.dev/database/api/internal/tja"
)

// Makers typed on the website, one per block, as on Fanmade: an upload, a replacement or
// an edit may set each block's maker, which otherwise comes from the TJA.
// The client identifies blocks, but cannot change their server-parsed difficulty.
func applyDifficultyMakers(raw string, difficulties []tja.Difficulty) error {
	if raw == "" {
		return nil
	}
	var entries []struct {
		Course string  `json:"course"`
		Side   string  `json:"side"`
		Maker  *string `json:"maker"`
	}
	if !utf8.ValidString(raw) || json.Unmarshal([]byte(raw), &entries) != nil || len(entries) != len(difficulties) {
		return fmt.Errorf("请为每个谱面块提供对应的制作者")
	}
	names := make(map[[2]string]string, len(entries))
	for _, entry := range entries {
		if entry.Course == "" || entry.Side == "" || entry.Maker == nil {
			return fmt.Errorf("制作者对应的谱面块无效")
		}
		slot := [2]string{entry.Course, entry.Side}
		if _, exists := names[slot]; exists {
			return fmt.Errorf("制作者对应的谱面块重复")
		}
		name := strings.TrimSpace(*entry.Maker)
		if len(name) > 500 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return fmt.Errorf("制作者名不能超过 500 字节或包含控制字符")
		}
		names[slot] = name
	}
	for i := range difficulties {
		name, exists := names[[2]string{difficulties[i].Course, difficulties[i].Side}]
		if !exists {
			return fmt.Errorf("制作者对应的难度无效")
		}
		difficulties[i].Maker = name
	}
	return nil
}
