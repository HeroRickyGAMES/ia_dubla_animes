// Package report gera o relatório final do pipeline + estimativa para
// episódio de 24 min (docs/04-resultados.md).
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

type Stage struct {
	Name    string  `json:"stage"`
	Seconds float64 `json:"seconds"`
}

type Report struct {
	Input       string         `json:"input"`
	EpDuration  float64        `json:"episode_seconds"`
	Stages      []Stage        `json:"stages"`
	NLines      int            `json:"n_lines"`
	TotalLines  int            `json:"total_lines"`
	MaxLines    int            `json:"max_lines"`
	NSpeakers   int            `json:"n_speakers"`
	Conflicts   []string       `json:"conflicts"`
	FlagsCount  map[string]int `json:"flags"`
	Model       string         `json:"whisper_model"`
	Separator   string         `json:"separator"`
	Out         string         `json:"output"`
	Total       float64        `json:"total_seconds"`
	Est24minMin float64        `json:"est_24min_minutes"`
}

// Add registra o tempo de um estágio.
func (r *Report) Add(name string, seconds float64) {
	r.Stages = append(r.Stages, Stage{Name: name, Seconds: seconds})
}

// stageSeconds retorna o tempo medido de um estágio (0 se ausente).
func (r *Report) stageSeconds(name string) float64 {
	for _, s := range r.Stages {
		if s.Name == name {
			return s.Seconds
		}
	}
	return 0
}

// ComputeTotals calcula total e a estimativa para 24 min.
//
// Modelo: o TTS domina e escala com o NÚMERO de falas (densidade medida no
// run, via total_lines antes do corte --max-lines); os demais estágios
// (sep, asr, diar, tradução, emoção, mix) escalam com a duração da mídia.
// Assim um teste parcial (fatia curta ou --max-lines) não subestima o ep.
func (r *Report) ComputeTotals() {
	for _, s := range r.Stages {
		r.Total += s.Seconds
	}
	durMin := r.EpDuration / 60
	if durMin <= 0 {
		r.Est24minMin = r.Total / 60
		return
	}
	tts := r.stageSeconds("TTS OmniVoice")
	totalLines := r.TotalLines
	if totalLines <= 0 {
		totalLines = r.NLines
	}
	if r.NLines > 0 && totalLines > 0 {
		perLine := tts / float64(r.NLines)
		estLines := float64(totalLines) / durMin * 24
		ttsEst := perLine * estLines
		otherEst := (r.Total - tts) * 24 / durMin
		r.Est24minMin = (ttsEst + otherEst) / 60
		return
	}
	r.Est24minMin = r.Total * 24 / durMin / 60
}

func (r *Report) Write(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Markdown renderiza o relatório como tabela.
func (r *Report) Markdown() string {
	sort.Slice(r.Stages, func(i, j int) bool { return r.Stages[i].Seconds > r.Stages[j].Seconds })
	sep := r.Separator
	if sep == "" {
		sep = "demucs"
	}
	s := fmt.Sprintf("# Relatório anime_dub — %s\n\n", r.Input)
	s += fmt.Sprintf("- Episódio: %.0f s (%.1f min)\n", r.EpDuration, r.EpDuration/60)
	s += fmt.Sprintf("- Falas dubladas: %d", r.NLines)
	if r.TotalLines > 0 {
		s += fmt.Sprintf(" (de %d selecionadas", r.TotalLines)
		if r.MaxLines > 0 {
			s += fmt.Sprintf(", máx %d", r.MaxLines)
		}
		s += ")"
	}
	s += "\n"
	s += fmt.Sprintf("- Personagens (clusters de voz): %d\n", r.NSpeakers)
	s += fmt.Sprintf("- ASR: faster-whisper `%s` | Separador: `%s` | Modelo TTS: OmniVoice (CPU)\n", r.Model, sep)
	s += fmt.Sprintf("- Saída: `%s`\n\n", r.Out)

	s += "| Estágio | Tempo (s) |\n|---|---|\n"
	for _, st := range r.Stages {
		s += fmt.Sprintf("| %s | %.1f |\n", st.Name, st.Seconds)
	}
	s += fmt.Sprintf("| **TOTAL** | **%.1f** |\n", r.Total)
	s += fmt.Sprintf("\n## Estimativa para episódio de 24 min\n\n**≈ %.1f min** (TTS por fala × densidade real de falas; demais estágios escalados pela duração).\n",
		r.Est24minMin)

	if len(r.Conflicts) > 0 {
		s += "\n## Voz × roteiro (bandaid)\n\n"
		for _, c := range r.Conflicts {
			s += fmt.Sprintf("- ⚠ %s\n", c)
		}
	}
	if len(r.FlagsCount) > 0 {
		s += "\n## Flags de timing\n\n| Flag | Ocorrências |\n|---|---|\n"
		keys := make([]string, 0, len(r.FlagsCount))
		for k := range r.FlagsCount {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s += fmt.Sprintf("| %s | %d |\n", k, r.FlagsCount[k])
		}
	}
	return s
}

// RunTime é um helper de medição.
type RunTime struct {
	Start time.Time
}

func Begin() RunTime { return RunTime{Start: time.Now()} }

func (rt RunTime) Elapsed() float64 { return time.Since(rt.Start).Seconds() }
