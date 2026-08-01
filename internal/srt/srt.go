// Package srt parseia roteiro/legenda (SRT ou ASS) e extrai falas com
// opção de rótulo de falante no início da linha: "[Nome] texto" ou "Nome: texto".
package srt

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cue é uma fala do roteiro.
type Cue struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker,omitempty"` // vazio = sem rótulo
}

var reTime = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{1,3})`)

func parseTimestamp(s string) (float64, error) {
	m := reTime.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("timestamp inválido: %q", s)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	se, _ := strconv.Atoi(m[3])
	msStr := m[4]
	for len(msStr) < 3 {
		msStr += "0"
	}
	ms, _ := strconv.Atoi(msStr[:3])
	d := time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute +
		time.Duration(se)*time.Second + time.Duration(ms)*time.Millisecond
	return d.Seconds(), nil
}

var reSpeaker = regexp.MustCompile(`^\s*[\[(]([^\]\)]{1,40})[\])]\s*(?::\s*)?(.*)$|^\s*([A-Za-zÀ-ÿ0-9_\- ]{1,30}):\s*(.*)$`)

// Parse lê o arquivo de roteiro. Suporta SRT, ASS (extrato de diálogo) e JSON.
func Parse(path string) ([]Cue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var cues []Cue
		if err := json.Unmarshal(data, &cues); err == nil {
			return cues, nil
		}
	}
	if strings.Contains(trimmed, "Dialogue:") {
		return parseASS(trimmed)
	}
	return parseSRT(trimmed)
}

func splitLines(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
}

func parseASS(s string) ([]Cue, error) {
	var cues []Cue
	for _, ln := range splitLines(s) {
		if !strings.HasPrefix(ln, "Dialogue:") {
			continue
		}
		body := strings.TrimPrefix(ln, "Dialogue:")
		fields := strings.SplitN(body, ",", 10)
		if len(fields) < 10 {
			continue
		}
		start, err1 := parseTimestamp(strings.TrimSpace(fields[1]))
		end, err2 := parseTimestamp(strings.TrimSpace(fields[2]))
		if err1 != nil || err2 != nil {
			continue
		}
		text := stripTags(fields[9])
		if strings.TrimSpace(text) == "" {
			continue
		}
		cues = append(cues, Cue{Start: start, End: end, Text: strings.TrimSpace(text)})
	}
	return cues, nil
}

var reTags = regexp.MustCompile(`\{[^}]*\}`)

func stripTags(s string) string {
	s = reTags.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, `\N`, "\n")
	return strings.TrimSpace(s)
}

func parseSRT(s string) ([]Cue, error) {
	lines := splitLines(s)
	var cues []Cue
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		// índice opcional
		if _, err := strconv.Atoi(line); err == nil && i+1 < len(lines) {
			i++
			line = strings.TrimSpace(lines[i])
		}
		if !strings.Contains(line, "-->") {
			i++
			continue
		}
		parts := strings.SplitN(line, "-->", 2)
		start, err1 := parseTimestamp(strings.TrimSpace(parts[0]))
		end, err2 := parseTimestamp(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			i++
			continue
		}
		i++
		var text strings.Builder
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			text.WriteString(strings.TrimSpace(lines[i]))
			text.WriteString(" ")
			i++
		}
		c := Cue{Start: start, End: end, Text: strings.TrimSpace(text.String())}
		if m := reSpeaker.FindStringSubmatch(c.Text); m != nil {
			if m[1] != "" {
				c.Speaker = strings.TrimSpace(m[1])
				c.Text = strings.TrimSpace(m[2])
			} else if m[3] != "" {
				c.Speaker = strings.TrimSpace(m[3])
				c.Text = strings.TrimSpace(m[4])
			}
		}
		if c.Text != "" {
			cues = append(cues, c)
		}
	}
	return cues, nil
}
