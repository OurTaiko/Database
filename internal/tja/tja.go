// Package tja reads the metadata the database stores from a TJA file. Unlike
// the Fanmade upload validator it accepts what official charts contain:
// single and double blocks in one file, scoring headers, and repeated or
// unknown headers.
package tja

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/unicode/norm"
)

const Version = "tja-official-v1"
const MaxTJA = 2 * 1024 * 1024
const MaxAudio = 100 * 1024 * 1024

type Issue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

func (e *Issue) Error() string { return e.Message }
func fail(code, message string, line int) *Issue {
	return &Issue{Code: code, Message: message, Line: line}
}

// Difficulty is one chart block: a slot of course × side, as the arcade
// machine keeps a single, a 1P and a 2P chart per course. Side follows the
// block's #START: plain is "single", #START P1 "p1", #START P2 "p2".
// Branching reports a #BRANCHSTART inside the block.
type Difficulty struct {
	// ID is the chart_difficulties row; zero for a file not yet stored.
	ID        int64  `json:"id,omitempty"`
	Course    string `json:"course"`
	Side      string `json:"side"`
	Level     int    `json:"level"`
	Maker     string `json:"maker"`
	Branching bool   `json:"branching"`
}

// Metadata is what the database stores from a TJA.
type Metadata struct {
	Title                string            `json:"title"`
	Subtitle             string            `json:"subtitle"`
	TitleTranslations    map[string]string `json:"titleTranslations"`
	SubtitleTranslations map[string]string `json:"subtitleTranslations"`
	Maker                string            `json:"maker"`
	BPM                  float64           `json:"bpm"`
	Offset               float64           `json:"offset"`
	DemoStart            float64           `json:"demoStart"`
	Wave                 string            `json:"wave"`
	Difficulties         []Difficulty      `json:"difficulties"`
}

func SafeFilename(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 240 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:"<>|?*`, r) {
			return false
		}
	}
	return true
}

var number = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)
var localizedFields = map[string]string{"TITLEEN": "en", "SUBTITLEEN": "en", "TITLEJA": "ja", "TITLEZH": "zh", "TITLEKO": "ko", "SUBTITLEJA": "ja", "SUBTITLEZH": "zh", "SUBTITLEKO": "ko"}
var courses = map[string]string{"0": "Easy", "1": "Normal", "2": "Hard", "3": "Oni", "4": "Edit", "easy": "Easy", "normal": "Normal", "hard": "Hard", "oni": "Oni", "edit": "Edit", "ura": "Edit"}
var courseNames = [...]string{"Easy", "Normal", "Hard", "Oni", "Edit"}

func numeric(s string) (float64, bool) {
	n, e := strconv.ParseFloat(s, 64)
	return n, e == nil && number.MatchString(s) && !math.IsNaN(n) && !math.IsInf(n, 0)
}

// DetectEncoding names the encoding NormalizeUTF8 should assume for raw bytes.
func DetectEncoding(data []byte) string {
	if utf8.Valid(data) {
		return "utf-8"
	}
	return "shift-jis"
}

