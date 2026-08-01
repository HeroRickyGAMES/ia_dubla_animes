// Package ffmpeg é um wrapper fino em torno de ffmpeg/ffprobe.
package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Bin retorna o caminho do binário (ffmpeg/ffprobe) ou erro amigável.
func Bin(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s não encontrado no PATH — instale via apt (ffmpeg)", name)
	}
	return p, nil
}

func mustBin(name string) string {
	p, err := Bin(name)
	if err != nil {
		return name // deixa o exec falhar com mensagem melhor
	}
	return p
}

// Run executa ffmpeg com stderr capturado para log.
func Run(args ...string) error {
	cmd := exec.Command(mustBin("ffmpeg"), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// pega as últimas linhas do stderr (mensagem de erro do ffmpeg)
		lines := strings.Split(string(out), "\n")
		tail := ""
		if len(lines) > 8 {
			tail = strings.Join(lines[len(lines)-8:], "\n")
		} else {
			tail = string(out)
		}
		return fmt.Errorf("ffmpeg: %v\n%s", err, tail)
	}
	return nil
}

// Probe retorna a duração em segundos e se há trilha de áudio.
func Probe(path string) (duration float64, hasAudio bool, err error) {
	cmd := exec.Command(mustBin("ffprobe"),
		"-v", "error",
		"-show_entries", "format=duration",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, false, fmt.Errorf("ffprobe: %v\n%s", err, out)
	}
	var dur float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if f, e := strconv.ParseFloat(line, 64); e == nil {
			dur = f
			continue
		}
		if line == "audio" {
			hasAudio = true
		}
	}
	return dur, hasAudio, nil
}

// Extract gera work/audio16k.wav (mono 16k p/ ASR) e work/audio48k.wav (stereo 48k p/ mix).
func Extract(input, out16k, out48k string, threads int) error {
	if err := Run("-y", "-i", input, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", out16k); err != nil {
		return fmt.Errorf("extract 16k: %w", err)
	}
	if err := Run("-y", "-i", input, "-vn", "-ac", "2", "-ar", "48000", "-c:a", "pcm_s16le", out48k); err != nil {
		return fmt.Errorf("extract 48k: %w", err)
	}
	return nil
}

// Slice extrai [start,end] do wav para arquivo mono 16k.
func Slice(input, out string, start, end float64) error {
	dur := end - start
	if dur <= 0 {
		return fmt.Errorf("slice inválido [%.3f,%.3f]", start, end)
	}
	return Run("-y", "-ss", fnum(start), "-i", input, "-t", fnum(dur),
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", out)
}

// Concat junta vários wavs 16k mono em um só.
func Concat(parts []string, out string) error {
	if len(parts) == 0 {
		return fmt.Errorf("concat sem partes")
	}
	if len(parts) == 1 {
		return Run("-y", "-i", parts[0], "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", out)
	}
	var sb strings.Builder
	for _, p := range parts {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		sb.WriteString("file '")
		sb.WriteString(strings.ReplaceAll(abs, "'", `'\''`))
		sb.WriteString("'\n")
	}
	list := out + ".list"
	if err := os.WriteFile(list, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	defer os.Remove(list)
	return Run("-y", "-f", "concat", "-safe", "0", "-i", list,
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", out)
}

// DurWav retorna a duração em segundos de um wav.
func DurWav(path string) (float64, error) {
	d, _, err := Probe(path)
	return d, err
}

func fnum(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}