// NormalizeUTF8 validates the declared input encoding and returns BOM-free UTF-8.
func NormalizeUTF8(data []byte, encoding string) ([]byte, *Issue) {
	if len(data) == 0 || len(data) > MaxTJA {
		return nil, fail("FILE_SIZE_INVALID", "TJA 不能为空且不能超过 2 MiB", 0)
	}
	var err error
	switch encoding {
	case "utf-8":
		if !utf8.Valid(data) {
			return nil, fail("TJA_ENCODING_INVALID", "TJA 不是有效 UTF-8，请转存为 UTF-8 后重新上传", 0)
		}
	case "shift-jis":
		data, err = japanese.ShiftJIS.NewDecoder().Bytes(data)
	default:
		return nil, fail("TJA_ENCODING_INVALID", "不支持此文本编码", 0)
	}
	if err != nil || !utf8.Valid(data) || bytes.Contains(data, []byte("\xEF\xBF\xBD")) || bytes.IndexByte(data, 0) >= 0 {
		return nil, fail("TJA_ENCODING_INVALID", "文本包含无法识别或损坏的字符", 0)
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if len(data) == 0 || len(data) > MaxTJA {
		return nil, fail("FILE_SIZE_INVALID", "转换后的 UTF-8 TJA 不能为空且不能超过 2 MiB", 0)
	}
	return data, nil
}

// Parse reads BOM-free UTF-8 TJA text. audioName, when not empty, must be the
// file the WAVE header names.
func Parse(data []byte, audioName string) (Metadata, *Issue) {
	m := Metadata{Difficulties: []Difficulty{}, TitleTranslations: map[string]string{}, SubtitleTranslations: map[string]string{}}
	if !utf8.Valid(data) {
		return m, fail("TJA_ENCODING_INVALID", "TJA 不是有效 UTF-8", 0)
	}
	waveLine := 0
	started, inBlock, hasNotes := false, false, false
	course, level := "Oni", 0
	style := "Single"
	// NOTESDESIGNER0–4 credit one course each; MAKER credits every block.
	designers := map[string]string{}
	seen := map[string]bool{}
	seenSlots := map[string]int{}
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line := i + 1
		if len(raw) > 65536 {
			return m, fail("TJA_STRUCTURE_INVALID", "单行内容超过 64 KiB", line)
		}
		s := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "#NEXTSONG") {
			return m, fail("TJA_COURSE_UNSUPPORTED", "不支持段位（Dan）等多曲谱面", line)
		}
		if strings.HasPrefix(s, "#START") {
			player := strings.TrimSpace(strings.TrimPrefix(s, "#START"))
			if (player != "" && player != "P1" && player != "P2") || inBlock || level == 0 {
				return m, fail("TJA_STRUCTURE_INVALID", "每个谱面需要 LEVEL:1–10 和独立的 #START / #END", line)
			}
			started, inBlock, hasNotes = true, true, false
			side := "single"
			if player != "" {
				side = strings.ToLower(player)
			} else if style == "Double" {
				return m, fail("TJA_PLAYER_REQUIRED", "双人谱面必须使用 #START P1 或 #START P2", line)
			}
			slot := course + " " + side
			if first, exists := seenSlots[slot]; exists {
				return m, fail("TJA_DIFFICULTY_DUPLICATE", fmt.Sprintf("%s 难度重复，第 %d 行已声明", slot, first), line)
			}
			seenSlots[slot] = line
			m.Difficulties = append(m.Difficulties, Difficulty{Course: course, Side: side, Level: level})
			continue
		}
		if inBlock && len(s) >= 12 && strings.EqualFold(s[:12], "#BRANCHSTART") {
			m.Difficulties[len(m.Difficulties)-1].Branching = true
			continue
		}
		if s == "#END" {
			if !inBlock || !hasNotes {
				return m, fail("TJA_STRUCTURE_INVALID", "谱面块为空或 #END 没有对应的 #START", line)
			}
			inBlock = false
			continue
		}
		key, value, ok := strings.Cut(s, ":")
		if !ok || strings.HasPrefix(s, "#") {
			if inBlock && !strings.HasPrefix(s, "#") && strings.ContainsAny(s, "0123456789") {
				hasNotes = true
			}
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if len(value) > 500 && key != "BALLOON" && !strings.HasPrefix(key, "BALLOON") {
			return m, fail("TJA_STRUCTURE_INVALID", "元数据字段过长", line)
		}
		if locale, ok := localizedFields[key]; ok {
			if started || seen[key] || value == "" {
				continue
			}
			seen[key] = true
			if strings.HasPrefix(key, "SUBTITLE") {
				m.SubtitleTranslations[locale] = value
			} else {
				m.TitleTranslations[locale] = value
			}
			continue
		}
		switch key {
		case "STYLE":
			if inBlock {
				return m, fail("TJA_STRUCTURE_INVALID", "STYLE 必须位于谱面块之外", line)
			}
			switch strings.ToLower(value) {
			case "single", "0", "1":
				style = "Single"
			case "double", "duet", "couple", "2":
				style = "Double"
			default:
				return m, fail("TJA_STRUCTURE_INVALID", "STYLE 需要 Single / Double", line)
			}
		case "WAVE":
			if started || seen[key] {
				return m, fail("TJA_WAVE_DUPLICATE", "WAVE 只能在第一个 #START 之前声明一次", line)
			}
			seen[key] = true
			waveLine = line
			m.Wave = value
		case "COURSE":
			normalized := strings.ToLower(value)
			if normalized == "tower" || normalized == "dan" || normalized == "5" || normalized == "6" {
				return m, fail("TJA_COURSE_UNSUPPORTED", "不支持塔（Tower）或段位（Dan）谱面", line)
			}
			c, ok := courses[normalized]
			if !ok || inBlock {
				return m, fail("TJA_STRUCTURE_INVALID", "难度需要 Easy / Normal / Hard / Oni / Edit（或 0–4）", line)
			}
			// STYLE belongs to the course header. A new course defaults to Single.
			course, level, style = c, 0, "Single"
		case "LEVEL":
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 || n > 10 || inBlock {
				return m, fail("TJA_STRUCTURE_INVALID", "LEVEL 必须是 1–10 的整数", line)
			}
			level = n
		case "NOTESDESIGNER0", "NOTESDESIGNER1", "NOTESDESIGNER2", "NOTESDESIGNER3", "NOTESDESIGNER4":
			designers[courseNames[key[len(key)-1]-'0']] = value
		case "TITLE", "SUBTITLE", "MAKER", "BPM", "OFFSET", "DEMOSTART":
			// The first declaration before the charts is the song's.
			if started || seen[key] {
				continue
			}
			seen[key] = true
			switch key {
			case "TITLE":
				m.Title = value
			case "SUBTITLE":
				m.Subtitle = value
			case "MAKER":
				m.Maker = value
			default:
				n, valid := numeric(value)
				if !valid {
					if value == "" {
						continue
					}
					return m, fail("TJA_STRUCTURE_INVALID", key+" 必须为有限数字", line)
				}
				switch key {
				case "BPM":
					m.BPM = n
				case "OFFSET":
					m.Offset = n
				case "DEMOSTART":
					m.DemoStart = n
				}
			}
		}
	}
	if m.Wave == "" {
		return m, fail("TJA_WAVE_MISSING", "TJA 缺少非空的 WAVE 音频引用", waveLine)
	}
	extension := strings.ToLower(m.Wave[strings.LastIndex(m.Wave, ".")+1:])
	if !SafeFilename(m.Wave) || (extension != "ogg" && extension != "mp3") {
		return m, fail("TJA_WAVE_PATH_INVALID", "WAVE 必须为不带路径、引号的 .ogg 或 .mp3 文件名", waveLine)
	}
	if audioName != "" && norm.NFC.String(m.Wave) != norm.NFC.String(audioName) {
		return m, &Issue{Code: "TJA_AUDIO_MISMATCH", Message: fmt.Sprintf("第 %d 行引用了「%s」，但你选择的是「%s」。请选择对应文件，或修改 TJA 的 WAVE 后重新选择。", waveLine, m.Wave, audioName), Line: waveLine, Expected: m.Wave, Actual: audioName}
	}
	if m.Title == "" || m.BPM <= 0 || inBlock || len(m.Difficulties) == 0 {
		return m, fail("TJA_STRUCTURE_INVALID", "需要 TITLE、正数 BPM 和完整谱面块", 0)
	}
	if len(m.Difficulties) > 15 {
		return m, fail("TJA_STRUCTURE_INVALID", "谱面块数量超过上限", 0)
	}
	makers, credited := []string{}, map[string]bool{}
	for i := range m.Difficulties {
		name := designers[m.Difficulties[i].Course]
		if name == "" {
			name = m.Maker
		}
		m.Difficulties[i].Maker = name
		if name != "" && !credited[name] {
			credited[name] = true
			makers = append(makers, name)
		}
	}
	m.Maker = strings.Join(makers, " | ")
	if _, ok := m.TitleTranslations["en"]; !ok {
		m.TitleTranslations["en"] = m.Title
	}
	if _, ok := m.SubtitleTranslations["en"]; !ok {
		m.SubtitleTranslations["en"] = m.Subtitle
	}
	return m, nil
}

// header returns the byte range of the named header line before the first
// #START, excluding its line ending, and the declared value.
func header(data []byte, name string) (start, end int, value string, ok bool) {
	for pos := 0; pos <= len(data); {
		stop := len(data)
		if i := bytes.IndexByte(data[pos:], '\n'); i >= 0 {
			stop = pos + i
		}
		raw := strings.TrimSuffix(string(data[pos:stop]), "\r")
		s := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if strings.HasPrefix(s, "#START") {
			break
		}
		if key, v, found := strings.Cut(s, ":"); found && strings.ToUpper(strings.TrimSpace(key)) == name {
			return pos, pos + len(raw), strings.TrimSpace(v), true
		}
		pos = stop + 1
	}
	return 0, 0, "", false
}

// WaveName returns the audio file name declared by a stored UTF-8 TJA.
func WaveName(data []byte) string {
	_, _, value, _ := header(data, "WAVE")
	return value
}

// SetWave points the WAVE header at name, leaving every other byte unchanged.
// Stored charts reference their audio by its content-addressed file name (R007).
func SetWave(data []byte, name string) ([]byte, bool) {
	start, end, _, ok := header(data, "WAVE")
	if !ok {
		return nil, false
	}
	out := make([]byte, 0, len(data)+len(name))
	out = append(out, data[:start]...)
	out = append(out, "WAVE:"+name...)
	return append(out, data[end:]...), true
}
